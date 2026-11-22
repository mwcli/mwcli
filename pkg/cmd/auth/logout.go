package auth

import (
	"fmt"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func newCmdLogout(f *cmdutil.Factory) *cobra.Command {
	var wiki, user string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of a wiki account",
		Long: "Remove a stored account. Without --user, the active account of the host is removed.\n" +
			"An account password's session is ended on the wiki too. This does not revoke an\n" +
			"OAuth token or a bot password; reset or disable an OAuth consumer at\n" +
			"Special:OAuthConsumerRegistration/list on the wiki where it was registered.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			io := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			host, err := storedSite(cfg, wiki)
			if err != nil {
				return err
			}
			if user == "" {
				user = cfg.Logins[host].User
			}
			if a := cfg.Logins[host].Users[user]; a != nil && a.Auth == config.AuthSession {
				// End the session on the wiki, as the browser's log out does;
				// the account is forgotten either way.
				c, err := cmdutil.NewClient(siteAPI(cfg, host), a)
				if err == nil {
					err = c.Logout(cmd.Context())
				}
				if err != nil {
					fmt.Fprintf(io.ErrOut, "%s Could not end the session on %s: %v\n", io.WarningIcon(), host, err)
				}
			}
			next, err := cfg.Logout(host, user)
			if err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(io.ErrOut, "%s Logged out of %s account %s\n", io.SuccessIcon(), host, io.Bold(user))
			if next != "" {
				fmt.Fprintf(io.ErrOut, "%s Switched active account for %s to %s\n", io.SuccessIcon(), host, io.Bold(next))
			}
			return nil
		},
	}
	cmdutil.AddWikiFlag(cmd, &wiki, "The `wiki` to log out of")
	cmd.Flags().StringVarP(&user, "user", "u", "", "The `account` to log out of")
	return cmd
}
