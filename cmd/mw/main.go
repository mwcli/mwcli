// Command mw is the MediaWiki CLI.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mwcli/mwcli/pkg/cmd/root"
	"github.com/mwcli/mwcli/pkg/cmdutil"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	f := cmdutil.New()
	cmd := root.NewCmdRoot(f)
	cmd.SetArgs(args)
	c, err := cmd.ExecuteC()
	if err == nil {
		return 0
	}
	err = f.Hint(err)
	var ee *cmdutil.ExitError
	if !errors.As(err, &ee) || ee.Err != nil {
		fmt.Fprintln(f.IOStreams.ErrOut, err)
	}
	var fe *cmdutil.FlagError
	if errors.As(err, &fe) {
		fmt.Fprintf(f.IOStreams.ErrOut, "\nRun '%s --help' for usage.\n", c.CommandPath())
	}
	return cmdutil.ExitCode(err)
}
