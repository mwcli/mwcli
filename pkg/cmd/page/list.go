package page

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// listFields are the --json fields of `mw page list`, named as in the MediaWiki API.
var listFields = []string{"title", "pageid", "ns"}

func newCmdList(f *cmdutil.Factory) *cobra.Command {
	var wiki, prefix, category, search, linksTo, embeds string
	var namespace, limit int
	var jsonFlags *cmdutil.JSONFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pages",
		Long: heredoc.Doc(`
			List page titles, one per line. Without a source, all pages are listed in
			title order. Choose at most one source: --prefix, --category, --search,
			--links-to or --embeds. --namespace limits any of them to one namespace ID.
		`),
		Example: heredoc.Doc(`
			$ mw page list --prefix 'Sand'
			$ mw page list --category 'Living people' -L 100
			$ mw page list --search 'jq json' --json title,pageid
			$ mw page list --embeds 'Template:Infobox' --namespace 0
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jsonFlags.Validate(); err != nil {
				return err
			}
			opts := api.ListOptions{Source: api.AllPages, Limit: limit}
			var set []string
			for _, s := range []struct {
				flag   string
				source api.ListSource
				value  string
			}{
				{"prefix", api.AllPages, prefix},
				{"category", api.CategoryMembers, category},
				{"search", api.Search, search},
				{"links-to", api.LinksTo, linksTo},
				{"embeds", api.EmbeddedIn, embeds},
			} {
				if cmd.Flags().Changed(s.flag) {
					set = append(set, "--"+s.flag)
					opts.Source, opts.Value = s.source, s.value
				}
			}
			if len(set) > 1 {
				return cmdutil.FlagErrorf("specify only one of %s", strings.Join(set, ", "))
			}
			if len(set) == 1 && opts.Source != api.AllPages && strings.TrimSpace(opts.Value) == "" {
				return cmdutil.FlagErrorf("%s needs a value", set[0])
			}
			if limit < 1 {
				return cmdutil.FlagErrorf("invalid limit: %d", limit)
			}
			if cmd.Flags().Changed("namespace") {
				opts.Namespace = &namespace
			}
			ios := f.IOStreams
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			t, err := f.Resolve(cmd.Context(), cfg, wiki)
			if err != nil {
				return err
			}
			c, _, err := cmdutil.Client(cfg, t)
			if err != nil {
				return err
			}
			if opts.Source == api.CategoryMembers {
				if opts.Value, err = categoryTitle(cmd.Context(), c, opts.Value); err != nil {
					return err
				}
			}
			pages, err := c.ListPages(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if jsonFlags.Enabled() {
				return jsonFlags.Write(ios.Out, pages, ios.IsStdoutTTY())
			}
			if len(pages) == 0 && ios.IsStdoutTTY() {
				fmt.Fprintln(ios.ErrOut, "No pages found")
				return nil
			}
			for _, p := range pages {
				fmt.Fprintln(ios.Out, p.Title)
			}
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().StringVar(&prefix, "prefix", "", "List pages whose titles start with `text`")
	cmd.Flags().StringVar(&category, "category", "", "List the members of a `category`, with or without its namespace prefix")
	cmd.Flags().StringVarP(&search, "search", "S", "", "List the results of a full-text search for `query`")
	cmd.Flags().StringVar(&linksTo, "links-to", "", "List pages that link to `title`")
	cmd.Flags().StringVar(&embeds, "embeds", "", "List pages that transclude `title`, such as a template")
	cmd.Flags().IntVarP(&namespace, "namespace", "n", 0, "Limit the list to one namespace `ID`")
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of pages to list")
	jsonFlags = cmdutil.AddJSONFlags(cmd, listFields)
	return cmd
}

// categoryTitle returns the title of a category: name as it is when it starts
// with a name of the category namespace, local, canonical or an alias, else
// Category: followed by name, so that a category named "A: B" works without
// its prefix too.
func categoryTitle(ctx context.Context, c *api.Client, name string) (string, error) {
	name = strings.TrimSpace(name)
	prefix, _, ok := strings.Cut(name, ":")
	if !ok {
		return "Category:" + name, nil
	}
	names, err := c.NamespaceNames(ctx, 14)
	if err != nil {
		return "", err
	}
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " ")) }
	for _, n := range names {
		if n != "" && norm(n) == norm(prefix) {
			return name, nil
		}
	}
	return "Category:" + name, nil
}
