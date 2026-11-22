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
		Short: "Read and edit wiki pages",
	}
	cmd.AddCommand(newCmdView(f), newCmdEdit(f))
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
				info, err := c.PageInfo(args[0], jsonFlags.Has("content"))
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

func newCmdEdit(f *cmdutil.Factory) *cobra.Command {
	var wiki, apiURL, summary, text, file string
	cmd := &cobra.Command{
		Use:   "edit <title>",
		Short: "Replace the wikitext of a page",
		Long:  "Replace the wikitext of a page, creating it when the login may create pages.",
		Example: heredoc.Doc(`
			$ mw page edit 'Sandbox' --summary 'Fix typo' --file page.wiki
			$ mw page view 'Sandbox' | sed 's/teh/the/' | mw page edit 'Sandbox' -F - -s 'Fix typo'
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			textSet, fileSet := cmd.Flags().Changed("text"), cmd.Flags().Changed("file")
			if textSet == fileSet {
				return cmdutil.FlagErrorf("specify exactly one of --text and --file")
			}
			if fileSet {
				var b []byte
				var err error
				if file == "-" {
					b, err = io.ReadAll(f.IOStreams.In)
				} else {
					b, err = os.ReadFile(file)
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
			t, err := f.Resolve(cfg, wiki, apiURL)
			if err != nil {
				return err
			}
			c, ok, err := cmdutil.Client(cfg, t)
			if err != nil {
				return err
			}
			if !ok {
				return cmdutil.NotLoggedIn(t.Host)
			}
			title, err := c.Edit(args[0], text, summary)
			var ae *api.APIError
			if errors.As(err, &ae) && ae.Code == "assertuserfailed" {
				return cmdutil.NotLoggedIn(t.Host)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(f.IOStreams.ErrOut, "%s Updated %s on %s\n", f.IOStreams.SuccessIcon(), title, t.Host)
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki, &apiURL)
	cmd.Flags().StringVarP(&summary, "summary", "s", "", "Edit summary")
	cmd.Flags().StringVar(&text, "text", "", "New wikitext")
	cmd.Flags().StringVarP(&file, "file", "F", "", "Read the new wikitext from `file` (use \"-\" to read from standard input)")
	return cmd
}
