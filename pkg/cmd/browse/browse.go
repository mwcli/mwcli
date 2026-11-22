// Package browse implements `mw browse`.
package browse

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdBrowse returns `mw browse`.
func NewCmdBrowse(f *cmdutil.Factory) *cobra.Command {
	var wiki string
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "browse [<title>]",
		Short: "Open a wiki page in the browser",
		Long: heredoc.Doc(`
			Open a page of the wiki in the browser, or its main page when no title is
			given. Special pages work too, such as Special:RecentChanges.

			With --no-browser, print the URL instead. The page is not checked to
			exist, and no request is made to the wiki once its API is known.
		`),
		Example: heredoc.Doc(`
			$ mw browse
			$ mw browse 'Main Page' --wiki commons
			$ mw browse Special:RecentChanges
			$ mw browse 'Main Page' -n
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ios := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			t, err := f.Resolve(cmd.Context(), cfg, wiki)
			if err != nil {
				return err
			}
			u := t.MainPageURL()
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				u = t.PageURL(args[0])
			}
			if noBrowser {
				_, err := fmt.Fprintln(ios.Out, u)
				return err
			}
			if ios.IsStdoutTTY() {
				fmt.Fprintf(ios.ErrOut, "Opening %s in your browser.\n", u)
			}
			return f.Browse(u)
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().BoolVarP(&noBrowser, "no-browser", "n", false, "Print the URL instead of opening the browser")
	return cmd
}
