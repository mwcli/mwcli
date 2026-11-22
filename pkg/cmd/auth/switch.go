package auth

import (
	"fmt"

	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func newCmdSwitch(f *cmdutil.Factory) *cobra.Command {
	var wiki, user string
	cmd := &cobra.Command{
		Use:   "switch",
		Short: "Switch the active account of a wiki",
		Long: "Make another stored account of a host active. With two accounts on the host,\n" +
			"mw switches to the other one; with more, give --user.",
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
			h := cfg.Logins[host]
			if user == "" {
				names := h.UserNames()
				switch {
				case len(names) < 2:
					return fmt.Errorf("no accounts on %s to switch to", host)
				case len(names) > 2:
					return cmdutil.FlagErrorf("--user required: %d accounts on %s", len(names), host)
				}
				user = names[1]
			}
			if err := cfg.Switch(host, user); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(io.ErrOut, "%s Switched active account for %s to %s\n", io.SuccessIcon(), host, io.Bold(user))
			return nil
		},
	}
	cmdutil.AddWikiFlag(cmd, &wiki, "The wiki whose active account to switch")
	cmd.Flags().StringVarP(&user, "user", "u", "", "The account to switch to")
	return cmd
}
