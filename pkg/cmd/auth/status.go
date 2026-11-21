package auth

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zetaoss/mwcli/api"
	"github.com/zetaoss/mwcli/internal/config"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
	"github.com/zetaoss/mwcli/pkg/iostreams"
)

const maxRights = 20

type statusOptions struct {
	hostname  string
	active    bool
	showToken bool
}

func newCmdStatus(f *cmdutil.Factory) *cobra.Command {
	opts := &statusOptions{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display active account and authentication state on each known wiki",
		Long: "Check every stored account against its wiki and show what the login may do.\n" +
			"The status is 1 when an account fails the check.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return statusRun(f, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.hostname, "hostname", "h", "", "Check only this host")
	cmd.Flags().BoolVarP(&opts.active, "active", "a", false, "Display the active account only")
	cmd.Flags().BoolVarP(&opts.showToken, "show-token", "t", false, "Display the auth token")
	return cmd
}

type entry struct {
	host    string
	api     string
	user    string
	account *config.Account
	active  bool
	source  string
}

func statusRun(f *cmdutil.Factory, opts *statusOptions) error {
	ios := f.IOStreams
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	byHost := map[string][]entry{}
	var hosts []string
	add := func(e entry) {
		if _, ok := byHost[e.host]; !ok {
			hosts = append(hosts, e.host)
		}
		byHost[e.host] = append(byHost[e.host], e)
	}
	envToken := false
	if src, ok := cmdutil.Credentials(cfg, cmdutil.Target{}); ok && src.FromEnv {
		t, err := f.Resolve(cfg, opts.hostname, "")
		if err != nil {
			return err
		}
		add(entry{host: t.Host, api: t.API, account: src.Account, active: true, source: cmdutil.TokenEnv})
		envToken = true
	}
	for _, host := range cfg.HostNames() {
		h := cfg.Hosts[host]
		for _, user := range h.UserNames() {
			active := user == h.User
			if envToken && byHost[host] != nil {
				active = false
			}
			apiURL, _ := f.APIFor(cfg, host)
			add(entry{host: host, api: apiURL, user: user, account: h.Users[user], active: active, source: cfg.HostsPath()})
		}
	}
	if opts.hostname != "" {
		if _, ok := byHost[opts.hostname]; !ok {
			fmt.Fprintf(ios.ErrOut, "You are not logged into any accounts on %s\n", opts.hostname)
			return cmdutil.ErrSilent
		}
		hosts = []string{opts.hostname}
	}
	if len(hosts) == 0 {
		fmt.Fprintln(ios.ErrOut, "You are not logged into any wikis. To log in, run: mw auth login")
		return cmdutil.ErrSilent
	}
	sort.Strings(hosts)

	failed := false
	for i, host := range hosts {
		if i > 0 {
			fmt.Fprintln(ios.Out)
		}
		fmt.Fprintln(ios.Out, ios.Bold(host))
		for _, e := range byHost[host] {
			if opts.active && !e.active {
				continue
			}
			if !printEntry(ios.Out, ios, e, opts.showToken) {
				failed = true
			}
		}
	}
	if failed {
		return cmdutil.ErrSilent
	}
	return nil
}

func printEntry(w io.Writer, ios *iostreams.IOStreams, e entry, showToken bool) bool {
	c, err := api.New(e.api, e.account)
	var u *api.UserInfo
	if err == nil {
		u, err = c.User()
	}
	name := e.user
	if u != nil {
		name = u.Name
	}
	if err != nil {
		fmt.Fprintf(w, "  %s Failed to log in to %s account %s (%s)\n", ios.FailureIcon(), e.host, name, e.source)
		fmt.Fprintf(w, "  - Active account: %t\n", e.active)
		switch {
		case errors.Is(err, api.ErrNotLoggedIn) && e.account.Auth == config.AuthSession:
			fmt.Fprintln(w, "  - The session has expired.")
		case errors.Is(err, api.ErrNotLoggedIn), isInvalidToken(err):
			fmt.Fprintln(w, "  - The token is invalid.")
		default:
			fmt.Fprintf(w, "  - Check failed: %v\n", err)
			return false
		}
		fmt.Fprintf(w, "  - To re-authenticate, run: mw auth login --hostname %s\n", e.host)
		fmt.Fprintf(w, "  - To forget about this account, run: mw auth logout --hostname %s --user %s\n", e.host, name)
		return false
	}
	fmt.Fprintf(w, "  %s Logged in to %s account %s (%s)\n", ios.SuccessIcon(), e.host, ios.Bold(name), e.source)
	fmt.Fprintf(w, "  - Active account: %t\n", e.active)
	fmt.Fprintf(w, "  - API: %s\n", e.api)
	if e.account.Auth == config.AuthOAuth2 {
		fmt.Fprintf(w, "  - Auth: OAuth 2 access token\n")
		fmt.Fprintf(w, "  - Token: %s\n", displayToken(e.account.OAuthToken, showToken))
	} else {
		fmt.Fprintf(w, "  - Auth: password session\n")
	}
	can, cannot := summarize(u.Rights)
	fmt.Fprintf(w, "  - Can: %s · Cannot: %s\n", orNone(can), orNone(cannot))
	fmt.Fprintf(w, "  - Rights: %s\n", listRights(u.Rights))
	return true
}

func isInvalidToken(err error) bool {
	var ae *api.APIError
	var he *api.HTTPError
	return (errors.As(err, &ae) && (strings.HasPrefix(ae.Code, "mwoauth-invalid") || strings.Contains(ae.Code, "token"))) ||
		(errors.As(err, &he) && strings.HasPrefix(he.Status, "401"))
}

func displayToken(token string, show bool) string {
	if show {
		return token
	}
	if len(token) <= 4 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", min(len(token)-4, 32))
}

func summarize(rights []string) (can, cannot []string) {
	has := map[string]bool{}
	for _, r := range rights {
		has[r] = true
	}
	for _, a := range api.Actions {
		if has[a.Right] {
			can = append(can, a.Name)
		} else {
			cannot = append(cannot, a.Name)
		}
	}
	return can, cannot
}

func orNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}

func listRights(rights []string) string {
	r := append([]string(nil), rights...)
	sort.Strings(r)
	if len(r) > maxRights {
		return fmt.Sprintf("%s, … (%d; list all with mw api action=query meta=userinfo uiprop=rights)", strings.Join(r[:maxRights], ", "), len(r))
	}
	return fmt.Sprintf("%s (%d)", strings.Join(r, ", "), len(r))
}
