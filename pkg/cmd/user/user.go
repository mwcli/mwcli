// Package user implements `mw user`.
package user

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdUser returns `mw user`.
func NewCmdUser(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user <command>",
		Short: "Look at wiki users",
	}
	cmd.AddCommand(newCmdContribs(f))
	return cmd
}

// contribFields are the --json fields of `mw user contribs`, named as in the
// MediaWiki API.
var contribFields = []string{
	"revid", "parentid", "pageid", "ns", "title", "timestamp", "user", "comment",
	"size", "sizediff", "minor", "new", "top", "tags",
}

// maxComment is the most display cells of an edit summary shown on a terminal.
const maxComment = 50

func newCmdContribs(f *cmdutil.Factory) *cobra.Command {
	var wiki string
	var namespace, limit int
	var web bool
	var jsonFlags *cmdutil.JSONFlags
	cmd := &cobra.Command{
		Use:   "contribs <user>",
		Short: "List the edits of a user",
		Long: heredoc.Doc(`
			List the edits of a user or an IP address, newest first: time (UTC), page,
			change in bytes, marks and edit summary. The marks are N for a new page, m for
			a minor edit and * for the latest revision of its page. On a terminal the
			columns are aligned and long summaries shortened; otherwise each line is the
			revision ID, ISO 8601 time, title, change in bytes, marks and summary,
			separated by tabs. Needs no login.

			--web opens the user's contributions page in the browser.
		`),
		Example: heredoc.Doc(`
			$ mw user contribs Example
			$ mw user contribs Example --namespace 0 -L 100
			$ mw user contribs Example --json title --jq '.[].title' | sort -u
			$ mw user contribs Example --web
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jsonFlags.Validate(); err != nil {
				return err
			}
			if web && jsonFlags.Enabled() {
				return cmdutil.FlagErrorf("specify only one of --web and --json")
			}
			if limit < 1 {
				return cmdutil.FlagErrorf("invalid value for --limit: %d", limit)
			}
			name := strings.TrimSpace(args[0])
			if name == "" || strings.Contains(name, "|") {
				return cmdutil.FlagErrorf("invalid user name %q", args[0])
			}
			ios := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			t, err := f.Resolve(cmd.Context(), cfg, wiki)
			if err != nil {
				return err
			}
			if web {
				u := t.PageURL("Special:Contributions/" + name)
				if ios.IsStdoutTTY() {
					fmt.Fprintf(ios.ErrOut, "Opening %s in your browser.\n", u)
				}
				return f.Browse(u)
			}
			c, _, err := cmdutil.Client(cmd.Context(), cfg, t)
			if err != nil {
				return err
			}
			opts := api.ContribOptions{User: name, Limit: limit}
			if cmd.Flags().Changed("namespace") {
				opts.Namespace = &namespace
			}
			contribs, err := c.Contributions(cmd.Context(), opts)
			if err != nil {
				return f.Hint(err)
			}
			if jsonFlags.Enabled() {
				return jsonFlags.Write(ios.Out, contribs, ios.IsStdoutTTY())
			}
			if len(contribs) == 0 {
				if ios.IsStdoutTTY() {
					fmt.Fprintln(ios.ErrOut, "No contributions found")
				}
				return nil
			}
			if !ios.IsStdoutTTY() {
				for _, e := range contribs {
					fmt.Fprintf(ios.Out, "%d\t%s\t%s\t%s\t%s\t%s\n", e.RevID, e.Timestamp, cmdutil.TSVField(e.Title), signed(e.SizeDiff), marks(e), cmdutil.TSVField(e.Comment))
				}
				return nil
			}
			rows := make([][]string, 0, len(contribs))
			for _, e := range contribs {
				rows = append(rows, []string{cmdutil.ShortTime(e.Timestamp), e.Title, signed(e.SizeDiff), marks(e), cmdutil.Truncate(cmdutil.OneLine(e.Comment), maxComment)})
			}
			return cmdutil.WriteTable(ios.Out, rows)
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().IntVarP(&namespace, "namespace", "n", 0, "Limit the list to one namespace `ID`")
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of edits to list")
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open the contributions page in the browser")
	jsonFlags = cmdutil.AddJSONFlags(cmd, contribFields)
	return cmd
}

// signed shows a change in bytes with its sign, as +240, -3 or 0.
func signed(n int) string {
	if n > 0 {
		return fmt.Sprintf("+%d", n)
	}
	return fmt.Sprint(n)
}

// marks are N for a new page, m for a minor edit and * for the latest revision.
func marks(e api.Contribution) string {
	var b strings.Builder
	if e.New {
		b.WriteString("N")
	}
	if e.Minor {
		b.WriteString("m")
	}
	if e.Top {
		b.WriteString("*")
	}
	return b.String()
}
