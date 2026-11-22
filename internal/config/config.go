// Package config reads and writes config.yml in the mw configuration directory.
// It holds wiki farms and their endpoints, the default context, and logins.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultSite is used when no site is given and none is configured.
	DefaultSite = "en.wikipedia.org"

	AuthOAuth2      = "oauth2"
	AuthSession     = "session"
	AuthBotPassword = "botpassword"

	configFile = "config.yml"
)

// Account is one logged-in user of a site.
type Account struct {
	Auth       string            `yaml:"auth"`
	OAuthToken string            `yaml:"oauth_token,omitempty"`
	Cookies    map[string]string `yaml:"cookies,omitempty"`
	// BotName and BotPassword are a bot password from Special:BotPasswords:
	// BotName is the login name, such as Alice@mybot.
	BotName     string `yaml:"bot_name,omitempty"`
	BotPassword string `yaml:"bot_password,omitempty"`
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
	var legacy struct {
		Sites struct {
			Default string           `yaml:"default"`
			Wikis   map[string]*Site `yaml:"wikis"`
		} `yaml:"sites"`
	}
	if len(b) > 0 {
		if err := yaml.Unmarshal(b, &legacy); err != nil {
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
	if err := migrateLegacySites(c, legacy.Sites.Default, legacy.Sites.Wikis); err != nil {
		return nil, fmt.Errorf("migrate %s: %w", c.Path(), err)
	}
	return c, nil
}

// migrateLegacySites moves the former sites.default/sites.wikis schema into
// context/farms. Credentials remain keyed by the same farm whenever possible.
func migrateLegacySites(c *Config, defaultSite string, wikis map[string]*Site) error {
	if c.Context == "" {
		c.Context = defaultSite
	}
	names := make([]string, 0, len(wikis))
	for name := range wikis {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		old := wikis[name]
		if old == nil || old.API == "" {
			return fmt.Errorf("legacy site %q has no API URL; add it to farms manually before saving", name)
		}
		if !ValidSiteName(name) {
			return fmt.Errorf("legacy site name %q cannot be migrated; rename it under farms before saving", name)
		}
		host, err := HostOf(old.API)
		if err != nil {
			return fmt.Errorf("legacy site %q has invalid API URL: %w", name, err)
		}
		aliases := append([]string(nil), old.Aliases...)
		if !strings.EqualFold(name, host) {
			aliases = append(aliases, name)
		}
		entry := FarmSite{Host: host, API: old.API, Aliases: uniqueNames(aliases), Server: old.Server, Script: old.Script, ArticlePath: old.ArticlePath}

		farmName, farmIndex, exists, err := farmForAPI(c.Farms, old.API)
		if err != nil {
			return err
		}
		if exists {
			if err := checkLegacyNameAvailable(c.Farms, name, old.API); err != nil {
				return err
			}
			if farmName != name {
				if err := moveLogin(c.Logins, name, farmName); err != nil {
					return err
				}
			}
			current := c.Farms[farmName][farmIndex]
			current.Aliases = uniqueNames(append(current.Aliases, entry.Aliases...))
			if current.Server == "" {
				current.Server = entry.Server
			}
			if current.Script == "" {
				current.Script = entry.Script
			}
			if current.ArticlePath == "" {
				current.ArticlePath = entry.ArticlePath
			}
			c.Farms[farmName][farmIndex] = current
			continue
		}

		actualFarm, configured := configuredFarm(c.Farms, name)
		if configured {
			if len(c.Farms[actualFarm]) != 0 {
				return fmt.Errorf("legacy site %q conflicts with farm %q at a different API; rename one entry in config.yml before saving", name, actualFarm)
			}
			farmName = actualFarm
		} else {
			farmName = name
		}
		if err := checkLegacyNameAvailable(c.Farms, name, old.API); err != nil {
			return err
		}
		c.Farms[farmName] = append(c.Farms[farmName], entry)
		if farmName != name {
			if err := moveLogin(c.Logins, name, farmName); err != nil {
				return err
			}
		}
	}
	return nil
}

func farmForAPI(farms map[string][]FarmSite, api string) (string, int, bool, error) {
	foundFarm, foundIndex := "", -1
	for farm, wikis := range farms {
		for i, wiki := range wikis {
			endpoint := wiki.API
			if endpoint == "" {
				endpoint = DefaultAPI(wiki.Host)
			}
			if endpoint == api {
				if foundFarm != "" {
					return "", -1, false, fmt.Errorf("API %s is already present more than once in farms; resolve the duplicate entries before migrating sites", api)
				}
				foundFarm, foundIndex = farm, i
			}
		}
	}
	return foundFarm, foundIndex, foundFarm != "", nil
}

func configuredFarm(farms map[string][]FarmSite, name string) (string, bool) {
	for farm := range farms {
		if strings.EqualFold(farm, name) {
			return farm, true
		}
	}
	return "", false
}

func checkLegacyNameAvailable(farms map[string][]FarmSite, name, api string) error {
	for farm, wikis := range farms {
		for _, wiki := range wikis {
			endpoint := wiki.API
			if endpoint == "" {
				endpoint = DefaultAPI(wiki.Host)
			}
			matches := strings.EqualFold(name, wiki.Host)
			if len(wikis) == 1 {
				matches = matches || strings.EqualFold(name, farm)
			}
			for _, alias := range wiki.Aliases {
				matches = matches || strings.EqualFold(name, alias)
			}
			if matches && endpoint != api {
				return fmt.Errorf("legacy site name %q conflicts with a different wiki in farm %q; rename one entry in config.yml before saving", name, farm)
			}
		}
	}
	return nil
}

func moveLogin(logins map[string]*Login, from, to string) error {
	source, ok := logins[from]
	if !ok || from == to {
		return nil
	}
	target := logins[to]
	if target == nil {
		logins[to] = source
		delete(logins, from)
		return nil
	}
	for user, account := range source.Users {
		if existing, exists := target.Users[user]; exists && !reflect.DeepEqual(existing, account) {
			return fmt.Errorf("accounts named %q in logins.%s and logins.%s differ; merge them manually before saving", user, from, to)
		}
		target.Users[user] = account
	}
	if target.User == "" {
		target.User = source.User
	}
	delete(logins, from)
	return nil
}

func uniqueNames(names []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(names))
	for _, name := range names {
		key := strings.ToLower(name)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, name)
		}
	}
	sort.Strings(unique)
	return unique
}

// Save writes config.yml with owner-only permissions: it holds credentials.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(b)) == "{}" {
		b = nil
	}
	return writeFile(c.Path(), b)
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
