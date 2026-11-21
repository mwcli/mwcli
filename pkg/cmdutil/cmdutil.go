// Package cmdutil holds what commands share: the factory, host resolution and errors.
package cmdutil

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zetaoss/mwcli/api"
	"github.com/zetaoss/mwcli/internal/config"
	"github.com/zetaoss/mwcli/pkg/iostreams"
)

// Factory provides commands with streams and configuration.
type Factory struct {
	IOStreams *iostreams.IOStreams
	Config    func() (*config.Config, error)
}

// New returns the factory for the running process.
func New() *Factory {
	var cached *config.Config
	f := &Factory{IOStreams: iostreams.System()}
	f.Config = func() (*config.Config, error) {
		if cached != nil {
			return cached, nil
		}
		c, err := config.Load()
		if err != nil {
			return nil, err
		}
		if c.Migrated != "" {
			fmt.Fprintf(f.IOStreams.ErrOut, "Moved login from %s to %s\n", c.Migrated, c.HostsPath())
		}
		cached = c
		return c, nil
	}
	return f
}

// TokenEnv is the environment variable whose access token overrides stored credentials.
const TokenEnv = "MW_TOKEN"

// Target is the wiki a command talks to.
type Target struct {
	Host string
	API  string
}

// Resolve picks the wiki: --api, then --hostname, MW_HOST, the default host
// set by the last login, and finally en.wikipedia.org.
func Resolve(cfg *config.Config, hostname, apiURL string) (Target, error) {
	if apiURL != "" {
		host, err := config.HostOf(apiURL)
		if err != nil {
			return Target{}, FlagErrorf("%v", err)
		}
		if hostname != "" && hostname != host {
			return Target{}, FlagErrorf("--api host %s does not match --hostname %s", host, hostname)
		}
		return Target{Host: host, API: apiURL}, nil
	}
	host := hostname
	if host == "" {
		host = os.Getenv("MW_HOST")
	}
	if host == "" {
		host = cfg.DefaultHost
	}
	if host == "" {
		host = config.DefaultHost
	}
	return Target{Host: host, API: cfg.API(host)}, nil
}

// AuthSource says where the credentials of a client came from.
type AuthSource struct {
	User    string
	Account *config.Account
	FromEnv bool
}

// Credentials returns the credentials for t: MW_TOKEN, else the active account.
func Credentials(cfg *config.Config, t Target) (AuthSource, bool) {
	if tok := os.Getenv(TokenEnv); tok != "" {
		return AuthSource{Account: &config.Account{Auth: config.AuthOAuth2, OAuthToken: tok}, FromEnv: true}, true
	}
	user, a, ok := cfg.ActiveAccount(t.Host)
	if !ok {
		return AuthSource{}, false
	}
	return AuthSource{User: user, Account: a}, true
}

// Client returns an API client for t, authenticated when credentials exist.
func Client(cfg *config.Config, t Target) (*api.Client, bool, error) {
	src, ok := Credentials(cfg, t)
	c, err := api.New(t.API, src.Account)
	return c, ok, err
}

// AddTargetFlags adds --hostname and --api to cmd.
func AddTargetFlags(cmd *cobra.Command, hostname, apiURL *string) {
	cmd.Flags().StringVarP(hostname, "hostname", "h", "", "The host name of the wiki")
	cmd.Flags().StringVar(apiURL, "api", "", "The api.php URL, when it is not https://HOST/w/api.php")
}

// FlagError is a usage error; it exits with status 2.
type FlagError struct{ Err error }

func (e *FlagError) Error() string { return e.Err.Error() }
func (e *FlagError) Unwrap() error { return e.Err }

// FlagErrorf returns a *FlagError.
func FlagErrorf(format string, a ...any) error {
	return &FlagError{Err: fmt.Errorf(format, a...)}
}

// AuthError means the command needs a login; it exits with status 4.
type AuthError struct{ Err error }

func (e *AuthError) Error() string { return e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }

// NotLoggedIn returns the error for a command that needs a login on host.
func NotLoggedIn(host string) error {
	return &AuthError{Err: fmt.Errorf("not logged in to %s; run `mw auth login --hostname %s`", host, host)}
}

// ExitError exits with Code after printing Err, unless Err is nil.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}
func (e *ExitError) Unwrap() error { return e.Err }

// ErrSilent exits with status 1 without printing anything.
var ErrSilent = &ExitError{Code: 1}

// ExitCode maps an error to the process exit status.
func ExitCode(err error) int {
	var ee *ExitError
	var fe *FlagError
	var ae *AuthError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.Code
	case errors.As(err, &fe):
		return 2
	case errors.As(err, &ae):
		return 4
	}
	return 1
}
