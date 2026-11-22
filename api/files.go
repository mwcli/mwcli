package api

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// FileInfo describes the latest version of a file (prop=imageinfo). JSON
// names follow the MediaWiki API; repository is local, or shared for a file
// of a shared repository such as Wikimedia Commons.
type FileInfo struct {
	Title          string `json:"title"`
	URL            string `json:"url"`
	DescriptionURL string `json:"descriptionurl"`
	Size           int64  `json:"size"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	MIME           string `json:"mime"`
	MediaType      string `json:"mediatype"`
	Timestamp      string `json:"timestamp"`
	User           string `json:"user"`
	Comment        string `json:"comment"`
	SHA1           string `json:"sha1"`
	Repository     string `json:"repository"`

	// Asked is the title as it was asked for. Missing is set for a file that
	// does not exist, Invalid for a title the wiki does not accept.
	Asked            string `json:"-"`
	Missing, Invalid bool   `json:"-"`
}

// FilesInfo returns the latest version of each file title, fifty a request,
// in the order asked.
func (c *Client) FilesInfo(ctx context.Context, titles []string) ([]FileInfo, error) {
	byTitle := map[string]FileInfo{}
	norm := map[string]string{}
	for start := 0; start < len(titles); start += pagesBatch {
		batch := titles[start:min(start+pagesBatch, len(titles))]
		if err := c.filesInfo(ctx, batch, byTitle, norm); err != nil {
			return nil, err
		}
	}
	out := make([]FileInfo, 0, len(titles))
	for _, asked := range titles {
		t := asked
		if n, ok := norm[t]; ok {
			t = n
		}
		fi, ok := byTitle[t]
		if !ok {
			return nil, fmt.Errorf("API did not return %q", asked)
		}
		fi.Asked = asked
		out = append(out, fi)
	}
	return out, nil
}

func (c *Client) filesInfo(ctx context.Context, titles []string, byTitle map[string]FileInfo, norm map[string]string) error {
	var ask []string
	for _, t := range titles {
		if strings.Contains(t, "|") {
			byTitle[t] = FileInfo{Title: t, Invalid: true}
			continue
		}
		ask = append(ask, t)
	}
	if len(ask) == 0 {
		return nil
	}
	params := url.Values{
		"action": {"query"}, "prop": {"imageinfo"}, "titles": {strings.Join(ask, "|")},
		"iiprop": {"url|size|mime|mediatype|timestamp|user|comment|sha1"},
	}
	for body, err := range c.Paginate(ctx, http.MethodPost, params) {
		if err != nil {
			return err
		}
		var result struct {
			Continue map[string]json.RawMessage `json:"continue"`
			Query    struct {
				Normalized []struct {
					From string `json:"from"`
					To   string `json:"to"`
				} `json:"normalized"`
				Pages []struct {
					Title      string     `json:"title"`
					Invalid    bool       `json:"invalid"`
					Repository string     `json:"imagerepository"`
					ImageInfo  []FileInfo `json:"imageinfo"`
				} `json:"pages"`
			} `json:"query"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("decode API response: %w", err)
		}
		for _, n := range result.Query.Normalized {
			norm[n.From] = n.To
		}
		for _, p := range result.Query.Pages {
			switch {
			case p.Invalid:
				byTitle[p.Title] = FileInfo{Title: p.Title, Invalid: true}
			case len(p.ImageInfo) == 0:
				// Check ImageInfo instead of Missing: shared repository files
				// (such as Commons files on Wikipedia) have missing=true when
				// they have no local description page, yet carry ImageInfo.
				if _, ok := byTitle[p.Title]; !ok || byTitle[p.Title].Missing {
					byTitle[p.Title] = FileInfo{Title: p.Title, Missing: true}
				}
			default:
				if cur, ok := byTitle[p.Title]; ok && !cur.Missing {
					continue
				}
				fi := p.ImageInfo[0]
				fi.Title, fi.Repository = p.Title, p.Repository
				fi.URL = c.normalizeFileURL(fi.URL)
				fi.DescriptionURL = c.normalizeFileURL(fi.DescriptionURL)
				byTitle[p.Title] = fi
			}
		}
		// iistart paginates older versions of a file we already have; only
		// iicontinue means more files in the batch did not fit this response.
		if _, more := result.Continue["iicontinue"]; !more {
			break
		}
	}
	return nil
}

// normalizeFileURL resolves relative or protocol-relative URLs against the
// API URL and drops the utm_ parameters some wikis add to file URLs.
func (c *Client) normalizeFileURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if c.url != nil && !u.IsAbs() {
		u = c.url.ResolveReference(u)
	}
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if strings.HasPrefix(k, "utm_") {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// Fetch downloads rawURL, such as the URL of a file, into w and returns the
// SHA-1 of what it wrote, in hex. Credentials go only to the API's origin.
func (c *Client) Fetch(ctx context.Context, rawURL string, w io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	sameOrigin := c.url != nil && strings.EqualFold(req.URL.Scheme, c.url.Scheme) && strings.EqualFold(req.URL.Host, c.url.Host)
	if sameOrigin {
		if c.botName != "" && c.botState == 0 {
			c.botState = 1
			c.botErr = c.BotLogin(ctx, c.botName, c.botPassword, c.loginDomain)
			c.botState = 2
		}
		if c.botErr != nil {
			return "", c.botErr
		}
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if sameOrigin {
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		if c.oauth1 != nil {
			c.oauth1.sign(req, nil)
		}
	}
	// Large files may take longer than the API request timeout; ctx cancels them.
	hc := *c.http
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &HTTPError{Status: resp.Status}
	}
	h := sha1.New()
	if _, err := io.Copy(io.MultiWriter(w, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
