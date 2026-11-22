//go:build !windows

package cmdutil

import "github.com/kballard/go-shellquote"

// splitCommand splits a command line with POSIX shell rules.
func splitCommand(s string) ([]string, error) {
	return shellquote.Split(s)
}
