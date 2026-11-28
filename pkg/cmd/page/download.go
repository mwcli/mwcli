package page

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func newCmdDownload(f *cmdutil.Factory) *cobra.Command {
	var wiki, dir string
	var clobber, skipExisting bool
	lf := &listFlags{}
	cmd := &cobra.Command{
		Use:   "download [<title>... | -]",
		Short: "Save the wikitext of pages as files",
		Long: heredoc.Doc(`
			Save the latest revision of pages as files, fifty pages a request. Name the
			pages as arguments, read them one per line from standard input with -, or
			choose them as 'mw page list' does: --prefix, --category, --search,
			--links-to, --embeds, --redirects-to, --filter-redirects, --namespace and
			--limit.

			--follow-redirects resolves redirects to their target page when fetching
			content, and with --links-to also includes pages linking through redirects.

			Each page is saved in the directory of --dir as its title with an extension
			for its content model, unless the title ends with it: .wiki for wikitext,
			.js, .css, .json, .lua, else .txt. Characters a file name cannot hold,
			such as / and :, and % itself, are written as %XX, so Help:Edit/ko is
			saved as Help%3AEdit%2Fko.wiki.

			An existing file is an error unless --clobber or --skip-existing is given.
			Pages that do not exist are reported and the command exits with status 1
			after saving the others.
		`),
		Example: heredoc.Doc(`
			$ mw page download 'Main Page' 'Help:Contents'
			$ mw page download 'Alias' --follow-redirects
			$ mw page download --category 'Living people' -L 500 --dir people
			$ mw page download --prefix 'Manual:Hooks/' -L 5000 --skip-existing
			$ mw user contribs Example --json title --jq '.[].title' | sort -u | mw page download -
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, sourceSet, err := lf.options(cmd, true)
			if err != nil {
				return err
			}
			if clobber && skipExisting {
				return cmdutil.FlagErrorf("specify only one of --clobber and --skip-existing")
			}
			if len(args) > 0 && sourceSet {
				return cmdutil.FlagErrorf("specify either titles or a source of pages such as --category")
			}
			if len(args) == 0 && !sourceSet {
				return cmdutil.FlagErrorf("specify titles, - to read them from standard input, or a source of pages such as --category")
			}
			ios := f.IOStreams
			titles, err := readTitles(args, ios.In)
			if err != nil {
				return err
			}
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
			if sourceSet {
				pages, err := list(cmd.Context(), c, opts)
				if err != nil {
					return err
				}
				for _, p := range pages {
					titles = append(titles, p.Title)
				}
			}
			if len(titles) == 0 {
				if ios.IsStdoutTTY() {
					fmt.Fprintln(ios.ErrOut, "No pages found")
				}
				return nil
			}
			texts, err := c.PagesText(cmd.Context(), titles, api.PageOptions{FollowRedirects: lf.followRedirects})
			if err != nil {
				return f.Hint(err)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			saved, skipped, failed := 0, 0, false
			seen := map[string]bool{}
			for _, p := range texts {
				switch {
				case p.Invalid:
					fmt.Fprintf(ios.ErrOut, "invalid title %q\n", p.Asked)
					failed = true
					continue
				case p.Interwiki != "":
					if p.Redirected {
						fmt.Fprintf(ios.ErrOut, "redirect %q points to another wiki (%s)\n", p.Asked, p.Interwiki)
					} else {
						fmt.Fprintf(ios.ErrOut, "%q is an interwiki title (%s)\n", p.Asked, p.Interwiki)
					}
					failed = true
					continue
				case p.Missing:
					fmt.Fprintf(ios.ErrOut, "page does not exist: %s\n", p.Title)
					failed = true
					continue
				case p.RevID == 0:
					fmt.Fprintf(ios.ErrOut, "page has no readable revision: %s\n", p.Title)
					failed = true
					continue
				}
				name := fileName(p.Title, p.ContentModel)
				if seen[name] {
					continue
				}
				seen[name] = true
				path := filepath.Join(dir, name)
				err := writeFile(path, p.Content, clobber)
				switch {
				case errors.Is(err, fs.ErrExist) && skipExisting:
					skipped++
					continue
				case errors.Is(err, fs.ErrExist):
					return fmt.Errorf("%s exists; use --clobber to overwrite it or --skip-existing to keep it", path)
				case err != nil:
					return err
				}
				saved++
			}
			if ios.IsStdoutTTY() {
				msg := fmt.Sprintf("Saved %d page(s) to %s", saved, dir)
				if skipped > 0 {
					msg += fmt.Sprintf("; kept %d existing file(s)", skipped)
				}
				fmt.Fprintf(ios.ErrOut, "%s %s\n", ios.SuccessIcon(), msg)
			}
			if failed {
				return cmdutil.ErrSilent
			}
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().StringVarP(&dir, "dir", "D", ".", "The `directory` to save the files in")
	cmd.Flags().BoolVar(&clobber, "clobber", false, "Overwrite existing files")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Keep existing files and skip those pages")
	lf.add(cmd, "Maximum number of pages to download from a source", "Follow redirects to target pages (and through redirects with --links-to)")
	return cmd
}

// readTitles returns the titles of the arguments; - reads them from in, one
// per line, skipping blank lines.
func readTitles(args []string, in io.Reader) ([]string, error) {
	var titles []string
	stdin := false
	for _, a := range args {
		if a != "-" {
			if strings.TrimSpace(a) == "" {
				return nil, cmdutil.FlagErrorf("empty title")
			}
			titles = append(titles, a)
			continue
		}
		if stdin {
			return nil, cmdutil.FlagErrorf("- can be given only once")
		}
		stdin = true
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			if t := strings.TrimSpace(strings.TrimSuffix(sc.Text(), "\r")); t != "" {
				titles = append(titles, t)
			}
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return titles, nil
}

// writeFile writes text to path, failing with fs.ErrExist when the file
// exists unless clobber is set.
func writeFile(path, text string, clobber bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !clobber {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	fh, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(fh, text); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

// extensions maps content models to file extensions.
var extensions = map[string]string{
	"wikitext":      ".wiki",
	"javascript":    ".js",
	"css":           ".css",
	"sanitized-css": ".css",
	"json":          ".json",
	"Scribunto":     ".lua",
}

// fileName is the file a page is saved as: its title made safe as a file
// name, with an extension for its content model.
func fileName(title, model string) string {
	name := cmdutil.SafeFileName(title)
	ext, ok := extensions[model]
	if !ok {
		ext = ".txt"
	}
	if strings.HasSuffix(strings.ToLower(name), ext) {
		// MediaWiki:Common.css and User:A/common.js already end with it.
		return name
	}
	return name + ext
}
