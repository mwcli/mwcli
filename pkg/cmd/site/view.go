package site

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/spf13/cobra"
)

// viewFields are the --json fields of `mw site view`, named as in the MediaWiki
// API, and api, the api.php mw uses.
var viewFields = []string{
	"sitename", "mainpage", "url", "api", "server", "articlepath", "scriptpath", "lang",
	"generator", "phpversion", "dbtype", "dbversion", "timezone", "readonly",
	"statistics", "extensions",
}

// siteView is a wiki as `mw site view --json` prints it.
type siteView struct {
	*api.Site
	API string `json:"api"`
}

func newCmdView(f *cmdutil.Factory) *cobra.Command {
	var wiki string
	var web bool
	var jsonFlags *cmdutil.JSONFlags
	cmd := &cobra.Command{
		Use:   "view [<wiki>]",
		Short: "Show information about a wiki",
		Long: heredoc.Doc(`
			Show the name, main page, software, language and counts of a wiki, and how
			many extensions it has. The wiki is given as for --wiki; without one, it is
			the default site. Needs no login.

			With --json, print the named fields instead; --jq filters them. The
			extensions field lists every installed extension and skin. --web opens the
			main page in the browser.
		`),
		Example: heredoc.Doc(`
			$ mw site view
			$ mw site view mediawiki.org
			$ mw site view --json generator,statistics
			$ mw site view --json extensions --jq '.extensions[].name'
			$ mw site view commons --web
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jsonFlags.Validate(); err != nil {
				return err
			}
			if web && jsonFlags.Enabled() {
				return cmdutil.FlagErrorf("specify only one of --web and --json")
			}
			if len(args) == 1 {
				if wiki != "" {
					return cmdutil.FlagErrorf("specify the wiki either as an argument or with --wiki")
				}
				wiki = args[0]
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
			if web {
				u := t.MainPageURL()
				if ios.IsStdoutTTY() {
					fmt.Fprintf(ios.ErrOut, "Opening %s in your browser.\n", u)
				}
				return f.Browse(u)
			}
			c, _, err := cmdutil.Client(cfg, t)
			if err != nil {
				return err
			}
			opts := api.SiteOptions{Statistics: true, Extensions: true}
			if jsonFlags.Enabled() {
				opts = api.SiteOptions{Statistics: jsonFlags.Has("statistics"), Extensions: jsonFlags.Has("extensions")}
			}
			s, err := c.SiteDetails(cmd.Context(), opts)
			if err != nil {
				return f.Hint(err)
			}
			if jsonFlags.Enabled() {
				return jsonFlags.Write(ios.Out, siteView{Site: s, API: t.API}, ios.IsStdoutTTY())
			}
			if ios.IsStdoutTTY() {
				return printSite(ios, s, t.API)
			}
			return printSiteRaw(ios.Out, s, t.API)
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open the main page in the browser")
	jsonFlags = cmdutil.AddJSONFlags(cmd, viewFields)
	return cmd
}

// software is the generator with PHP and the database, as Special:Version names them.
func software(s *api.Site) string {
	var more []string
	if s.PHPVersion != "" {
		more = append(more, "PHP "+s.PHPVersion)
	}
	if db := strings.TrimSpace(s.DBType + " " + s.DBVersion); db != "" {
		more = append(more, db)
	}
	if len(more) == 0 {
		return s.Generator
	}
	return s.Generator + " (" + strings.Join(more, ", ") + ")"
}

func printSite(ios *iostreams.IOStreams, s *api.Site, apiURL string) error {
	n := func(v int) string { return cmdutil.Thousands(int64(v)) }
	fmt.Fprintln(ios.Out, ios.Bold(s.SiteName))
	if s.URL != "" {
		fmt.Fprintln(ios.Out, s.URL)
	}
	fmt.Fprintln(ios.Out)
	st := s.Statistics
	rows := [][]string{
		{"Software", software(s)},
		{"Language", s.Lang},
		{"API", apiURL},
		{"Pages", fmt.Sprintf("%s (%s articles)", n(st.Pages), n(st.Articles))},
		{"Edits", n(st.Edits)},
		{"Files", n(st.Images)},
		{"Users", fmt.Sprintf("%s (%s active, %s admins)", n(st.Users), n(st.ActiveUsers), n(st.Admins))},
		{"Extensions", strconv.Itoa(len(s.Extensions))},
	}
	if s.ReadOnly {
		rows = append(rows, []string{"Read-only", "yes"})
	}
	return cmdutil.WriteTable(ios.Out, rows)
}

// printSiteRaw prints one field a line, name and value separated by a tab.
func printSiteRaw(w io.Writer, s *api.Site, apiURL string) error {
	st := s.Statistics
	for _, kv := range [][2]string{
		{"sitename", s.SiteName},
		{"url", s.URL},
		{"api", apiURL},
		{"generator", s.Generator},
		{"phpversion", s.PHPVersion},
		{"dbtype", s.DBType},
		{"dbversion", s.DBVersion},
		{"lang", s.Lang},
		{"pages", strconv.Itoa(st.Pages)},
		{"articles", strconv.Itoa(st.Articles)},
		{"edits", strconv.Itoa(st.Edits)},
		{"images", strconv.Itoa(st.Images)},
		{"users", strconv.Itoa(st.Users)},
		{"activeusers", strconv.Itoa(st.ActiveUsers)},
		{"admins", strconv.Itoa(st.Admins)},
		{"extensions", strconv.Itoa(len(s.Extensions))},
		{"readonly", strconv.FormatBool(s.ReadOnly)},
	} {
		if _, err := fmt.Fprintf(w, "%s:\t%s\n", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}
