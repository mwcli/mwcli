package sites

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mwcli/mwcli/internal/config"
)

// A host pattern such as *.wikipedia.org names every wiki whose host is one
// more label in front of the rest, so ko.wikipedia.org matches but
// ko.m.wikipedia.org and wikipedia.org do not. Its API is the entry's api with
// {host} replaced by the host, or https://HOST/w/api.php.

// IsPattern reports whether host is a host pattern.
func IsPattern(host string) bool { return strings.HasPrefix(host, "*.") }

// CheckPattern reports a pattern that is not one leading *. before a fixed
// part with a dot in it, which would take in too many hosts.
func CheckPattern(host string) error {
	rest := strings.TrimPrefix(host, "*.")
	if !IsPattern(host) || strings.Contains(rest, "*") || !strings.Contains(rest, ".") {
		return fmt.Errorf("host pattern %q: use one leading *. before a domain such as *.wikipedia.org", host)
	}
	return nil
}

// matches reports whether host is one label in front of the pattern's rest.
func matches(pattern, host string) bool {
	rest := strings.ToLower(strings.TrimPrefix(pattern, "*"))
	host = strings.ToLower(host)
	label, ok := strings.CutSuffix(host, rest)
	return ok && label != "" && !strings.Contains(label, ".")
}

// patternAPI is the API of host under the pattern entry w.
func patternAPI(w config.FarmSite, host string) string {
	if w.API == "" {
		return config.DefaultAPI(strings.ToLower(host))
	}
	return strings.ReplaceAll(w.API, "{host}", strings.ToLower(host))
}

type patternEntry struct {
	farm  string
	entry config.FarmSite
	kind  Kind
}

// patterns lists the host patterns of the user's farms, then of the built-in
// ones, each in name order, which is the order they are tried in.
func patterns(cfg *config.Config) []patternEntry {
	var out []patternEntry
	add := func(farms map[string][]config.FarmSite, kind Kind) {
		names := make([]string, 0, len(farms))
		for n := range farms {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			for _, w := range farms[n] {
				if IsPattern(w.Host) && CheckPattern(w.Host) == nil {
					out = append(out, patternEntry{farm: n, entry: w, kind: kind})
				}
			}
		}
	}
	if cfg != nil {
		add(cfg.Farms, Added)
	}
	add(builtinFarms, BuiltIn)
	return out
}

// byPattern resolves a host that a farm's pattern takes in. Its logins are the
// farm's.
func byPattern(cfg *config.Config, host string) (Resolved, bool) {
	for _, p := range patterns(cfg) {
		if matches(p.entry.Host, host) {
			host = strings.ToLower(host)
			return Resolved{Name: host, LoginKey: p.farm, Site: &config.Site{API: patternAPI(p.entry, host)}, Kind: p.kind}, true
		}
	}
	return Resolved{}, false
}

// patternForAPI resolves an API URL that is exactly the API a pattern gives
// its host, so that the farm's logins go only to the API the farm names.
func patternForAPI(cfg *config.Config, api string) (Resolved, bool) {
	host, err := config.HostOf(api)
	if err != nil {
		return Resolved{}, false
	}
	r, ok := byPattern(cfg, host)
	if !ok || r.Site.API != api {
		return Resolved{}, false
	}
	return r, true
}

// checkPatterns reports a host pattern of the user's farms that is too wide,
// rather than leave it out unnoticed.
func checkPatterns(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	for _, farm := range sortedFarmNames(cfg.Farms) {
		for _, w := range cfg.Farms[farm] {
			if strings.Contains(w.Host, "*") {
				if err := CheckPattern(w.Host); err != nil {
					return fmt.Errorf("farm %s in %s: %w", farm, cfg.Path(), err)
				}
			}
		}
	}
	return nil
}

// TemplateAPI is the API of a pattern entry with {host} left in, for lists.
func TemplateAPI(w config.FarmSite) string {
	if w.API == "" {
		return "https://{host}/w/api.php"
	}
	return w.API
}

// BuiltinPatterns returns the host patterns of the built-in farms, sorted.
func BuiltinPatterns() []config.FarmSite {
	var out []config.FarmSite
	for _, p := range patterns(nil) {
		out = append(out, p.entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}
