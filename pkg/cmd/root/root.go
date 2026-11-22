// Package root assembles the mw command tree.
package root

import (
	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/internal/build"
	apiCmd "github.com/mwcli/mwcli/pkg/cmd/api"
	authCmd "github.com/mwcli/mwcli/pkg/cmd/auth"
	browseCmd "github.com/mwcli/mwcli/pkg/cmd/browse"
	pageCmd "github.com/mwcli/mwcli/pkg/cmd/page"
	siteCmd "github.com/mwcli/mwcli/pkg/cmd/site"
	userCmd "github.com/mwcli/mwcli/pkg/cmd/user"
	versionCmd "github.com/mwcli/mwcli/pkg/cmd/version"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdRoot returns `mw`.
func NewCmdRoot(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mw <command> <subcommand> [flags]",
		Short: "MediaWiki CLI",
		Long:  "Work with MediaWiki wikis from the command line.",
		Example: heredoc.Doc(`
			$ mw auth login --wiki mediawiki.org
			$ mw page view 'Main Page' --wiki wikipedia
			$ mw api action=query meta=siteinfo
		`),
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       build.Version,
	}
	cmd.SetVersionTemplate(versionCmd.Format(build.Version, build.Date))
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
	cmd.Flags().Bool("version", false, "Show mw version")
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return &cmdutil.FlagError{Err: err}
	})
	cmd.AddCommand(authCmd.NewCmdAuth(f))
	cmd.AddCommand(pageCmd.NewCmdPage(f))
	cmd.AddCommand(siteCmd.NewCmdSite(f))
	cmd.AddCommand(userCmd.NewCmdUser(f))
	cmd.AddCommand(browseCmd.NewCmdBrowse(f))
	cmd.AddCommand(apiCmd.NewCmdAPI(f))
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
