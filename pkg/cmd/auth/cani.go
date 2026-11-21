package auth

import (
	"errors"
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"
	"github.com/zetaoss/mwcli/api"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
)

func newCmdCanI(f *cmdutil.Factory) *cobra.Command {
	var hostname, apiURL string
	var list, quiet bool
	cmd := &cobra.Command{
		Use:   "can-i (<action> [<title>] | --list)",
		Short: "Check whether the login may perform an action",
		Long: heredoc.Doc(`
			Print yes or no and exit with status 0 or 1, like kubectl auth can-i, so that
			scripts can check before acting. A failed check exits with status 2, so it is
			never mistaken for no.

			<action> is edit, create, move, delete or protect. With a <title>, the wiki is
			asked whether the action would succeed on that page, including protection and
			blocks. Without one, only the matching user right is checked.
		`),
		Example: heredoc.Doc(`
			$ mw auth can-i edit 'Sandbox'
			$ mw auth can-i -q delete 'Sandbox' && mw ...
			$ mw auth can-i --list
		`),
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := canIRun(f, hostname, apiURL, args, list, quiet)
			var fe *cmdutil.FlagError
			var ee *cmdutil.ExitError
			if err != nil && !errors.As(err, &fe) && !errors.As(err, &ee) {
				return &cmdutil.ExitError{Code: 2, Err: err}
			}
			return err
		},
	}
	cmdutil.AddTargetFlags(cmd, &hostname, &apiURL)
	cmd.Flags().BoolVar(&list, "list", false, "List the rights of the login")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Print nothing; only set the exit status")
	return cmd
}

func canIRun(f *cmdutil.Factory, hostname, apiURL string, args []string, list, quiet bool) error {
	out := f.IOStreams.Out
	say := func(format string, a ...any) {
		if !quiet {
			fmt.Fprintf(out, format, a...)
		}
	}
	if list != (len(args) == 0) {
		return cmdutil.FlagErrorf("give an <action> or --list")
	}
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	t, err := cmdutil.Resolve(cfg, hostname, apiURL)
	if err != nil {
		return err
	}
	c, _, err := cmdutil.Client(cfg, t)
	if err != nil {
		return err
	}
	if list {
		u, err := c.User()
		if errors.Is(err, api.ErrNotLoggedIn) {
			return cmdutil.NotLoggedIn(t.Host)
		}
		if err != nil {
			return err
		}
		for _, r := range u.Rights {
			fmt.Fprintln(out, r)
		}
		return nil
	}
	right, ok := api.RightFor(args[0])
	if !ok {
		return cmdutil.FlagErrorf("unknown action %q (edit, create, move, delete, protect)", args[0])
	}
	if len(args) == 2 {
		reasons, err := c.TestAction(args[0], args[1])
		if err != nil {
			return err
		}
		if len(reasons) > 0 {
			say("no\n")
			for _, r := range reasons {
				say("  %s\n", r)
			}
			return cmdutil.ErrSilent
		}
		say("yes\n")
		return nil
	}
	u, err := c.CurrentUser()
	if err != nil {
		return err
	}
	for _, r := range u.Rights {
		if r == right {
			say("yes (has %s right; give a <title> to include protection and blocks)\n", right)
			return nil
		}
	}
	say("no (missing %s right)\n", right)
	return cmdutil.ErrSilent
}
