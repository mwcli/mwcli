package apicache

import (
	"testing"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKey(t *testing.T) {
	assert.Equal(t, "wiki.example.org", Key(" Wiki.Example.ORG "))
	assert.Equal(t, "https://wiki.example.org/wiki/Main_Page?action=view", Key("HTTPS://Wiki.Example.org/wiki/Main_Page?action=view#top"))
	assert.Equal(t, "https://wiki.example.org/", Key("https://wiki.example.org"))
}

func TestPutGetDelete(t *testing.T) {
	t.Setenv("MW_CACHE_DIR", t.TempDir())
	c := Load()
	require.NoError(t, c.Put("wiki.example.org", &config.Site{API: "https://wiki.example.org/w/api.php", Server: "//wiki.example.org"}))
	e, _, ok := Load().Get("WIKI.example.org")
	require.True(t, ok)
	assert.Equal(t, "https://wiki.example.org/w/api.php", e.Site().API)
	assert.Equal(t, "//wiki.example.org", e.Site().Server)
	assert.False(t, e.Found.IsZero())
	forgot, err := c.Delete("wiki.example.org")
	require.NoError(t, err)
	assert.True(t, forgot)
	forgot, err = c.Delete("wiki.example.org")
	require.NoError(t, err)
	assert.False(t, forgot)
	assert.Empty(t, Load().Entries)
}

// A page URL is kept by the prefix of the wiki's page URLs, so another page of
// the same wiki finds it, and a longer prefix wins.
func TestPagePrefix(t *testing.T) {
	t.Setenv("MW_CACHE_DIR", t.TempDir())
	c := Load()
	require.NoError(t, c.Put("https://wiki.example.org/wiki/Main_Page", &config.Site{API: "https://wiki.example.org/w/api.php", Server: "//wiki.example.org", ArticlePath: "/wiki/$1"}))
	require.NoError(t, c.Put("https://wiki.example.org/b/wiki/Start", &config.Site{API: "https://wiki.example.org/b/w/api.php", Server: "https://wiki.example.org", ArticlePath: "/b/wiki/$1"}))
	assert.Equal(t, []string{"https://wiki.example.org/b/wiki/", "https://wiki.example.org/wiki/"}, c.Keys())
	e, key, ok := Load().Get("https://wiki.example.org/wiki/Other")
	require.True(t, ok)
	assert.Equal(t, "https://wiki.example.org/wiki/", key)
	assert.Equal(t, "https://wiki.example.org/w/api.php", e.API)
	e, _, ok = Load().Get("https://wiki.example.org/b/wiki/Other")
	require.True(t, ok)
	assert.Equal(t, "https://wiki.example.org/b/w/api.php", e.API)
	_, _, ok = Load().Get("https://wiki.example.org/c/Other")
	assert.False(t, ok)
	// A page URL outside the article path keeps its own key.
	require.NoError(t, c.Put("https://wiki.example.org/w/index.php", &config.Site{API: "https://wiki.example.org/w/api.php", Server: "//wiki.example.org", ArticlePath: "/wiki/$1"}))
	_, key, ok = c.Get("https://wiki.example.org/w/index.php")
	assert.True(t, ok)
	assert.Equal(t, "https://wiki.example.org/w/index.php", key)
}

// A query can pick the wiki: URLs that differ only in it are kept apart, are
// not stored under a page URL prefix, and do not match one.
func TestQueryPicksWiki(t *testing.T) {
	t.Setenv("MW_CACHE_DIR", t.TempDir())
	c := Load()
	site := func(api string) *config.Site {
		return &config.Site{API: api, Server: "https://example.org", ArticlePath: "/wiki/$1"}
	}
	require.NoError(t, c.Put("https://example.org/wiki/Main?wiki=a", site("https://example.org/a/api.php")))
	_, _, ok := c.Get("https://example.org/wiki/Main?wiki=b")
	assert.False(t, ok)
	_, _, ok = c.Get("https://example.org/wiki/Other")
	assert.False(t, ok, "a query URL is not stored under the page prefix")
	require.NoError(t, c.Put("https://example.org/wiki/Main?wiki=b", site("https://example.org/b/api.php")))
	a, _, _ := Load().Get("https://example.org/wiki/Main?wiki=a")
	b, _, _ := Load().Get("https://example.org/wiki/Main?wiki=b")
	assert.Equal(t, "https://example.org/a/api.php", a.API)
	assert.Equal(t, "https://example.org/b/api.php", b.API)

	require.NoError(t, c.Put("https://example.org/wiki/Main", site("https://example.org/w/api.php")))
	_, _, ok = c.Get("https://example.org/wiki/Other?wiki=c")
	assert.False(t, ok, "a query URL does not match a page prefix")
}
