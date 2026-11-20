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
	api  string
	http *http.Client
}

type profile struct {
	API      string         `json:"api"`
	Username string         `json:"username"`
	Cookies  []*http.Cookie `json:"cookies,omitempty"`
}

func newClient(api string) (*client, error) {
	u, err := url.Parse(api)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", api)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &client{api: api, http: &http.Client{Jar: jar, Timeout: 45 * time.Second}}
	if p, err := readProfile(); err == nil && p.API == api {
		jar.SetCookies(u, p.Cookies)
	}
	return c, nil
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
	p := profile{API: c.api, Username: username, Cookies: c.http.Jar.Cookies(u)}
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
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("MediaWiki returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	var envelope struct {
		Error *struct{ Code, Info string } `json:"error"`
	}
	if b, err := json.Marshal(out); err == nil {
		_ = json.Unmarshal(b, &envelope)
	}
	if envelope.Error != nil {
		return fmt.Errorf("MediaWiki API error %s: %s", envelope.Error.Code, envelope.Error.Info)
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
	fmt.Fprintln(os.Stderr, "mw: MediaWiki from your terminal\n\nUsage:\n  mw auth status [--api URL]\n  mw auth login [--api URL] [--username NAME]\n  mw page get TITLE [--api URL]\n  mw page edit TITLE [--api URL] [--summary TEXT] [--text TEXT | --file PATH]\n\nSet MWTOOL_API to choose a wiki API endpoint (default: "+defaultAPI+").")
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage()
		return nil
	}
	if args[0] == "auth" && len(args) > 1 && (args[1] == "login" || args[1] == "status") {
		fs := flag.NewFlagSet("auth "+args[1], flag.ContinueOnError)
		api := fs.String("api", defaultAPIEndpoint(), "MediaWiki API endpoint")
		username := fs.String("username", os.Getenv("MWTOOL_USERNAME"), "Wiki username")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if args[1] == "status" {
			p, err := readProfile()
			if err != nil || p.API != *api {
				return fmt.Errorf("not logged in to %s; run `mw auth login --api %s`", *api, *api)
			}
			c, err := newClient(*api)
			if err != nil {
				return err
			}
			user, err := c.user()
			if err != nil {
				return err
			}
			fmt.Printf("Logged in to %s as %s\n", *api, user)
			return nil
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
			return errors.New("username and password are required")
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
			return fmt.Errorf("unknown page command %q", args[1])
		}
		fs := flag.NewFlagSet("page "+args[1], flag.ContinueOnError)
		api := fs.String("api", apiFromFlags(), "MediaWiki API endpoint")
		summary := fs.String("summary", "", "Edit summary")
		textArg := fs.String("text", "", "Page wikitext")
		file := fs.String("file", "", "Read page wikitext from a file")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("page command requires a title")
		}
		if *api == "" {
			*api = defaultAPIEndpoint()
		}
		c, err := newClient(*api)
		if err != nil {
			return err
		}
		if args[1] == "get" {
			content, err := c.page(fs.Arg(0))
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
			return errors.New("provide --text or --file")
		}
		p, err := readProfile()
		if err != nil || p.API != *api {
			return errors.New("not logged in; run `mw auth login`")
		}
		return c.edit(fs.Arg(0), content, *summary)
	}
	usage()
	return fmt.Errorf("unknown command")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mwtool:", err)
		os.Exit(1)
	}
}
