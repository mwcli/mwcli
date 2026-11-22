package cmdutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserCommand(t *testing.T) {
	const u = "https://example.org/wiki/A"
	dir := filepath.Join(t.TempDir(), "Program Files", "Browser")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	exe := filepath.Join(dir, "browser.exe")
	require.NoError(t, os.WriteFile(exe, nil, 0o755))

	cases := []struct {
		name, mw, browser, goos string
		want                    []string
	}{
		{"system linux", "", "", "linux", []string{"xdg-open", u}},
		{"system mac", "", "", "darwin", []string{"open", u}},
		{"system windows", "", "", "windows", []string{"rundll32", "url.dll,FileProtocolHandler", u}},
		{"plain", "firefox", "", "linux", []string{"firefox", u}},
		{"arguments", "firefox --new-window", "", "linux", []string{"firefox", "--new-window", u}},
		{"quoted path with spaces", `"/opt/My Browser/run" --private`, "", "linux", []string{"/opt/My Browser/run", "--private", u}},
		{"existing path with spaces", exe, "", "windows", []string{exe, u}},
		{"MW_BROWSER first", "a", "b", "linux", []string{"a", u}},
		{"BROWSER", "", "b", "linux", []string{"b", u}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, args, err := browserCommand(c.mw, c.browser, c.goos, u)
			require.NoError(t, err)
			assert.Equal(t, c.want, append([]string{name}, args...))
		})
	}

	_, _, err := browserCommand(`"unterminated`, "", "linux", u)
	assert.ErrorContains(t, err, "parse MW_BROWSER")
}
