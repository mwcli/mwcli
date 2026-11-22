// Command mw is the MediaWiki CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/mwcli/mwcli/pkg/cmd/root"
	"github.com/mwcli/mwcli/pkg/cmdutil"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	// Ctrl-C cancels the requests in flight rather than leaving them to time out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	f := cmdutil.New()
	cmd := root.NewCmdRoot(f)
	cmd.SetArgs(args)
	c, err := cmd.ExecuteContextC(ctx)
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		// 128 + SIGINT, as a shell reports a command stopped by Ctrl-C.
		return 130
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
