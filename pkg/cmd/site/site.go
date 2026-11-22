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
		Short: "Add and choose wikis",
		Long: heredoc.Doc(`
			A site is one wiki: a name and its api.php. --wiki takes a site name, an alias
			or a host. Built-in sites, such as en.wikipedia.org (alias wikipedia) and
			commons.wikimedia.org (alias commons), need no adding; any other host is used
			at https://HOST/w/api.php until it is added.
		`),
	}
	cmd.AddCommand(newCmdAdd(f), newCmdList(f), newCmdRemove(f), newCmdDefault(f))
	return cmd
}

func newCmdAdd(f *cmdutil.Factory) *cobra.Command {
	var aliases []string
	cmd := &cobra.Command{
		Use:   "add [<name>] <wiki>",
		Short: "Add a wiki",
		Long: heredoc.Doc(`
			Find the api.php of a wiki and add it as a site. The wiki is a host name
			(https is assumed), the URL of any page of the wiki, which tells apart wikis
			under different paths of one host, or the URL of its api.php, which is only
			checked. The name is the host unless given.
		`),
		Example: heredoc.Doc(`
			$ mw site add wiki.example.org
			$ mw site add docs https://wiki.example.org/docs/index.php
			$ mw site add intra http://intranet.example/w/api.php --alias in
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
			if sites.IsBuiltinFarmName(name) {
				return cmdutil.FlagErrorf("site name %q conflicts with a farm name", name)
			}
			for _, a := range aliases {
				if !config.ValidSiteName(a) {
					return cmdutil.FlagErrorf("invalid alias %q", a)
				}
			}
			s, err := f.Find(target)
			if err != nil {
				return err
			}
			s.Aliases = aliases
			sort.Strings(s.Aliases)
			farm := cfg.Farms[name]
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
			cfg.Farms[name] = farm
			if err := cfg.Save(); err != nil {
				return err
			}
			verb := "Added"
			if replaced {
				verb = "Updated"
			}
			fmt.Fprintf(ios.ErrOut, "%s %s site %s (%s)\n", ios.SuccessIcon(), verb, ios.Bold(name), s.API)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&aliases, "alias", nil, "Another `name` for the site; repeat or separate with commas")
	return cmd
}

func newCmdList(f *cmdutil.Factory) *cobra.Command {
	var builtin bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List added wikis",
		Long:  "List the added sites, the default one marked with *. --builtin lists the built-in sites instead.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ios := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			entries := listedSites(cfg, builtin)
			names := make([]string, 0, len(entries))
			for name := range entries {
				names = append(names, name)
			}
			sort.Strings(names)
			if len(names) == 0 {
				if ios.IsStdoutTTY() {
					fmt.Fprintln(ios.ErrOut, "No sites added. Add one with: mw site add WIKI")
				}
				return nil
			}
			width := 0
			for _, n := range names {
				width = max(width, len(n))
			}
			for _, n := range names {
				site := entries[n]
				mark := " "
				if !builtin && cfg.Context != "" {
					if cfg.Context == n || cfg.Context == site.API || strings.EqualFold(cfg.Context, hostOf(site.API)) || containsName(site.Aliases, cfg.Context) {
						mark = "*"
					}
				}
				line := fmt.Sprintf("%s %-*s  %s", mark, width, n, site.API)
				if len(site.Aliases) > 0 {
					aliases := append([]string(nil), site.Aliases...)
					sort.Strings(aliases)
					line += "  (" + strings.Join(aliases, ", ") + ")"
				}
				fmt.Fprintln(ios.Out, strings.TrimRight(line, " "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&builtin, "builtin", false, "List the built-in sites")
	return cmd
}

func containsName(names []string, name string) bool {
	for _, candidate := range names {
		if strings.EqualFold(candidate, name) {
			return true
		}
	}
	return false
}

func newCmdRemove(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an added wiki and its logins",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ios := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			name := args[0]
			farm, index, removedSite := findFarmSite(cfg, name)
			if farm == "" {
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

func listedSites(cfg *config.Config, builtin bool) map[string]*config.Site {
	if builtin {
		return sites.Builtin()
	}
	m := map[string]*config.Site{}
	for farm, wikis := range cfg.Farms {
		for _, wiki := range wikis {
			name := wiki.Host
			if len(wikis) == 1 {
				name = farm
			}
			if name == "" {
				name = farm
			}
			s := &config.Site{API: wiki.API, Aliases: wiki.Aliases, Server: wiki.Server, Script: wiki.Script, ArticlePath: wiki.ArticlePath}
			if s.API == "" {
				s.API = config.DefaultAPI(wiki.Host)
			}
			m[name] = s
		}
	}
	return m
}

func findFarmSite(cfg *config.Config, name string) (string, int, config.FarmSite) {
	for farm, wikis := range cfg.Farms {
		for i, wiki := range wikis {
			if strings.EqualFold(name, farm) && len(wikis) == 1 || strings.EqualFold(name, wiki.Host) {
				return farm, i, wiki
			}
			for _, alias := range wiki.Aliases {
				if strings.EqualFold(name, alias) {
					return farm, i, wiki
				}
			}
		}
	}
	return "", -1, config.FarmSite{}
}
