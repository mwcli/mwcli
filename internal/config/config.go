// Package config reads and writes config.yml in the mw configuration directory.
// It holds wiki farms and their endpoints, the default context, and logins.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

const keyringService = "mwcli"

type keyringResult struct {
	value string
	err   error
}

func keyringGet(service, user string) (string, error) {
	result := make(chan keyringResult, 1)
	go func() {
		value, err := keyring.Get(service, user)
		result <- keyringResult{value: value, err: err}
	}()
	select {
	case r := <-result:
		return r.value, r.err
	case <-time.After(time.Minute):
		return "", errors.New("timeout while reading system keyring")
	}
}

func keyringSet(service, user, secret string) error {
	result := make(chan error, 1)
	go func() { result <- keyring.Set(service, user, secret) }()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Minute):
		return errors.New("timeout while writing system keyring")
	}
}

func keyringDelete(service, user string) error {
	result := make(chan error, 1)
	go func() { result <- keyring.Delete(service, user) }()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Minute):
		return errors.New("timeout while deleting from system keyring")
	}
}

const (
	// DefaultSite is used when no site is given and none is configured.
	DefaultSite = "en.wikipedia.org"

	AuthOAuth2      = "oauth2"
	AuthOAuth1      = "oauth1"
	AuthSession     = "session"
	AuthBotPassword = "botpassword"

	configFile = "config.yml"
)

// Account is one logged-in user of a site.
type Account struct {
	Auth            string `yaml:"auth"`
	OAuthToken      string `yaml:"oauth_token,omitempty"`
	CredentialRef   string `yaml:"credential_ref,omitempty"`
	InsecureStorage bool   `yaml:"-"`
	// OAuthClientID identifies the public OAuth consumer used to obtain the
	// account's token. It is stored per wiki account, never read from env.
	OAuthClientID string            `yaml:"oauth_client_id,omitempty"`
	Cookies       map[string]string `yaml:"cookies,omitempty"`
	// BotName and BotPassword are a bot password from Special:BotPasswords:
	// BotName is the login name, such as Alice@mybot.
	BotName     string `yaml:"bot_name,omitempty"`
	BotPassword string `yaml:"bot_password,omitempty"`
	// Domain is the LDAP authentication domain a bot password logs in with.
	Domain string `yaml:"domain,omitempty"`
	// ConsumerKey, ConsumerSecret, AccessToken and AccessSecret are the keys of
	// an owner-only OAuth 1.0a consumer.
	ConsumerKey    string `yaml:"consumer_key,omitempty"`
	ConsumerSecret string `yaml:"consumer_secret,omitempty"`
	AccessToken    string `yaml:"access_token,omitempty"`
	AccessSecret   string `yaml:"access_secret,omitempty"`
}

// Site is one wiki: one api.php. Server, Script and ArticlePath come from the
// wiki's siteinfo when the site is added, for page URLs.
type Site struct {
	API string `yaml:"api"`
	// Aliases are other names --wiki accepts for the site, such as an
	// interwiki prefix or a domain that redirects to it.
	Aliases     []string `yaml:"aliases,omitempty"`
	Server      string   `yaml:"server,omitempty"`
	Script      string   `yaml:"script,omitempty"`
	ArticlePath string   `yaml:"articlepath,omitempty"`
}

// PageURL returns the URL of an index.php page such as "Special:Version".
func (s *Site) PageURL(title string) string {
	server := s.Server
	if strings.HasPrefix(server, "//") {
		server = "https:" + server
	}
	if server == "" || s.Script == "" {
		return strings.TrimSuffix(s.API, "api.php") + "index.php?title=" + url.QueryEscape(title)
	}
	return server + s.Script + "?title=" + url.QueryEscape(title)
}

// FarmSite is a wiki endpoint and the names accepted for it within a farm.
type FarmSite struct {
	Host        string   `yaml:"host"`
	Aliases     []string `yaml:"aliases,omitempty"`
	API         string   `yaml:"api,omitempty"`
	Server      string   `yaml:"server,omitempty"`
	Script      string   `yaml:"script,omitempty"`
	ArticlePath string   `yaml:"articlepath,omitempty"`
}

// Login holds the accounts of one site and which one is active.
type Login struct {
	User  string              `yaml:"user,omitempty"`
	Users map[string]*Account `yaml:"users,omitempty"`
}

// Config is config.yml of the configuration directory.
type Config struct {
	Dir     string                `yaml:"-"`
	Context string                `yaml:"context,omitempty"`
	Farms   map[string][]FarmSite `yaml:"farms,omitempty"`
	Logins  map[string]*Login     `yaml:"logins,omitempty"`
}

// Dir returns the configuration directory: MW_CONFIG_DIR, $XDG_CONFIG_HOME/mw,
// %AppData%\mw on Windows, else ~/.config/mw.
func Dir() string {
	if d := os.Getenv("MW_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "mw")
	}
	if d := os.Getenv("AppData"); runtime.GOOS == "windows" && d != "" {
		return filepath.Join(d, "mw")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mw"
	}
	return filepath.Join(home, ".config", "mw")
}

// Path is the path of config.yml.
func (c *Config) Path() string { return filepath.Join(c.Dir, configFile) }

// Load reads the configuration directory. A missing file is an empty config.
func Load() (*Config, error) {
	return LoadDir(Dir())
}

// LoadDir reads the configuration from dir.
func LoadDir(dir string) (*Config, error) {
	c := &Config{Dir: dir}
	b, err := os.ReadFile(c.Path())
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := yaml.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", c.Path(), err)
		}
	}
	if c.Farms == nil {
		c.Farms = map[string][]FarmSite{}
	}
	if c.Logins == nil {
		c.Logins = map[string]*Login{}
	}
	for _, l := range c.Logins {
		if l.Users == nil {
			l.Users = map[string]*Account{}
		}
	}
	if err := c.loadCredentials(); err != nil {
		return nil, err
	}
	return c, nil
}

// Save writes config.yml with owner-only permissions. Credentials go in the
// system keyring unless an account explicitly requests insecure storage.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	persisted := c.withStoredCredentials()
	b, err := yaml.Marshal(persisted)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(b)) == "{}" {
		b = nil
	}
	oldRefs := readCredentialRefs(c.Path())
	if err := writeFile(c.Path(), b); err != nil {
		return err
	}
	newRefs := credentialRefs(persisted)
	for ref := range oldRefs {
		if !newRefs[ref] {
			if err := keyringDelete(keyringService, ref); err != nil && !errors.Is(err, keyring.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "warning: could not remove an unused credential from the system keyring: %v\n", err)
			}
		}
	}
	return nil
}

type accountSecrets struct {
	OAuthToken     string            `json:"oauth_token,omitempty"`
	Cookies        map[string]string `json:"cookies,omitempty"`
	BotPassword    string            `json:"bot_password,omitempty"`
	ConsumerSecret string            `json:"consumer_secret,omitempty"`
	AccessToken    string            `json:"access_token,omitempty"`
	AccessSecret   string            `json:"access_secret,omitempty"`
}

func (a *Account) secrets() accountSecrets {
	return accountSecrets{OAuthToken: a.OAuthToken, Cookies: a.Cookies, BotPassword: a.BotPassword, ConsumerSecret: a.ConsumerSecret, AccessToken: a.AccessToken, AccessSecret: a.AccessSecret}
}

func (a *Account) setSecrets(s accountSecrets) {
	a.OAuthToken, a.Cookies, a.BotPassword = s.OAuthToken, s.Cookies, s.BotPassword
	a.ConsumerSecret, a.AccessToken, a.AccessSecret = s.ConsumerSecret, s.AccessToken, s.AccessSecret
}

func (a *Account) clearSecrets() { a.setSecrets(accountSecrets{}) }

func hasSecrets(s accountSecrets) bool {
	return s.OAuthToken != "" || len(s.Cookies) != 0 || s.BotPassword != "" || s.ConsumerSecret != "" || s.AccessToken != "" || s.AccessSecret != ""
}

func credentialRef(site, user string) string {
	h := sha256.Sum256([]byte(site + "\x00" + user))
	return hex.EncodeToString(h[:])
}

func (c *Config) loadCredentials() error {
	for site, login := range c.Logins {
		for user, account := range login.Users {
			if account.CredentialRef == "" {
				continue
			}
			data, err := keyringGet(keyringService, account.CredentialRef)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: credentials for %s account %s are unavailable in the system keyring; log in again: %v\n", site, user, err)
				continue
			}
			var secrets accountSecrets
			if err := json.Unmarshal([]byte(data), &secrets); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not decode credentials for %s account %s from the system keyring; log in again: %v\n", site, user, err)
				continue
			}
			account.setSecrets(secrets)
		}
	}
	return nil
}

func (c *Config) withStoredCredentials() *Config {
	stored := *c
	stored.Logins = make(map[string]*Login, len(c.Logins))
	for site, login := range c.Logins {
		l := *login
		l.Users = make(map[string]*Account, len(login.Users))
		for user, account := range login.Users {
			a := *account
			secrets := a.secrets()
			if hasSecrets(secrets) {
				if a.InsecureStorage {
					a.CredentialRef = ""
				} else {
					ref := credentialRef(site, user)
					data, err := json.Marshal(secrets)
					if err == nil {
						err = keyringSet(keyringService, ref, string(data))
					}
					if err == nil {
						a.clearSecrets()
						a.CredentialRef = ref
					} else {
						a.CredentialRef = ""
						fmt.Fprintf(os.Stderr, "warning: could not save credentials in the system keyring; saving them in config.yml instead: %v\n", err)
					}
				}
			}
			l.Users[user] = &a
		}
		stored.Logins[site] = &l
	}
	return &stored
}

func readCredentialRefs(path string) map[string]bool {
	refs := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return refs
	}
	var c Config
	if yaml.Unmarshal(b, &c) != nil {
		return refs
	}
	return credentialRefs(&c)
}

func credentialRefs(c *Config) map[string]bool {
	refs := map[string]bool{}
	for _, login := range c.Logins {
		for _, account := range login.Users {
			if account.CredentialRef != "" {
				refs[account.CredentialRef] = true
			}
		}
	}
	return refs
}

func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ValidSiteName reports whether name may name a site: not empty, and without
// "/" or whitespace, which other names use.
func ValidSiteName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "/ \t\n")
}

// LoginNames returns the sites with logins in sorted order.
func (c *Config) LoginNames() []string {
	return sortedKeys(c.Logins)
}

func sortedKeys[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// UserNames returns the users of a login in sorted order, active user first.
func (l *Login) UserNames() []string {
	names := make([]string, 0, len(l.Users))
	for u := range l.Users {
		if u != l.User {
			names = append(names, u)
		}
	}
	sort.Strings(names)
	if _, ok := l.Users[l.User]; ok {
		names = append([]string{l.User}, names...)
	}
	return names
}

// DefaultAPI is the usual MediaWiki API endpoint of a host.
func DefaultAPI(host string) string {
	return "https://" + host + "/w/api.php"
}

// HostOf returns the host name of an API URL.
func HostOf(api string) (string, error) {
	u, err := url.Parse(api)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("invalid API URL %q", api)
	}
	return u.Host, nil
}

// ActiveAccount returns the active user and account of site.
func (c *Config) ActiveAccount(site string) (string, *Account, bool) {
	l, ok := c.Logins[site]
	if !ok {
		return "", nil, false
	}
	a, ok := l.Users[l.User]
	return l.User, a, ok
}

// Login stores an account and makes it active on site. The site becomes the
// default only when there is none yet; `mw site default` changes it later.
func (c *Config) Login(site, user string, a *Account) {
	l, ok := c.Logins[site]
	if !ok {
		l = &Login{Users: map[string]*Account{}}
		c.Logins[site] = l
	}
	l.Users[user] = a
	l.User = user
	if c.Context == "" {
		c.Context = site
	}
}

// Logout removes a user from site. It returns the user that became active, if any.
func (c *Config) Logout(site, user string) (string, error) {
	l, ok := c.Logins[site]
	if !ok {
		return "", fmt.Errorf("not logged in to %s", site)
	}
	if _, ok := l.Users[user]; !ok {
		return "", fmt.Errorf("not logged in to %s account %s", site, user)
	}
	delete(l.Users, user)
	if len(l.Users) == 0 {
		delete(c.Logins, site)
		return "", nil
	}
	if l.User == user {
		l.User = l.UserNames()[0]
		return l.User, nil
	}
	return "", nil
}

// Switch makes user the active account of site.
func (c *Config) Switch(site, user string) error {
	l, ok := c.Logins[site]
	if !ok {
		return fmt.Errorf("not logged in to %s", site)
	}
	if _, ok := l.Users[user]; !ok {
		return fmt.Errorf("not logged in to %s account %s", site, user)
	}
	l.User = user
	return nil
}
