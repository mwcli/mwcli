// Package auth implements `mw auth`.
package auth

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/internal/sites"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdAuth returns `mw auth`.
func NewCmdAuth(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <command>",
		Short: "Authenticate mw with a wiki",
		Long: heredoc.Doc(`
			Log in to wikis, switch between accounts and see what each login may do.

			Logins are stored per site in config.yml in the mw configuration directory.
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

// storedSite picks a site with logins for commands that act on stored
// accounts: --wiki, else the only one, else the default site.
func storedSite(cfg *config.Config, wiki string) (string, error) {
	if wiki != "" {
		r, err := sites.Lookup(cfg, wiki)
		if err != nil {
			return "", cmdutil.FlagErrorf("%v", err)
		}
		if _, ok := cfg.Logins[r.LoginKey]; !ok {
			return "", fmt.Errorf("not logged in to %s", r.Name)
		}
		return r.LoginKey, nil
	}
	names := cfg.LoginNames()
	switch {
	case len(names) == 0:
		return "", fmt.Errorf("not logged in to any wikis")
	case len(names) == 1:
		return names[0], nil
	}
	if cfg.Sites.Default != "" {
		if r, err := sites.Lookup(cfg, cfg.Sites.Default); err == nil {
			if _, ok := cfg.Logins[r.LoginKey]; ok {
				return r.LoginKey, nil
			}
		}
	}
	return "", cmdutil.FlagErrorf("--wiki required: logged in to %s", strings.Join(names, ", "))
}

// siteAPI returns the api.php of a site with logins.
func siteAPI(cfg *config.Config, name string) string {
	if r, err := sites.Lookup(cfg, name); err == nil {
		return r.Site.API
	}
	return ""
}
