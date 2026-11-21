// Package api is a small client for the MediaWiki Action API.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/zetaoss/mwcli/internal/build"
	"github.com/zetaoss/mwcli/internal/config"
)

// Client talks to one wiki's api.php.
type Client struct {
	API   string
	Token string
	http  *http.Client
	url   *url.URL
}

// New returns a client for api, authenticated with account when it is not nil.
func New(api string, account *config.Account) (*Client, error) {
	u, err := url.Parse(api)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", api)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &Client{API: api, url: u, http: &http.Client{Jar: jar, Timeout: 45 * time.Second}}
	if account != nil {
		if account.Auth == config.AuthOAuth2 {
			c.Token = account.OAuthToken
		} else {
			var cookies []*http.Cookie
			for name, value := range account.Cookies {
				cookies = append(cookies, &http.Cookie{Name: name, Value: value})
			}
			jar.SetCookies(u, cookies)
		}
	}
	return c, nil
}

// Cookies returns the session cookies to store after a password login.
func (c *Client) Cookies() map[string]string {
	m := map[string]string{}
	for _, ck := range c.http.Jar.Cookies(c.url) {
		m[ck.Name] = ck.Value
	}
	return m
}

// HTTPError is a non-2xx response.
type HTTPError struct {
	Status string
	Body   string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return "MediaWiki returned " + e.Status
	}
	return "MediaWiki returned " + e.Status + ": " + e.Body
}

// APIError is an error reported in the API response body.
type APIError struct {
	Code string
	Info string
}

func (e *APIError) Error() string { return "MediaWiki API error " + e.Code + ": " + e.Info }

// Raw sends params with method and returns the response body. A non-2xx status
// is an *HTTPError; API errors in the body are not checked.
func (c *Client) Raw(method string, params url.Values) ([]byte, error) {
	var req *http.Request
	var err error
	if method == http.MethodGet {
		u := *c.url
		u.RawQuery = params.Encode()
		req, err = http.NewRequest(method, u.String(), nil)
	} else {
		req, err = http.NewRequest(method, c.API, strings.NewReader(params.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mw/"+build.Version+" (https://github.com/zetaoss/mwcli)")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &HTTPError{Status: resp.Status}
		if strings.Contains(resp.Header.Get("Content-Type"), "html") {
			e.Body = "HTML error page (the site may be blocking this client or network)"
		} else {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			e.Body = strings.TrimSpace(string(b))
		}
		return nil, e
	}
	return io.ReadAll(resp.Body)
}

// CheckError returns the first API error reported in body, if any.
func CheckError(body []byte) error {
	var envelope struct {
		Error  *struct{ Code, Info string }  `json:"error"`
		Errors []struct{ Code, Text string } `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	if envelope.Error != nil {
		return &APIError{Code: envelope.Error.Code, Info: envelope.Error.Info}
	}
	if len(envelope.Errors) > 0 {
		return &APIError{Code: envelope.Errors[0].Code, Info: envelope.Errors[0].Text}
	}
	return nil
}

func (c *Client) request(method string, params url.Values, out any) error {
	params.Set("format", "json")
	params.Set("formatversion", "2")
	body, err := c.Raw(method, params)
	if err != nil {
		return err
	}
	if err := CheckError(body); err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	return nil
}

// TokenOf fetches a token of kind (csrf, login, ...).
func (c *Client) TokenOf(kind string) (string, error) {
	var result struct {
		Query struct {
			Tokens map[string]string `json:"tokens"`
		} `json:"query"`
	}
	if err := c.request(http.MethodGet, url.Values{"action": {"query"}, "meta": {"tokens"}, "type": {kind}}, &result); err != nil {
		return "", err
	}
	token := result.Query.Tokens[kind+"token"]
	if token == "" {
		return "", fmt.Errorf("API did not return %s token", kind)
	}
	return token, nil
}

// Login runs the clientlogin flow with a username and password.
func (c *Client) Login(username, password string) error {
	token, err := c.TokenOf("login")
	if err != nil {
		return err
	}
	form := url.Values{"action": {"clientlogin"}, "loginreturnurl": {c.API}, "logintoken": {token}, "username": {username}, "password": {password}}
	var result struct {
		ClientLogin struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"clientlogin"`
	}
	if err := c.request(http.MethodPost, form, &result); err != nil {
		return err
	}
	if result.ClientLogin.Status != "PASS" {
		return fmt.Errorf("login %s: %s", result.ClientLogin.Status, result.ClientLogin.Message)
	}
	return nil
}

// ErrNotLoggedIn means the wiki sees the request as anonymous.
var ErrNotLoggedIn = errors.New("not logged in")

// UserInfo is the current user and the rights the login grants.
type UserInfo struct {
	Name   string
	Anon   bool
	Rights []string
}

// User returns the logged-in user and rights, or ErrNotLoggedIn.
func (c *Client) User() (*UserInfo, error) {
	u, err := c.CurrentUser()
	if err == nil && u.Anon {
		return nil, ErrNotLoggedIn
	}
	return u, err
}

// CurrentUser returns the user the wiki sees, which may be anonymous.
func (c *Client) CurrentUser() (*UserInfo, error) {
	var result struct {
		Query struct {
			UserInfo struct {
				Name   string   `json:"name"`
				ID     int      `json:"id"`
				Anon   bool     `json:"anon"`
				Rights []string `json:"rights"`
			} `json:"userinfo"`
		} `json:"query"`
	}
	if err := c.request(http.MethodGet, url.Values{"action": {"query"}, "meta": {"userinfo"}, "uiprop": {"rights"}}, &result); err != nil {
		return nil, err
	}
	ui := result.Query.UserInfo
	return &UserInfo{Name: ui.Name, Anon: ui.Anon || ui.ID == 0, Rights: ui.Rights}, nil
}

// ErrMissingPage means the page does not exist.
var ErrMissingPage = errors.New("page does not exist")

// Page returns the wikitext of the latest revision of title.
func (c *Client) Page(title string) (string, error) {
	var result struct {
		Query struct {
			Pages []struct {
				Missing   bool `json:"missing"`
				Invalid   bool `json:"invalid"`
				Revisions []struct {
					Slots map[string]struct {
						Content string `json:"content"`
					} `json:"slots"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
	}
	params := url.Values{"action": {"query"}, "prop": {"revisions"}, "rvprop": {"content"}, "rvslots": {"main"}, "titles": {title}}
	if err := c.request(http.MethodGet, params, &result); err != nil {
		return "", err
	}
	if len(result.Query.Pages) != 1 {
		return "", fmt.Errorf("page %q not found", title)
	}
	p := result.Query.Pages[0]
	switch {
	case p.Invalid:
		return "", fmt.Errorf("invalid title %q", title)
	case p.Missing:
		return "", fmt.Errorf("%w: %s", ErrMissingPage, title)
	case len(p.Revisions) == 0:
		return "", errors.New("page has no readable revision")
	}
	return p.Revisions[0].Slots["main"].Content, nil
}

// Edit replaces the text of title and returns the normalized title.
func (c *Client) Edit(title, text, summary string) (string, error) {
	token, err := c.TokenOf("csrf")
	if err != nil {
		return "", err
	}
	form := url.Values{"action": {"edit"}, "assert": {"user"}, "title": {title}, "text": {text}, "summary": {summary}, "token": {token}}
	var result struct {
		Edit struct {
			Result string `json:"result"`
			Title  string `json:"title"`
		} `json:"edit"`
	}
	if err := c.request(http.MethodPost, form, &result); err != nil {
		return "", err
	}
	if result.Edit.Result != "Success" {
		return "", fmt.Errorf("edit did not succeed (result: %q)", result.Edit.Result)
	}
	return result.Edit.Title, nil
}

// Actions maps the actions `mw auth status` summarizes to the user right they need.
var Actions = []struct{ Name, Right string }{
	{"edit", "edit"},
	{"create", "createpage"},
	{"move", "move"},
	{"delete", "delete"},
	{"protect", "protect"},
}
