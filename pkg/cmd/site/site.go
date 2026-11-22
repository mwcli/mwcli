// Package site implements `mw site`.
package site

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/internal/sites"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdSite returns `mw site`.
func NewCmdSite(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site <command>",
		Short: "Add, choose and show wikis",
		Long: heredoc.Doc(`
			A site is one wiki: a name and its api.php. --wiki takes a site name, an alias
			or a host. Built-in sites, such as en.wikipedia.org (alias wikipedia) and
			commons.wikimedia.org (alias commons), need no adding; any other host is used
			at https://HOST/w/api.php until it is added.
		`),
	}
	cmd.AddCommand(newCmdAdd(f), newCmdList(f), newCmdView(f), newCmdRemove(f), newCmdDefault(f))
	return cmd
}

func newCmdAdd(f *cmdutil.Factory) *cobra.Command {
	var aliases []string
	var farmFlag string
	cmd := &cobra.Command{
		Use:   "add [<name>] <wiki> [--farm FARM]",
		Short: "Add a wiki",
		Long: heredoc.Doc(`
			Find the api.php of a wiki and add it as a site. The wiki is a host name
			(https is assumed), the URL of any page of the wiki, which tells apart wikis
			under different paths of one host, or the URL of its api.php, which is only
			checked. The name is the host unless given. An explicitly named site is one
			farm with one wiki by default. Use --farm to deliberately add another wiki to
			an existing farm and share its login.
		`),
		Example: heredoc.Doc(`
			$ mw site add wiki.example.org
			$ mw site add docs https://wiki.example.org/docs/index.php
			$ mw site add intra http://intranet.example/w/api.php --alias in
			$ mw site add handbook https://docs.example.org/w/api.php --farm docs
		`),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ios := f.IOStreams
			target := args[len(args)-1]
			name := hostOf(target)
			if len(args) == 2 {
				name = args[0]
			}
			if !config.ValidSiteName(name) {
				return cmdutil.FlagErrorf("invalid site name %q: use no spaces or /", name)
			}
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if farmFlag == "" && sites.IsBuiltinFarmName(name) {
				return cmdutil.FlagErrorf("site name %q conflicts with a farm name", name)
			}
			if farmFlag != "" && !config.ValidSiteName(farmFlag) {
				return cmdutil.FlagErrorf("invalid farm name %q", farmFlag)
			}
			for _, a := range aliases {
				if !config.ValidSiteName(a) {
					return cmdutil.FlagErrorf("invalid alias %q", a)
				}
			}
			s, err := f.Find(cmd.Context(), target)
			if err != nil {
				return err
			}
			farmName := name
			if farmFlag != "" {
				var ok bool
				farmName, ok = configuredFarmName(cfg, farmFlag)
				if !ok {
					return cmdutil.FlagErrorf("farm %q is not configured; add a site without --farm first", farmFlag)
				}
				if len(args) == 2 && !strings.EqualFold(name, hostOf(s.API)) {
					aliases = append(aliases, name)
				}
			} else if existingName, ok := configuredFarmName(cfg, farmName); ok {
				farmName = existingName
				endpointExists := false
				for _, previous := range cfg.Farms[farmName] {
					if farmAPI(previous) == s.API {
						endpointExists = true
						break
					}
				}
				if !endpointExists {
					return cmdutil.FlagErrorf("farm %q already exists; use --farm %q to explicitly share its login, or choose a different name", farmName, farmName)
				}
			}
			for otherFarm, wikis := range cfg.Farms {
				if strings.EqualFold(otherFarm, farmName) {
					continue
				}
				for _, wiki := range wikis {
					if farmAPI(wiki) == s.API {
						return cmdutil.FlagErrorf("API %s is already registered in farm %q", s.API, otherFarm)
					}
				}
			}
			s.Aliases = sortedUnique(aliases)
			farm := cfg.Farms[farmName]
			replaced := false
			entry := config.FarmSite{Host: hostOf(s.API), API: s.API, Aliases: s.Aliases, Server: s.Server, Script: s.Script, ArticlePath: s.ArticlePath}
			for i, previous := range farm {
				oldAPI := previous.API
				if oldAPI == "" {
					oldAPI = config.DefaultAPI(previous.Host)
				}
				if oldAPI == s.API {
					farm[i] = entry
					replaced = true
					break
				}
			}
			if !replaced {
				farm = append(farm, entry)
			}
			cfg.Farms[farmName] = farm
			if err := cfg.Save(); err != nil {
				return err
			}
			verb := "Added"
			if replaced {
				verb = "Updated"
			}
			fmt.Fprintf(ios.ErrOut, "%s %s site %s (%s)\n", ios.SuccessIcon(), verb, ios.Bold(name), s.API)
			if farmFlag != "" && !replaced {
				fmt.Fprintf(ios.ErrOut, "  Added to farm %s; its login is shared with the other wikis in that farm\n", ios.Bold(farmName))
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&aliases, "alias", nil, "Another `name` for the site; repeat or separate with commas")
	cmd.Flags().StringVar(&farmFlag, "farm", "", "Add this wiki to an existing `farm` and share its login")
	return cmd
}

func newCmdList(f *cmdutil.Factory) *cobra.Command {
	var builtin, cached bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List added wikis",
		Long: heredoc.Doc(`
			List the added sites, the default one marked with *. --builtin lists the
			built-in sites instead. --cached lists the wikis given by host or URL whose
			API mw found and keeps in its cache, with when it found each.
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ios := f.IOStreams
			if builtin && cached {
				return cmdutil.FlagErrorf("specify only one of --builtin and --cached")
			}
			if cached {
				return listCached(f)
			}
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			defer func() {
				if n := len(f.APICache().Entries); n > 0 && !builtin && ios.IsStdoutTTY() {
					fmt.Fprintf(ios.ErrOut, "mw also keeps the API of %d wiki(s) not added; list them with: mw site list --cached\n", n)
				}
			}()
			entries := listedSites(cfg, builtin)
			if len(entries) == 0 {
				if ios.IsStdoutTTY() {
					fmt.Fprintln(ios.ErrOut, "No sites added. Add one with: mw site add WIKI")
				}
				return nil
			}
			width := 0
			for _, entry := range entries {
				width = max(width, len(entry.Name))
			}
			var contextAPI string
			if cfg.Context != "" {
				if current, err := sites.Lookup(cfg, cfg.Context); err == nil {
					contextAPI = current.Site.API
				}
			}
			for _, entry := range entries {
				mark := " "
				if !builtin && contextAPI != "" && contextAPI == entry.Site.API {
					mark = "*"
				}
				line := fmt.Sprintf("%s %-*s  %s", mark, width, entry.Name, entry.Site.API)
				if len(entry.Aliases) > 0 {
					aliases := append([]string(nil), entry.Aliases...)
					aliases = withoutName(aliases, entry.Name)
					sort.Strings(aliases)
					if len(aliases) == 0 {
						fmt.Fprintln(ios.Out, strings.TrimRight(line, " "))
						continue
					}
					line += "  (" + strings.Join(aliases, ", ") + ")"
				}
				fmt.Fprintln(ios.Out, strings.TrimRight(line, " "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&builtin, "builtin", false, "List the built-in sites")
	cmd.Flags().BoolVar(&cached, "cached", false, "List the wikis whose API mw found and keeps in its cache")
	return cmd
}

func listCached(f *cmdutil.Factory) error {
	ios := f.IOStreams
	c := f.APICache()
	if len(c.Entries) == 0 {
		if ios.IsStdoutTTY() {
			fmt.Fprintln(ios.ErrOut, "No APIs cached")
		}
		return nil
	}
	rows := [][]string{}
	for _, k := range c.Keys() {
		e := c.Entries[k]
		rows = append(rows, []string{k, e.API, e.Found.Format("2006-01-02")})
	}
	return cmdutil.WriteTable(ios.Out, rows)
}

func withoutName(names []string, name string) []string {
	filtered := names[:0]
	for _, candidate := range names {
		if !strings.EqualFold(candidate, name) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func newCmdRemove(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an added wiki",
		Long: heredoc.Doc(`
			Remove an added wiki by its listed name, an alias, its host or its exact API URL.
			When one host has multiple API endpoints, use an alias or the exact API URL.
			Farm logins are removed only when the last wiki in that farm is removed.
			A name listed by --cached forgets that cached API instead.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ios := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			name := args[0]
			farm, index, removedSite, err := findFarmSite(cfg, name)
			if err != nil {
				return cmdutil.FlagErrorf("%v", err)
			}
			if farm == "" {
				if forgot, err := f.APICache().Delete(name); err != nil {
					return err
				} else if forgot {
					fmt.Fprintf(ios.ErrOut, "%s Forgot the cached API of %s\n", ios.SuccessIcon(), ios.Bold(name))
					return nil
				}
				return fmt.Errorf("no site named %s was added", name)
			}
			defaultPointsToRemoved := cfg.Context == name || cfg.Context == removedSite.API
			if cfg.Context != "" && !defaultPointsToRemoved {
				if current, err := sites.Lookup(cfg, cfg.Context); err == nil {
					defaultPointsToRemoved = current.Site.API == removedSite.API
				}
			}
			wikis := cfg.Farms[farm]
			wikis = append(wikis[:index], wikis[index+1:]...)
			if len(wikis) == 0 {
				delete(cfg.Farms, farm)
			} else {
				cfg.Farms[farm] = wikis
			}
			hadLogin := false
			if len(wikis) == 0 {
				_, hadLogin = cfg.Logins[farm]
				delete(cfg.Logins, farm)
			}
			if defaultPointsToRemoved {
				cfg.Context = ""
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(ios.ErrOut, "%s Removed site %s\n", ios.SuccessIcon(), ios.Bold(name))
			if hadLogin {
				fmt.Fprintf(ios.ErrOut, "%s Logged out of %s; this does not revoke tokens or bot passwords on the wiki\n", ios.SuccessIcon(), name)
			}
			return nil
		},
	}
}

func newCmdDefault(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "default [<name>]",
		Short: "Show or set the default wiki",
		Long:  "Print the default site, or make a site the default for commands without --wiki. The name may be an added site, a built-in one, an alias or a host.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ios := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				fmt.Fprintln(ios.Out, cmdutil.SiteName(cfg, ""))
				return nil
			}
			r, err := sites.Lookup(cfg, args[0])
			if err != nil {
				return cmdutil.FlagErrorf("%v", err)
			}
			cfg.Context = r.Name
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(ios.ErrOut, "%s Default site is now %s\n", ios.SuccessIcon(), ios.Bold(r.Name))
			return nil
		},
	}
}

// hostOf returns the host of a wiki given as a host or a URL.
func hostOf(target string) string {
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return target
}

type listedSite struct {
	Name    string
	Site    *config.Site
	Aliases []string
}

func listedSites(cfg *config.Config, builtin bool) []listedSite {
	if builtin {
		m := sites.Builtin()
		entries := make([]listedSite, 0, len(m))
		for name, site := range m {
			entries = append(entries, listedSite{Name: name, Site: site, Aliases: site.Aliases})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		for _, p := range sites.BuiltinPatterns() {
			entries = append(entries, listedSite{Name: p.Host, Site: &config.Site{API: sites.TemplateAPI(p)}})
		}
		return entries
	}
	var entries []listedSite
	for farm, wikis := range cfg.Farms {
		for _, wiki := range wikis {
			api := farmAPI(wiki)
			if sites.IsPattern(wiki.Host) {
				api = sites.TemplateAPI(wiki)
			}
			aliases := sortedUnique(wiki.Aliases)
			name := displayName(cfg, farm, wikis, wiki, aliases, api)
			s := &config.Site{API: api, Aliases: aliases, Server: wiki.Server, Script: wiki.Script, ArticlePath: wiki.ArticlePath}
			entries = append(entries, listedSite{Name: name, Site: s, Aliases: aliases})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == entries[j].Name {
			return entries[i].Site.API < entries[j].Site.API
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func findFarmSite(cfg *config.Config, name string) (string, int, config.FarmSite, error) {
	var matches []struct {
		farm string
		idx  int
		wiki config.FarmSite
	}
	apiName := strings.Contains(name, "://")
	for farm, wikis := range cfg.Farms {
		for i, wiki := range wikis {
			endpoint := farmAPI(wiki)
			matched := apiName && name == endpoint
			if !apiName {
				matched = strings.EqualFold(name, wiki.Host) || len(wikis) == 1 && strings.EqualFold(name, farm)
			}
			if matched {
				matches = append(matches, struct {
					farm string
					idx  int
					wiki config.FarmSite
				}{farm, i, wiki})
				continue
			}
			if !apiName {
				for _, alias := range wiki.Aliases {
					if strings.EqualFold(name, alias) {
						matches = append(matches, struct {
							farm string
							idx  int
							wiki config.FarmSite
						}{farm, i, wiki})
						break
					}
				}
			}
		}
	}
	if len(matches) > 1 {
		return "", -1, config.FarmSite{}, fmt.Errorf("%q matches multiple wikis; use an alias or exact API URL", name)
	}
	if len(matches) == 1 {
		m := matches[0]
		return m.farm, m.idx, m.wiki, nil
	}
	return "", -1, config.FarmSite{}, nil
}

func farmAPI(wiki config.FarmSite) string {
	if wiki.API != "" {
		return wiki.API
	}
	return config.DefaultAPI(wiki.Host)
}

func countFarmHost(wikis []config.FarmSite, host string) int {
	count := 0
	for _, wiki := range wikis {
		if strings.EqualFold(wiki.Host, host) {
			count++
		}
	}
	return count
}

func displayName(cfg *config.Config, farm string, wikis []config.FarmSite, wiki config.FarmSite, aliases []string, api string) string {
	candidates := append([]string(nil), aliases...)
	if len(wikis) == 1 {
		candidates = append([]string{farm}, candidates...)
	} else if countFarmHost(wikis, wiki.Host) == 1 {
		candidates = append(candidates, wiki.Host)
	}
	for _, candidate := range candidates {
		matchedFarm, _, matched, err := findFarmSite(cfg, candidate)
		if err == nil && matchedFarm != "" && farmAPI(matched) == api {
			return candidate
		}
	}
	return api
}

func configuredFarmName(cfg *config.Config, name string) (string, bool) {
	for farm := range cfg.Farms {
		if strings.EqualFold(farm, name) {
			return farm, true
		}
	}
	return "", false
}

func sortedUnique(names []string) []string {
	seen := map[string]bool{}
	var unique []string
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
