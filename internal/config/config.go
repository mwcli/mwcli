// Package config reads and writes the mw configuration directory:
// hosts.yml holds per-host accounts and credentials, config.yml holds settings.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultHost is used when no host is given and none is configured.
	DefaultHost = "en.wikipedia.org"

	AuthOAuth2  = "oauth2"
	AuthSession = "session"

	hostsFile  = "hosts.yml"
	configFile = "config.yml"
)

// Account is one logged-in user on a host.
type Account struct {
	Auth       string            `yaml:"auth"`
	OAuthToken string            `yaml:"oauth_token,omitempty"`
	Cookies    map[string]string `yaml:"cookies,omitempty"`
}

// Host is one wiki, keyed by its host name in hosts.yml.
type Host struct {
	API   string              `yaml:"api,omitempty"`
	User  string              `yaml:"user,omitempty"`
	Users map[string]*Account `yaml:"users,omitempty"`
}

type settings struct {
	DefaultHost string `yaml:"default_host,omitempty"`
}

// Config is the loaded configuration directory.
type Config struct {
	Dir         string
	Hosts       map[string]*Host
	DefaultHost string
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

// HostsPath is the path of hosts.yml.
func (c *Config) HostsPath() string { return filepath.Join(c.Dir, hostsFile) }

// Load reads the configuration directory. Missing files mean an empty config.
func Load() (*Config, error) {
	return LoadDir(Dir())
}

// LoadDir reads the configuration from dir.
func LoadDir(dir string) (*Config, error) {
	c := &Config{Dir: dir, Hosts: map[string]*Host{}}
	b, err := os.ReadFile(filepath.Join(dir, hostsFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := yaml.Unmarshal(b, &c.Hosts); err != nil {
			return nil, fmt.Errorf("parse %s: %w", c.HostsPath(), err)
		}
		if c.Hosts == nil {
			c.Hosts = map[string]*Host{}
		}
	}
	b, err = os.ReadFile(filepath.Join(dir, configFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		var s settings
		if err := yaml.Unmarshal(b, &s); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, configFile), err)
		}
		if c.DefaultHost == "" {
			c.DefaultHost = s.DefaultHost
		}
	}
	for _, h := range c.Hosts {
		if h.Users == nil {
			h.Users = map[string]*Account{}
		}
	}
	return c, nil
}

// Save writes hosts.yml and config.yml with owner-only permissions.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	hosts, err := yaml.Marshal(c.Hosts)
	if err != nil {
		return err
	}
	if len(c.Hosts) == 0 {
		hosts = nil
	}
	if err := writeFile(c.HostsPath(), hosts); err != nil {
		return err
	}
	s, err := yaml.Marshal(settings{DefaultHost: c.DefaultHost})
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(c.Dir, configFile), s)
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

// HostNames returns the configured hosts in sorted order.
func (c *Config) HostNames() []string {
	names := make([]string, 0, len(c.Hosts))
	for h := range c.Hosts {
		names = append(names, h)
	}
	sort.Strings(names)
	return names
}

// UserNames returns the users of a host in sorted order, active user first.
func (h *Host) UserNames() []string {
	names := make([]string, 0, len(h.Users))
	for u := range h.Users {
		if u != h.User {
			names = append(names, u)
		}
	}
	sort.Strings(names)
	if _, ok := h.Users[h.User]; ok {
		names = append([]string{h.User}, names...)
	}
	return names
}

// API returns the API endpoint stored for host, or the MediaWiki default path.
func (c *Config) API(host string) string {
	if h, ok := c.Hosts[host]; ok && h.API != "" {
		return h.API
	}
	return DefaultAPI(host)
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

// ActiveAccount returns the active user and account of host.
func (c *Config) ActiveAccount(host string) (string, *Account, bool) {
	h, ok := c.Hosts[host]
	if !ok {
		return "", nil, false
	}
	a, ok := h.Users[h.User]
	return h.User, a, ok
}

// Login stores an account, makes it active on host and makes host the default.
func (c *Config) Login(host, api, user string, a *Account) {
	h, ok := c.Hosts[host]
	if !ok {
		h = &Host{Users: map[string]*Account{}}
		c.Hosts[host] = h
	}
	if api != "" && api != DefaultAPI(host) {
		h.API = api
	} else if api == DefaultAPI(host) {
		h.API = ""
	}
	h.Users[user] = a
	h.User = user
	c.DefaultHost = host
}

// Logout removes a user from host. It returns the user that became active, if any.
func (c *Config) Logout(host, user string) (string, error) {
	h, ok := c.Hosts[host]
	if !ok {
		return "", fmt.Errorf("not logged in to %s", host)
	}
	if _, ok := h.Users[user]; !ok {
		return "", fmt.Errorf("not logged in to %s account %s", host, user)
	}
	delete(h.Users, user)
	if len(h.Users) == 0 {
		delete(c.Hosts, host)
		if c.DefaultHost == host {
			c.DefaultHost = ""
		}
		return "", nil
	}
	if h.User == user {
		h.User = h.UserNames()[0]
		return h.User, nil
	}
	return "", nil
}

// Switch makes user the active account of host.
func (c *Config) Switch(host, user string) error {
	h, ok := c.Hosts[host]
	if !ok {
		return fmt.Errorf("not logged in to %s", host)
	}
	if _, ok := h.Users[user]; !ok {
		return fmt.Errorf("not logged in to %s account %s", host, user)
	}
	h.User = user
	return nil
}
