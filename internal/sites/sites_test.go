package sites

import (
	"testing"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFarmLookupAndAPIIsolation(t *testing.T) {
	cfg := &config.Config{
		Sites: config.Sites{Wikis: map[string]*config.Site{}},
		Farms: map[string][]config.FarmSite{
			"docs": {{Host: "wiki.example.org", Aliases: []string{"handbook"}, API: "https://wiki.example.org/docs/api.php"}},
		},
	}
	r, err := Lookup(cfg, "handbook")
	require.NoError(t, err)
	assert.Equal(t, "docs", r.LoginKey)
	assert.Equal(t, "https://wiki.example.org/docs/api.php", r.Site.API)
	r, err = ForAPI(cfg, "https://wiki.example.org/docs/api.php")
	require.NoError(t, err)
	assert.Equal(t, "docs", r.LoginKey)

	r, err = ForAPI(cfg, "https://wiki.example.org/other/api.php")
	require.NoError(t, err)
	assert.Equal(t, "api:https://wiki.example.org/other/api.php", r.LoginKey)
}

func TestBuiltinFarmLoginKeyRequiresExactAPI(t *testing.T) {
	cfg := &config.Config{Sites: config.Sites{Wikis: map[string]*config.Site{}}}
	r, err := Lookup(cfg, "commons")
	require.NoError(t, err)
	assert.Equal(t, "wikimedia", r.LoginKey)

	r, err = ForAPI(cfg, "https://commons.wikimedia.org/other/api.php")
	require.NoError(t, err)
	assert.Equal(t, "api:https://commons.wikimedia.org/other/api.php", r.LoginKey)
}

func TestFarmNamesAreReserved(t *testing.T) {
	cfg := &config.Config{Farms: map[string][]config.FarmSite{"custom": nil}}
	assert.True(t, IsFarmName(cfg, "wikimedia"))
	assert.True(t, IsFarmName(cfg, "custom"))
	assert.False(t, IsFarmName(cfg, "standalone"))
}

func TestUnregisteredHostCannotSelectSameNamedFarm(t *testing.T) {
	cfg := &config.Config{Sites: config.Sites{Wikis: map[string]*config.Site{}}, Farms: map[string][]config.FarmSite{"unrelated.example.org": {{Host: "wiki.example.org"}}}}
	r, err := Lookup(cfg, "unrelated.example.org")
	require.NoError(t, err)
	assert.Equal(t, "api:https://unrelated.example.org/w/api.php", r.LoginKey)
}
