package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func siteinfo(rw http.ResponseWriter, name string) {
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{"query": map[string]any{"general": map[string]string{
		"sitename": name, "generator": "MediaWiki 1.43.1", "server": "//wiki.test", "script": "/w/index.php", "articlepath": "/wiki/$1",
	}}})
}

func TestDefaultPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/w/api.php" {
			http.NotFound(rw, r)
			return
		}
		siteinfo(rw, "Test")
	}))
	defer srv.Close()
	s, err := Find(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/w/api.php", s.API)
	assert.Equal(t, "//wiki.test", s.Server)
	assert.Equal(t, "/w/index.php", s.Script)
	assert.Equal(t, "/wiki/$1", s.ArticlePath)
	assert.Equal(t, "https://wiki.test/w/index.php?title=Special%3AVersion", s.PageURL("Special:Version"))
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
	s, err := Find(context.Background(), srv.URL+"/")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/api.php", s.API)
}

// A page URL finds the API through its EditURI link, so wikis under different
// paths of one host are told apart.
func TestEditURI(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/b/index.php":
			dir := map[string]string{"/": "/mw", "/b/index.php": "/b"}[r.URL.Path]
			host := strings.TrimPrefix(srv.URL, "http:")
			rw.Header().Set("Content-Type", "text/html")
			_, _ = rw.Write([]byte(`<html><head><link type="application/rsd+xml" rel="EditURI" href="` + host + dir + `/api.php?action=rsd&amp;x=1"></head></html>`))
		case "/mw/api.php", "/b/api.php":
			siteinfo(rw, "Custom")
		case "/w/api.php":
			rw.Header().Set("Content-Type", "text/html")
			_, _ = rw.Write([]byte("<html>not found</html>"))
		default:
			http.NotFound(rw, r)
		}
	}))
	defer srv.Close()
	s, err := Find(context.Background(), srv.URL)
	require.NoError(t, err)
	// action=rsd is dropped; another parameter of the link belongs to the API URL.
	assert.Equal(t, srv.URL+"/mw/api.php?x=1", s.API)

	s, err = Find(context.Background(), srv.URL+"/b/index.php?title=Main_Page")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/b/api.php?x=1", s.API)
}

func TestAPIURLIsOnlyChecked(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		siteinfo(rw, "Direct")
	}))
	defer srv.Close()
	s, err := Find(context.Background(), srv.URL+"/x/api.php")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/x/api.php", s.API)
	assert.Equal(t, []string{"/x/api.php"}, paths)
}

func TestRedirectRecordsFinalAPI(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { siteinfo(rw, "Www") }))
	defer target.Close()
	bare := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, target.URL+r.URL.RequestURI(), http.StatusMovedPermanently)
	}))
	defer bare.Close()
	s, err := Find(context.Background(), bare.URL)
	require.NoError(t, err)
	assert.Equal(t, target.URL+"/w/api.php", s.API)
}

func TestNotMediaWiki(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"query":{"general":{"generator":"Other"}}}`))
	}))
	defer srv.Close()
	_, err := Find(context.Background(), srv.URL)
	assert.ErrorContains(t, err, "could not find the MediaWiki API of "+srv.URL)
}

func TestHostDefaultsToHTTPS(t *testing.T) {
	_, err := Find(context.Background(), "wiki.invalid")
	assert.ErrorContains(t, err, "https://wiki.invalid")
}

func TestEscapeQuery(t *testing.T) {
	assert.Equal(t, "title=%EB%8C%80%EB%AC%B8&wiki=a", escapeQuery("title=대문&wiki=a"))
	assert.Equal(t, "title=A%20B&x=%EB%8C%80", escapeQuery("title=A B&x=%EB%8C%80"), "escapes stay as they are")
	assert.Equal(t, "", escapeQuery(""))
}

// A page URL with a query copied unescaped from a browser is read with the
// query escaped, so a server that refuses raw non-ASCII bytes still answers.
func TestFindUnescapedQuery(t *testing.T) {
	var raw string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/w/index.php":
			raw = r.URL.RawQuery
			for _, c := range []byte(r.URL.RawQuery) {
				if c >= 0x80 {
					http.Error(rw, "bad request", http.StatusBadRequest)
					return
				}
			}
			rw.Header().Set("Content-Type", "text/html")
			_, _ = rw.Write([]byte(`<link rel="EditURI" type="application/rsd+xml" href="` + srv.URL + `/w/api.php?action=rsd">`))
		case "/w/api.php":
			siteinfo(rw, "Fake")
		default:
			http.NotFound(rw, r)
		}
	}))
	defer srv.Close()
	s, err := Find(context.Background(), srv.URL+"/w/index.php?title=대문")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/w/api.php", s.API)
	assert.Equal(t, "title=%EB%8C%80%EB%AC%B8", raw)
}
