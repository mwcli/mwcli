package auth

import (
	"fmt"

	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func newCmdLogout(f *cmdutil.Factory) *cobra.Command {
	var wiki, user string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of a wiki account",
		Long: "Remove a stored account. Without --user, the active account of the host is removed.\n" +
			"This does not revoke an OAuth token; reset or disable its consumer on the wiki at\n" +
			"Special:OAuthConsumerRegistration/list.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			io := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			host, err := storedHost(f, cfg, wiki)
			if err != nil {
				return err
			}
			if user == "" {
				user = cfg.Hosts[host].User
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
	cmdutil.AddWikiFlag(cmd, &wiki, "The wiki to log out of")
	cmd.Flags().StringVarP(&user, "user", "u", "", "The account to log out of")
	return cmd
}
