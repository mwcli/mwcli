package cmdutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitCommandWindows(t *testing.T) {
	cases := map[string][]string{
		`C:\Tools\Firefox\firefox.exe --private-window`:           {`C:\Tools\Firefox\firefox.exe`, "--private-window"},
		`"C:\Program Files\Mozilla Firefox\firefox.exe" -new-tab`: {`C:\Program Files\Mozilla Firefox\firefox.exe`, "-new-tab"},
		`browser.exe "a""b"`:   {"browser.exe", `a"b`},
		`browser.exe a\\"b c"`: {"browser.exe", `a\b c`},
		`browser.exe a\"b`:     {"browser.exe", `a"b`},
	}
	for in, want := range cases {
		got, err := splitCommand(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}
