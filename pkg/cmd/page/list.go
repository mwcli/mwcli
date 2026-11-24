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

// listFlags are the sources of pages `mw page list` and `mw page download` share.
type listFlags struct {
	prefix, category, search, linksTo, embeds string
	namespace, limit                          int
}

func (l *listFlags) add(cmd *cobra.Command, limitUsage string) {
	cmd.Flags().StringVar(&l.prefix, "prefix", "", "Pages whose titles start with `text`")
	cmd.Flags().StringVar(&l.category, "category", "", "Members of a `category`, with or without its namespace prefix")
	cmd.Flags().StringVarP(&l.search, "search", "S", "", "Results of a full-text search for `query`")
	cmd.Flags().StringVar(&l.linksTo, "links-to", "", "Pages that link to `title`")
	cmd.Flags().StringVar(&l.embeds, "embeds", "", "Pages that transclude `title`, such as a template")
	cmd.Flags().IntVarP(&l.namespace, "namespace", "n", 0, "Limit the pages to one namespace `ID`")
	cmd.Flags().IntVarP(&l.limit, "limit", "L", 30, limitUsage)
}

// options checks the flags and returns the list they ask for; set reports
// whether a source flag or --namespace was given.
func (l *listFlags) options(cmd *cobra.Command) (opts api.ListOptions, set bool, err error) {
	opts = api.ListOptions{Source: api.AllPages, Limit: l.limit}
	var sources []string
	for _, s := range []struct {
		flag   string
		source api.ListSource
		value  string
	}{
		{"prefix", api.AllPages, l.prefix},
		{"category", api.CategoryMembers, l.category},
		{"search", api.Search, l.search},
		{"links-to", api.LinksTo, l.linksTo},
		{"embeds", api.EmbeddedIn, l.embeds},
	} {
		if cmd.Flags().Changed(s.flag) {
			sources = append(sources, "--"+s.flag)
			opts.Source, opts.Value = s.source, s.value
		}
	}
	if len(sources) > 1 {
		return opts, true, cmdutil.FlagErrorf("specify only one of %s", strings.Join(sources, ", "))
	}
	if len(sources) == 1 && opts.Source != api.AllPages && strings.TrimSpace(opts.Value) == "" {
		return opts, true, cmdutil.FlagErrorf("%s needs a value", sources[0])
	}
	if l.limit < 1 {
		return opts, true, cmdutil.FlagErrorf("invalid limit: %d", l.limit)
	}
	if cmd.Flags().Changed("namespace") {
		opts.Namespace = &l.namespace
	}
	return opts, len(sources) > 0 || opts.Namespace != nil, nil
}

// list fetches the pages of opts, after resolving a namespace in --prefix
// and the namespace of --category.
func list(ctx context.Context, c *api.Client, opts api.ListOptions) ([]api.PageRef, error) {
	if opts.Source == api.AllPages && strings.Contains(opts.Value, ":") {
		if err := splitPrefix(ctx, c, &opts); err != nil {
			return nil, err
		}
	}
	if opts.Source == api.CategoryMembers {
		var err error
		if opts.Value, err = cmdutil.NamespacedTitle(ctx, c, opts.Value, 14, "Category"); err != nil {
			return nil, err
		}
	}
	return c.ListPages(ctx, opts)
}

func newCmdList(f *cmdutil.Factory) *cobra.Command {
	var wiki string
	lf := &listFlags{}
	var jsonFlags *cmdutil.JSONFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pages",
		Long: heredoc.Doc(`
			List page titles, one per line. Without a source, all pages are listed in
			title order. Choose at most one source: --prefix, --category, --search,
			--links-to or --embeds. --namespace limits any of them to one namespace ID.
			A --prefix may start with a namespace, such as Help:, which picks that
			namespace.
		`),
		Example: heredoc.Doc(`
			$ mw page list --prefix 'Sand'
			$ mw page list --prefix 'Help:Edit'
			$ mw page list --category 'Living people' -L 100
			$ mw page list --search 'jq json' --json title,pageid
			$ mw page list --embeds 'Template:Infobox' --namespace 0
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jsonFlags.Validate(); err != nil {
				return err
			}
			opts, _, err := lf.options(cmd)
			if err != nil {
				return err
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
			c, _, err := cmdutil.Client(cmd.Context(), cfg, t)
			if err != nil {
				return err
			}
			pages, err := list(cmd.Context(), c, opts)
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
	lf.add(cmd, "Maximum number of pages to list")
	jsonFlags = cmdutil.AddJSONFlags(cmd, listFields)
	return cmd
}

// splitPrefix moves the namespace of a --prefix such as Manual:Hooks/ into
// opts.Namespace, since the API takes the prefix without it. A prefix whose
// part before the colon is not a namespace name stays as it is.
func splitPrefix(ctx context.Context, c *api.Client, opts *api.ListOptions) error {
	name, rest, _ := strings.Cut(opts.Value, ":")
	id, ok, err := c.NamespaceID(ctx, name)
	if err != nil || !ok {
		return err
	}
	if opts.Namespace != nil && *opts.Namespace != id {
		return cmdutil.FlagErrorf("--prefix %q is in namespace %d, not %d of --namespace", opts.Value, id, *opts.Namespace)
	}
	opts.Namespace = &id
	opts.Value = strings.TrimLeft(rest, " _")
	return nil
}
