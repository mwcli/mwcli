package auth

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zetaoss/mwcli/internal/config"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
)

func newCmdToken(f *cmdutil.Factory) *cobra.Command {
	var hostname, user string
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the access token of an account",
		Long:  "Print the OAuth 2 access token mw uses for a host, MW_TOKEN when it is set.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			t, err := cmdutil.Resolve(cfg, hostname, "")
			if err != nil {
				return err
			}
			var a *config.Account
			if user == "" {
				src, ok := cmdutil.Credentials(cfg, t)
				if !ok {
					return cmdutil.NotLoggedIn(t.Host)
				}
				a, user = src.Account, src.User
			} else if h, ok := cfg.Hosts[t.Host]; ok {
				a = h.Users[user]
			}
			if a == nil {
				return fmt.Errorf("not logged in to %s account %s", t.Host, user)
			}
			if a.Auth != config.AuthOAuth2 {
				return fmt.Errorf("%s account %s uses a password session, not a token", t.Host, user)
			}
			fmt.Fprintln(f.IOStreams.Out, a.OAuthToken)
			return nil
		},
	}
	cmd.Flags().StringVarP(&hostname, "hostname", "h", "", "The host name of the wiki")
	cmd.Flags().StringVarP(&user, "user", "u", "", "The account to print the token of")
	return cmd
}
