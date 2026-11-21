// Package interwiki maps interwiki prefixes such as "commons" to wikis.
package interwiki

import (
	"net/url"
	"sort"
	"strings"
)

// prefixes are the entries of MediaWiki core maintenance/interwiki.list
// (master 37b526c152, 2026-07-07) that name an API URL, i.e. MediaWiki wikis.
// hrwiki is left out because its API did not answer on 2026-11-21.
//
// To regenerate:
//
//	curl -s https://raw.githubusercontent.com/wikimedia/mediawiki/master/maintenance/interwiki.list |
//	  awk -F'|' '!/^#/ && $4 != "" {print $1, $4}'
//
// and keep the entries whose API answers `mw api --api URL action=query meta=siteinfo`.
var prefixes = map[string]string{
	"commons":       "https://commons.wikimedia.org/w/api.php",
	"mediawikiwiki": "https://www.mediawiki.org/w/api.php",
	"memoryalpha":   "https://memory-alpha.fandom.com/api.php",
	"metawikimedia": "https://meta.wikimedia.org/w/api.php",
	"mozillawiki":   "https://wiki.mozilla.org/api.php",
	"mw":            "https://www.mediawiki.org/w/api.php",
	"shoutwiki":     "https://www.shoutwiki.com/w/api.php",
	"tmbw":          "https://tmbw.net/wiki/api.php",
	"uncyclopedia":  "https://en.uncyclopedia.co/w/api.php",
	"wikibooks":     "https://en.wikibooks.org/w/api.php",
	"wikidata":      "https://www.wikidata.org/w/api.php",
	"wikihow":       "https://www.wikihow.com/api.php",
	"wikimedia":     "https://foundation.wikimedia.org/w/api.php",
	"wikipedia":     "https://en.wikipedia.org/w/api.php",
	"wikiquote":     "https://en.wikiquote.org/w/api.php",
	"wikisource":    "https://wikisource.org/w/api.php",
	"wikispecies":   "https://species.wikimedia.org/w/api.php",
	"wikiversity":   "https://en.wikiversity.org/w/api.php",
	"wikivoyage":    "https://en.wikivoyage.org/w/api.php",
	"wikt":          "https://en.wiktionary.org/w/api.php",
	"wiktionary":    "https://en.wiktionary.org/w/api.php",
}

// Lookup returns the host and API URL of an interwiki prefix. Names with a dot
// are host names, never prefixes.
func Lookup(name string) (host, api string, ok bool) {
	if strings.Contains(name, ".") {
		return "", "", false
	}
	api, ok = prefixes[strings.ToLower(name)]
	if !ok {
		return "", "", false
	}
	u, err := url.Parse(api)
	if err != nil {
		return "", "", false
	}
	return u.Host, api, true
}

// APIForHost returns the API URL a prefix gives for host, if any.
func APIForHost(host string) (string, bool) {
	for _, p := range All() {
		if p.Host == host {
			return p.API, true
		}
	}
	return "", false
}

// Prefix is one known prefix.
type Prefix struct{ Name, Host, API string }

// All returns the known prefixes sorted by name.
func All() []Prefix {
	var all []Prefix
	for name := range prefixes {
		host, api, _ := Lookup(name)
		all = append(all, Prefix{name, host, api})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all
}
