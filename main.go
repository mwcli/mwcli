package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

const defaultAPI = "https://zetawiki.com/w/api.php"

type client struct {
	api         string
	accessToken string
	http        *http.Client
}

type profile struct {
	API      string         `json:"api"`
	Username string         `json:"username"`
	Auth     string         `json:"auth,omitempty"`
	Token    string         `json:"token,omitempty"`
	Cookies  []*http.Cookie `json:"cookies,omitempty"`
}

const authOAuth2 = "oauth2"

func newClient(api string) (*client, error) {
	u, err := url.Parse(api)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", api)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &client{api: api, accessToken: os.Getenv("MWTOOL_TOKEN"), http: &http.Client{Jar: jar, Timeout: 45 * time.Second}}
	if c.accessToken != "" {
		return c, nil
	}
	if p, err := readProfile(); err == nil && p.API == api {
		if p.Auth == authOAuth2 {
			c.accessToken = p.Token
		} else {
			jar.SetCookies(u, p.Cookies)
		}
	}
	return c, nil
}

func (c *client) authenticated() bool {
	if c.accessToken != "" {
		return true
	}
	p, err := readProfile()
	return err == nil && p.API == c.api
}

func (c *client) authMethod() string {
	if c.accessToken != "" {
		return "OAuth 2 access token"
	}
	return "password session"
}

func readProfile() (profile, error) {
	var p profile
	b, err := os.ReadFile(filepath.Join(stateDir(), "profile.json"))
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	return p, err
}

func (c *client) saveProfile(username string) error {
	u, _ := url.Parse(c.api)
	p := profile{API: c.api, Username: username}
	if c.accessToken != "" {
		p.Auth, p.Token = authOAuth2, c.accessToken
	} else {
		p.Cookies = c.http.Jar.Cookies(u)
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(stateDir(), 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir(), "profile.json"), b, 0600)
}

func (c *client) request(method string, values url.Values, out any) error {
	var req *http.Request
	var err error
	if method == http.MethodGet {
		u, _ := url.Parse(c.api)
		u.RawQuery = values.Encode()
		req, err = http.NewRequest(method, u.String(), nil)
	} else {
		req, err = http.NewRequest(method, c.api, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mwtool/0.1 (https://github.com/jmnote/mwtool)")
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("MediaWiki returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var envelope struct {
		Error  *struct{ Code, Info string }  `json:"error"`
		Errors []struct{ Code, Text string } `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("MediaWiki API error %s: %s", envelope.Error.Code, envelope.Error.Info)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("MediaWiki API error %s: %s", envelope.Errors[0].Code, envelope.Errors[0].Text)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	return nil
}

func (c *client) token(kind string) (string, error) {
	var result struct {
		Query struct {
			Tokens map[string]string `json:"tokens"`
		} `json:"query"`
	}
	if err := c.request(http.MethodGet, url.Values{"action": {"query"}, "meta": {"tokens"}, "type": {kind}, "format": {"json"}}, &result); err != nil {
		return "", err
	}
	key := kind + "token"
	token := result.Query.Tokens[key]
	if token == "" {
		return "", fmt.Errorf("API did not return %s token", kind)
	}
	return token, nil
}

func (c *client) login(username, password string) error {
	token, err := c.token("login")
	if err != nil {
		return err
	}
	form := url.Values{"action": {"clientlogin"}, "format": {"json"}, "loginreturnurl": {c.api}, "logintoken": {token}, "username": {username}, "password": {password}}
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

func (c *client) user() (string, error) {
	var result struct {
		Query struct {
			UserInfo struct {
				Name string `json:"name"`
				ID   int    `json:"id"`
				Anon bool   `json:"anon"`
			} `json:"userinfo"`
		} `json:"query"`
	}
	params := url.Values{"action": {"query"}, "meta": {"userinfo"}, "format": {"json"}}
	if err := c.request(http.MethodGet, params, &result); err != nil {
		return "", err
	}
	if result.Query.UserInfo.Anon || result.Query.UserInfo.ID == 0 {
		return "", errors.New("not logged in")
	}
	return result.Query.UserInfo.Name, nil
}

func (c *client) page(title string) (string, error) {
	var result struct {
		Query struct {
			Pages []struct {
				Missing   bool `json:"missing"`
				Revisions []struct {
					Slots map[string]struct {
						Content string `json:"content"`
					} `json:"slots"`
					Legacy string `json:"*"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
	}
	params := url.Values{"action": {"query"}, "prop": {"revisions"}, "rvprop": {"content"}, "rvslots": {"main"}, "titles": {title}, "format": {"json"}, "formatversion": {"2"}}
	if err := c.request(http.MethodGet, params, &result); err != nil {
		return "", err
	}
	for _, p := range result.Query.Pages {
		if p.Missing {
			return "", fmt.Errorf("page %q does not exist", title)
		}
		if len(p.Revisions) == 0 {
			return "", errors.New("page has no readable revision")
		}
		rev := p.Revisions[0]
		if main, ok := rev.Slots["main"]; ok {
			return main.Content, nil
		}
		return rev.Legacy, nil
	}
	return "", fmt.Errorf("page %q not found", title)
}

func (c *client) edit(title, text, summary string) error {
	token, err := c.token("csrf")
	if err != nil {
		return err
	}
	form := url.Values{"action": {"edit"}, "title": {title}, "text": {text}, "summary": {summary}, "token": {token}, "format": {"json"}}
	var result struct {
		Edit struct {
			Result string `json:"result"`
			Title  string `json:"title"`
		} `json:"edit"`
	}
	if err := c.request(http.MethodPost, form, &result); err != nil {
		return err
	}
	if result.Edit.Result != "Success" {
		return fmt.Errorf("edit did not succeed (result: %q)", result.Edit.Result)
	}
	fmt.Printf("Updated %s\n", result.Edit.Title)
	return nil
}

var canIActions = map[string]string{
	"edit":    "edit",
	"create":  "createpage",
	"move":    "move",
	"delete":  "delete",
	"protect": "protect",
}

func (c *client) rights() ([]string, error) {
	var result struct {
		Query struct {
			UserInfo struct {
				Rights []string `json:"rights"`
			} `json:"userinfo"`
		} `json:"query"`
	}
	params := url.Values{"action": {"query"}, "meta": {"userinfo"}, "uiprop": {"rights"}, "format": {"json"}, "formatversion": {"2"}}
	if err := c.request(http.MethodGet, params, &result); err != nil {
		return nil, err
	}
	return result.Query.UserInfo.Rights, nil
}

func (c *client) canOnTitle(action, title string) ([]string, error) {
	var result struct {
		Query struct {
			Pages []struct {
				Invalid bool `json:"invalid"`
				Actions map[string][]struct {
					Code string `json:"code"`
					Text string `json:"text"`
				} `json:"actions"`
			} `json:"pages"`
		} `json:"query"`
	}
	params := url.Values{"action": {"query"}, "prop": {"info"}, "intestactions": {action}, "intestactionsdetail": {"full"}, "titles": {title}, "errorformat": {"plaintext"}, "format": {"json"}, "formatversion": {"2"}}
	if err := c.request(http.MethodGet, params, &result); err != nil {
		return nil, err
	}
	if len(result.Query.Pages) != 1 || result.Query.Pages[0].Invalid {
		return nil, fmt.Errorf("invalid title %q", title)
	}
	var reasons []string
	for _, e := range result.Query.Pages[0].Actions[action] {
		reasons = append(reasons, e.Code+": "+e.Text)
	}
	return reasons, nil
}

var errNo = errors.New("no")

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func usageError(format string, a ...any) error {
	return &exitError{code: 2, err: fmt.Errorf(format, a...)}
}

func parseError(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return err
	}
	return &exitError{code: 2, err: err}
}

func canI(c *client, args []string, list, quiet bool) error {
	say := func(format string, a ...any) {
		if !quiet {
			fmt.Printf(format, a...)
		}
	}
	if list {
		rights, err := c.rights()
		if err != nil {
			return err
		}
		for _, r := range rights {
			fmt.Println(r)
		}
		return nil
	}
	if len(args) < 1 || len(args) > 2 {
		return usageError("usage: mw auth can-i [--quiet] ACTION [TITLE] | --list")
	}
	action := args[0]
	right, ok := canIActions[action]
	if !ok {
		return usageError("unknown action %q (edit, create, move, delete, protect)", action)
	}
	if len(args) == 2 {
		reasons, err := c.canOnTitle(action, args[1])
		if err != nil {
			return err
		}
		if len(reasons) > 0 {
			say("no\n")
			for _, r := range reasons {
				say("  %s\n", r)
			}
			return errNo
		}
		say("yes\n")
		return nil
	}
	rights, err := c.rights()
	if err != nil {
		return err
	}
	for _, r := range rights {
		if r == right {
			say("yes (has %s right; give a TITLE to include protection and blocks)\n", right)
			return nil
		}
	}
	say("no (missing %s right)\n", right)
	return errNo
}

func stateDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return "."
	}
	return filepath.Join(d, "mwtool")
}
func apiFromFlags() string { return os.Getenv("MWTOOL_API") }
func defaultAPIEndpoint() string {
	if api := apiFromFlags(); api != "" {
		return api
	}
	if p, err := readProfile(); err == nil && p.API != "" {
		return p.API
	}
	return defaultAPI
}
func usage() {
	fmt.Fprintln(os.Stderr, "mw: MediaWiki from your terminal\n\nUsage:\n  mw auth status [--api URL]\n  mw auth login [--api URL] [--username NAME]\n  mw auth login --token [--api URL]\n  mw auth can-i [--quiet] ACTION [TITLE] [--api URL]\n  mw auth can-i --list [--api URL]\n  mw page get TITLE [--api URL]\n  mw page edit TITLE [--api URL] [--summary TEXT] [--text TEXT | --file PATH]\n\nSet MWTOOL_API to choose a wiki API endpoint (default: "+defaultAPI+").")
}

func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func loginWithToken(api string) error {
	token := os.Getenv("MWTOOL_TOKEN")
	if token == "" {
		fmt.Fprint(os.Stderr, "Access token: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("read token without echo: %w (set MWTOOL_TOKEN for non-interactive use)", err)
		}
		token = strings.TrimSpace(string(b))
	}
	if token == "" {
		return usageError("access token is required")
	}
	c, err := newClient(api)
	if err != nil {
		return err
	}
	c.accessToken = token
	user, err := c.user()
	if err != nil {
		return fmt.Errorf("token check failed: %w", err)
	}
	if err = c.saveProfile(user); err != nil {
		return err
	}
	fmt.Printf("Logged in to %s as %s (%s)\n", api, user, c.authMethod())
	return nil
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage()
		return nil
	}
	if args[0] == "auth" && len(args) > 1 && args[1] == "can-i" {
		fs := flag.NewFlagSet("auth can-i", flag.ContinueOnError)
		api := fs.String("api", defaultAPIEndpoint(), "MediaWiki API endpoint")
		list := fs.Bool("list", false, "List the rights of the current login")
		quiet := fs.Bool("quiet", false, "Print nothing; only set the exit status")
		fs.BoolVar(quiet, "q", false, "Shorthand for --quiet")
		positional, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return parseError(err)
		}
		c, err := newClient(*api)
		if err == nil {
			err = canI(c, positional, *list, *quiet)
		}
		var ee *exitError
		if err != nil && !errors.Is(err, errNo) && !errors.As(err, &ee) {
			err = &exitError{code: 2, err: err}
		}
		return err
	}
	if args[0] == "auth" && len(args) > 1 && (args[1] == "login" || args[1] == "status") {
		fs := flag.NewFlagSet("auth "+args[1], flag.ContinueOnError)
		api := fs.String("api", defaultAPIEndpoint(), "MediaWiki API endpoint")
		username := fs.String("username", os.Getenv("MWTOOL_USERNAME"), "Wiki username")
		useToken := false
		if args[1] == "login" {
			fs.BoolVar(&useToken, "token", false, "Log in with an OAuth 2 access token")
		}
		if err := fs.Parse(args[2:]); err != nil {
			return parseError(err)
		}
		if args[1] == "status" {
			c, err := newClient(*api)
			if err != nil {
				return err
			}
			if !c.authenticated() {
				return fmt.Errorf("not logged in to %s; run `mw auth login --api %s`", *api, *api)
			}
			user, err := c.user()
			if err != nil {
				return err
			}
			fmt.Printf("Logged in to %s as %s (%s)\n", *api, user, c.authMethod())
			return nil
		}
		if useToken {
			return loginWithToken(*api)
		}
		if *username == "" {
			fmt.Fprint(os.Stderr, "Username: ")
			if _, err := fmt.Fscanln(os.Stdin, username); err != nil {
				return fmt.Errorf("read username: %w", err)
			}
		}
		password := os.Getenv("MWTOOL_PASSWORD")
		if password == "" {
			fmt.Fprint(os.Stderr, "Password: ")
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fmt.Errorf("read password without echo: %w (set MWTOOL_PASSWORD for non-interactive use)", err)
			}
			password = string(b)
		}
		if *username == "" || password == "" {
			return usageError("username and password are required")
		}
		c, err := newClient(*api)
		if err != nil {
			return err
		}
		if err = c.login(*username, password); err != nil {
			return err
		}
		if err = c.saveProfile(*username); err != nil {
			return err
		}
		fmt.Printf("Logged in to %s as %s\n", *api, *username)
		return nil
	}
	if args[0] == "page" && len(args) > 1 {
		if args[1] != "get" && args[1] != "edit" {
			return usageError("unknown page command %q", args[1])
		}
		fs := flag.NewFlagSet("page "+args[1], flag.ContinueOnError)
		api := fs.String("api", apiFromFlags(), "MediaWiki API endpoint")
		summary := fs.String("summary", "", "Edit summary")
		textArg := fs.String("text", "", "Page wikitext")
		file := fs.String("file", "", "Read page wikitext from a file")
		positional, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return parseError(err)
		}
		if len(positional) != 1 {
			return usageError("page command requires a title")
		}
		title := positional[0]
		if *api == "" {
			*api = defaultAPIEndpoint()
		}
		c, err := newClient(*api)
		if err != nil {
			return err
		}
		if args[1] == "get" {
			content, err := c.page(title)
			if err == nil {
				fmt.Print(content)
			}
			return err
		}
		content := *textArg
		if *file != "" {
			b, err := os.ReadFile(*file)
			if err != nil {
				return err
			}
			content = string(b)
		} else if content == "" {
			return usageError("provide --text or --file")
		}
		if !c.authenticated() {
			return errors.New("not logged in; run `mw auth login`")
		}
		return c.edit(title, content, *summary)
	}
	usage()
	return usageError("unknown command")
}

func main() {
	err := run(os.Args[1:])
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return
	case errors.Is(err, errNo):
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "mwtool:", err)
	var ee *exitError
	if errors.As(err, &ee) {
		os.Exit(ee.code)
	}
	os.Exit(1)
}
