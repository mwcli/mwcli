//go:build !windows

package cmdutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitCommandPOSIX(t *testing.T) {
	cases := map[string][]string{
		`firefox --new-window`:            {"firefox", "--new-window"},
		`"/opt/My Browser/run" --private`: {"/opt/My Browser/run", "--private"},
		`'/opt/a b/run'`:                  {"/opt/a b/run"},
	}
	for in, want := range cases {
		got, err := splitCommand(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, err := splitCommand(`"unterminated`)
	assert.Error(t, err)
}
