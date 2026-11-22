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
		Farms map[string][]config.FarmSite `yaml:"farms"`
	}
	if err := yaml.Unmarshal(builtinYAML, &f); err != nil {
		panic("builtin.yml: " + err.Error())
	}
	wikis := make(map[string]*config.Site)
	for _, farm := range f.Farms {
		for _, wiki := range farm {
			api := wiki.API
			if api == "" {
				api = config.DefaultAPI(wiki.Host)
			}
			wikis[strings.ToLower(wiki.Host)] = &config.Site{API: api, Aliases: wiki.Aliases}
		}
	}
	return wikis
}()

var builtinFarm = func() map[string]string {
	var f struct {
		Farms map[string][]config.FarmSite `yaml:"farms"`
	}
	if err := yaml.Unmarshal(builtinYAML, &f); err != nil {
		panic("builtin.yml: " + err.Error())
	}
	m := map[string]string{}
	for farm, wikis := range f.Farms {
		for _, wiki := range wikis {
			api := wiki.API
			if api == "" {
				api = config.DefaultAPI(wiki.Host)
			}
			m[api] = farm
		}
	}
	return m
}()

var builtinFarmAPI = func() map[string]string {
	var f struct {
		Farms map[string][]config.FarmSite `yaml:"farms"`
	}
	if err := yaml.Unmarshal(builtinYAML, &f); err != nil {
		panic("builtin.yml: " + err.Error())
	}
	m := map[string]string{}
	for farm, wikis := range f.Farms {
		if len(wikis) == 0 {
			continue
		}
		api := wikis[0].API
		if api == "" {
			api = config.DefaultAPI(wikis[0].Host)
		}
		m[farm] = api
	}
	return m
}()

// Builtin returns the built-in sites by name.
func Builtin() map[string]*config.Site { return builtin }

// IsFarmName reports whether name is reserved for a built-in or user farm.
func IsFarmName(cfg *config.Config, name string) bool {
	if _, ok := builtinFarmName[strings.ToLower(name)]; ok {
		return true
	}
	for farm := range cfg.Farms {
		if strings.EqualFold(farm, name) {
			return true
		}
	}
	return false
}

// FarmAPI returns a registered validation API for a farm login key.
func FarmAPI(cfg *config.Config, name string) string {
	for farm, wikis := range cfg.Farms {
		if strings.EqualFold(farm, name) && len(wikis) > 0 {
			api := wikis[0].API
			if api == "" {
				return config.DefaultAPI(wikis[0].Host)
			}
			return api
		}
	}
	for farm, api := range builtinFarmAPI {
		if strings.EqualFold(farm, name) {
			return api
		}
	}
	return ""
}

var builtinFarmName = func() map[string]struct{} {
	var f struct {
		Farms map[string][]config.FarmSite `yaml:"farms"`
	}
	if err := yaml.Unmarshal(builtinYAML, &f); err != nil {
		panic("builtin.yml: " + err.Error())
	}
	m := map[string]struct{}{}
	for farm := range f.Farms {
		m[strings.ToLower(farm)] = struct{}{}
	}
	return m
}()

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
	Name     string
	LoginKey string
	Site     *config.Site
	Kind     Kind
}

func resolved(cfg *config.Config, name string, site *config.Site, kind Kind) Resolved {
	key := name
	if kind == Unregistered {
		key = "api:" + site.API
		return Resolved{Name: name, LoginKey: key, Site: site, Kind: kind}
	}
	if cfg != nil {
		for farm, wikis := range cfg.Farms {
			for _, wiki := range wikis {
				api := wiki.API
				if api == "" {
					api = config.DefaultAPI(wiki.Host)
				}
				if api == site.API {
					key = farm
				}
			}
		}
	}
	if farm := builtinFarm[site.API]; farm != "" {
		key = farm
	}
	return Resolved{Name: name, LoginKey: key, Site: site, Kind: kind}
}

// Lookup resolves name: an added site by name or alias, a built-in site by name
// or alias, an added or built-in site whose API is on that host, else the host
// itself when name looks like one.
func Lookup(cfg *config.Config, name string) (Resolved, error) {
	name = strings.TrimSpace(name)
	if s, ok := cfg.Sites.Wikis[name]; ok {
		return resolved(cfg, name, s, Added), nil
	}
	if r, ok := byAlias(cfg.Sites.Wikis, name, Added); ok {
		return resolved(cfg, r.Name, r.Site, r.Kind), nil
	}
	if s, ok := builtin[strings.ToLower(name)]; ok {
		return resolved(cfg, strings.ToLower(name), s, BuiltIn), nil
	}
	if r, ok := byAlias(builtin, name, BuiltIn); ok {
		return resolved(cfg, r.Name, r.Site, r.Kind), nil
	}
	if r, ok := byFarm(cfg, name); ok {
		return r, nil
	}
	if !strings.Contains(name, ".") {
		return Resolved{}, fmt.Errorf("unknown site %q; add it with `mw site add %s URL`, or give a host name", name, name)
	}
	host := strings.ToLower(name)
	if r, ok := byHost(cfg.Sites.Wikis, host, Added); ok {
		return resolved(cfg, r.Name, r.Site, r.Kind), nil
	}
	if r, ok := byHost(builtin, host, BuiltIn); ok {
		return resolved(cfg, r.Name, r.Site, r.Kind), nil
	}
	return resolved(cfg, host, &config.Site{API: config.DefaultAPI(host)}, Unregistered), nil
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
	for _, farm := range sortedFarmNames(cfg.Farms) {
		for _, wiki := range cfg.Farms[farm] {
			endpoint := wiki.API
			if endpoint == "" {
				endpoint = config.DefaultAPI(wiki.Host)
			}
			if endpoint == api {
				return Resolved{Name: strings.ToLower(wiki.Host), LoginKey: farm, Site: &config.Site{API: endpoint, Aliases: wiki.Aliases}, Kind: Added}, nil
			}
		}
	}
	for _, src := range []struct {
		m    map[string]*config.Site
		kind Kind
	}{{cfg.Sites.Wikis, Added}, {builtin, BuiltIn}} {
		for _, n := range sortedNames(src.m) {
			if src.m[n].API == api {
				return resolved(cfg, n, src.m[n], src.kind), nil
			}
		}
	}
	name := strings.ToLower(host)
	if _, taken := cfg.Sites.Wikis[name]; taken {
		name = api
	} else if _, taken := builtin[name]; taken {
		name = api
	}
	return resolved(cfg, name, &config.Site{API: api}, Unregistered), nil
}

func sortedFarmNames(farms map[string][]config.FarmSite) []string {
	names := make([]string, 0, len(farms))
	for name := range farms {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func byAlias(m map[string]*config.Site, name string, kind Kind) (Resolved, bool) {
	for _, n := range sortedNames(m) {
		for _, a := range m[n].Aliases {
			if strings.EqualFold(a, name) {
				return resolved(nil, n, m[n], kind), true
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
	return resolved(nil, found[0], m[found[0]], kind), true
}

func sortedNames(m map[string]*config.Site) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func byFarm(cfg *config.Config, name string) (Resolved, bool) {
	for farm, wikis := range cfg.Farms {
		for _, wiki := range wikis {
			matched := strings.EqualFold(name, wiki.Host)
			for _, alias := range wiki.Aliases {
				matched = matched || strings.EqualFold(name, alias)
			}
			if matched {
				api := wiki.API
				if api == "" {
					api = config.DefaultAPI(wiki.Host)
				}
				return Resolved{Name: strings.ToLower(wiki.Host), LoginKey: farm, Site: &config.Site{API: api, Aliases: wiki.Aliases}, Kind: Added}, true
			}
		}
	}
	return Resolved{}, false
}
