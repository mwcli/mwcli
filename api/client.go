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
	"strconv"
	"strings"
	"time"

	"github.com/mwcli/mwcli/internal/build"
	"github.com/mwcli/mwcli/internal/config"
)

// Client talks to one wiki's api.php.
type Client struct {
	API   string
	Token string
	// Resolved is the api.php URL the last request ended at after redirects.
	Resolved string
	http     *http.Client
	url      *url.URL
	// botName and botPassword log in with action=login before the first request.
	botName, botPassword string
	botState             int // 0 not tried, 1 logging in, 2 done
	botErr               error
	// oauth1 signs every request to the API host with OAuth 1.0a.
	oauth1 *OAuth1
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
	c := &Client{API: api, url: u}
	c.http = &http.Client{Jar: jar, Timeout: 45 * time.Second, CheckRedirect: c.checkRedirect}
	if account != nil {
		if account.Auth == config.AuthOAuth2 {
			c.Token = account.OAuthToken
		} else if account.Auth == config.AuthOAuth1 {
			c.oauth1 = &OAuth1{ConsumerKey: account.ConsumerKey, ConsumerSecret: account.ConsumerSecret, AccessToken: account.AccessToken, AccessSecret: account.AccessSecret}
		} else if account.Auth == config.AuthBotPassword {
			c.botName, c.botPassword = account.BotName, account.BotPassword
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
	if c.botName != "" && c.botState == 0 {
		// A bot password session is short lived, so each run logs in afresh.
		c.botState = 1
		c.botErr = c.BotLogin(c.botName, c.botPassword)
		c.botState = 2
	}
	if c.botErr != nil {
		return nil, c.botErr
	}
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
		req, err = http.NewRequest(method, u.String(), nil)
	} else {
		u.RawQuery = q.Encode()
		req, err = http.NewRequest(method, u.String(), strings.NewReader(params.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mw/"+build.Version+" (https://github.com/mwcli/mwcli)")
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
		return nil, err
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

// General is the general site information of a wiki (meta=siteinfo).
type General struct {
	SiteName    string `json:"sitename"`
	Generator   string `json:"generator"`
	Server      string `json:"server"`
	Script      string `json:"script"`
	ArticlePath string `json:"articlepath"`
}

// SiteInfo returns the general site information.
func (c *Client) SiteInfo() (*General, error) {
	var result struct {
		Query struct {
			General General `json:"general"`
		} `json:"query"`
	}
	if err := c.request(http.MethodGet, url.Values{"action": {"query"}, "meta": {"siteinfo"}, "siprop": {"general"}}, &result); err != nil {
		return nil, err
	}
	g := result.Query.General
	if !strings.HasPrefix(g.Generator, "MediaWiki") {
		return nil, fmt.Errorf("%s is not a MediaWiki API", c.API)
	}
	return &g, nil
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
	form := url.Values{"action": {"clientlogin"}, "loginreturnurl": {c.API}, "logintoken": {token}, "username": {username}, "password": {password}, "rememberMe": {"1"}}
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

// EditOptions are the options of Edit besides the title and text.
type EditOptions struct {
	Summary string
	Mode    EditMode
	// Minor marks the edit minor; Bot marks it a bot edit, which hides it from
	// recent changes when the account has the bot right.
	Minor, Bot bool
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
func (c *Client) Edit(title, text string, opts EditOptions) (*EditResult, error) {
	token, err := c.TokenOf("csrf")
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
	var result struct {
		Edit struct {
			Result   string `json:"result"`
			Title    string `json:"title"`
			NoChange bool   `json:"nochange"`
			New      bool   `json:"new"`
		} `json:"edit"`
	}
	err = c.request(http.MethodPost, form, &result)
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case "missingtitle":
			return nil, fmt.Errorf("%w: %s", ErrMissingPage, title)
		case "articleexists":
			return nil, fmt.Errorf("%w: %s", ErrPageExists, title)
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
func (c *Client) PageInfo(title string, opts PageInfoOptions) (*PageInfo, error) {
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
	if err := c.request(http.MethodGet, params, &result); err != nil {
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
		if err := c.request(http.MethodGet, params, &next); err != nil {
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
func (c *Client) ListPages(opts ListOptions) ([]PageRef, error) {
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
	cont := map[string]string{}
	for len(pages) < opts.Limit {
		params := url.Values{}
		for k, v := range base {
			params[k] = v
		}
		params.Set(prefix+"limit", fmt.Sprint(min(opts.Limit-len(pages), 500)))
		for k, v := range cont {
			params.Set(k, v)
		}
		var result struct {
			Continue map[string]json.RawMessage `json:"continue"`
			Query    map[string]json.RawMessage `json:"query"`
		}
		if err := c.request(http.MethodGet, params, &result); err != nil {
			return nil, err
		}
		var batch []PageRef
		if raw, ok := result.Query[list]; ok {
			if err := json.Unmarshal(raw, &batch); err != nil {
				return nil, fmt.Errorf("decode API response: %w", err)
			}
		}
		pages = append(pages, batch...)
		if len(result.Continue) == 0 {
			break
		}
		// Continuation values are strings or, like sroffset, numbers.
		cont = map[string]string{}
		for k, raw := range result.Continue {
			var str string
			if json.Unmarshal(raw, &str) != nil {
				str = string(raw)
			}
			cont[k] = str
		}
	}
	if len(pages) > opts.Limit {
		pages = pages[:opts.Limit]
	}
	return pages, nil
}

// NamespaceNames returns the names a namespace can be written with: the local
// name, the canonical name and the aliases.
func (c *Client) NamespaceNames(id int) ([]string, error) {
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
	if err := c.request(http.MethodGet, params, &result); err != nil {
		return nil, err
	}
	var names []string
	for _, ns := range result.Query.Namespaces {
		if ns.ID == id {
			names = append(names, ns.Name, ns.Canonical)
		}
	}
	for _, a := range result.Query.Aliases {
		if a.ID == id {
			names = append(names, a.Alias)
		}
	}
	return names, nil
}

// IsBotName reports whether name looks like the login name of a bot password,
// such as Alice@mybot: @ is MediaWiki's default delimiter, and user names
// cannot contain it by default.
func IsBotName(name string) bool { return strings.Contains(name, "@") }

// BotLogin logs in with a bot password from Special:BotPasswords through
// action=login, the only login module that accepts them.
func (c *Client) BotLogin(name, password string) error {
	token, err := c.TokenOf("login")
	if err != nil {
		return err
	}
	form := url.Values{"action": {"login"}, "lgname": {name}, "lgpassword": {password}, "lgtoken": {token}}
	var result struct {
		Login struct {
			Result string `json:"result"`
			Reason string `json:"reason"`
		} `json:"login"`
	}
	if err := c.request(http.MethodPost, form, &result); err != nil {
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
func (c *Client) History(title string, limit int) ([]HistoryEntry, error) {
	entries := []HistoryEntry{}
	cont := map[string]string{}
	for len(entries) < limit {
		params := url.Values{
			"action": {"query"}, "prop": {"revisions"}, "titles": {title},
			"rvprop":  {"ids|timestamp|user|comment|size|flags|tags"},
			"rvlimit": {fmt.Sprint(min(limit-len(entries), 500))},
		}
		for k, v := range cont {
			params.Set(k, v)
		}
		var result struct {
			Continue map[string]string `json:"continue"`
			Query    struct {
				Pages []struct {
					Missing   bool           `json:"missing"`
					Invalid   bool           `json:"invalid"`
					Revisions []HistoryEntry `json:"revisions"`
				} `json:"pages"`
			} `json:"query"`
		}
		if err := c.request(http.MethodGet, params, &result); err != nil {
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
		}
		entries = append(entries, p.Revisions...)
		if len(result.Continue) == 0 {
			break
		}
		cont = result.Continue
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
func (c *Client) RevisionByID(revid int) (*RevisionText, error) {
	return c.revisionText(url.Values{"revids": {strconv.Itoa(revid)}}, fmt.Sprint("revision ", revid))
}

// LatestRevision returns the latest revision of title and its wikitext.
func (c *Client) LatestRevision(title string) (*RevisionText, error) {
	return c.revisionText(url.Values{"titles": {title}}, title)
}

func (c *Client) revisionText(params url.Values, what string) (*RevisionText, error) {
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
	if err := c.request(http.MethodGet, params, &result); err != nil {
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
func (c *Client) PreSaveTransform(title, text, model string) (string, error) {
	form := url.Values{"action": {"parse"}, "onlypst": {"1"}, "title": {title}, "text": {text}}
	if model != "" {
		form.Set("contentmodel", model)
	}
	var result struct {
		Parse struct {
			Text *string `json:"text"`
		} `json:"parse"`
	}
	if err := c.request(http.MethodPost, form, &result); err != nil {
		return "", err
	}
	if result.Parse.Text == nil {
		return "", errors.New("the API returned no transformed text")
	}
	return *result.Parse.Text, nil
}
