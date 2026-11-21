package auth

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"
	"github.com/zetaoss/mwcli/api"
	"github.com/zetaoss/mwcli/internal/config"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
	"github.com/zetaoss/mwcli/pkg/iostreams"
)

type loginOptions struct {
	hostname  string
	apiURL    string
	withToken bool
	username  string
}

func newCmdLogin(f *cmdutil.Factory) *cobra.Command {
	opts := &loginOptions{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a wiki",
		Long: heredoc.Doc(`
			Log in to a wiki with an OAuth 2 access token or a username and password.

			An owner-only OAuth 2 access token is preferred. Create one on the wiki at
			Special:OAuthConsumerRegistration/propose/oauth2 with the grants you need,
			and revoke it at Special:OAuthManageMyGrants.

			--with-token reads the token from standard input. --username logs in with a
			password, read from MW_PASSWORD or a prompt. Without either, mw asks.

			The host becomes the default for later commands.
		`),
		Example: heredoc.Doc(`
			# interactive
			$ mw auth login --hostname zetawiki.com

			# token from a file
			$ mw auth login --hostname zetawiki.com --with-token < token.txt

			# a wiki whose API is not at /w/api.php
			$ mw auth login --api https://wiki.example.org/api.php
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return loginRun(f, opts)
		},
	}
	cmdutil.AddTargetFlags(cmd, &opts.hostname, &opts.apiURL)
	cmd.Flags().BoolVar(&opts.withToken, "with-token", false, "Read an OAuth 2 access token from standard input")
	cmd.Flags().StringVarP(&opts.username, "username", "u", "", "Log in with this username and a password")
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
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	t, err := cmdutil.Resolve(cfg, opts.hostname, opts.apiURL)
	if err != nil {
		return err
	}
	if opts.hostname == "" && opts.apiURL == "" && io.CanPrompt() {
		h, err := io.Prompt(fmt.Sprintf("? Wiki host name [%s]: ", t.Host))
		if err != nil {
			return err
		}
		if h != "" {
			if t, err = cmdutil.Resolve(cfg, h, ""); err != nil {
				return err
			}
		}
	}

	useToken := opts.withToken
	if !opts.withToken && opts.username == "" {
		if !io.CanPrompt() {
			return cmdutil.FlagErrorf("--with-token or --username required when not running interactively")
		}
		fmt.Fprintln(io.ErrOut, "? How would you like to authenticate mw?")
		fmt.Fprintln(io.ErrOut, "  1. Paste an OAuth 2 access token")
		fmt.Fprintln(io.ErrOut, "  2. Username and password")
		choice, err := io.Prompt("Choose [1]: ")
		if err != nil {
			return err
		}
		switch choice {
		case "", "1":
			useToken = true
		case "2":
		default:
			return cmdutil.FlagErrorf("unknown choice %q", choice)
		}
	}

	var account *config.Account
	var user string
	if useToken {
		account, user, err = tokenLogin(f, t, opts.withToken)
	} else {
		account, user, err = passwordLogin(f, t, opts.username)
	}
	if err != nil {
		return err
	}
	cfg.Login(t.Host, t.API, user, account)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(io.ErrOut, "%s Logged in to %s as %s\n", io.SuccessIcon(), t.Host, io.Bold(user))
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
		fmt.Fprintf(ios.ErrOut, "Create an owner-only token at %s\n", specialPage(t.API, "OAuthConsumerRegistration/propose/oauth2"))
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
		return nil, "", fmt.Errorf("the token was not accepted by %s", t.Host)
	}
	if err != nil {
		return nil, "", fmt.Errorf("check token: %w", err)
	}
	return account, u.Name, nil
}

func passwordLogin(f *cmdutil.Factory, t cmdutil.Target, username string) (*config.Account, string, error) {
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
	if err := c.Login(username, password); err != nil {
		return nil, "", err
	}
	u, err := c.User()
	if err != nil {
		return nil, "", fmt.Errorf("check login: %w", err)
	}
	return &config.Account{Auth: config.AuthSession, Cookies: c.Cookies()}, u.Name, nil
}

// specialPage turns an api.php URL into the URL of a special page.
func specialPage(apiURL, name string) string {
	base := strings.TrimSuffix(apiURL, "api.php")
	return base + "index.php?title=Special:" + name
}
