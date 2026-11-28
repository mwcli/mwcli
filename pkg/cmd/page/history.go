package page

import (
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// historyFields are the --json fields of `mw page history`, named as in the
// MediaWiki API.
var historyFields = []string{"revid", "parentid", "timestamp", "user", "comment", "size", "minor", "tags"}

// maxComment is the most display cells of an edit summary shown on a terminal.
const maxComment = 60

func newCmdHistory(f *cmdutil.Factory) *cobra.Command {
	var wiki string
	var limit int
	var followRedirects bool
	var jsonFlags *cmdutil.JSONFlags
	cmd := &cobra.Command{
		Use:   "history <title>",
		Short: "List the revisions of a page",
		Long: heredoc.Doc(`
			List the revisions of a page, newest first: revision ID, time (UTC), user,
			size in bytes and edit summary. A minor edit is marked m. On a terminal the
			columns are aligned and long summaries shortened; otherwise they are
			separated by tabs, with the time in ISO 8601. --follow-redirects resolves
			redirects to their target page.
		`),
		Example: heredoc.Doc(`
			$ mw page history 'Main Page'
			$ mw page history 'Main Page' -L 100 --json revid,user,timestamp
			$ mw page history 'Main Page' --json revid --jq '.[0].revid'
			$ mw page history 'Alias' --follow-redirects
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jsonFlags.Validate(); err != nil {
				return err
			}
			if limit < 1 {
				return cmdutil.FlagErrorf("invalid value for --limit: %d", limit)
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
			c, _, err := cmdutil.Client(cmd.Context(), cfg, t)
			if err != nil {
				return err
			}
			revs, err := c.History(cmd.Context(), args[0], api.HistoryOptions{Limit: limit, FollowRedirects: followRedirects})
			if err != nil {
				return err
			}
			if jsonFlags.Enabled() {
				return jsonFlags.Write(ios.Out, revs, ios.IsStdoutTTY())
			}
			if len(revs) == 0 {
				fmt.Fprintln(ios.ErrOut, "No revisions found")
				return nil
			}
			if !ios.IsStdoutTTY() {
				for _, r := range revs {
					fmt.Fprintf(ios.Out, "%d\t%s\t%s\t%d\t%s\t%s\n", r.RevID, r.Timestamp, r.User, r.Size, minorMark(r), cmdutil.TSVField(r.Comment))
				}
				return nil
			}
			rows := make([][]string, 0, len(revs))
			for _, r := range revs {
				rows = append(rows, []string{fmt.Sprint(r.RevID), cmdutil.ShortTime(r.Timestamp), r.User, fmt.Sprint(r.Size), minorMark(r), cmdutil.Truncate(cmdutil.OneLine(r.Comment), maxComment)})
			}
			return cmdutil.WriteTable(ios.Out, rows)
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of revisions to list")
	cmd.Flags().BoolVar(&followRedirects, "follow-redirects", false, "Follow redirects to the target page")
	jsonFlags = cmdutil.AddJSONFlags(cmd, historyFields)
	return cmd
}

func minorMark(r api.HistoryEntry) string {
	if r.Minor {
		return "m"
	}
	return ""
}
