package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RESTURL returns the URL of path under the wiki's rest.php, which sits
// next to its api.php. path starts with / and may have a query.
func (c *Client) RESTURL(path string) (*url.URL, error) {
	ref, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(ref.Path, "/") || ref.Host != "" || ref.Scheme != "" {
		return nil, fmt.Errorf("invalid REST path %q: give a path such as /v1/page/Main_Page", path)
	}
	u := *c.url
	if !strings.HasSuffix(u.Path, "api.php") {
		return nil, fmt.Errorf("cannot find the rest.php of %s", c.API)
	}
	u.Path = strings.TrimSuffix(u.Path, "api.php") + "rest.php" + ref.Path
	// Keep the path as escaped, so that %2F in a title stays one segment.
	u.RawPath = strings.TrimSuffix(c.url.EscapedPath(), "api.php") + "rest.php" + ref.EscapedPath()
	q := u.Query()
	for k, vs := range ref.Query() {
		q[k] = vs
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return &u, nil
}

// SessionAuth reports whether the client authenticates with a session
// cookie, from a password or bot password login, or not at all, rather than
// with OAuth sent in every request. REST requests that change something then
// need a CSRF token.
func (c *Client) SessionAuth() bool {
	return c.Token == "" && c.oauth1 == nil
}

// RESTResponse is the answer to a REST request.
type RESTResponse struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
}

// REST sends a request to path under rest.php, with body as its content when
// not nil, and returns the response whatever its status.
func (c *Client) REST(ctx context.Context, method, path string, query url.Values, body []byte, contentType string) (*RESTResponse, error) {
	if c.botName != "" && c.botState == 0 {
		c.botState = 1
		c.botErr = c.BotLogin(ctx, c.botName, c.botPassword, c.loginDomain)
		c.botState = 2
	}
	if c.botErr != nil {
		return nil, c.botErr
	}
	u, err := c.RESTURL(path)
	if err != nil {
		return nil, err
	}
	if len(query) > 0 {
		q := u.Query()
		for k, vs := range query {
			q[k] = vs
		}
		u.RawQuery = q.Encode()
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return nil, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.oauth1 != nil {
		c.oauth1.sign(req, nil)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &RESTResponse{StatusCode: resp.StatusCode, Status: resp.Status, Header: resp.Header, Body: b}, nil
}
