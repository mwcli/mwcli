package page

import (
	"fmt"
	"strings"
	"time"

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
	var jsonFlags *cmdutil.JSONFlags
	cmd := &cobra.Command{
		Use:   "history <title>",
		Short: "List the revisions of a page",
		Long: heredoc.Doc(`
			List the revisions of a page, newest first: revision ID, time (UTC), user,
			size in bytes and edit summary. A minor edit is marked m. On a terminal the
			columns are aligned and long summaries shortened; otherwise they are
			separated by tabs, with the time in ISO 8601.
		`),
		Example: heredoc.Doc(`
			$ mw page history 'Main Page'
			$ mw page history 'Main Page' -L 100 --json revid,user,timestamp
			$ mw page history 'Main Page' --json revid --jq '.[0].revid'
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
			t, err := f.Resolve(cfg, wiki)
			if err != nil {
				return err
			}
			c, _, err := cmdutil.Client(cfg, t)
			if err != nil {
				return err
			}
			revs, err := c.History(args[0], limit)
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
					fmt.Fprintf(ios.Out, "%d\t%s\t%s\t%d\t%s\t%s\n", r.RevID, r.Timestamp, r.User, r.Size, minorMark(r), tsvField(r.Comment))
				}
				return nil
			}
			rows := make([][]string, 0, len(revs))
			for _, r := range revs {
				rows = append(rows, []string{fmt.Sprint(r.RevID), shortTime(r.Timestamp), r.User, fmt.Sprint(r.Size), minorMark(r), cmdutil.Truncate(oneLine(r.Comment), maxComment)})
			}
			return cmdutil.WriteTable(ios.Out, rows)
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of revisions to list")
	jsonFlags = cmdutil.AddJSONFlags(cmd, historyFields)
	return cmd
}

func minorMark(r api.HistoryEntry) string {
	if r.Minor {
		return "m"
	}
	return ""
}

// shortTime shows an API timestamp to the minute, as 2026-11-22 08:41.
func shortTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// oneLine folds all white space into single spaces, for a terminal.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// tsvReplacer turns each line break or tab, which would split a line or a
// column, into one space and leaves the rest of the text as it is.
var tsvReplacer = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

func tsvField(s string) string { return tsvReplacer.Replace(s) }
