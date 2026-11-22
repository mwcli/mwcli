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
			if sites.IsFarmName(cfg, name) {
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
			previous, replaced := cfg.Sites.Wikis[name]
			if replaced && previous.API != s.API {
				if _, scopedByFarm := loginKeyForAPI(cfg, previous.API); !scopedByFarm {
					delete(cfg.Logins, name)
				}
			}
			cfg.Sites.Wikis[name] = s
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
			m := cfg.Sites.Wikis
			if builtin {
				m = sites.Builtin()
			}
			names := sortedNames(m)
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
				mark := " "
				if !builtin && n == cfg.Sites.Default {
					mark = "*"
				}
				line := fmt.Sprintf("%s %-*s  %s", mark, width, n, m[n].API)
				if len(m[n].Aliases) > 0 {
					aliases := append([]string(nil), m[n].Aliases...)
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
			if _, ok := cfg.Sites.Wikis[name]; !ok {
				return fmt.Errorf("no site named %s was added", name)
			}
			delete(cfg.Sites.Wikis, name)
			_, hadLogin := cfg.Logins[name]
			delete(cfg.Logins, name)
			if cfg.Sites.Default == name {
				cfg.Sites.Default = ""
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
			cfg.Sites.Default = r.Name
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

func sortedNames(m map[string]*config.Site) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func loginKeyForAPI(cfg *config.Config, api string) (string, bool) {
	for farm, wikis := range cfg.Farms {
		for _, wiki := range wikis {
			endpoint := wiki.API
			if endpoint == "" {
				endpoint = config.DefaultAPI(wiki.Host)
			}
			if endpoint == api {
				return farm, true
			}
		}
	}
	return "", false
}
