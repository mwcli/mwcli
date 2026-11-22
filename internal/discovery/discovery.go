// Package discovery finds the api.php of a wiki: when a site is added, at a
// first login, and for a host or page URL that was not added, whose result
// the API cache keeps.
package discovery

import (
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
func Find(target string) (*config.Site, error) {
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
	u.RawQuery, u.Fragment = "", ""
	if strings.HasSuffix(u.Path, "/api.php") {
		return probe(u.String())
	}
	base := u.Scheme + "://" + u.Host
	var errs []error
	if u.Path == "" || u.Path == "/" {
		for _, candidate := range []string{base + "/w/api.php", base + "/api.php"} {
			s, err := probe(candidate)
			if err == nil {
				return s, nil
			}
			errs = append(errs, err)
		}
	}
	page := u.String()
	if u.Path == "" {
		page = base + "/"
	}
	if found, err := editURI(page); err != nil {
		errs = append(errs, err)
	} else if s, err := probe(found); err != nil {
		errs = append(errs, err)
	} else {
		return s, nil
	}
	return nil, fmt.Errorf("could not find the MediaWiki API of %s: %w", target, errors.Join(errs...))
}

func probe(apiURL string) (*config.Site, error) {
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
	return &config.Site{API: apiURL, Server: g.Server, Script: g.Script, ArticlePath: g.ArticlePath}, nil
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
	req.Header.Set("User-Agent", "mw/"+build.Version+" (https://github.com/mwcli/mwcli)")
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
