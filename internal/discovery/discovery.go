// Package discovery finds the api.php of a wiki and caches what it learns,
// like the kubectl discovery cache: one directory per host, refreshed after TTL.
package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/zetaoss/mwcli/api"
	"github.com/zetaoss/mwcli/internal/build"
)

// TTL is how long a discovered site stays valid.
const TTL = 24 * time.Hour

// Site is what discovery learned about a wiki.
type Site struct {
	API         string    `json:"api"`
	Server      string    `json:"server"`
	Script      string    `json:"script"`
	ArticlePath string    `json:"articlepath"`
	Generator   string    `json:"generator"`
	SiteName    string    `json:"sitename"`
	Checked     time.Time `json:"checked"`
}

// Dir returns the cache directory: $XDG_CACHE_HOME/mw, %LocalAppData%\mw on
// Windows, else ~/.cache/mw.
func Dir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "mw")
	}
	if d := os.Getenv("LocalAppData"); runtime.GOOS == "windows" && d != "" {
		return filepath.Join(d, "mw")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "mw-cache")
	}
	return filepath.Join(home, ".cache", "mw")
}

// Cache discovers sites and keeps them under Dir/discovery/<host>/siteinfo.json.
type Cache struct {
	Dir string
	// Base returns the site root URL of a host; tests point it at a local server.
	Base func(host string) string
	Now  func() time.Time
}

// New returns the cache in Dir().
func New() *Cache {
	return &Cache{Dir: Dir(), Base: func(h string) string { return "https://" + h }, Now: time.Now}
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func (c *Cache) path(host string) string {
	return filepath.Join(c.Dir, "discovery", unsafe.ReplaceAllString(host, "_"), "siteinfo.json")
}

// Lookup returns the cached site of host when it is younger than TTL.
func (c *Cache) Lookup(host string) (*Site, bool) {
	b, err := os.ReadFile(c.path(host))
	if err != nil {
		return nil, false
	}
	var s Site
	if json.Unmarshal(b, &s) != nil || s.API == "" || c.Now().Sub(s.Checked) > TTL {
		return nil, false
	}
	return &s, true
}

// Invalidate removes the cached site of host.
func (c *Cache) Invalidate(host string) {
	_ = os.RemoveAll(filepath.Dir(c.path(host)))
}

// Site returns the cached site of host, or discovers and caches it.
func (c *Cache) Site(host string) (*Site, error) {
	if s, ok := c.Lookup(host); ok {
		return s, nil
	}
	s, err := c.Discover(host)
	if err != nil {
		return nil, err
	}
	c.save(host, s)
	return s, nil
}

// Discover probes host: /w/api.php, then /api.php, then the EditURI link of the main page.
func (c *Cache) Discover(host string) (*Site, error) {
	base := strings.TrimSuffix(c.Base(host), "/")
	var errs []error
	for _, candidate := range []string{base + "/w/api.php", base + "/api.php"} {
		s, err := probe(candidate)
		if err == nil {
			s.Checked = c.Now().UTC()
			return s, nil
		}
		errs = append(errs, err)
	}
	if found, err := editURI(base + "/"); err != nil {
		errs = append(errs, err)
	} else if s, err := probe(found); err != nil {
		errs = append(errs, err)
	} else {
		s.Checked = c.Now().UTC()
		return s, nil
	}
	return nil, fmt.Errorf("could not find the MediaWiki API of %s: %w", host, errors.Join(errs...))
}

func (c *Cache) save(host string, s *Site) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	p := c.path(host)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, append(b, '\n'), 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

func probe(apiURL string) (*Site, error) {
	c, err := api.New(apiURL, nil)
	if err != nil {
		return nil, err
	}
	g, err := c.SiteInfo()
	if err != nil {
		return nil, err
	}
	if c.Resolved != "" {
		apiURL = c.Resolved
	}
	return &Site{API: apiURL, Server: g.Server, Script: g.Script, ArticlePath: g.ArticlePath, Generator: g.Generator, SiteName: g.SiteName}, nil
}

var (
	linkTag = regexp.MustCompile(`(?i)<link\b[^>]*\brel=["']?EditURI["']?[^>]*>`)
	hrefRe  = regexp.MustCompile(`(?i)\bhref=["']([^"']+)["']`)
)

// editURI reads page and returns the api.php URL of its EditURI link.
func editURI(page string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, page, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "mw/"+build.Version+" (https://github.com/zetaoss/mwcli)")
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
	u := resp.Request.URL.ResolveReference(ref)
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// PageURL returns the URL of an index.php page such as "Special:Version".
func (s *Site) PageURL(title string) string {
	server := s.Server
	if strings.HasPrefix(server, "//") {
		server = "https:" + server
	}
	if server == "" || s.Script == "" {
		return strings.TrimSuffix(s.API, "api.php") + "index.php?title=" + url.QueryEscape(title)
	}
	return server + s.Script + "?title=" + url.QueryEscape(title)
}
