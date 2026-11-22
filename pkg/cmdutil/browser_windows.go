package cmdutil

import "golang.org/x/sys/windows"

// splitCommand splits a command line with the Windows rules (CommandLineToArgvW),
// which keep the backslashes of paths.
func splitCommand(s string) ([]string, error) {
	return windows.DecomposeCommandLine(s)
}
