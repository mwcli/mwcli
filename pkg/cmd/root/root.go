// Package root assembles the mw command tree.
package root

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/internal/build"
	"github.com/mwcli/mwcli/internal/interwiki"
	apiCmd "github.com/mwcli/mwcli/pkg/cmd/api"
	authCmd "github.com/mwcli/mwcli/pkg/cmd/auth"
	pageCmd "github.com/mwcli/mwcli/pkg/cmd/page"
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
	cmd.AddCommand(apiCmd.NewCmdApi(f))
	cmd.AddCommand(versionCmd.NewCmdVersion(f))
	cmd.AddCommand(interwikiHelp())
	wrapArgs(cmd)
	return cmd
}

// interwikiHelp is the `mw help interwiki` topic.
func interwikiHelp() *cobra.Command {
	var b strings.Builder
	b.WriteString("--wiki and MW_WIKI accept these interwiki prefixes from MediaWiki's\n")
	b.WriteString("default interwiki list, limited to MediaWiki wikis:\n\n")
	for _, p := range interwiki.All() {
		fmt.Fprintf(&b, "  %-14s %s\n", p.Name, p.API)
	}
	b.WriteString("\nA name with a dot is always a host name.\n")
	return &cobra.Command{
		Use:   "interwiki",
		Short: "Interwiki prefixes accepted as host names",
		Long:  b.String(),
	}
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
