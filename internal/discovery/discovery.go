// Package discovery finds the api.php of a wiki: when a site is added, at a
// first login, and for a host or page URL that was not added, whose result
// the API cache keeps.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/internal/build"
	"github.com/mwcli/mwcli/internal/config"
)

// Find returns the site that target names: a host such as wiki.example.org
// (https), the URL of any page of the wiki, or the URL of its api.php, which is
// only checked. A host or page is tried at /w/api.php and /api.php under it,
// then through the EditURI link of the page.
func Find(ctx context.Context, target string) (*config.Site, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("no wiki given")
	}
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("invalid wiki URL %q", target)
	}
	// The query stays: it can pick the wiki, as in index.php?wiki=a. A URL
	// copied from a browser may hold it unescaped, such as ?title=대문, which
	// some servers refuse.
	u.Fragment = ""
	u.RawQuery = escapeQuery(u.RawQuery)
	if strings.HasSuffix(u.Path, "/api.php") {
		return probe(ctx, u.String())
	}
	base := u.Scheme + "://" + u.Host
	var errs []error
	if (u.Path == "" || u.Path == "/") && u.RawQuery == "" {
		for _, candidate := range []string{base + "/w/api.php", base + "/api.php"} {
			s, err := probe(ctx, candidate)
			if err == nil {
				return s, nil
			}
			errs = append(errs, err)
		}
	}
	if u.Path == "" {
		u.Path = "/"
	}
	page := u.String()
	if found, err := editURI(ctx, page); err != nil {
		errs = append(errs, err)
	} else if s, err := probe(ctx, found); err != nil {
		errs = append(errs, err)
	} else {
		return s, nil
	}
	return nil, fmt.Errorf("could not find the MediaWiki API of %s: %w", target, errors.Join(errs...))
}

func probe(ctx context.Context, apiURL string) (*config.Site, error) {
	c, err := api.New(apiURL, api.WithUserAgent(build.UserAgent()))
	if err != nil {
		return nil, err
	}
	g, err := c.SiteInfo(ctx)
	if err != nil {
		return nil, err
	}
	if c.Resolved != "" {
		apiURL = c.Resolved
	}
	return &config.Site{API: apiURL, Server: g.Server, Script: g.Script, ArticlePath: g.ArticlePath}, nil
}

var (
	linkTag = regexp.MustCompile(`(?i)<link\b[^>]*\brel=["']?EditURI["']?[^>]*>`)
	hrefRe  = regexp.MustCompile(`(?i)\bhref=["']([^"']+)["']`)
)

// editURI reads page and returns the api.php URL of its EditURI link.
func editURI(ctx context.Context, page string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", build.UserAgent())
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s returned %s", page, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	tag := linkTag.Find(body)
	m := hrefRe.FindSubmatch(tag)
	if m == nil {
		return "", fmt.Errorf("%s has no EditURI link", page)
	}
	ref, err := url.Parse(strings.ReplaceAll(string(m[1]), "&amp;", "&"))
	if err != nil {
		return "", err
	}
	// The link is the RSD request, api.php?action=rsd; other parameters, such
	// as one that picks the wiki, belong to the API URL.
	u := resp.Request.URL.ResolveReference(ref)
	q := u.Query()
	q.Del("action")
	u.RawQuery, u.Fragment = q.Encode(), ""
	return u.String(), nil
}

// escapeQuery percent-encodes the bytes of a query that may not appear in a
// URL as they are, such as non-ASCII letters and spaces, and leaves the rest,
// and so the order and form of its parameters, unchanged.
func escapeQuery(q string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c <= ' ' || c >= 0x7f || c == '"' || c == '<' || c == '>' || c == '`' {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
