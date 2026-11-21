package discovery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func siteinfo(rw http.ResponseWriter, name string) {
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{"query": map[string]any{"general": map[string]string{
		"sitename": name, "generator": "MediaWiki 1.43.1", "server": "//wiki.test", "script": "/w/index.php", "articlepath": "/wiki/$1",
	}}})
}

func newCache(t *testing.T, srv *httptest.Server, now time.Time) *Cache {
	return &Cache{Dir: t.TempDir(), Base: func(string) string { return srv.URL }, Now: func() time.Time { return now }}
}

func TestDefaultPathAndCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/w/api.php" {
			http.NotFound(rw, r)
			return
		}
		siteinfo(rw, "Test")
	}))
	defer srv.Close()
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	c := newCache(t, srv, now)

	s, err := c.Site("wiki.test")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/w/api.php", s.API)
	assert.Equal(t, "MediaWiki 1.43.1", s.Generator)
	assert.Equal(t, now, s.Checked)
	assert.Equal(t, "https://wiki.test/w/index.php?title=Special%3AVersion", s.PageURL("Special:Version"))
	assert.Equal(t, 1, calls)

	_, err = c.Site("wiki.test")
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "served from cache")

	c.Now = func() time.Time { return now.Add(TTL + time.Minute) }
	_, ok := c.Lookup("wiki.test")
	assert.False(t, ok, "stale after TTL")
	_, err = c.Site("wiki.test")
	require.NoError(t, err)
	assert.Equal(t, 2, calls)

	c.Invalidate("wiki.test")
	_, ok = c.Lookup("wiki.test")
	assert.False(t, ok)
}

func TestRootAPIPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api.php" {
			siteinfo(rw, "Root")
			return
		}
		http.NotFound(rw, r)
	}))
	defer srv.Close()
	s, err := newCache(t, srv, time.Now()).Discover("wiki.test")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/api.php", s.API)
}

func TestEditURI(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			host := strings.TrimPrefix(srv.URL, "http:")
			rw.Header().Set("Content-Type", "text/html")
			_, _ = rw.Write([]byte(`<html><head><link type="application/rsd+xml" rel="EditURI" href="` + host + `/mw/api.php?action=rsd&amp;x=1"></head></html>`))
		case "/mw/api.php":
			siteinfo(rw, "Custom")
		case "/w/api.php":
			rw.Header().Set("Content-Type", "text/html")
			_, _ = rw.Write([]byte("<html>not found</html>"))
		default:
			http.NotFound(rw, r)
		}
	}))
	defer srv.Close()
	s, err := newCache(t, srv, time.Now()).Discover("wiki.test")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/mw/api.php", s.API)
	assert.Equal(t, "Custom", s.SiteName)
}

func TestRedirectRecordsFinalAPI(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { siteinfo(rw, "Www") }))
	defer target.Close()
	bare := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, target.URL+r.URL.RequestURI(), http.StatusMovedPermanently)
	}))
	defer bare.Close()
	s, err := newCache(t, bare, time.Now()).Discover("mediawiki.test")
	require.NoError(t, err)
	assert.Equal(t, target.URL+"/w/api.php", s.API)
}

func TestNotMediaWiki(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"query":{"general":{"generator":"Other"}}}`))
	}))
	defer srv.Close()
	c := newCache(t, srv, time.Now())
	_, err := c.Site("wiki.test")
	assert.ErrorContains(t, err, "could not find the MediaWiki API of wiki.test")
	_, ok := c.Lookup("wiki.test")
	assert.False(t, ok)
}
