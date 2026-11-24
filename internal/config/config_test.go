package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
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

	c.Farms["zeta"] = []FarmSite{{Host: "wiki.example.org", API: "https://wiki.example.org/api.php", Aliases: []string{"z"}}}
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
	assert.Equal(t, []string{"z"}, c.Farms["zeta"][0].Aliases)
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

func TestSaveStoresCredentialsInKeyring(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	c, err := LoadDir(dir)
	require.NoError(t, err)
	c.Login("wiki.example.org", "Alice", &Account{Auth: AuthOAuth2, OAuthToken: "private-token"})
	require.NoError(t, c.Save())

	data, err := os.ReadFile(c.Path())
	require.NoError(t, err)
	ref := credentialRef("wiki.example.org", "Alice")
	if strings.Contains(string(data), ref) {
		assert.NotContains(t, string(data), "private-token")
	} else {
		assert.Contains(t, string(data), "private-token", "fall back to config.yml when the OS keyring is unavailable")
	}

	loaded, err := LoadDir(dir)
	require.NoError(t, err)
	_, account, ok := loaded.ActiveAccount("wiki.example.org")
	require.True(t, ok)
	assert.Equal(t, "private-token", account.OAuthToken)

	_, err = loaded.Logout("wiki.example.org", "Alice")
	require.NoError(t, err)
	require.NoError(t, loaded.Save())
	_, err = keyring.Get(keyringService, ref)
	assert.ErrorIs(t, err, keyring.ErrNotFound)
}

func TestInsecureStorageKeepsCredentialsInConfig(t *testing.T) {
	keyring.MockInitWithError(errors.New("keyring unavailable"))
	c, err := LoadDir(t.TempDir())
	require.NoError(t, err)
	c.Login("wiki.example.org", "Alice", &Account{Auth: AuthOAuth2, OAuthToken: "private-token", InsecureStorage: true})
	require.NoError(t, c.Save())

	data, err := os.ReadFile(c.Path())
	require.NoError(t, err)
	assert.Contains(t, string(data), "private-token")
	assert.NotContains(t, string(data), "credential_ref")
}
