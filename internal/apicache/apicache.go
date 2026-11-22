// Package apicache remembers the api.php that discovery found for a host or a
// page URL that was not added as a site, so that later commands do not look
// for it again. It lives in the cache directory, apart from config.yml, and
// holds no credentials.
package apicache

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mwcli/mwcli/internal/config"
	"gopkg.in/yaml.v3"
)

const file = "apis.yml"

// Entry is a site discovery found and when.
type Entry struct {
	API         string    `yaml:"api"`
	Server      string    `yaml:"server,omitempty"`
	Script      string    `yaml:"script,omitempty"`
	ArticlePath string    `yaml:"articlepath,omitempty"`
	Found       time.Time `yaml:"found"`
}

// Site returns the entry as a site.
func (e Entry) Site() *config.Site {
	return &config.Site{API: e.API, Server: e.Server, Script: e.Script, ArticlePath: e.ArticlePath}
}

// Cache maps a host or page URL to what discovery found for it.
type Cache struct {
	Dir     string           `yaml:"-"`
	Entries map[string]Entry `yaml:"apis,omitempty"`
}

// Dir returns the cache directory: MW_CACHE_DIR, else mw under the user cache
// directory ($XDG_CACHE_HOME or ~/.cache on Linux, ~/Library/Caches on macOS,
// %LocalAppData% on Windows).
func Dir() string {
	if d := os.Getenv("MW_CACHE_DIR"); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "mw")
	}
	return filepath.Join(config.Dir(), "cache")
}

// Path is the path of the cache file.
func (c *Cache) Path() string { return filepath.Join(c.Dir, file) }

// Load reads the cache. A missing or unreadable file is an empty cache: the
// cache only saves requests.
func Load() *Cache {
	c := &Cache{Dir: Dir()}
	if b, err := os.ReadFile(c.Path()); err == nil {
		_ = yaml.Unmarshal(b, c)
	}
	if c.Entries == nil {
		c.Entries = map[string]Entry{}
	}
	return c
}

// Key normalizes a host or page URL: lowercase scheme and host, no fragment.
// The query stays, since a query such as ?wiki=a can pick the wiki.
func Key(target string) string {
	target = strings.TrimSpace(target)
	if !strings.Contains(target, "://") {
		return strings.ToLower(target)
	}
	if i := strings.Index(target, "#"); i >= 0 {
		target = target[:i]
	}
	scheme, rest, _ := strings.Cut(target, "://")
	host, path, _ := strings.Cut(rest, "/")
	return strings.ToLower(scheme) + "://" + strings.ToLower(host) + "/" + path
}

// Get returns the entry for target and its key: the entry of target itself,
// else, for a page URL without a query, the entry whose page URL prefix it
// starts with. A URL with a query matches only itself, as the query may pick
// another wiki under the same path.
func (c *Cache) Get(target string) (Entry, string, bool) {
	k := Key(target)
	if e, ok := c.Entries[k]; ok {
		return e, k, true
	}
	if strings.Contains(k, "?") {
		return Entry{}, "", false
	}
	best := ""
	for p := range c.Entries {
		if strings.HasSuffix(p, "/") && strings.Contains(p, "://") && strings.HasPrefix(k, p) && len(p) > len(best) {
			best = p
		}
	}
	if best == "" {
		return Entry{}, "", false
	}
	return c.Entries[best], best, true
}

// Put stores site for target and saves the cache. A page URL is stored by the
// prefix of the wiki's page URLs, such as https://wiki.example.org/wiki/, so
// that other pages of the wiki find the entry too.
func (c *Cache) Put(target string, s *config.Site) error {
	k := Key(target)
	if p := pagePrefix(k, s); p != "" && strings.HasPrefix(k, p) && !strings.Contains(k, "?") {
		k = p
	}
	c.Entries[k] = Entry{API: s.API, Server: s.Server, Script: s.Script, ArticlePath: s.ArticlePath, Found: time.Now().UTC().Truncate(time.Second)}
	return c.save()
}

// pagePrefix is the key of the page URLs of site as key, a page URL, names
// them: its scheme and host, which may differ from the wiki's server, and the
// article path up to $1.
func pagePrefix(key string, s *config.Site) string {
	i := strings.Index(s.ArticlePath, "$1")
	scheme, rest, ok := strings.Cut(key, "://")
	if !ok || i < 0 || !strings.HasPrefix(s.ArticlePath, "/") {
		return ""
	}
	host, _, _ := strings.Cut(rest, "/")
	host, _, _ = strings.Cut(host, "?")
	p := Key(scheme + "://" + host + s.ArticlePath[:i])
	if !strings.HasSuffix(p, "/") {
		return ""
	}
	return p
}

// Delete forgets target and saves the cache. It reports whether there was an
// entry.
func (c *Cache) Delete(target string) (bool, error) {
	k := Key(target)
	if _, ok := c.Entries[k]; !ok {
		return false, nil
	}
	delete(c.Entries, k)
	return true, c.save()
}

// Keys returns the cached targets in sorted order.
func (c *Cache) Keys() []string {
	keys := make([]string, 0, len(c.Entries))
	for k := range c.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (c *Cache) save() error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.Dir, "."+file+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.Path())
}
