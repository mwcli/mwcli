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
	t.Setenv("XDG_CONFIG_HOME", dir)
	c, err := LoadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, c.Hosts)

	c.Login("wiki.example.com", DefaultAPI("wiki.example.com"), "Alice", &Account{Auth: AuthOAuth2, OAuthToken: "t1"})
	c.Login("wiki.example.com", "", "Bob", &Account{Auth: AuthSession, Cookies: map[string]string{"s": "v"}})
	c.Login("wiki.example.org", "https://wiki.example.org/api.php", "Carol", &Account{Auth: AuthOAuth2, OAuthToken: "t2"})
	require.NoError(t, c.Save())

	info, err := os.Stat(c.HostsPath())
	require.NoError(t, err)
	if runtime.GOOS != "windows" { // Windows has no Unix permission bits
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	c, err = LoadDir(dir)
	require.NoError(t, err)
	assert.Equal(t, "wiki.example.org", c.DefaultHost)
	assert.Equal(t, []string{"wiki.example.com", "wiki.example.org"}, c.HostNames())
	assert.Equal(t, "https://wiki.example.org/api.php", c.API("wiki.example.org"))
	assert.Equal(t, "https://wiki.example.com/w/api.php", c.API("wiki.example.com"))
	assert.Empty(t, c.Hosts["wiki.example.com"].API)

	user, a, ok := c.ActiveAccount("wiki.example.com")
	require.True(t, ok)
	assert.Equal(t, "Bob", user)
	assert.Equal(t, "v", a.Cookies["s"])
	assert.Equal(t, []string{"Bob", "Alice"}, c.Hosts["wiki.example.com"].UserNames())

	require.NoError(t, c.Switch("wiki.example.com", "Alice"))
	user, _, _ = c.ActiveAccount("wiki.example.com")
	assert.Equal(t, "Alice", user)
	assert.Error(t, c.Switch("wiki.example.com", "Nobody"))

	next, err := c.Logout("wiki.example.com", "Alice")
	require.NoError(t, err)
	assert.Equal(t, "Bob", next)
	_, err = c.Logout("wiki.example.com", "Bob")
	require.NoError(t, err)
	assert.NotContains(t, c.Hosts, "wiki.example.com")

	_, err = c.Logout("wiki.example.org", "Carol")
	require.NoError(t, err)
	assert.Empty(t, c.DefaultHost)
	require.NoError(t, c.Save())
	b, err := os.ReadFile(c.HostsPath())
	require.NoError(t, err)
	assert.Empty(t, b)
}
