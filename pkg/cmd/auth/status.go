package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/internal/sites"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/spf13/cobra"
)

const maxRights = 20

type statusOptions struct {
	wiki      string
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
	cmdutil.AddWikiFlag(cmd, &opts.wiki, "Check only this wiki")
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
		t, err := f.Resolve(cfg, opts.wiki)
		if err != nil {
			return err
		}
		add(entry{host: t.LoginKey, api: t.API, account: src.Account, active: true, source: cmdutil.TokenEnv})
		envToken = true
	}
	for _, site := range cfg.LoginNames() {
		l := cfg.Logins[site]
		for _, user := range l.UserNames() {
			active := user == l.User
			if envToken && byHost[site] != nil {
				active = false
			}
			add(entry{host: site, api: siteAPI(cfg, site), user: user, account: l.Users[user], active: active, source: cfg.Path()})
		}
	}
	if opts.wiki != "" {
		r, err := sites.Lookup(cfg, opts.wiki)
		if err != nil {
			return cmdutil.FlagErrorf("%v", err)
		}
		entries, ok := byHost[r.LoginKey]
		if !ok {
			fmt.Fprintf(ios.ErrOut, "You are not logged into any accounts on %s\n", r.Name)
			return cmdutil.ErrSilent
		}
		for i := range entries {
			entries[i].host = r.Name
			entries[i].api = r.Site.API
		}
		byHost[r.Name] = entries
		hosts = []string{r.Name}
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
	c, err := cmdutil.NewClient(e.api, e.account)
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
		case e.account.Auth == config.AuthBotPassword && strings.Contains(err.Error(), "bot password login failed"):
			fmt.Fprintln(w, "  - The bot password was not accepted; it may have been deleted or reset.")
		case e.account.Auth == config.AuthOAuth1 && (errors.Is(err, api.ErrNotLoggedIn) || isInvalidToken(err)):
			fmt.Fprintln(w, "  - The OAuth 1.0a keys are invalid; the consumer may have been disabled or its secret reset.")
		case errors.Is(err, api.ErrNotLoggedIn), isInvalidToken(err):
			fmt.Fprintln(w, "  - The token is invalid.")
		default:
			fmt.Fprintf(w, "  - Check failed: %v\n", err)
			return false
		}
		fmt.Fprintf(w, "  - To re-authenticate, run: mw auth login --wiki %s\n", e.host)
		fmt.Fprintf(w, "  - To forget about this account, run: mw auth logout --wiki %s --user %s\n", e.host, name)
		return false
	}
	fmt.Fprintf(w, "  %s Logged in to %s account %s (%s)\n", ios.SuccessIcon(), e.host, ios.Bold(name), e.source)
	fmt.Fprintf(w, "  - Active account: %t\n", e.active)
	fmt.Fprintf(w, "  - API: %s\n", e.api)
	if e.account.Auth == config.AuthOAuth2 {
		fmt.Fprintf(w, "  - Auth: OAuth 2 access token\n")
		claims, _ := parseJWT(e.account.OAuthToken)
		fmt.Fprintf(w, "  - Token: %s%s\n", displayToken(e.account.OAuthToken, showToken), claims.describe())
		if len(claims.Scopes) > 0 {
			fmt.Fprintf(w, "  - Grants: %s\n", strings.Join(claims.Scopes, ", "))
		}
	} else if e.account.Auth == config.AuthOAuth1 {
		fmt.Fprintf(w, "  - Auth: OAuth 1.0a consumer %s\n", e.account.ConsumerKey)
		fmt.Fprintf(w, "  - Token: %s\n", displayToken(e.account.AccessToken, showToken))
	} else if e.account.Auth == config.AuthBotPassword {
		fmt.Fprintf(w, "  - Auth: bot password %s\n", e.account.BotName)
		fmt.Fprintf(w, "  - Password: %s\n", displayToken(e.account.BotPassword, showToken))
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

const tokenMask = "********************************"

// displayToken masks a token to its last 4 characters, as `aws configure list`
// does. MediaWiki OAuth 2 tokens are JWTs, which all start the same.
func displayToken(token string, show bool) string {
	switch {
	case show:
		return token
	case len(token) <= 8:
		return tokenMask
	}
	return tokenMask + token[len(token)-4:]
}

// jwtClaims are the claims of a MediaWiki OAuth 2 access token shown by status.
// They are read without verifying the signature, only for display.
type jwtClaims struct {
	Audience audience `json:"aud"`
	IssuedAt float64  `json:"iat"`
	Scopes   []string `json:"scopes"`
}

type audience string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*a = audience(one)
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	if len(many) > 0 {
		*a = audience(many[0])
	}
	return nil
}

func parseJWT(token string) (jwtClaims, bool) {
	var c jwtClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return c, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil || json.Unmarshal(payload, &c) != nil {
		return jwtClaims{}, false
	}
	return c, true
}

func (c jwtClaims) describe() string {
	var parts []string
	if c.Audience != "" {
		client := string(c.Audience)
		if len(client) > 8 {
			client = client[:8]
		}
		parts = append(parts, "client "+client)
	}
	if c.IssuedAt > 0 {
		parts = append(parts, "issued "+time.Unix(int64(c.IssuedAt), 0).UTC().Format("2006-01-02"))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
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
