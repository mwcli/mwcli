package page

import (
	"errors"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func newCmdMove(f *cmdutil.Factory) *cobra.Command {
	var wiki, reason string
	var opts api.MoveOptions
	cmd := &cobra.Command{
		Use:   "move <title> <new-title>",
		Short: "Rename a page",
		Long: heredoc.Doc(`
			Rename a page, with its history, and leave a redirect at the old title.
			--move-talk moves its talk page too and --move-subpages its subpages, as far
			as the wiki allows. --no-redirect leaves no redirect, which needs the right to
			suppress redirects. Moving needs the move right, such as the
			createeditmovepage grant of a token.
		`),
		Example: heredoc.Doc(`
			$ mw page move 'Sandbox/Old' 'Sandbox/New' --reason 'Better name'
			$ mw page move 'Draft:Plan' 'Plan' --move-talk --move-subpages
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			from, to := args[0], args[1]
			if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
				return cmdutil.FlagErrorf("empty title")
			}
			ios := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			t, err := f.Resolve(ctx, cfg, wiki)
			if err != nil {
				return err
			}
			c, ok, err := cmdutil.Client(cfg, t)
			if err != nil {
				return err
			}
			if !ok {
				return cmdutil.NotLoggedIn(t.Name)
			}
			opts.Reason = reason
			res, err := c.Move(ctx, from, to, opts)
			if err := cmdutil.LoginError(err, t); err != nil {
				return err
			}
			switch {
			case errors.Is(err, api.ErrPageExists):
				return fmt.Errorf("%w on %s; choose another title, or move or delete that page first", err, t.Name)
			case err != nil:
				return fmt.Errorf("%w on %s", err, t.Name)
			}
			fmt.Fprintf(ios.ErrOut, "%s Moved %s to %s on %s\n", ios.SuccessIcon(), res.From, res.To, t.Name)
			if res.TalkTo != "" {
				fmt.Fprintf(ios.ErrOut, "  Moved %s to %s\n", res.TalkFrom, res.TalkTo)
			}
			for _, s := range res.Subpages {
				fmt.Fprintf(ios.ErrOut, "  Moved %s to %s\n", s[0], s[1])
			}
			if !res.RedirectCreated {
				fmt.Fprintf(ios.ErrOut, "  Left no redirect at %s\n", res.From)
			}
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().StringVar(&reason, "reason", "", "The reason for the move, shown in the log and history")
	cmd.Flags().BoolVar(&opts.MoveTalk, "move-talk", false, "Move the talk page too")
	cmd.Flags().BoolVar(&opts.MoveSubpages, "move-subpages", false, "Move the subpages too")
	cmd.Flags().BoolVar(&opts.NoRedirect, "no-redirect", false, "Leave no redirect at the old title")
	return cmd
}

func newCmdDelete(f *cmdutil.Factory) *cobra.Command {
	var wiki, reason string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <title>",
		Short: "Delete a page",
		Long: heredoc.Doc(`
			Delete a page with all its revisions. Administrators can undelete it.
			Deleting needs the delete right, such as the delete grant of a token.

			On a terminal mw asks to type the title to confirm; otherwise --yes is
			needed.
		`),
		Example: heredoc.Doc(`
			$ mw page delete 'Sandbox/Old' --reason 'No longer needed'
			$ mw page delete 'Sandbox/Old' --yes
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := args[0]
			if strings.TrimSpace(title) == "" {
				return cmdutil.FlagErrorf("empty title")
			}
			ios := f.IOStreams
			if !yes && !ios.CanPrompt() {
				return cmdutil.FlagErrorf("--yes required when not running interactively")
			}
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			t, err := f.Resolve(ctx, cfg, wiki)
			if err != nil {
				return err
			}
			c, ok, err := cmdutil.Client(cfg, t)
			if err != nil {
				return err
			}
			if !ok {
				return cmdutil.NotLoggedIn(t.Name)
			}
			if !yes {
				answer, err := ios.Prompt(fmt.Sprintf("Type %s to delete it from %s: ", title, t.Name))
				if err != nil {
					return err
				}
				if answer != strings.TrimSpace(title) {
					return &cmdutil.ExitError{Code: 1, Err: errors.New("the title did not match; nothing was deleted")}
				}
			}
			deleted, err := c.Delete(ctx, title, reason)
			if err := cmdutil.LoginError(err, t); err != nil {
				return err
			}
			if err != nil {
				return fmt.Errorf("%w on %s", err, t.Name)
			}
			fmt.Fprintf(ios.ErrOut, "%s Deleted %s on %s\n", ios.SuccessIcon(), deleted, t.Name)
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().StringVar(&reason, "reason", "", "The reason for the deletion, shown in the log")
	cmd.Flags().BoolVar(&yes, "yes", false, "Delete without asking for confirmation")
	return cmd
}
