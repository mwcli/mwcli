package sites

import (
	"testing"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatternMatches(t *testing.T) {
	assert.True(t, matches("*.wikipedia.org", "de.wikipedia.org"))
	assert.True(t, matches("*.wikipedia.org", "DE.Wikipedia.org"))
	assert.False(t, matches("*.wikipedia.org", "wikipedia.org"))
	assert.False(t, matches("*.wikipedia.org", "de.m.wikipedia.org"), "one label only")
	assert.False(t, matches("*.wikipedia.org", "evilwikipedia.org"))
	assert.False(t, matches("*.wikipedia.org", "de.wikipedia.org.example.com"))

	assert.NoError(t, CheckPattern("*.wikipedia.org"))
	for _, bad := range []string{"*.org", "*", "wiki.*.org", "*.*.org", "a*.example.org"} {
		assert.Error(t, CheckPattern(bad), bad)
	}
}

// A language edition that is not listed is a Wikimedia wiki through the
// pattern and shares the wikimedia login; hosts outside the pattern, and
// other APIs on a pattern host, do not.
func TestBuiltinPatterns(t *testing.T) {
	cfg := &config.Config{}
	r, err := Lookup(cfg, "de.wikipedia.org")
	require.NoError(t, err)
	assert.Equal(t, Resolved{Name: "de.wikipedia.org", LoginKey: "wikimedia", Site: &config.Site{API: "https://de.wikipedia.org/w/api.php"}, Kind: BuiltIn}, r)

	r, err = ForAPI(cfg, "https://fr.wiktionary.org/w/api.php")
	require.NoError(t, err)
	assert.Equal(t, "wikimedia", r.LoginKey)

	for _, api := range []string{"https://fr.wiktionary.org/x/api.php", "http://fr.wiktionary.org/w/api.php"} {
		r, err = ForAPI(cfg, api)
		require.NoError(t, err)
		assert.Equal(t, Unregistered, r.Kind, api)
		assert.NotEqual(t, "wikimedia", r.LoginKey, api)
	}

	r, err = Lookup(cfg, "de.m.wikipedia.org")
	require.NoError(t, err)
	assert.Equal(t, Unregistered, r.Kind)

	// A wiki named exactly keeps its name and aliases.
	r, err = Lookup(cfg, "wikipedia")
	require.NoError(t, err)
	assert.Equal(t, "https://en.wikipedia.org/w/api.php", r.Site.API)
	r, err = Lookup(cfg, "enwiki")
	require.NoError(t, err)
	assert.Equal(t, "https://en.wikipedia.org/w/api.php", r.Site.API)
	assert.Equal(t, "wikimedia", r.LoginKey)
	_, ok := Builtin()["*.wikipedia.org"]
	assert.False(t, ok)
}

func TestUserPatterns(t *testing.T) {
	cfg := &config.Config{Dir: t.TempDir(), Farms: map[string][]config.FarmSite{
		"acme":   {{Host: "wiki.acme.example", Aliases: []string{"acme"}}, {Host: "*.acme.example", API: "https://{host}/api.php"}},
		"mywiki": {{Host: "*.wikipedia.org", API: "https://{host}/w/api.php"}},
	}}
	r, err := Lookup(cfg, "docs.acme.example")
	require.NoError(t, err)
	assert.Equal(t, Resolved{Name: "docs.acme.example", LoginKey: "acme", Site: &config.Site{API: "https://docs.acme.example/api.php"}, Kind: Added}, r)
	r, err = ForAPI(cfg, "https://docs.acme.example/api.php")
	require.NoError(t, err)
	assert.Equal(t, "acme", r.LoginKey)
	r, err = Lookup(cfg, "acme")
	require.NoError(t, err)
	assert.Equal(t, "https://wiki.acme.example/w/api.php", r.Site.API)
	assert.Equal(t, "https://wiki.acme.example/w/api.php", FarmAPI(cfg, "acme"), "a farm's API is that of a wiki named exactly")

	// The user's pattern comes before the built-in one.
	r, err = Lookup(cfg, "de.wikipedia.org")
	require.NoError(t, err)
	assert.Equal(t, "mywiki", r.LoginKey)

	// By API URL, the pattern whose API it is wins, though another comes first.
	cfg.Farms["mywiki"] = []config.FarmSite{{Host: "*.wikipedia.org", API: "https://{host}/api.php"}}
	r, err = ForAPI(cfg, "https://de.wikipedia.org/w/api.php")
	require.NoError(t, err)
	assert.Equal(t, "wikimedia", r.LoginKey)
	assert.Equal(t, BuiltIn, r.Kind)
	r, err = ForAPI(cfg, "https://de.wikipedia.org/api.php")
	require.NoError(t, err)
	assert.Equal(t, "mywiki", r.LoginKey)

	cfg.Farms["wide"] = []config.FarmSite{{Host: "*.example"}}
	_, err = Lookup(cfg, "x.example")
	assert.ErrorContains(t, err, `farm wide in `)
	assert.ErrorContains(t, err, `host pattern "*.example"`)
}
