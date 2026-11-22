// Package sites resolves the names --wiki accepts to a site: a site the user
// added, a built-in site, or a host that was not added.
package sites

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/mwcli/mwcli/internal/config"
	"gopkg.in/yaml.v3"
)

//go:embed builtin.yml
var builtinYAML []byte

var builtin = func() map[string]*config.Site {
	var f struct {
		Wikis map[string]*config.Site `yaml:"wikis"`
	}
	if err := yaml.Unmarshal(builtinYAML, &f); err != nil {
		panic("builtin.yml: " + err.Error())
	}
	return f.Wikis
}()

// Builtin returns the built-in sites by name.
func Builtin() map[string]*config.Site { return builtin }

// Kind says where a resolved site comes from.
type Kind int

const (
	// Added is a site in config.yml.
	Added Kind = iota
	// BuiltIn is a site of builtin.yml.
	BuiltIn
	// Unregistered is a host that is neither, used at https://HOST/w/api.php.
	Unregistered
)

// Resolved is a site and its name. Logins are stored under Name.
type Resolved struct {
	Name string
	Site *config.Site
	Kind Kind
}

// Lookup resolves name: an added site by name or alias, a built-in site by name
// or alias, an added or built-in site whose API is on that host, else the host
// itself when name looks like one.
func Lookup(cfg *config.Config, name string) (Resolved, error) {
	name = strings.TrimSpace(name)
	if s, ok := cfg.Sites.Wikis[name]; ok {
		return Resolved{name, s, Added}, nil
	}
	if r, ok := byAlias(cfg.Sites.Wikis, name, Added); ok {
		return r, nil
	}
	if s, ok := builtin[strings.ToLower(name)]; ok {
		return Resolved{strings.ToLower(name), s, BuiltIn}, nil
	}
	if r, ok := byAlias(builtin, name, BuiltIn); ok {
		return r, nil
	}
	if !strings.Contains(name, ".") {
		return Resolved{}, fmt.Errorf("unknown site %q; add it with `mw site add %s URL`, or give a host name", name, name)
	}
	host := strings.ToLower(name)
	if r, ok := byHost(cfg.Sites.Wikis, host, Added); ok {
		return r, nil
	}
	if r, ok := byHost(builtin, host, BuiltIn); ok {
		return r, nil
	}
	return Resolved{host, &config.Site{API: config.DefaultAPI(host)}, Unregistered}, nil
}

// ForAPI resolves an API URL: the added or built-in site with that API, else
// an unregistered site named after its host. When a site of that name exists
// with another API, such as another path of the host, the unregistered site is
// named by the URL itself, which no login can have, so the logins of the
// existing site are never sent to it.
func ForAPI(cfg *config.Config, api string) (Resolved, error) {
	host, err := config.HostOf(api)
	if err != nil {
		return Resolved{}, err
	}
	for _, src := range []struct {
		m    map[string]*config.Site
		kind Kind
	}{{cfg.Sites.Wikis, Added}, {builtin, BuiltIn}} {
		for _, n := range sortedNames(src.m) {
			if src.m[n].API == api {
				return Resolved{n, src.m[n], src.kind}, nil
			}
		}
	}
	name := strings.ToLower(host)
	if _, taken := cfg.Sites.Wikis[name]; taken {
		name = api
	} else if _, taken := builtin[name]; taken {
		name = api
	}
	return Resolved{name, &config.Site{API: api}, Unregistered}, nil
}

func byAlias(m map[string]*config.Site, name string, kind Kind) (Resolved, bool) {
	for _, n := range sortedNames(m) {
		for _, a := range m[n].Aliases {
			if strings.EqualFold(a, name) {
				return Resolved{n, m[n], kind}, true
			}
		}
	}
	return Resolved{}, false
}

// byHost finds the one site whose API is on host. Two sites on one host, such
// as wikis under different paths, are ambiguous and match neither.
func byHost(m map[string]*config.Site, host string, kind Kind) (Resolved, bool) {
	var found []string
	for _, n := range sortedNames(m) {
		if h, err := config.HostOf(m[n].API); err == nil && strings.EqualFold(h, host) {
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		return Resolved{}, false
	}
	return Resolved{found[0], m[found[0]], kind}, true
}

func sortedNames(m map[string]*config.Site) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
