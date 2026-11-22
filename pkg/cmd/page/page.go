// Package page implements `mw page`.
package page

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdPage returns `mw page`.
func NewCmdPage(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "page <command>",
		Short: "Read, list, create and edit wiki pages and their history",
	}
	cmd.AddCommand(newCmdView(f), newCmdList(f), newCmdHistory(f), newCmdCreate(f), newCmdEdit(f))
	return cmd
}

// viewFields are the --json fields of `mw page view`, named as in the MediaWiki API.
var viewFields = []string{
	"title", "pageid", "ns", "url", "contentmodel", "length", "touched", "redirect",
	"protection", "categories", "revision", "content",
}

func newCmdView(f *cmdutil.Factory) *cobra.Command {
	var wiki, apiURL string
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
			t, err := f.Resolve(cfg, wiki, apiURL)
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
				info, err := c.PageInfo(args[0], api.PageInfoOptions{Content: jsonFlags.Has("content"), Categories: jsonFlags.Has("categories")})
				if err != nil {
					return err
				}
				return jsonFlags.Write(ios.Out, info, ios.IsStdoutTTY())
			}
			text, err := c.Page(args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(ios.Out, text)
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki, &apiURL)
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open the page in the browser")
	jsonFlags = cmdutil.AddJSONFlags(cmd, viewFields)
	return cmd
}

// editFlags are the flags `mw page edit` and `mw page create` share.
type editFlags struct {
	wiki, apiURL, summary, text, file string
	minor, bot                        bool
}

func (o *editFlags) add(cmd *cobra.Command) {
	cmdutil.AddTargetFlags(cmd, &o.wiki, &o.apiURL)
	cmd.Flags().StringVarP(&o.summary, "summary", "s", "", "Edit summary")
	cmd.Flags().StringVar(&o.text, "text", "", "New wikitext")
	cmd.Flags().StringVarP(&o.file, "file", "F", "", "Read the new wikitext from `file` (use \"-\" to read from standard input)")
	cmd.Flags().BoolVar(&o.minor, "minor", false, "Mark the edit as minor")
	cmd.Flags().BoolVar(&o.bot, "bot", false, "Mark the edit as a bot edit, when the account has the bot right")
}

func newCmdEdit(f *cmdutil.Factory) *cobra.Command {
	o := &editFlags{}
	cmd := &cobra.Command{
		Use:   "edit <title>",
		Short: "Replace the wikitext of a page",
		Long: heredoc.Doc(`
			Replace the wikitext of an existing page. To make a new page, use
			'mw page create'.
		`),
		Example: heredoc.Doc(`
			$ mw page edit 'Sandbox' --summary 'Fix typo' --file page.wiki
			$ mw page view 'Sandbox' | sed 's/teh/the/' | mw page edit 'Sandbox' -F - -s 'Fix typo' --minor
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return saveRun(f, cmd, o, args[0], api.EditOnly)
		},
	}
	o.add(cmd)
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
			them, such as the createeditmovepage grant of a token.
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
	t, err := f.Resolve(cfg, o.wiki, o.apiURL)
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
	res, err := c.Edit(title, text, api.EditOptions{Summary: o.summary, Mode: mode, Minor: o.minor, Bot: o.bot})
	var ae *api.APIError
	switch {
	case errors.As(err, &ae) && ae.Code == "assertuserfailed":
		return cmdutil.NotLoggedIn(t.Name)
	// The hints name the other command but keep the user's own command line,
	// which holds the wiki, the text and its source, rather than rebuild it.
	case errors.Is(err, api.ErrMissingPage):
		return fmt.Errorf("%w on %s; to make it, run the same command with `mw page create` in place of `mw page edit`", err, t.Name)
	case errors.Is(err, api.ErrPageExists):
		return fmt.Errorf("%w on %s; to change it, run the same command with `mw page edit` in place of `mw page create`", err, t.Name)
	case err != nil:
		return err
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
