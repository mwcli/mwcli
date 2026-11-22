// Package cmdutil holds what commands share: the factory, host resolution and errors.
package cmdutil

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/internal/discovery"
	"github.com/mwcli/mwcli/internal/sites"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/spf13/cobra"
)

// Factory provides commands with streams and configuration.
type Factory struct {
	IOStreams *iostreams.IOStreams
	Config    func() (*config.Config, error)
	// Browse opens a URL for --web.
	Browse func(url string) error
	// Find discovers a site for `mw site add` and a first login.
	Find func(target string) (*config.Site, error)

	last *Target
}

// New returns the factory for the running process.
func New() *Factory {
	var cached *config.Config
	f := &Factory{IOStreams: iostreams.System(), Browse: OpenBrowser, Find: discovery.Find}
	f.Config = func() (*config.Config, error) {
		if cached != nil {
			return cached, nil
		}
		c, err := config.Load()
		if err != nil {
			return nil, err
		}
		cached = c
		return c, nil
	}
	return f
}

// TokenEnv is the environment variable whose access token overrides stored credentials.
const TokenEnv = "MW_TOKEN"

// Target is the site a command talks to.
type Target struct {
	// Name is the site name; logins are stored under it.
	Name string
	API  string
	Site *config.Site
	Kind sites.Kind
}

// PageURL returns the URL of an index.php page of the target.
func (t Target) PageURL(title string) string {
	if t.Site != nil {
		return t.Site.PageURL(title)
	}
	return (&config.Site{API: t.API}).PageURL(title)
}

// SiteName picks the site name: --wiki, MW_WIKI, the default site, en.wikipedia.org.
func SiteName(cfg *config.Config, wiki string) string {
	for _, n := range []string{wiki, os.Getenv("MW_WIKI"), cfg.Sites.Default} {
		if n != "" {
			return n
		}
	}
	return config.DefaultSite
}

// Resolve picks the site: --api, else --wiki, MW_WIKI, the default site and
// finally en.wikipedia.org, each a site name, an alias or a host.
func (f *Factory) Resolve(cfg *config.Config, wiki, apiURL string) (Target, error) {
	if apiURL != "" {
		r, err := sites.ForAPI(cfg, apiURL)
		if err != nil {
			return Target{}, FlagErrorf("%v", err)
		}
		if wiki != "" {
			w, err := sites.Lookup(cfg, wiki)
			if err != nil {
				return Target{}, FlagErrorf("%v", err)
			}
			if w.Name != r.Name {
				return Target{}, FlagErrorf("--api %s is not the API of --wiki %s", apiURL, wiki)
			}
		}
		return f.target(r), nil
	}
	r, err := sites.Lookup(cfg, SiteName(cfg, wiki))
	if err != nil {
		return Target{}, FlagErrorf("%v", err)
	}
	return f.target(r), nil
}

func (f *Factory) target(r sites.Resolved) Target {
	t := Target{Name: r.Name, API: r.Site.API, Site: r.Site, Kind: r.Kind}
	f.last = &t
	return t
}

// Hint adds advice to an error of the command when it talked to a site that
// was not added: its API may not be at /w/api.php.
func (f *Factory) Hint(err error) error {
	if err == nil || f.last == nil || f.last.Kind != sites.Unregistered {
		return err
	}
	t := *f.last
	var he *api.HTTPError
	if errors.As(err, &he) || strings.Contains(err.Error(), "decode API response") {
		return fmt.Errorf("%w\nmw tried %s; if %s is a MediaWiki wiki elsewhere, add it with `mw site add %s`", err, t.API, t.Name, t.Name)
	}
	return err
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
	user, a, ok := cfg.ActiveAccount(t.Name)
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

// AddWikiFlag adds --wiki to cmd.
func AddWikiFlag(cmd *cobra.Command, wiki *string, usage string) {
	cmd.Flags().StringVar(wiki, "wiki", "", usage)
}

// AddTargetFlags adds --wiki and --api to cmd.
func AddTargetFlags(cmd *cobra.Command, wiki, apiURL *string) {
	AddWikiFlag(cmd, wiki, "The wiki: a host name such as mediawiki.org or an interwiki prefix such as wikipedia")
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
	return &AuthError{Err: fmt.Errorf("not logged in to %s; run `mw auth login --wiki %s`", host, host)}
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
