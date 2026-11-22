// Package cmdutil holds what commands share: the factory, host resolution and errors.
package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/internal/apicache"
	"github.com/mwcli/mwcli/internal/build"
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
	// Find discovers a site: for `mw site add`, a first login, and a host or
	// page URL that was not added, whose result the cache keeps.
	Find func(ctx context.Context, target string) (*config.Site, error)
	// Cache returns the API cache; nil loads it from its directory.
	Cache func() *apicache.Cache

	last  *Target
	cache *apicache.Cache
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
	Name     string
	LoginKey string
	API      string
	Site     *config.Site
	Kind     sites.Kind
	// Cached is the cache key when the API came from the cache.
	Cached string
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
	for _, n := range []string{wiki, os.Getenv("MW_WIKI"), cfg.Context} {
		if n != "" {
			return n
		}
	}
	return config.DefaultSite
}

// Resolve picks the site of --wiki, MW_WIKI, the default site or finally
// en.wikipedia.org: a site name or alias, a host, the URL of a page of the
// wiki, or the URL of its api.php. The api.php of a host or page that was not
// added is found once and kept in the cache.
func (f *Factory) Resolve(ctx context.Context, cfg *config.Config, wiki string) (Target, error) {
	name := strings.TrimSpace(SiteName(cfg, wiki))
	if !IsURL(name) || IsAPIURL(name) {
		r, err := sites.Lookup(cfg, name)
		if err != nil {
			return Target{}, FlagErrorf("%v", err)
		}
		if r.Kind != sites.Unregistered || IsAPIURL(name) {
			return f.target(r, ""), nil
		}
	}
	cache := f.APICache()
	if e, key, ok := cache.Get(name); ok {
		return f.found(cfg, e.Site(), key)
	}
	site, err := f.Find(ctx, name)
	if err != nil {
		return Target{}, fmt.Errorf("could not find the API of %s: %w\ngive the URL of its api.php with --wiki, or add it with `mw site add NAME URL`", name, err)
	}
	if err := cache.Put(name, site); err != nil {
		fmt.Fprintf(f.IOStreams.ErrOut, "warning: could not save %s: %v\n", cache.Path(), err)
	}
	return f.found(cfg, site, "")
}

// found resolves a site discovery found or the cache had.
func (f *Factory) found(cfg *config.Config, site *config.Site, cached string) (Target, error) {
	r, err := sites.ForAPI(cfg, site.API)
	if err != nil {
		return Target{}, FlagErrorf("%v", err)
	}
	if r.Kind == sites.Unregistered {
		r.Site = site
	}
	return f.target(r, cached), nil
}

// APICache returns the cache of APIs found for wikis that were not added.
func (f *Factory) APICache() *apicache.Cache {
	if f.cache == nil {
		if f.Cache != nil {
			f.cache = f.Cache()
		} else {
			f.cache = apicache.Load()
		}
	}
	return f.cache
}

// IsURL reports whether s is a URL rather than a name or host.
func IsURL(s string) bool { return strings.Contains(s, "://") }

// IsAPIURL reports whether s is the URL of an api.php.
func IsAPIURL(s string) bool {
	if !IsURL(s) {
		return false
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	return strings.HasSuffix(s, "/api.php")
}

func (f *Factory) target(r sites.Resolved, cached string) Target {
	t := Target{Name: r.Name, LoginKey: r.LoginKey, API: r.Site.API, Site: r.Site, Kind: r.Kind, Cached: cached}
	f.last = &t
	return t
}

// Hint adds advice to an error of the command when it talked to a site that
// was not added. An API from the cache that no longer answers as MediaWiki is
// forgotten, so that the next run looks for it again.
func (f *Factory) Hint(err error) error {
	if err == nil || f.last == nil || f.last.Kind != sites.Unregistered {
		return err
	}
	t := *f.last
	var he *api.HTTPError
	if !errors.As(err, &he) && !strings.Contains(err.Error(), "decode API response") {
		return err
	}
	if t.Cached != "" {
		if forgot, derr := f.APICache().Delete(t.Cached); derr == nil && forgot {
			return fmt.Errorf("%w\nmw had found the API of %s at %s and has now forgotten it; run the command again to look for it anew", err, t.Cached, t.API)
		}
	}
	return fmt.Errorf("%w\nmw tried %s; check the URL, or add the wiki with `mw site add NAME URL`", err, t.API)
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
	user, a, ok := cfg.ActiveAccount(loginKey(t))
	if !ok {
		return AuthSource{}, false
	}
	return AuthSource{User: user, Account: a}, true
}

// Client returns an API client for t, authenticated when credentials exist.
func Client(cfg *config.Config, t Target) (*api.Client, bool, error) {
	src, ok := Credentials(cfg, t)
	c, err := NewClient(t.API, src.Account)
	return c, ok, err
}

// AddWikiFlag adds --wiki to cmd.
func AddWikiFlag(cmd *cobra.Command, wiki *string, usage string) {
	cmd.Flags().StringVar(wiki, "wiki", "", usage)
}

// AddTargetFlags adds --wiki to a command that talks to a wiki.
func AddTargetFlags(cmd *cobra.Command, wiki *string) {
	AddWikiFlag(cmd, wiki, "The `wiki`: a site name, a host such as mediawiki.org, an interwiki prefix such as wikipedia, or a URL of the wiki or its api.php")
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

func loginKey(t Target) string {
	if t.LoginKey != "" {
		return t.LoginKey
	}
	return t.Name
}

// NewClient returns an API client for apiURL with mw's User-Agent,
// authenticated with account when it is not nil.
func NewClient(apiURL string, account *config.Account) (*api.Client, error) {
	return api.New(apiURL, ClientOptions(account)...)
}

// ClientOptions turns a stored account into the options of an API client.
func ClientOptions(account *config.Account) []api.Option {
	opts := []api.Option{api.WithUserAgent(build.UserAgent()), api.OnMaxlag(func(wait time.Duration, lag string) {
		// A request the wiki refused for lag is sent again after the wait.
		if lag != "" {
			lag = " (" + lag + " s)"
		}
		fmt.Fprintf(os.Stderr, "The wiki's database is lagging%s; trying again in %s\n", lag, wait)
	})}
	if account == nil {
		return opts
	}
	switch account.Auth {
	case config.AuthOAuth2:
		opts = append(opts, api.WithOAuth2Token(account.OAuthToken))
	case config.AuthOAuth1:
		opts = append(opts, api.WithOAuth1(api.OAuth1{ConsumerKey: account.ConsumerKey, ConsumerSecret: account.ConsumerSecret, AccessToken: account.AccessToken, AccessSecret: account.AccessSecret}))
	case config.AuthBotPassword:
		opts = append(opts, api.WithBotPassword(account.BotName, account.BotPassword))
		if account.Domain != "" {
			opts = append(opts, api.WithLoginDomain(account.Domain))
		}
	default:
		opts = append(opts, api.WithCookies(account.Cookies))
	}
	return opts
}
