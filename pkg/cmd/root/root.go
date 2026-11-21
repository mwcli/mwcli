// Package root assembles the mw command tree.
package root

import (
	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"
	"github.com/zetaoss/mwcli/internal/build"
	apiCmd "github.com/zetaoss/mwcli/pkg/cmd/api"
	authCmd "github.com/zetaoss/mwcli/pkg/cmd/auth"
	pageCmd "github.com/zetaoss/mwcli/pkg/cmd/page"
	versionCmd "github.com/zetaoss/mwcli/pkg/cmd/version"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
)

// NewCmdRoot returns `mw`.
func NewCmdRoot(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mw <command> <subcommand> [flags]",
		Short: "MediaWiki CLI",
		Long:  "Work with MediaWiki wikis from the command line.",
		Example: heredoc.Doc(`
			$ mw auth login --hostname www.mediawiki.org
			$ mw page view 'Main Page' --hostname en.wikipedia.org
			$ mw api action=query meta=siteinfo
		`),
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       build.Version,
	}
	cmd.SetVersionTemplate(versionCmd.Format(build.Version, build.Date))
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
	// -h is --hostname in subcommands, as in gh; help is --help only.
	cmd.PersistentFlags().Bool("help", false, "Show help for command")
	cmd.Flags().Bool("version", false, "Show mw version")
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return &cmdutil.FlagError{Err: err}
	})
	cmd.AddCommand(authCmd.NewCmdAuth(f))
	cmd.AddCommand(pageCmd.NewCmdPage(f))
	cmd.AddCommand(apiCmd.NewCmdApi(f))
	cmd.AddCommand(versionCmd.NewCmdVersion(f))
	wrapArgs(cmd)
	return cmd
}

// wrapArgs makes argument count errors usage errors, like flag errors.
func wrapArgs(cmd *cobra.Command) {
	if args := cmd.Args; args != nil {
		cmd.Args = func(c *cobra.Command, a []string) error {
			if err := args(c, a); err != nil {
				return &cmdutil.FlagError{Err: err}
			}
			return nil
		}
	}
	for _, c := range cmd.Commands() {
		wrapArgs(c)
	}
}
