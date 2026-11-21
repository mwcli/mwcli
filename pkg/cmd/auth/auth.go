// Package auth implements `mw auth`.
package auth

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"
	"github.com/zetaoss/mwcli/internal/config"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
)

// NewCmdAuth returns `mw auth`.
func NewCmdAuth(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <command>",
		Short: "Authenticate mw with a wiki",
		Long: heredoc.Doc(`
			Log in to wikis, switch between accounts and see what each login may do.

			Credentials are stored per host in hosts.yml in the mw configuration directory.
			Setting MW_TOKEN uses that OAuth 2 access token instead of stored credentials.
		`),
	}
	cmd.AddCommand(newCmdLogin(f))
	cmd.AddCommand(newCmdLogout(f))
	cmd.AddCommand(newCmdStatus(f))
	cmd.AddCommand(newCmdSwitch(f))
	cmd.AddCommand(newCmdToken(f))
	return cmd
}

// storedHost picks a configured host for commands that act on stored accounts:
// --wiki, else the only host, else the default host.
func storedHost(f *cmdutil.Factory, cfg *config.Config, wiki string) (string, error) {
	host := f.HostKey(cfg, wiki)
	if host != "" {
		if _, ok := cfg.Hosts[host]; !ok {
			return "", fmt.Errorf("not logged in to %s", host)
		}
		return host, nil
	}
	names := cfg.HostNames()
	switch {
	case len(names) == 0:
		return "", fmt.Errorf("not logged in to any hosts")
	case len(names) == 1:
		return names[0], nil
	}
	if _, ok := cfg.Hosts[cfg.DefaultHost]; ok {
		return cfg.DefaultHost, nil
	}
	return "", cmdutil.FlagErrorf("--wiki required: logged in to %s", strings.Join(names, ", "))
}
