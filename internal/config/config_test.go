package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDir(t *testing.T) {
	t.Setenv("MW_CONFIG_DIR", "/tmp/mwconf")
	assert.Equal(t, "/tmp/mwconf", Dir())
	t.Setenv("MW_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	assert.Equal(t, "/tmp/xdg/mw", Dir())
}

func TestLoginLogoutSwitchSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	c, err := LoadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, c.Hosts)

	c.Login("zetawiki.com", DefaultAPI("zetawiki.com"), "Alice", &Account{Auth: AuthOAuth2, OAuthToken: "t1"})
	c.Login("zetawiki.com", "", "Bob", &Account{Auth: AuthSession, Cookies: map[string]string{"s": "v"}})
	c.Login("wiki.example.org", "https://wiki.example.org/api.php", "Carol", &Account{Auth: AuthOAuth2, OAuthToken: "t2"})
	require.NoError(t, c.Save())

	info, err := os.Stat(c.HostsPath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	c, err = LoadDir(dir)
	require.NoError(t, err)
	assert.Equal(t, "wiki.example.org", c.DefaultHost)
	assert.Equal(t, []string{"wiki.example.org", "zetawiki.com"}, c.HostNames())
	assert.Equal(t, "https://wiki.example.org/api.php", c.API("wiki.example.org"))
	assert.Equal(t, "https://zetawiki.com/w/api.php", c.API("zetawiki.com"))
	assert.Empty(t, c.Hosts["zetawiki.com"].API)

	user, a, ok := c.ActiveAccount("zetawiki.com")
	require.True(t, ok)
	assert.Equal(t, "Bob", user)
	assert.Equal(t, "v", a.Cookies["s"])
	assert.Equal(t, []string{"Bob", "Alice"}, c.Hosts["zetawiki.com"].UserNames())

	require.NoError(t, c.Switch("zetawiki.com", "Alice"))
	user, _, _ = c.ActiveAccount("zetawiki.com")
	assert.Equal(t, "Alice", user)
	assert.Error(t, c.Switch("zetawiki.com", "Nobody"))

	next, err := c.Logout("zetawiki.com", "Alice")
	require.NoError(t, err)
	assert.Equal(t, "Bob", next)
	_, err = c.Logout("zetawiki.com", "Bob")
	require.NoError(t, err)
	assert.NotContains(t, c.Hosts, "zetawiki.com")

	_, err = c.Logout("wiki.example.org", "Carol")
	require.NoError(t, err)
	assert.Empty(t, c.DefaultHost)
	require.NoError(t, c.Save())
	b, err := os.ReadFile(c.HostsPath())
	require.NoError(t, err)
	assert.Empty(t, b)
}

func TestMigrateLegacyProfile(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	legacy := filepath.Join(xdg, "mwtool", "profile.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0o700))
	require.NoError(t, os.WriteFile(legacy, []byte(`{"api":"https://zetawiki.com/w/api.php","username":"Jmnote","auth":"oauth2","token":"secret"}`), 0o600))

	dir := filepath.Join(xdg, "mw")
	c, err := LoadDir(dir)
	require.NoError(t, err)
	assert.Equal(t, legacy, c.Migrated)
	assert.Equal(t, "zetawiki.com", c.DefaultHost)
	user, a, ok := c.ActiveAccount("zetawiki.com")
	require.True(t, ok)
	assert.Equal(t, "Jmnote", user)
	assert.Equal(t, &Account{Auth: AuthOAuth2, OAuthToken: "secret"}, a)
	assert.NoFileExists(t, legacy)
	assert.NoDirExists(t, filepath.Dir(legacy))

	c, err = LoadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, c.Migrated)
	assert.Contains(t, c.Hosts, "zetawiki.com")
}

func TestMigrateLegacySession(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	legacy := filepath.Join(xdg, "mwtool", "profile.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0o700))
	require.NoError(t, os.WriteFile(legacy, []byte(`{"api":"https://wiki.example.org/api.php","username":"Ann","cookies":[{"Name":"sess","Value":"abc"}]}`), 0o600))

	c, err := LoadDir(filepath.Join(xdg, "mw"))
	require.NoError(t, err)
	_, a, ok := c.ActiveAccount("wiki.example.org")
	require.True(t, ok)
	assert.Equal(t, AuthSession, a.Auth)
	assert.Equal(t, map[string]string{"sess": "abc"}, a.Cookies)
	assert.Equal(t, "https://wiki.example.org/api.php", c.API("wiki.example.org"))
}
