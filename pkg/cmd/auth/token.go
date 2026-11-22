package auth

import (
	"fmt"
	"os"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func newCmdToken(f *cmdutil.Factory) *cobra.Command {
	var wiki, user string
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the access token of an account",
		Long: "Print the OAuth 2 access token mw uses for a host, MW_TOKEN when it is set.\n" +
			"The token is printed in plain text; keep it out of shared logs and terminals.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			t := cmdutil.Target{Host: f.HostKey(cfg, cmdutil.HostName(cfg, wiki))}
			var a *config.Account
			if tok := os.Getenv(cmdutil.TokenEnv); user == "" && tok != "" {
				fmt.Fprintln(f.IOStreams.Out, tok)
				return nil
			}
			if user == "" {
				var ok bool
				if user, a, ok = cfg.ActiveAccount(t.Host); !ok {
					return cmdutil.NotLoggedIn(t.Host)
				}
			} else if h, ok := cfg.Hosts[t.Host]; ok {
				a = h.Users[user]
			}
			if a == nil {
				return fmt.Errorf("not logged in to %s account %s", t.Host, user)
			}
			switch a.Auth {
			case config.AuthOAuth2:
			case config.AuthBotPassword:
				return fmt.Errorf("%s account %s uses a bot password, not a token", t.Host, user)
			default:
				return fmt.Errorf("%s account %s uses a password session, not a token", t.Host, user)
			}
			fmt.Fprintln(f.IOStreams.Out, a.OAuthToken)
			return nil
		},
	}
	cmdutil.AddWikiFlag(cmd, &wiki, "The wiki whose token to print")
	cmd.Flags().StringVarP(&user, "user", "u", "", "The account to print the token of")
	return cmd
}
