package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PageText is the latest revision of one page asked for by PagesText.
type PageText struct {
	// Asked is the title as it was asked for; Title is the wiki's form of it.
	Asked        string
	Title        string
	RevID        int
	Timestamp    string
	ContentModel string
	Content      string
	// Missing is set for a page that does not exist, Invalid for a title the
	// wiki does not accept; neither has a revision.
	Missing, Invalid bool
}

// pagesBatch is how many titles one request asks for, the most the API
// takes from an account without the apihighlimits right.
const pagesBatch = 50

// PagesText fetches the latest revision of each title, many in one request,
// and returns them in the order asked. A title asked twice, or two titles
// with the same wiki form, are fetched once and returned for each.
func (c *Client) PagesText(ctx context.Context, titles []string) ([]PageText, error) {
	byTitle := map[string]PageText{}
	norm := map[string]string{}
	for start := 0; start < len(titles); start += pagesBatch {
		batch := titles[start:min(start+pagesBatch, len(titles))]
		if err := c.pagesText(ctx, batch, byTitle, norm); err != nil {
			return nil, err
		}
	}
	out := make([]PageText, 0, len(titles))
	for _, asked := range titles {
		t := asked
		if n, ok := norm[t]; ok {
			t = n
		}
		p, ok := byTitle[t]
		if !ok {
			return nil, fmt.Errorf("API did not return %q", asked)
		}
		p.Asked = asked
		out = append(out, p)
	}
	return out, nil
}

func (c *Client) pagesText(ctx context.Context, titles []string, byTitle map[string]PageText, norm map[string]string) error {
	for _, t := range titles {
		if strings.Contains(t, "|") {
			byTitle[t] = PageText{Title: t, Invalid: true}
			norm[t] = t
		}
	}
	var ask []string
	for _, t := range titles {
		if !strings.Contains(t, "|") {
			ask = append(ask, t)
		}
	}
	if len(ask) == 0 {
		return nil
	}
	params := url.Values{
		"action": {"query"}, "prop": {"revisions"}, "titles": {strings.Join(ask, "|")},
		"rvprop": {"ids|timestamp|content"}, "rvslots": {"main"},
	}
	// A large batch can come in parts: pages whose content did not fit come
	// again in the continued response.
	for body, err := range c.Paginate(ctx, http.MethodPost, params) {
		if err != nil {
			return err
		}
		var result struct {
			Query struct {
				Normalized []struct {
					From string `json:"from"`
					To   string `json:"to"`
				} `json:"normalized"`
				Pages []struct {
					Title     string `json:"title"`
					Missing   bool   `json:"missing"`
					Invalid   bool   `json:"invalid"`
					Revisions []struct {
						RevID     int    `json:"revid"`
						Timestamp string `json:"timestamp"`
						Slots     map[string]struct {
							ContentModel string `json:"contentmodel"`
							Content      string `json:"content"`
						} `json:"slots"`
					} `json:"revisions"`
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
			if p.Invalid {
				// An invalid title comes back as it was asked for.
				byTitle[p.Title] = PageText{Title: p.Title, Invalid: true}
				continue
			}
			if p.Missing {
				byTitle[p.Title] = PageText{Title: p.Title, Missing: true}
				continue
			}
			if len(p.Revisions) == 0 {
				if _, ok := byTitle[p.Title]; !ok {
					byTitle[p.Title] = PageText{Title: p.Title}
				}
				continue
			}
			r := p.Revisions[0]
			main := r.Slots["main"]
			byTitle[p.Title] = PageText{Title: p.Title, RevID: r.RevID, Timestamp: r.Timestamp, ContentModel: main.ContentModel, Content: main.Content}
		}
	}
	return nil
}
