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
	"github.com/mwcli/mwcli/internal/interwiki"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/spf13/cobra"
)

// Factory provides commands with streams and configuration.
type Factory struct {
	IOStreams *iostreams.IOStreams
	Config    func() (*config.Config, error)
	// Discovery finds the api.php of hosts without a configured one; nil means /w/api.php.
	Discovery *discovery.Cache

	cached []string
}

// New returns the factory for the running process.
func New() *Factory {
	var cached *config.Config
	f := &Factory{IOStreams: iostreams.System(), Discovery: discovery.New()}
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

// Target is the wiki a command talks to.
type Target struct {
	Host string
	API  string
	// Site is what discovery knows about the wiki, when it was used.
	Site *discovery.Site
}

// PageURL returns the URL of an index.php page of the target.
func (t Target) PageURL(title string) string {
	if t.Site != nil {
		return t.Site.PageURL(title)
	}
	return (&discovery.Site{API: t.API}).PageURL(title)
}

// Resolve picks the wiki: --api, then --wiki, MW_WIKI, the default host
// set by the last login, and finally en.wikipedia.org. The API is --api, the
// api of the host in hosts.yml, or the one discovery finds.
func (f *Factory) Resolve(cfg *config.Config, wiki, apiURL string) (Target, error) {
	if apiURL != "" {
		host, err := config.HostOf(apiURL)
		if err != nil {
			return Target{}, FlagErrorf("%v", err)
		}
		if wiki != "" && CanonicalHost(wiki) != host {
			return Target{}, FlagErrorf("--api host %s does not match --wiki %s", host, wiki)
		}
		return Target{Host: host, API: apiURL}, nil
	}
	host := HostName(cfg, wiki)
	api, site := f.APIFor(cfg, host)
	return Target{Host: f.keyFor(cfg, host, api), API: api, Site: site}, nil
}

// keyFor is the host under which accounts of host are stored: the host itself
// when hosts.yml names its API, else the host of the API it resolves to, so
// that mediawiki.org, www.mediawiki.org and the prefix mw are one wiki.
func (f *Factory) keyFor(cfg *config.Config, host, api string) string {
	if h, ok := cfg.Hosts[host]; ok && h.API != "" {
		return host
	}
	if apiHost, err := config.HostOf(api); err == nil {
		return apiHost
	}
	return host
}

// HostKey returns the stored host name for a user-given host or prefix.
func (f *Factory) HostKey(cfg *config.Config, name string) string {
	name = CanonicalHost(name)
	if name == "" {
		return ""
	}
	if _, ok := cfg.Hosts[name]; ok {
		return name
	}
	api, _ := f.APIFor(cfg, name)
	return f.keyFor(cfg, name, api)
}

// HostName picks the host: --wiki, MW_WIKI, the default host, en.wikipedia.org.
// An interwiki prefix such as "commons" stands for its host.
func HostName(cfg *config.Config, wiki string) string {
	for _, h := range []string{wiki, os.Getenv("MW_WIKI"), cfg.DefaultHost} {
		if h != "" {
			return CanonicalHost(h)
		}
	}
	return config.DefaultHost
}

// CanonicalHost turns an interwiki prefix into its host; other names are kept.
func CanonicalHost(name string) string {
	if host, _, ok := interwiki.Lookup(name); ok {
		return host
	}
	return name
}

// APIFor returns the api.php of host: hosts.yml, the interwiki list, discovery,
// else /w/api.php.
func (f *Factory) APIFor(cfg *config.Config, host string) (string, *discovery.Site) {
	if h, ok := cfg.Hosts[host]; ok && h.API != "" {
		return h.API, nil
	}
	if api, ok := interwiki.APIForHost(host); ok {
		return api, nil
	}
	if f.Discovery == nil {
		return config.DefaultAPI(host), nil
	}
	if s, ok := f.Discovery.Lookup(host); ok {
		f.cached = append(f.cached, host)
		return s.API, s
	}
	s, err := f.Discovery.Site(host)
	if err != nil {
		return config.DefaultAPI(host), nil
	}
	return s.API, s
}

// Forget drops cached discovery results when err suggests the API moved.
func (f *Factory) Forget(err error) {
	var he *api.HTTPError
	if f.Discovery == nil || err == nil {
		return
	}
	if (errors.As(err, &he) && (strings.HasPrefix(he.Status, "404") || strings.HasPrefix(he.Status, "410"))) ||
		strings.Contains(err.Error(), "decode API response") {
		for _, h := range f.cached {
			f.Discovery.Invalidate(h)
		}
	}
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
