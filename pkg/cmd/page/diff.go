package page

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/aymanbagabas/go-udiff"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func newCmdDiff(f *cmdutil.Factory) *cobra.Command {
	var wiki, color string
	var web bool
	cmd := &cobra.Command{
		Use:   "diff <revision | title> [<revision | title>]",
		Short: "Show the changes between two revisions",
		Long: heredoc.Doc(`
			Show the changes between two revisions of wikitext as a unified diff.

			A number is a revision ID; anything else is a page title, meaning its latest
			revision. With one argument, the diff is from the revision before it; with
			two, from the first to the second, which may be different pages. A page's
			first revision is compared with empty text.

			--web opens the wiki's own diff page instead.
		`),
		Example: heredoc.Doc(`
			# the latest edit of a page
			$ mw page diff 'Main Page'

			# what changed between two revisions
			$ mw page diff 1001 1042

			# a revision against the current text
			$ mw page diff 1001 'Main Page'
		`),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if color != "auto" && color != "always" && color != "never" {
				return cmdutil.FlagErrorf("invalid value for --color: %q; use always, never or auto", color)
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
			newer, err := lookupRevision(cmd.Context(), c, args[len(args)-1])
			if err != nil {
				return err
			}
			var older *api.RevisionText
			switch {
			case len(args) == 2:
				if older, err = lookupRevision(cmd.Context(), c, args[0]); err != nil {
					return err
				}
			case newer.ParentID > 0:
				if older, err = c.RevisionByID(cmd.Context(), newer.ParentID); err != nil {
					return err
				}
			}
			if web {
				u := t.PageURL(fmt.Sprintf("Special:Diff/%d", newer.RevID))
				if older != nil {
					u = t.PageURL(fmt.Sprintf("Special:Diff/%d/%d", older.RevID, newer.RevID))
				}
				if ios.IsStdoutTTY() {
					fmt.Fprintf(ios.ErrOut, "Opening %s in your browser.\n", u)
				}
				return f.Browse(u)
			}
			// The texts are compared as they are, so that a change of the
			// final newline shows; a text without one is marked as diff -u does.
			oldLabel, oldText := "/dev/null", ""
			if older != nil {
				oldLabel, oldText = label(older), older.Content
			}
			out := udiff.Unified(oldLabel, label(newer), oldText, newer.Content)
			useColor := color == "always" || color == "auto" && ios.IsStdoutTTY() && os.Getenv("NO_COLOR") == ""
			if useColor {
				out = colorize(out)
			}
			fmt.Fprint(ios.Out, out)
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open the diff in the browser")
	cmd.Flags().StringVar(&color, "color", "auto", "Use color in diff output: `when` is always, never or auto")
	return cmd
}

// lookupRevision reads a revision ID, or the latest revision of a title.
func lookupRevision(ctx context.Context, c *api.Client, arg string) (*api.RevisionText, error) {
	if id, err := strconv.Atoi(arg); err == nil && id > 0 {
		return c.RevisionByID(ctx, id)
	}
	return c.LatestRevision(ctx, arg)
}

// label names a side of the diff: the title, then after a tab, as diff -u puts
// a time, the revision, its time and user.
func label(r *api.RevisionText) string {
	return fmt.Sprintf("%s\trevision %d (%s, %s)", r.Title, r.RevID, r.Timestamp, r.User)
}

func colorize(diff string) string {
	lines := strings.SplitAfter(diff, "\n")
	for i, l := range lines {
		body := strings.TrimSuffix(l, "\n")
		code := ""
		switch {
		case strings.HasPrefix(body, "---"), strings.HasPrefix(body, "+++"):
			code = "1"
		case strings.HasPrefix(body, "@@"):
			code = "36"
		case strings.HasPrefix(body, "-"):
			code = "31"
		case strings.HasPrefix(body, "+"):
			code = "32"
		}
		if code != "" && body != "" {
			lines[i] = "\x1b[" + code + "m" + body + "\x1b[0m" + l[len(body):]
		}
	}
	return strings.Join(lines, "")
}
