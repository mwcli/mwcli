// Package api is a small client for the MediaWiki Action API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultUserAgent is the User-Agent of a client made without WithUserAgent.
// Wikimedia wikis ask each tool to name itself and a way to reach its author.
const DefaultUserAgent = "mwcli-api (https://github.com/mwcli/mwcli)"

// Client talks to one wiki's api.php.
type Client struct {
	API   string
	Token string
	// UserAgent is sent with every request.
	UserAgent string
	// Resolved is the api.php URL the last request ended at after redirects.
	Resolved string
	http     *http.Client
	url      *url.URL
	// botName and botPassword log in with action=login before the first request.
	botName, botPassword string
	botState             int // 0 not tried, 1 logging in, 2 done
	botErr               error
	loginDomain          string
	// maxlag is sent with each request; onMaxlag hears of each wait, and
	// sleep waits, which tests replace.
	maxlag   int
	onMaxlag func(wait time.Duration, lag string)
	sleep    func(ctx context.Context, d time.Duration) error
	// oauth1 signs every request to the API host with OAuth 1.0a.
	oauth1 *OAuth1
}

// An Option configures a client made by New.
type Option func(*Client)

// WithOAuth2Token authenticates with an OAuth 2 access token, sent as a Bearer
// token.
func WithOAuth2Token(token string) Option {
	return func(c *Client) { c.Token = token }
}

// WithOAuth1 authenticates by signing every request with the keys of an OAuth
// 1.0a consumer.
func WithOAuth1(keys OAuth1) Option {
	return func(c *Client) { c.oauth1 = &keys }
}

// WithBotPassword logs in with a bot password from Special:BotPasswords before
// the first request. name is the login name, such as Alice@mybot.
func WithBotPassword(name, password string) Option {
	return func(c *Client) { c.botName, c.botPassword = name, password }
}

// WithLoginDomain sets the authentication domain of the bot password login,
// for a wiki that logs in through LDAP with several domains.
func WithLoginDomain(domain string) Option {
	return func(c *Client) { c.loginDomain = domain }
}

// WithCookies sends session cookies, such as those Cookies returned after a
// password login.
func WithCookies(cookies map[string]string) Option {
	return func(c *Client) {
		var list []*http.Cookie
		for name, value := range cookies {
			list = append(list, &http.Cookie{Name: name, Value: value})
		}
		c.http.Jar.SetCookies(c.url, list)
	}
}

// DefaultMaxlag is the maxlag a client sends unless WithMaxlag says another:
// the value Wikimedia asks of tools that are not run by a person.
const DefaultMaxlag = 5

// WithMaxlag sets the maxlag parameter of every request, in seconds: the wiki
// refuses a request while its database replicas lag more, and the client waits
// as the wiki asks and sends it again, up to 5 times. 0 sends none. A request
// with its own maxlag parameter keeps it.
func WithMaxlag(seconds int) Option {
	return func(c *Client) { c.maxlag = seconds }
}

// OnMaxlag calls f before each wait for a wiki that refused a request for lag,
// with the wait and the lag the wiki reported, such as to tell a person.
func OnMaxlag(f func(wait time.Duration, lag string)) Option {
	return func(c *Client) { c.onMaxlag = f }
}

// WithUserAgent sets the User-Agent of every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.UserAgent = ua }
}

// New returns a client for the api.php at apiURL, configured by opts. Without
// an authentication option, the wiki sees the requests as anonymous.
func New(apiURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(apiURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", apiURL)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &Client{API: apiURL, url: u, UserAgent: DefaultUserAgent, maxlag: DefaultMaxlag, sleep: sleepContext}
	c.http = &http.Client{Jar: jar, Timeout: 45 * time.Second, CheckRedirect: c.checkRedirect}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// checkRedirect follows redirects of GET requests, which discovery relies on,
// but never of a POST: a 307 or 308 would send its body, with a password, a
// bot password or an edit, to another address. An OAuth 1.0a signature holds
// for one URL, so a GET to the same host is signed again and one to another
// host is sent without it.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if via[0].Method != http.MethodGet && via[0].Method != http.MethodHead {
		return fmt.Errorf("refusing to follow a redirect of a %s request to %s; check the API URL", via[0].Method, req.URL.Redacted())
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if c.oauth1 != nil {
		if strings.EqualFold(req.URL.Host, c.url.Host) {
			c.oauth1.sign(req, nil)
		} else {
			req.Header.Del("Authorization")
		}
	}
	return nil
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
	// StatusCode is the HTTP status code, such as 413; Status has its text too.
	StatusCode int
	Status     string
	Body       string
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
func (c *Client) Raw(ctx context.Context, method string, params url.Values) ([]byte, error) {
	if c.botName != "" && c.botState == 0 {
		// A bot password session is short lived, so each run logs in afresh.
		c.botState = 1
		c.botErr = c.BotLogin(ctx, c.botName, c.botPassword, c.loginDomain)
		c.botState = 2
	}
	if c.botErr != nil {
		return nil, c.botErr
	}
	if c.maxlag > 0 && !params.Has("maxlag") {
		p := url.Values{}
		for k, v := range params {
			p[k] = v
		}
		p.Set("maxlag", strconv.Itoa(c.maxlag))
		params = p
	}
	for retry := 0; ; retry++ {
		body, header, err := c.send(ctx, method, params)
		if err != nil || retry >= maxlagRetries || !isMaxlag(body) {
			return body, err
		}
		// The wiki refused the request before doing anything, as its database
		// replicas lag, so it is safe to send again, a POST too.
		wait := retryAfter(header)
		if c.onMaxlag != nil {
			c.onMaxlag(wait, header.Get("X-Database-Lag"))
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// maxlagRetries is how many times a request refused for lag is sent again.
const maxlagRetries = 5

// isMaxlag reports whether body is the error of a request refused because the
// wiki's database replicas lag more than maxlag seconds.
func isMaxlag(body []byte) bool {
	var ae *APIError
	return errors.As(CheckError(body), &ae) && ae.Code == "maxlag"
}

// retryAfter is the wait the wiki asks for, 5 seconds when it names none.
func retryAfter(h http.Header) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 5 * time.Second
}

// sleepContext waits d, or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// send makes one request and returns its body and headers.
func (c *Client) send(ctx context.Context, method string, params url.Values) ([]byte, http.Header, error) {
	var req *http.Request
	var err error
	// Keep the query of the API URL, such as one that picks the wiki, but let a
	// parameter of the request replace a value of the same name there, such as
	// a continue value replacing a start offset.
	u := *c.url
	q := u.Query()
	for k := range params {
		q.Del(k)
	}
	if method == http.MethodGet {
		for k, vs := range params {
			q[k] = vs
		}
		u.RawQuery = q.Encode()
		req, err = http.NewRequestWithContext(ctx, method, u.String(), nil)
	} else {
		u.RawQuery = q.Encode()
		req, err = http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(params.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.oauth1 != nil {
		var form url.Values
		if method != http.MethodGet {
			form = params
		}
		c.oauth1.sign(req, form)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	// Keep the query of the API URL itself, such as one that picks the wiki,
	// and drop the parameters of this request.
	final := *resp.Request.URL
	fq := final.Query()
	if method == http.MethodGet {
		for k := range params {
			fq.Del(k)
		}
	}
	final.RawQuery, final.Fragment = fq.Encode(), ""
	c.Resolved = final.String()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, newHTTPError(resp)
	}
	b, err := io.ReadAll(resp.Body)
	return b, resp.Header, err
}

// newHTTPError describes a response whose status is not 2xx. An HTML error
// page, as a proxy or firewall sends, is named in one line rather than shown.
func newHTTPError(resp *http.Response) *HTTPError {
	e := &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	switch {
	case !strings.Contains(resp.Header.Get("Content-Type"), "html"):
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		e.Body = strings.TrimSpace(string(b))
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		e.Body = "HTML error page (a server in front of the wiki limits the size of a request)"
	default:
		e.Body = "HTML error page (the site may be blocking this client or network)"
	}
	return e
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

func (c *Client) request(ctx context.Context, method string, params url.Values, out any) error {
	params.Set("format", "json")
	params.Set("formatversion", "2")
	body, err := c.Raw(ctx, method, params)
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

// General is the general site information of a wiki (meta=siteinfo).
type General struct {
	SiteName    string `json:"sitename"`
	Generator   string `json:"generator"`
	Server      string `json:"server"`
	Script      string `json:"script"`
	ArticlePath string `json:"articlepath"`
}

// SiteInfo returns the general site information.
func (c *Client) SiteInfo(ctx context.Context) (*General, error) {
	var result struct {
		Query struct {
			General General `json:"general"`
		} `json:"query"`
	}
	if err := c.request(ctx, http.MethodGet, url.Values{"action": {"query"}, "meta": {"siteinfo"}, "siprop": {"general"}}, &result); err != nil {
		return nil, err
	}
	g := result.Query.General
	if !strings.HasPrefix(g.Generator, "MediaWiki") {
		return nil, fmt.Errorf("%s is not a MediaWiki API", c.API)
	}
	return &g, nil
}

// TokenOf fetches a token of kind (csrf, login, ...).
func (c *Client) TokenOf(ctx context.Context, kind string) (string, error) {
	var result struct {
		Query struct {
			Tokens map[string]string `json:"tokens"`
		} `json:"query"`
	}
	if err := c.request(ctx, http.MethodGet, url.Values{"action": {"query"}, "meta": {"tokens"}, "type": {kind}}, &result); err != nil {
		return "", err
	}
	token := result.Query.Tokens[kind+"token"]
	if token == "" {
		return "", fmt.Errorf("API did not return %s token", kind)
	}
	return token, nil
}

// LoginField is a field the wiki asks for in the middle of a login, such as
// the code of a two-factor authenticator (OATHToken) or one sent by email.
type LoginField struct {
	// Name is the parameter to send the answer in.
	Name string
	// Label and Help describe the field for a person.
	Label, Help string
	// Sensitive fields, such as passwords, should not be echoed.
	Sensitive bool
	// Options are the values a select field takes, value to label.
	Options map[string]string
}

// LoginOptions are the options of Login besides the username and password.
type LoginOptions struct {
	// Domain is the authentication domain, for a wiki that logs in through
	// LDAP with several domains.
	Domain string
	// Answer is asked for the fields the wiki needs after the password, with
	// the wiki's message; it returns a value for each field name. Without it,
	// a login that needs more fails.
	Answer func(message string, fields []LoginField) (map[string]string, error)
}

// ErrLoginNeedsMore means the wiki asked for more than a password, such as a
// two-factor code, and LoginOptions.Answer was not given.
var ErrLoginNeedsMore = errors.New("the wiki asks for more than a password")

// maxLoginSteps bounds the rounds of a login that keeps asking for fields.
const maxLoginSteps = 5

// Login runs the clientlogin flow with a username and password, answering the
// fields the wiki asks for after the password, such as a two-factor code,
// through opts.Answer.
func (c *Client) Login(ctx context.Context, username, password string, opts LoginOptions) error {
	token, err := c.TokenOf(ctx, "login")
	if err != nil {
		return err
	}
	form := url.Values{"action": {"clientlogin"}, "loginreturnurl": {c.API}, "logintoken": {token}, "username": {username}, "password": {password}, "rememberMe": {"1"}}
	if opts.Domain != "" {
		form.Set("domain", opts.Domain)
	}
	for step := 0; ; step++ {
		var result struct {
			ClientLogin struct {
				Status   string `json:"status"`
				Message  string `json:"message"`
				Requests []struct {
					Fields map[string]struct {
						Type    string            `json:"type"`
						Label   string            `json:"label"`
						Help    string            `json:"help"`
						Options map[string]string `json:"options"`
					} `json:"fields"`
				} `json:"requests"`
			} `json:"clientlogin"`
		}
		if err := c.request(ctx, http.MethodPost, form, &result); err != nil {
			return err
		}
		cl := result.ClientLogin
		switch cl.Status {
		case "PASS":
			return nil
		case "UI":
		default:
			return fmt.Errorf("login %s: %s", cl.Status, cl.Message)
		}
		if opts.Answer == nil {
			return fmt.Errorf("%w: %s", ErrLoginNeedsMore, cl.Message)
		}
		if step+1 >= maxLoginSteps {
			return fmt.Errorf("login: the wiki kept asking for more after %d steps: %s", maxLoginSteps, cl.Message)
		}
		var fields []LoginField
		for _, r := range cl.Requests {
			for name, f := range r.Fields {
				if f.Type == "hidden" || f.Type == "null" {
					continue
				}
				fields = append(fields, LoginField{Name: name, Label: f.Label, Help: f.Help, Sensitive: f.Type == "password", Options: f.Options})
			}
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		answers, err := opts.Answer(cl.Message, fields)
		if err != nil {
			return err
		}
		form = url.Values{"action": {"clientlogin"}, "logincontinue": {"1"}, "logintoken": {token}}
		for k, v := range answers {
			form.Set(k, v)
		}
	}
}

// Logout ends the session of the client on the wiki.
func (c *Client) Logout(ctx context.Context) error {
	token, err := c.TokenOf(ctx, "csrf")
	if err != nil {
		return err
	}
	var result map[string]any
	return c.request(ctx, http.MethodPost, url.Values{"action": {"logout"}, "token": {token}}, &result)
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
func (c *Client) User(ctx context.Context) (*UserInfo, error) {
	u, err := c.CurrentUser(ctx)
	if err == nil && u.Anon {
		return nil, ErrNotLoggedIn
	}
	return u, err
}

// CurrentUser returns the user the wiki sees, which may be anonymous.
func (c *Client) CurrentUser(ctx context.Context) (*UserInfo, error) {
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
	if err := c.request(ctx, http.MethodGet, url.Values{"action": {"query"}, "meta": {"userinfo"}, "uiprop": {"rights"}}, &result); err != nil {
		return nil, err
	}
	ui := result.Query.UserInfo
	return &UserInfo{Name: ui.Name, Anon: ui.Anon || ui.ID == 0, Rights: ui.Rights}, nil
}

// ErrMissingPage means the page does not exist.
var ErrMissingPage = errors.New("page does not exist")

// Page returns the wikitext of the latest revision of title.
func (c *Client) Page(ctx context.Context, title string) (string, error) {
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
	if err := c.request(ctx, http.MethodGet, params, &result); err != nil {
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

// EditMode says whether an edit may create a page or change an existing one.
type EditMode int

const (
	// CreateOrEdit saves the text whether or not the page exists.
	CreateOrEdit EditMode = iota
	// EditOnly fails with ErrMissingPage when the page does not exist.
	EditOnly
	// CreateOnly fails with ErrPageExists when the page exists.
	CreateOnly
)

// ErrPageExists means the page to create exists already.
var ErrPageExists = errors.New("page already exists")

// ErrEditConflict means the page changed since the base revision in a way the
// wiki could not merge with the edit.
var ErrEditConflict = errors.New("edit conflict")

// EditOptions are the options of Edit besides the title and text.
type EditOptions struct {
	Summary string
	Mode    EditMode
	// Minor marks the edit minor; Bot marks it a bot edit, which hides it from
	// recent changes when the account has the bot right.
	Minor, Bot bool
	// BaseRevID is the revision the text was made from (baserevid). When
	// others edited the page since, the wiki merges their changes as the
	// browser editor does, or the edit fails with ErrEditConflict.
	BaseRevID int
}

// EditResult is the outcome of an edit.
type EditResult struct {
	// Title is the normalized title.
	Title string
	// NoChange is true when the text was the same, so no revision was saved.
	NoChange bool
	// New is true when the edit created the page.
	New bool
}

// Edit saves text as the new wikitext of title.
func (c *Client) Edit(ctx context.Context, title, text string, opts EditOptions) (*EditResult, error) {
	token, err := c.TokenOf(ctx, "csrf")
	if err != nil {
		return nil, err
	}
	form := url.Values{"action": {"edit"}, "assert": {"user"}, "title": {title}, "text": {text}, "summary": {opts.Summary}, "token": {token}}
	switch opts.Mode {
	case EditOnly:
		form.Set("nocreate", "1")
	case CreateOnly:
		form.Set("createonly", "1")
	}
	if opts.Minor {
		form.Set("minor", "1")
	}
	if opts.Bot {
		form.Set("bot", "1")
	}
	if opts.BaseRevID > 0 {
		form.Set("baserevid", strconv.Itoa(opts.BaseRevID))
	}
	var result struct {
		Edit struct {
			Result   string `json:"result"`
			Title    string `json:"title"`
			NoChange bool   `json:"nochange"`
			New      bool   `json:"new"`
		} `json:"edit"`
	}
	err = c.request(ctx, http.MethodPost, form, &result)
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case "missingtitle":
			return nil, fmt.Errorf("%w: %s", ErrMissingPage, title)
		case "articleexists":
			return nil, fmt.Errorf("%w: %s", ErrPageExists, title)
		case "editconflict":
			return nil, fmt.Errorf("%w: %s changed since revision %d", ErrEditConflict, title, opts.BaseRevID)
		}
	}
	if err != nil {
		return nil, err
	}
	if result.Edit.Result != "Success" {
		return nil, fmt.Errorf("edit did not succeed (result: %q)", result.Edit.Result)
	}
	return &EditResult{Title: result.Edit.Title, NoChange: result.Edit.NoChange, New: result.Edit.New}, nil
}

// Actions maps the actions `mw auth status` summarizes to the user right they need.
var Actions = []struct{ Name, Right string }{
	{"edit", "edit"},
	{"create", "createpage"},
	{"move", "move"},
	{"delete", "delete"},
	{"protect", "protect"},
}

// PageInfo is what `mw page view --json` can show about a page. Field names
// follow the MediaWiki API.
type PageInfo struct {
	Title        string       `json:"title"`
	PageID       int          `json:"pageid"`
	NS           int          `json:"ns"`
	URL          string       `json:"url"`
	ContentModel string       `json:"contentmodel"`
	Length       int          `json:"length"`
	Touched      string       `json:"touched"`
	Redirect     bool         `json:"redirect"`
	Protection   []Protection `json:"protection"`
	Categories   []string     `json:"categories"`
	Revision     Revision     `json:"revision"`
	Content      string       `json:"content"`
}

// Protection is one protection of a page.
type Protection struct {
	Type   string `json:"type"`
	Level  string `json:"level"`
	Expiry string `json:"expiry"`
}

// Revision describes the latest revision of a page.
type Revision struct {
	RevID     int    `json:"revid"`
	ParentID  int    `json:"parentid"`
	Timestamp string `json:"timestamp"`
	User      string `json:"user"`
	Comment   string `json:"comment"`
	Size      int    `json:"size"`
}

// PageInfoOptions chooses the costly parts of PageInfo.
type PageInfoOptions struct {
	// Content fetches the wikitext of the latest revision.
	Content bool
	// Categories fetches every category, hidden ones included, following
	// continuation until the list is complete.
	Categories bool
}

// PageInfo returns information about a page and its latest revision.
func (c *Client) PageInfo(ctx context.Context, title string, opts PageInfoOptions) (*PageInfo, error) {
	rvprop := "ids|timestamp|user|comment|size"
	if opts.Content {
		rvprop += "|content"
	}
	prop := "info|revisions"
	if opts.Categories {
		prop += "|categories"
	}
	params := url.Values{
		"action": {"query"}, "titles": {title}, "prop": {prop},
		"inprop": {"url|protection"},
		"rvprop": {rvprop}, "rvslots": {"main"},
	}
	if opts.Categories {
		params.Set("cllimit", "max")
	}
	var result pageInfoResponse
	if err := c.request(ctx, http.MethodGet, params, &result); err != nil {
		return nil, err
	}
	if len(result.Query.Pages) != 1 {
		return nil, fmt.Errorf("page %q not found", title)
	}
	p := result.Query.Pages[0]
	switch {
	case p.Invalid:
		return nil, fmt.Errorf("invalid title %q", title)
	case p.Missing:
		return nil, fmt.Errorf("%w: %s", ErrMissingPage, title)
	case len(p.Revisions) == 0:
		return nil, errors.New("page has no readable revision")
	}
	info := &PageInfo{
		Title: p.Title, PageID: p.PageID, NS: p.NS, URL: p.FullURL,
		ContentModel: p.ContentModel, Length: p.Length, Touched: p.Touched,
		Redirect: p.Redirect, Protection: p.Protection, Categories: []string{},
		Revision: p.Revisions[0].Revision, Content: p.Revisions[0].Slots["main"].Content,
	}
	if info.Protection == nil {
		info.Protection = []Protection{}
	}
	for _, cat := range p.Categories {
		info.Categories = append(info.Categories, cat.Title)
	}
	// More categories than one response holds come back with clcontinue;
	// fetch only the categories for the rest.
	cont := result.Continue
	for opts.Categories && cont["clcontinue"] != "" {
		params := url.Values{
			"action": {"query"}, "pageids": {fmt.Sprint(info.PageID)}, "prop": {"categories"}, "cllimit": {"max"},
		}
		for k, v := range cont {
			params.Set(k, v)
		}
		var next pageInfoResponse
		if err := c.request(ctx, http.MethodGet, params, &next); err != nil {
			return nil, err
		}
		for _, np := range next.Query.Pages {
			for _, cat := range np.Categories {
				info.Categories = append(info.Categories, cat.Title)
			}
		}
		cont = next.Continue
	}
	return info, nil
}

type pageInfoResponse struct {
	Continue map[string]string `json:"continue"`
	Query    struct {
		Pages []struct {
			Title        string       `json:"title"`
			PageID       int          `json:"pageid"`
			NS           int          `json:"ns"`
			Missing      bool         `json:"missing"`
			Invalid      bool         `json:"invalid"`
			FullURL      string       `json:"fullurl"`
			ContentModel string       `json:"contentmodel"`
			Length       int          `json:"length"`
			Touched      string       `json:"touched"`
			Redirect     bool         `json:"redirect"`
			Protection   []Protection `json:"protection"`
			Categories   []struct {
				Title string `json:"title"`
			} `json:"categories"`
			Revisions []struct {
				Revision
				Slots map[string]struct {
					Content string `json:"content"`
				} `json:"slots"`
			} `json:"revisions"`
		} `json:"pages"`
	} `json:"query"`
}

// PageRef is one page in a list. Field names follow the MediaWiki API.
type PageRef struct {
	Title  string `json:"title"`
	PageID int    `json:"pageid"`
	NS     int    `json:"ns"`
}

// ListSource is where ListPages takes pages from.
type ListSource int

const (
	// AllPages lists pages in title order, optionally from a prefix.
	AllPages ListSource = iota
	// CategoryMembers lists the members of a category.
	CategoryMembers
	// Search lists the results of a full-text search.
	Search
	// LinksTo lists the pages that link to a title.
	LinksTo
	// EmbeddedIn lists the pages that transclude a title, such as a template.
	EmbeddedIn
)

// ListOptions selects the pages ListPages returns.
type ListOptions struct {
	Source ListSource
	// Value is the prefix, category, search query or title of the source.
	Value string
	// Namespace limits the list to one namespace ID when not nil.
	Namespace *int
	// Limit is the most pages to return.
	Limit int
}

// ListPages returns up to opts.Limit pages from one list module, following
// continuation across requests.
func (c *Client) ListPages(ctx context.Context, opts ListOptions) ([]PageRef, error) {
	var list, prefix string
	switch opts.Source {
	case AllPages:
		list, prefix = "allpages", "ap"
	case CategoryMembers:
		list, prefix = "categorymembers", "cm"
	case Search:
		list, prefix = "search", "sr"
	case LinksTo:
		list, prefix = "backlinks", "bl"
	case EmbeddedIn:
		list, prefix = "embeddedin", "ei"
	default:
		return nil, fmt.Errorf("unknown list source %d", opts.Source)
	}
	base := url.Values{"action": {"query"}, "list": {list}}
	switch opts.Source {
	case AllPages:
		if opts.Value != "" {
			base.Set("apprefix", opts.Value)
		}
	case CategoryMembers:
		base.Set("cmtitle", opts.Value)
		base.Set("cmprop", "ids|title")
	case Search:
		base.Set("srsearch", opts.Value)
		base.Set("srprop", "")
		base.Set("srinfo", "")
	case LinksTo:
		base.Set("bltitle", opts.Value)
	case EmbeddedIn:
		base.Set("eititle", opts.Value)
	}
	if opts.Namespace != nil {
		base.Set(prefix+"namespace", fmt.Sprint(*opts.Namespace))
	}
	pages := []PageRef{}
	if opts.Limit <= 0 {
		return pages, nil
	}
	// Each request asks for no more than the pages still wanted.
	base.Set(prefix+"limit", fmt.Sprint(min(opts.Limit, 500)))
	for body, err := range c.Paginate(ctx, http.MethodGet, base) {
		if err != nil {
			return nil, err
		}
		var result struct {
			Query map[string]json.RawMessage `json:"query"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decode API response: %w", err)
		}
		var batch []PageRef
		if raw, ok := result.Query[list]; ok {
			if err := json.Unmarshal(raw, &batch); err != nil {
				return nil, fmt.Errorf("decode API response: %w", err)
			}
		}
		pages = append(pages, batch...)
		if len(pages) >= opts.Limit {
			break
		}
		base.Set(prefix+"limit", fmt.Sprint(min(opts.Limit-len(pages), 500)))
	}
	if len(pages) > opts.Limit {
		pages = pages[:opts.Limit]
	}
	return pages, nil
}

// NamespaceNames returns the names a namespace can be written with: the local
// name, the canonical name and the aliases.
func (c *Client) NamespaceNames(ctx context.Context, id int) ([]string, error) {
	all, err := c.namespaceNames(ctx)
	if err != nil {
		return nil, err
	}
	return all[id], nil
}

// NamespaceID returns the ID of the namespace that name, local, canonical or
// an alias, stands for, ignoring case and treating _ as a space; ok is false
// when no namespace has that name.
func (c *Client) NamespaceID(ctx context.Context, name string) (id int, ok bool, err error) {
	all, err := c.namespaceNames(ctx)
	if err != nil {
		return 0, false, err
	}
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " ")) }
	want := norm(name)
	if want == "" {
		return 0, false, nil
	}
	for id, names := range all {
		for _, n := range names {
			if n != "" && norm(n) == want {
				return id, true, nil
			}
		}
	}
	return 0, false, nil
}

// namespaceNames returns the names of every namespace by ID.
func (c *Client) namespaceNames(ctx context.Context) (map[int][]string, error) {
	var result struct {
		Query struct {
			Namespaces map[string]struct {
				ID        int    `json:"id"`
				Name      string `json:"name"`
				Canonical string `json:"canonical"`
			} `json:"namespaces"`
			Aliases []struct {
				ID    int    `json:"id"`
				Alias string `json:"alias"`
			} `json:"namespacealiases"`
		} `json:"query"`
	}
	params := url.Values{"action": {"query"}, "meta": {"siteinfo"}, "siprop": {"namespaces|namespacealiases"}}
	if err := c.request(ctx, http.MethodGet, params, &result); err != nil {
		return nil, err
	}
	all := map[int][]string{}
	for _, ns := range result.Query.Namespaces {
		all[ns.ID] = append(all[ns.ID], ns.Name, ns.Canonical)
	}
	for _, a := range result.Query.Aliases {
		all[a.ID] = append(all[a.ID], a.Alias)
	}
	return all, nil
}

// IsBotName reports whether name looks like the login name of a bot password,
// such as Alice@mybot: @ is MediaWiki's default delimiter, and user names
// cannot contain it by default.
func IsBotName(name string) bool { return strings.Contains(name, "@") }

// BotLogin logs in with a bot password from Special:BotPasswords through
// action=login, the only login module that accepts them. domain is the
// authentication domain of an LDAP wiki, or empty.
func (c *Client) BotLogin(ctx context.Context, name, password, domain string) error {
	token, err := c.TokenOf(ctx, "login")
	if err != nil {
		return err
	}
	form := url.Values{"action": {"login"}, "lgname": {name}, "lgpassword": {password}, "lgtoken": {token}}
	if domain != "" {
		form.Set("lgdomain", domain)
	}
	var result struct {
		Login struct {
			Result string `json:"result"`
			Reason string `json:"reason"`
		} `json:"login"`
	}
	if err := c.request(ctx, http.MethodPost, form, &result); err != nil {
		return err
	}
	if result.Login.Result != "Success" {
		reason := result.Login.Reason
		if reason == "" {
			reason = result.Login.Result
		}
		return fmt.Errorf("bot password login failed for %s: %s", name, reason)
	}
	return nil
}

// HistoryEntry is one revision in the history of a page. Field names follow
// the MediaWiki API; a hidden user or comment is empty.
type HistoryEntry struct {
	Revision
	Minor bool     `json:"minor"`
	Tags  []string `json:"tags"`
}

// History returns up to limit revisions of title, newest first, following
// continuation across requests.
func (c *Client) History(ctx context.Context, title string, limit int) ([]HistoryEntry, error) {
	entries := []HistoryEntry{}
	if limit <= 0 {
		return entries, nil
	}
	params := url.Values{
		"action": {"query"}, "prop": {"revisions"}, "titles": {title},
		"rvprop":  {"ids|timestamp|user|comment|size|flags|tags"},
		"rvlimit": {fmt.Sprint(min(limit, 500))},
	}
	for body, err := range c.Paginate(ctx, http.MethodGet, params) {
		if err != nil {
			return nil, err
		}
		var result struct {
			Query struct {
				Pages []struct {
					Missing   bool           `json:"missing"`
					Invalid   bool           `json:"invalid"`
					Revisions []HistoryEntry `json:"revisions"`
				} `json:"pages"`
			} `json:"query"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decode API response: %w", err)
		}
		if len(result.Query.Pages) != 1 {
			return nil, fmt.Errorf("page %q not found", title)
		}
		p := result.Query.Pages[0]
		switch {
		case p.Invalid:
			return nil, fmt.Errorf("invalid title %q", title)
		case p.Missing:
			return nil, fmt.Errorf("%w: %s", ErrMissingPage, title)
		}
		entries = append(entries, p.Revisions...)
		if len(entries) >= limit {
			break
		}
		params.Set("rvlimit", fmt.Sprint(min(limit-len(entries), 500)))
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	for i := range entries {
		if entries[i].Tags == nil {
			entries[i].Tags = []string{}
		}
	}
	return entries, nil
}

// RevisionText is one revision with its wikitext.
type RevisionText struct {
	Title    string
	RevID    int
	ParentID int
	Timestamp,
	User,
	Content string
	// ContentModel is the model of the main slot, such as wikitext or json.
	ContentModel string
}

// ErrMissingRevision means no revision has the ID.
var ErrMissingRevision = errors.New("revision does not exist")

// RevisionByID returns a revision and its wikitext.
func (c *Client) RevisionByID(ctx context.Context, revid int) (*RevisionText, error) {
	return c.revisionText(ctx, url.Values{"revids": {strconv.Itoa(revid)}}, fmt.Sprint("revision ", revid))
}

// LatestRevision returns the latest revision of title and its wikitext.
func (c *Client) LatestRevision(ctx context.Context, title string) (*RevisionText, error) {
	return c.revisionText(ctx, url.Values{"titles": {title}}, title)
}

func (c *Client) revisionText(ctx context.Context, params url.Values, what string) (*RevisionText, error) {
	params.Set("action", "query")
	params.Set("prop", "revisions")
	params.Set("rvprop", "ids|timestamp|user|content|contentmodel")
	params.Set("rvslots", "main")
	var result struct {
		Query struct {
			// badrevids is an object keyed by revision ID, also in formatversion 2.
			BadRevIDs map[string]json.RawMessage `json:"badrevids"`
			Pages     []struct {
				Title     string `json:"title"`
				Missing   bool   `json:"missing"`
				Invalid   bool   `json:"invalid"`
				Revisions []struct {
					RevID      int    `json:"revid"`
					ParentID   int    `json:"parentid"`
					Timestamp  string `json:"timestamp"`
					User       string `json:"user"`
					TextHidden bool   `json:"texthidden"`
					Slots      map[string]struct {
						// Content is nil when the response has none, which
						// differs from an empty text.
						Content      *string `json:"content"`
						ContentModel string  `json:"contentmodel"`
						TextHidden   bool    `json:"texthidden"`
						TextMissing  bool    `json:"textmissing"`
					} `json:"slots"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := c.request(ctx, http.MethodGet, params, &result); err != nil {
		return nil, err
	}
	if len(result.Query.BadRevIDs) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMissingRevision, what)
	}
	if len(result.Query.Pages) != 1 {
		return nil, fmt.Errorf("%s not found", what)
	}
	p := result.Query.Pages[0]
	switch {
	case p.Invalid:
		return nil, fmt.Errorf("invalid title %q", what)
	case p.Missing && len(p.Revisions) == 0:
		return nil, fmt.Errorf("%w: %s", ErrMissingPage, what)
	case len(p.Revisions) == 0:
		return nil, fmt.Errorf("%s has no readable revision", what)
	}
	r := p.Revisions[0]
	main, ok := r.Slots["main"]
	if r.TextHidden || main.TextHidden || main.TextMissing {
		return nil, fmt.Errorf("the text of revision %d of %s is hidden or missing", r.RevID, p.Title)
	}
	if !ok || main.Content == nil {
		return nil, fmt.Errorf("the API returned no text for revision %d of %s", r.RevID, p.Title)
	}
	return &RevisionText{Title: p.Title, RevID: r.RevID, ParentID: r.ParentID, Timestamp: r.Timestamp, User: r.User, Content: *main.Content, ContentModel: main.ContentModel}, nil
}

// PreSaveTransform returns text as the wiki would save it as title in the
// content model model: for wikitext, with signatures, substitutions and
// trailing white space handled, as the logged-in user when the client has
// credentials; JSON, for one, is reformatted. An empty model is the model the
// wiki gives title, as for a new page.
func (c *Client) PreSaveTransform(ctx context.Context, title, text, model string) (string, error) {
	form := url.Values{"action": {"parse"}, "onlypst": {"1"}, "title": {title}, "text": {text}}
	if model != "" {
		form.Set("contentmodel", model)
	}
	var result struct {
		Parse struct {
			Text *string `json:"text"`
		} `json:"parse"`
	}
	if err := c.request(ctx, http.MethodPost, form, &result); err != nil {
		return "", err
	}
	if result.Parse.Text == nil {
		return "", errors.New("the API returned no transformed text")
	}
	return *result.Parse.Text, nil
}
