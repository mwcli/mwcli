package auth

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/internal/sites"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/spf13/cobra"
)

type loginOptions struct {
	wiki        string
	apiURL      string
	withToken   bool
	username    string
	botPassword bool
}

func newCmdLogin(f *cmdutil.Factory) *cobra.Command {
	opts := &loginOptions{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a wiki",
		Long: heredoc.Doc(`
			Log in to a wiki with an OAuth 2 access token, a bot password, or a username
			and password.

			An owner-only OAuth 2 access token is preferred where the wiki has the OAuth
			extension; otherwise a bot password from Special:BotPasswords, which limits mw
			to the grants you choose. Create a token on the wiki at
			Special:OAuthConsumerRegistration/propose/oauth2 with the grants you need.
			To replace a token, reset the consumer's secret key at
			Special:OAuthConsumerRegistration/list on the wiki where it was registered
			(the central wiki of a farm, such as meta.wikimedia.org); the old token
			stops working. Log in again with --wiki, or update MW_TOKEN where a script
			sets it.

			--with-token reads the token from standard input. --username logs in with a
			password, read from MW_PASSWORD or a prompt; a name such as Alice@mybot, or
			any name with --bot-password, is a bot password, which mw stores and uses to
			log in again on each run, only at the site it was created for. An
			account password is never stored, only its session. Without either, mw asks.

			A wiki that was not added with 'mw site add' is found and added. The first
			login also makes its site the default; 'mw site default' changes it later.
		`),
		Example: heredoc.Doc(`
			# interactive
			$ mw auth login --wiki mediawiki.org

			# token from a file
			$ mw auth login --wiki mediawiki.org --with-token < token.txt

			# a wiki whose API is not at /w/api.php
			$ mw auth login --api https://wiki.example.org/api.php
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return loginRun(f, opts)
		},
	}
	cmdutil.AddTargetFlags(cmd, &opts.wiki, &opts.apiURL)
	cmd.Flags().BoolVar(&opts.withToken, "with-token", false, "Read an OAuth 2 access token from standard input")
	cmd.Flags().StringVarP(&opts.username, "username", "u", "", "Log in with this username and a password, or a bot password login name such as Alice@mybot")
	cmd.Flags().BoolVar(&opts.botPassword, "bot-password", false, "Treat --username as a bot password login name, for wikis whose delimiter is not @")
	return cmd
}

func loginRun(f *cmdutil.Factory, opts *loginOptions) error {
	io := f.IOStreams
	if os.Getenv(cmdutil.TokenEnv) != "" {
		return fmt.Errorf("the value of the %s environment variable is being used for authentication.\nTo have mw store credentials instead, first clear the value from the environment", cmdutil.TokenEnv)
	}
	if opts.withToken && opts.username != "" {
		return cmdutil.FlagErrorf("specify only one of --with-token and --username")
	}
	if opts.botPassword && opts.username == "" {
		return cmdutil.FlagErrorf("--bot-password needs --username with the bot login name")
	}
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	t, err := f.Resolve(cfg, opts.wiki, opts.apiURL)
	if err != nil {
		return err
	}
	if opts.wiki == "" && opts.apiURL == "" && io.CanPrompt() {
		h, err := io.Prompt(fmt.Sprintf("? Wiki (site name or host) [%s]: ", t.Name))
		if err != nil {
			return err
		}
		if h != "" {
			if t, err = f.Resolve(cfg, h, ""); err != nil {
				return err
			}
		}
	}
	if t.Kind == sites.Unregistered {
		// A first login to a wiki that was not added finds and adds it.
		if sites.IsFarmName(cfg, t.Name) {
			return cmdutil.FlagErrorf("site name %q conflicts with a farm name; add it with a different site name", t.Name)
		}
		target := opts.apiURL
		if target == "" {
			target = t.Name
		}
		if !config.ValidSiteName(t.Name) {
			return fmt.Errorf("a site with this host already points to another API; add this one with `mw site add NAME %s` and log in with --wiki NAME", target)
		}
		site, err := f.Find(target)
		if err != nil {
			return err
		}
		cfg.Sites.Wikis[t.Name] = site
		t = cmdutil.Target{Name: t.Name, LoginKey: t.Name, API: site.API, Site: site, Kind: sites.Added}
		fmt.Fprintf(io.ErrOut, "%s Added site %s (%s)\n", io.SuccessIcon(), t.Name, site.API)
	}

	useToken := opts.withToken
	if !opts.withToken && opts.username == "" {
		if !io.CanPrompt() {
			return cmdutil.FlagErrorf("--with-token or --username required when not running interactively")
		}
		fmt.Fprintln(io.ErrOut, "? How would you like to authenticate mw?")
		fmt.Fprintln(io.ErrOut, "  1. Paste an OAuth 2 access token")
		fmt.Fprintln(io.ErrOut, "  2. Bot password (from Special:BotPasswords)")
		fmt.Fprintln(io.ErrOut, "  3. Username and password")
		choice, err := io.Prompt("Choose [1]: ")
		if err != nil {
			return err
		}
		switch choice {
		case "", "1":
			useToken = true
		case "2":
			fmt.Fprintf(io.ErrOut, "Create a bot password at %s\n", t.PageURL("Special:BotPasswords"))
			if opts.username, err = io.Prompt("? Bot login name (such as Alice@mybot): "); err != nil {
				return err
			}
			opts.botPassword = true
		case "3":
		default:
			return cmdutil.FlagErrorf("unknown choice %q", choice)
		}
	}

	var account *config.Account
	var user string
	if useToken {
		account, user, err = tokenLogin(f, t, opts.withToken)
	} else {
		account, user, err = passwordLogin(f, t, opts.username, opts.botPassword || api.IsBotName(opts.username))
	}
	if err != nil {
		return err
	}
	wasDefaultUnset := cfg.Sites.Default == ""
	cfg.Login(t.LoginKey, user, account)
	if wasDefaultUnset {
		cfg.Sites.Default = t.Name
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(io.ErrOut, "%s Logged in to %s as %s\n", io.SuccessIcon(), t.Name, io.Bold(user))
	return nil
}

func tokenLogin(f *cmdutil.Factory, t cmdutil.Target, fromStdin bool) (*config.Account, string, error) {
	ios := f.IOStreams
	var token string
	if fromStdin {
		b, err := io.ReadAll(ios.In)
		if err != nil {
			return nil, "", fmt.Errorf("read token from standard input: %w", err)
		}
		token = strings.TrimSpace(string(b))
	} else {
		fmt.Fprintf(ios.ErrOut, "Create an owner-only token at %s\n", t.PageURL("Special:OAuthConsumerRegistration/propose/oauth2"))
		var err error
		if token, err = ios.PromptSecret("? Paste your access token: "); err != nil {
			return nil, "", err
		}
	}
	if token == "" {
		return nil, "", cmdutil.FlagErrorf("no access token given")
	}
	account := &config.Account{Auth: config.AuthOAuth2, OAuthToken: token}
	c, err := api.New(t.API, account)
	if err != nil {
		return nil, "", err
	}
	u, err := c.User()
	if errors.Is(err, api.ErrNotLoggedIn) {
		return nil, "", fmt.Errorf("the token was not accepted by %s", t.Name)
	}
	if err != nil {
		return nil, "", fmt.Errorf("check token: %w", err)
	}
	return account, u.Name, nil
}

func passwordLogin(f *cmdutil.Factory, t cmdutil.Target, username string, bot bool) (*config.Account, string, error) {
	ios := f.IOStreams
	var err error
	if username == "" {
		if username, err = ios.Prompt("? Username: "); err != nil {
			return nil, "", err
		}
	}
	password := os.Getenv("MW_PASSWORD")
	if password == "" {
		if password, err = ios.PromptSecret("? Password: "); err != nil {
			if errors.Is(err, iostreams.ErrNoPrompt) {
				return nil, "", cmdutil.FlagErrorf("set MW_PASSWORD when not running interactively")
			}
			return nil, "", err
		}
	}
	if username == "" || password == "" {
		return nil, "", cmdutil.FlagErrorf("username and password are required")
	}
	c, err := api.New(t.API, nil)
	if err != nil {
		return nil, "", err
	}
	if bot {
		if err := c.BotLogin(username, password); err != nil {
			return nil, "", err
		}
		u, err := c.User()
		if err != nil {
			return nil, "", fmt.Errorf("check login: %w", err)
		}
		return &config.Account{Auth: config.AuthBotPassword, BotName: username, BotPassword: password}, u.Name, nil
	}
	if err := c.Login(username, password); err != nil {
		return nil, "", err
	}
	u, err := c.User()
	if err != nil {
		return nil, "", fmt.Errorf("check login: %w", err)
	}
	return &config.Account{Auth: config.AuthSession, Cookies: c.Cookies()}, u.Name, nil
}
