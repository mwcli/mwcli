package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDir(t *testing.T) {
	t.Setenv("MW_CONFIG_DIR", "/tmp/mwconf")
	assert.Equal(t, "/tmp/mwconf", Dir())
	t.Setenv("MW_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	assert.Equal(t, filepath.Join("/tmp/xdg", "mw"), Dir())
}

func TestLoginLogoutSwitchSave(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, c.Logins)

	c.Sites.Wikis["zeta"] = &Site{API: "https://wiki.example.org/api.php", Aliases: []string{"z"}}
	c.Login("zeta", "Alice", &Account{Auth: AuthOAuth2, OAuthToken: "t1"})
	c.Login("zeta", "Bob", &Account{Auth: AuthSession, Cookies: map[string]string{"s": "v"}})
	c.Login("wiki.example.com", "Carol", &Account{Auth: AuthBotPassword, BotName: "Carol@b", BotPassword: "p"})
	require.NoError(t, c.Save())

	info, err := os.Stat(c.Path())
	require.NoError(t, err)
	if runtime.GOOS != "windows" { // Windows has no Unix permission bits
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	c, err = LoadDir(dir)
	require.NoError(t, err)
	assert.Equal(t, "zeta", c.Context, "the first login sets the default; later ones keep it")
	assert.Equal(t, []string{"wiki.example.com", "zeta"}, c.LoginNames())
	assert.Equal(t, []string{"zeta"}, c.SiteNames())
	assert.Equal(t, []string{"z"}, c.Sites.Wikis["zeta"].Aliases)
	user, a, ok := c.ActiveAccount("zeta")
	require.True(t, ok)
	assert.Equal(t, "Bob", user)
	assert.Equal(t, map[string]string{"s": "v"}, a.Cookies)
	assert.Equal(t, []string{"Bob", "Alice"}, c.Logins["zeta"].UserNames())

	require.NoError(t, c.Switch("zeta", "Alice"))
	_, a, _ = c.ActiveAccount("zeta")
	assert.Equal(t, "t1", a.OAuthToken)
	assert.Error(t, c.Switch("zeta", "Nobody"))

	next, err := c.Logout("zeta", "Alice")
	require.NoError(t, err)
	assert.Equal(t, "Bob", next)
	next, err = c.Logout("zeta", "Bob")
	require.NoError(t, err)
	assert.Empty(t, next)
	assert.NotContains(t, c.Logins, "zeta")
	_, err = c.Logout("zeta", "Bob")
	assert.Error(t, err)
}

func TestEmptyConfigSavesEmptyFile(t *testing.T) {
	c, err := LoadDir(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, c.Save())
	b, err := os.ReadFile(c.Path())
	require.NoError(t, err)
	assert.Empty(t, b)
}

func TestValidSiteName(t *testing.T) {
	assert.True(t, ValidSiteName("zeta"))
	assert.True(t, ValidSiteName("wiki.example.org"))
	assert.False(t, ValidSiteName(""))
	assert.False(t, ValidSiteName("farm/x"))
	assert.False(t, ValidSiteName("a b"))
}
