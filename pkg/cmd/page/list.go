package page

import (
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
	var wiki, apiURL, prefix, category, search, linksTo, embeds string
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
				{"category", api.CategoryMembers, categoryTitle(category)},
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
			t, err := f.Resolve(cfg, wiki, apiURL)
			if err != nil {
				return err
			}
			c, _, err := cmdutil.Client(cfg, t)
			if err != nil {
				return err
			}
			pages, err := c.ListPages(opts)
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
	cmdutil.AddTargetFlags(cmd, &wiki, &apiURL)
	cmd.Flags().StringVar(&prefix, "prefix", "", "List pages whose titles start with `text`")
	cmd.Flags().StringVar(&category, "category", "", "List the members of a `category`, with or without the Category: prefix")
	cmd.Flags().StringVarP(&search, "search", "S", "", "List the results of a full-text search for `query`")
	cmd.Flags().StringVar(&linksTo, "links-to", "", "List pages that link to `title`")
	cmd.Flags().StringVar(&embeds, "embeds", "", "List pages that transclude `title`, such as a template")
	cmd.Flags().IntVarP(&namespace, "namespace", "n", 0, "Limit the list to one namespace `ID`")
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of pages to list")
	jsonFlags = cmdutil.AddJSONFlags(cmd, listFields)
	return cmd
}

// categoryTitle adds the canonical Category: prefix unless the name already
// has a namespace prefix, such as a local one.
func categoryTitle(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, ":") {
		return name
	}
	return "Category:" + name
}
