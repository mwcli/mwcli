package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Contribution is one edit of a user (list=usercontribs). JSON names follow
// the MediaWiki API.
type Contribution struct {
	RevID     int      `json:"revid"`
	ParentID  int      `json:"parentid"`
	PageID    int      `json:"pageid"`
	NS        int      `json:"ns"`
	Title     string   `json:"title"`
	Timestamp string   `json:"timestamp"`
	User      string   `json:"user"`
	Comment   string   `json:"comment"`
	Size      int      `json:"size"`
	SizeDiff  int      `json:"sizediff"`
	Minor     bool     `json:"minor"`
	New       bool     `json:"new"`
	Top       bool     `json:"top"`
	Tags      []string `json:"tags"`
}

// ContribOptions picks the edits Contributions lists.
type ContribOptions struct {
	User string
	// Namespace limits the edits to one namespace when not nil.
	Namespace *int
	Limit     int
}

// Contributions lists the edits of a user, newest first. A user with no
// edits, or who does not exist, has none.
func (c *Client) Contributions(ctx context.Context, opts ContribOptions) ([]Contribution, error) {
	contribs := []Contribution{}
	if strings.Contains(opts.User, "|") {
		return nil, fmt.Errorf("invalid user name %q", opts.User)
	}
	if opts.Limit <= 0 {
		return contribs, nil
	}
	params := url.Values{
		"action": {"query"}, "list": {"usercontribs"}, "ucuser": {opts.User},
		"ucprop":  {"ids|title|timestamp|comment|size|sizediff|flags|tags"},
		"uclimit": {fmt.Sprint(min(opts.Limit, 500))},
	}
	if opts.Namespace != nil {
		params.Set("ucnamespace", fmt.Sprint(*opts.Namespace))
	}
	for body, err := range c.Paginate(ctx, http.MethodGet, params) {
		if err != nil {
			return nil, err
		}
		var result struct {
			Query struct {
				UserContribs []Contribution `json:"usercontribs"`
			} `json:"query"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decode API response: %w", err)
		}
		contribs = append(contribs, result.Query.UserContribs...)
		if len(contribs) >= opts.Limit {
			break
		}
		params.Set("uclimit", fmt.Sprint(min(opts.Limit-len(contribs), 500)))
	}
	if len(contribs) > opts.Limit {
		contribs = contribs[:opts.Limit]
	}
	for i := range contribs {
		if contribs[i].Tags == nil {
			contribs[i].Tags = []string{}
		}
	}
	return contribs, nil
}
