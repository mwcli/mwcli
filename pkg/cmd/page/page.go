// Package page implements `mw page`.
package page

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/MakeNowJust/heredoc"
	"github.com/aymanbagabas/go-udiff"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/spf13/cobra"
)

// NewCmdPage returns `mw page`.
func NewCmdPage(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "page <command>",
		Short: "Read, list, download, create and edit wiki pages and their history",
	}
	cmd.AddCommand(newCmdView(f), newCmdList(f), newCmdHistory(f), newCmdDiff(f), newCmdCreate(f), newCmdEdit(f), newCmdDownload(f))
	return cmd
}

// viewFields are the --json fields of `mw page view`, named as in the MediaWiki API.
var viewFields = []string{
	"title", "pageid", "ns", "url", "contentmodel", "length", "touched", "redirect",
	"protection", "categories", "revision", "content",
}

func newCmdView(f *cmdutil.Factory) *cobra.Command {
	var wiki string
	var web bool
	var jsonFlags *cmdutil.JSONFlags
	cmd := &cobra.Command{
		Use:   "view <title>",
		Short: "Print the wikitext of a page",
		Long: heredoc.Doc(`
			Print the wikitext of the latest revision of a page. Public pages need no login.

			With --json, print the named fields instead; --jq filters them. --web opens
			the page in the browser.
		`),
		Example: heredoc.Doc(`
			$ mw page view 'Main Page'
			$ mw page view 'Main Page' --json pageid,url,revision
			$ mw page view 'Main Page' --json categories --jq '.categories[]'
			$ mw page view 'Main Page' --web
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jsonFlags.Validate(); err != nil {
				return err
			}
			if web && jsonFlags.Enabled() {
				return cmdutil.FlagErrorf("specify only one of --web and --json")
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
				u := t.PageURL(args[0])
				if ios.IsStdoutTTY() {
					fmt.Fprintf(ios.ErrOut, "Opening %s in your browser.\n", u)
				}
				return f.Browse(u)
			}
			c, _, err := cmdutil.Client(cfg, t)
			if err != nil {
				return err
			}
			if jsonFlags.Enabled() {
				info, err := c.PageInfo(cmd.Context(), args[0], api.PageInfoOptions{Content: jsonFlags.Has("content"), Categories: jsonFlags.Has("categories")})
				if err != nil {
					return err
				}
				return jsonFlags.Write(ios.Out, info, ios.IsStdoutTTY())
			}
			text, err := c.Page(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(ios.Out, text)
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open the page in the browser")
	jsonFlags = cmdutil.AddJSONFlags(cmd, viewFields)
	return cmd
}

// editFlags are the flags `mw page edit` and `mw page create` share.
type editFlags struct {
	wiki, summary, text, file string
	minor, bot, dryRun        bool
	baseRevID                 int
}

func (o *editFlags) add(cmd *cobra.Command) {
	cmdutil.AddTargetFlags(cmd, &o.wiki)
	cmd.Flags().StringVarP(&o.summary, "summary", "s", "", "Edit summary `text`")
	cmd.Flags().StringVar(&o.text, "text", "", "New `wikitext`")
	cmd.Flags().StringVarP(&o.file, "file", "F", "", "Read the new wikitext from `file` (use \"-\" to read from standard input)")
	cmd.Flags().BoolVar(&o.minor, "minor", false, "Mark the edit as minor")
	cmd.Flags().BoolVar(&o.bot, "bot", false, "Mark the edit as a bot edit, when the account has the bot right")
	cmd.Flags().BoolVar(&o.dryRun, "dry-run", false, "Print the changes as a diff without saving")
}

func newCmdEdit(f *cmdutil.Factory) *cobra.Command {
	o := &editFlags{}
	cmd := &cobra.Command{
		Use:   "edit <title>",
		Short: "Replace the wikitext of a page",
		Long: heredoc.Doc(`
			Replace the wikitext of an existing page. To make a new page, use
			'mw page create'.

			--dry-run prints the changes as a diff, with the text as the wiki would
			save it (signatures, substitutions, trailing white space), and saves
			nothing; it needs no login.

			--baserevid names the revision the new text was made from, such as the
			revid of 'mw page view --json revision'. When others edited the page
			since, the wiki merges their changes as the browser editor does, or the
			edit fails with an edit conflict rather than undo them. Later edits by
			the same account do not count as a conflict.
		`),
		Example: heredoc.Doc(`
			$ mw page edit 'Sandbox' --summary 'Fix typo' --file page.wiki
			$ mw page view 'Sandbox' | sed 's/teh/the/' | mw page edit 'Sandbox' -F - -s 'Fix typo' --minor

			# keep the edit from overwriting changes made meanwhile
			$ rev=$(mw page view 'Sandbox' --json revision --jq .revision.revid)
			$ mw page view 'Sandbox' > sandbox.wiki
			$ mw page edit 'Sandbox' -F sandbox.wiki --baserevid "$rev"
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("baserevid") && o.baseRevID <= 0 {
				return cmdutil.FlagErrorf("--baserevid must be a revision ID greater than 0")
			}
			return saveRun(f, cmd, o, args[0], api.EditOnly)
		},
	}
	o.add(cmd)
	cmd.Flags().IntVar(&o.baseRevID, "baserevid", 0, "Fail with an edit conflict, rather than undo them, when others changed the page since revision `ID`")
	return cmd
}

func newCmdCreate(f *cmdutil.Factory) *cobra.Command {
	o := &editFlags{}
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a page",
		Long: heredoc.Doc(`
			Create a page with the given wikitext. It fails when the page exists; to
			change one, use 'mw page edit'. Creating pages needs the right to create
			them, such as the createeditmovepage grant of a token. --dry-run prints
			the new text as the wiki would save it, as a diff, and saves nothing.
		`),
		Example: heredoc.Doc(`
			$ mw page create 'Sandbox/Test' --text 'Hello' --summary 'New test page'
			$ mw page create 'Sandbox/Test' -F page.wiki
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return saveRun(f, cmd, o, args[0], api.CreateOnly)
		},
	}
	o.add(cmd)
	return cmd
}

func saveRun(f *cmdutil.Factory, cmd *cobra.Command, o *editFlags, title string, mode api.EditMode) error {
	ios := f.IOStreams
	text := o.text
	textSet, fileSet := cmd.Flags().Changed("text"), cmd.Flags().Changed("file")
	if textSet == fileSet {
		return cmdutil.FlagErrorf("specify exactly one of --text and --file")
	}
	if fileSet {
		var b []byte
		var err error
		if o.file == "-" {
			b, err = io.ReadAll(ios.In)
		} else {
			b, err = os.ReadFile(o.file)
		}
		if err != nil {
			return err
		}
		text = string(b)
	}
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	t, err := f.Resolve(ctx, cfg, o.wiki)
	if err != nil {
		return err
	}
	c, ok, err := cmdutil.Client(cfg, t)
	if err != nil {
		return err
	}
	if o.dryRun {
		return dryRun(ctx, ios, c, t, title, text, mode, o.baseRevID)
	}
	if !ok {
		return cmdutil.NotLoggedIn(t.Name)
	}
	res, err := c.Edit(ctx, title, text, api.EditOptions{Summary: o.summary, Mode: mode, Minor: o.minor, Bot: o.bot, BaseRevID: o.baseRevID})
	var ae *api.APIError
	if errors.As(err, &ae) && ae.Code == "assertuserfailed" {
		return cmdutil.NotLoggedIn(t.Name)
	}
	if err != nil {
		return saveError(err, t)
	}
	switch {
	case res.NoChange:
		fmt.Fprintf(ios.ErrOut, "%s No changes to %s on %s\n", ios.WarningIcon(), res.Title, t.Name)
	case res.New:
		fmt.Fprintf(ios.ErrOut, "%s Created %s on %s\n", ios.SuccessIcon(), res.Title, t.Name)
	default:
		fmt.Fprintf(ios.ErrOut, "%s Updated %s on %s\n", ios.SuccessIcon(), res.Title, t.Name)
	}
	return nil
}

// saveError adds to a refusal of edit or create the command to use instead. The
// hints keep the user's own command line, which holds the wiki, the text and
// its source, rather than rebuild it.
func saveError(err error, t cmdutil.Target) error {
	switch {
	case errors.Is(err, api.ErrMissingPage):
		return fmt.Errorf("%w on %s; to make it, run the same command with `mw page create` in place of `mw page edit`", err, t.Name)
	case errors.Is(err, api.ErrPageExists):
		return fmt.Errorf("%w on %s; to change it, run the same command with `mw page edit` in place of `mw page create`", err, t.Name)
	case errors.Is(err, api.ErrEditConflict):
		return fmt.Errorf("%w on %s; view the latest text with `mw page view`, apply your change to it, and run the command again with its revision", err, t.Name)
	}
	return err
}

// dryRun prints what saving text would change, without saving: a diff from the
// latest revision, or from nothing for a new page, to the text as the wiki
// would save it.
func dryRun(ctx context.Context, ios *iostreams.IOStreams, c *api.Client, t cmdutil.Target, title, text string, mode api.EditMode, baseRevID int) error {
	cur, err := c.LatestRevision(ctx, title)
	switch {
	case errors.Is(err, api.ErrMissingPage) && mode == api.CreateOnly:
		cur = nil
	case err != nil:
		return saveError(err, t)
	case mode == api.CreateOnly:
		return saveError(fmt.Errorf("%w: %s", api.ErrPageExists, cur.Title), t)
	}
	if baseRevID > 0 && cur != nil && cur.RevID != baseRevID {
		fmt.Fprintf(ios.ErrOut, "%s %s changed since revision %d (now %d); saving would merge those changes or fail with an edit conflict\n", ios.WarningIcon(), cur.Title, baseRevID, cur.RevID)
	}
	// The text is saved in the page's model, or for a new page the model the
	// wiki gives the title, such as JSON for a .json page.
	model := ""
	if cur != nil {
		model = cur.ContentModel
	}
	saved, err := c.PreSaveTransform(ctx, title, text, model)
	if err != nil {
		return err
	}
	oldLabel, oldText, name := "/dev/null", "", title
	if cur != nil {
		oldLabel, oldText, name = label(cur), cur.Content, cur.Title
	}
	if cur != nil && oldText == saved {
		fmt.Fprintf(ios.ErrOut, "%s No changes to %s on %s\n", ios.WarningIcon(), name, t.Name)
		return nil
	}
	out := udiff.Unified(oldLabel, name+"\t(new text, not saved)", oldText, saved)
	if ios.IsStdoutTTY() && os.Getenv("NO_COLOR") == "" {
		out = colorize(out)
	}
	fmt.Fprint(ios.Out, out)
	return nil
}
