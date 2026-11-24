// Package file implements `mw file`.
package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/spf13/cobra"
)

// NewCmdFile returns `mw file`.
func NewCmdFile(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "file <command>",
		Short: "Look at, download and upload files",
		Long: heredoc.Doc(`
			A file is given by its name, with or without the File: prefix or the
			wiki's own name for that namespace. Files of a shared repository, such as
			Wikimedia Commons files used on Wikipedia, work too.
		`),
	}
	cmd.AddCommand(newCmdView(f), newCmdDownload(f), newCmdUpload(f))
	return cmd
}

// viewFields are the --json fields of `mw file view`, named as in the MediaWiki API.
var viewFields = []string{
	"title", "url", "descriptionurl", "size", "width", "height", "mime", "mediatype",
	"timestamp", "user", "comment", "sha1", "repository",
}

// titler turns file names into File: titles, asking the wiki for the names of
// the file namespace at most once.
type titler struct {
	ctx   context.Context
	c     *api.Client
	names []string
	err   error
	done  bool
}

func (t *titler) title(name string) (string, error) {
	name = strings.TrimSpace(name)
	prefix, _, ok := strings.Cut(name, ":")
	if !ok {
		return "File:" + name, nil
	}
	if !t.done {
		t.names, t.err = t.c.NamespaceNames(t.ctx, 6)
		t.done = true
	}
	if t.err != nil {
		return "", t.err
	}
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " ")) }
	for _, n := range t.names {
		if n != "" && norm(n) == norm(prefix) {
			return name, nil
		}
	}
	return "File:" + name, nil
}

func newCmdView(f *cmdutil.Factory) *cobra.Command {
	var wiki string
	var web bool
	var jsonFlags *cmdutil.JSONFlags
	cmd := &cobra.Command{
		Use:   "view <file>",
		Short: "Show information about a file",
		Long: heredoc.Doc(`
			Show the latest version of a file: its URL, size, dimensions, type, and
			who uploaded it, when and why. Public files need no login.

			With --json, print the named fields instead; --jq filters them. --web
			opens the file's description page.
		`),
		Example: heredoc.Doc(`
			$ mw file view Example.jpg
			$ mw file view 'File:Example.jpg' --json url,sha1
			$ mw file view Example.jpg --wiki commons --web
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := jsonFlags.Validate(); err != nil {
				return err
			}
			if web && jsonFlags.Enabled() {
				return cmdutil.FlagErrorf("specify only one of --web and --json")
			}
			if strings.TrimSpace(args[0]) == "" {
				return cmdutil.FlagErrorf("empty file name")
			}
			ios := f.IOStreams
			ctx := cmd.Context()
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			t, err := f.Resolve(ctx, cfg, wiki)
			if err != nil {
				return err
			}
			c, _, err := cmdutil.Client(cmd.Context(), cfg, t)
			if err != nil {
				return err
			}
			title, err := (&titler{ctx: ctx, c: c}).title(args[0])
			if err != nil {
				return f.Hint(err)
			}
			infos, err := c.FilesInfo(ctx, []string{title})
			if err != nil {
				return f.Hint(err)
			}
			fi := infos[0]
			switch {
			case fi.Invalid:
				return fmt.Errorf("invalid file name %q", args[0])
			case fi.Missing:
				return fmt.Errorf("file does not exist: %s on %s", fi.Title, t.Name)
			}
			if web {
				u := fi.DescriptionURL
				if u == "" {
					u = t.PageURL(fi.Title)
				}
				if ios.IsStdoutTTY() {
					fmt.Fprintf(ios.ErrOut, "Opening %s in your browser.\n", u)
				}
				return f.Browse(u)
			}
			if jsonFlags.Enabled() {
				return jsonFlags.Write(ios.Out, fi, ios.IsStdoutTTY())
			}
			if ios.IsStdoutTTY() {
				return printFile(ios, fi)
			}
			return printFileRaw(ios.Out, fi)
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open the description page in the browser")
	jsonFlags = cmdutil.AddJSONFlags(cmd, viewFields)
	return cmd
}

func printFile(ios *iostreams.IOStreams, fi api.FileInfo) error {
	fmt.Fprintln(ios.Out, ios.Bold(fi.Title))
	if fi.URL != "" {
		fmt.Fprintln(ios.Out, fi.URL)
	}
	fmt.Fprintln(ios.Out)
	size := cmdutil.Thousands(fi.Size) + " bytes"
	if fi.Width > 0 && fi.Height > 0 {
		size += fmt.Sprintf(" (%d × %d)", fi.Width, fi.Height)
	}
	uploaded := cmdutil.ShortTime(fi.Timestamp)
	if fi.User != "" {
		uploaded += " by " + fi.User
	}
	rows := [][]string{
		{"Size", size},
		{"Type", strings.TrimSpace(fi.MIME + " " + parens(fi.MediaType))},
		{"Uploaded", uploaded},
	}
	if c := cmdutil.OneLine(fi.Comment); c != "" {
		rows = append(rows, []string{"Summary", cmdutil.Truncate(c, 80)})
	}
	if fi.Repository != "" && fi.Repository != "local" {
		repo := fi.Repository
		if fi.DescriptionURL != "" {
			repo += ", " + fi.DescriptionURL
		}
		rows = append(rows, []string{"Repository", repo})
	}
	return cmdutil.WriteTable(ios.Out, rows)
}

func parens(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

// printFileRaw prints one field a line, name and value separated by a tab.
func printFileRaw(w io.Writer, fi api.FileInfo) error {
	for _, kv := range [][2]string{
		{"title", fi.Title},
		{"url", fi.URL},
		{"descriptionurl", fi.DescriptionURL},
		{"size", strconv.FormatInt(fi.Size, 10)},
		{"width", strconv.Itoa(fi.Width)},
		{"height", strconv.Itoa(fi.Height)},
		{"mime", fi.MIME},
		{"mediatype", fi.MediaType},
		{"timestamp", fi.Timestamp},
		{"user", fi.User},
		{"comment", cmdutil.TSVField(fi.Comment)},
		{"sha1", fi.SHA1},
		{"repository", fi.Repository},
	} {
		if _, err := fmt.Fprintf(w, "%s:\t%s\n", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

func newCmdDownload(f *cmdutil.Factory) *cobra.Command {
	var wiki, dir, category string
	var limit int
	var clobber, skipExisting bool
	cmd := &cobra.Command{
		Use:   "download [<file>... | -]",
		Short: "Download files",
		Long: heredoc.Doc(`
			Download the latest version of files into the directory of --dir. Name the
			files as arguments, read them one per line from standard input with -, or
			take the files of a category with --category, up to --limit.

			Each file is saved under its name without the File: prefix, with characters
			a file name cannot hold written as %XX, and checked against the SHA-1 the
			wiki has. An existing file is an error unless --clobber or --skip-existing
			is given. Files that do not exist are reported and the command exits with
			status 1 after downloading the others.
		`),
		Example: heredoc.Doc(`
			$ mw file download Example.jpg
			$ mw file download --wiki commons --category 'Cats in snow' -L 10 --dir cats
			$ mw page list --prefix 'File:Logo' | mw file download - --skip-existing
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if clobber && skipExisting {
				return cmdutil.FlagErrorf("specify only one of --clobber and --skip-existing")
			}
			catSet := cmd.Flags().Changed("category")
			if catSet && strings.TrimSpace(category) == "" {
				return cmdutil.FlagErrorf("--category needs a value")
			}
			if len(args) > 0 && catSet {
				return cmdutil.FlagErrorf("specify either files or --category")
			}
			if len(args) == 0 && !catSet {
				return cmdutil.FlagErrorf("specify files, - to read them from standard input, or --category")
			}
			if limit < 1 {
				return cmdutil.FlagErrorf("invalid limit: %d", limit)
			}
			ios := f.IOStreams
			names, err := readNames(args, ios.In)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			t, err := f.Resolve(ctx, cfg, wiki)
			if err != nil {
				return err
			}
			c, _, err := cmdutil.Client(cmd.Context(), cfg, t)
			if err != nil {
				return err
			}
			var titles []string
			if catSet {
				cat, err := cmdutil.NamespacedTitle(ctx, c, category, 14, "Category")
				if err != nil {
					return f.Hint(err)
				}
				ns := 6
				pages, err := c.ListPages(ctx, api.ListOptions{Source: api.CategoryMembers, Value: cat, Namespace: &ns, Limit: limit})
				if err != nil {
					return f.Hint(err)
				}
				for _, p := range pages {
					titles = append(titles, p.Title)
				}
			}
			tl := &titler{ctx: ctx, c: c}
			for _, n := range names {
				title, err := tl.title(n)
				if err != nil {
					return f.Hint(err)
				}
				titles = append(titles, title)
			}
			if len(titles) == 0 {
				if ios.IsStdoutTTY() {
					fmt.Fprintln(ios.ErrOut, "No files found")
				}
				return nil
			}
			infos, err := c.FilesInfo(ctx, titles)
			if err != nil {
				return f.Hint(err)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			saved, skipped, failed := 0, 0, false
			seen := map[string]bool{}
			for _, fi := range infos {
				switch {
				case fi.Invalid:
					fmt.Fprintf(ios.ErrOut, "invalid file name %q\n", fi.Asked)
					failed = true
					continue
				case fi.Missing:
					fmt.Fprintf(ios.ErrOut, "file does not exist: %s\n", fi.Title)
					failed = true
					continue
				case fi.URL == "":
					fmt.Fprintf(ios.ErrOut, "file has no readable version: %s\n", fi.Title)
					failed = true
					continue
				}
				base := fi.Title
				if _, after, ok := strings.Cut(fi.Title, ":"); ok {
					base = after
				}
				name := cmdutil.SafeFileName(base)
				if seen[name] {
					continue
				}
				seen[name] = true
				path := filepath.Join(dir, name)
				_, err := os.Lstat(path)
				switch {
				case err == nil && skipExisting:
					skipped++
					continue
				case err == nil && !clobber:
					return fmt.Errorf("%s exists; use --clobber to overwrite it or --skip-existing to keep it", path)
				case err != nil && !errors.Is(err, fs.ErrNotExist):
					return err
				}
				if ios.IsStdoutTTY() {
					fmt.Fprintf(ios.ErrOut, "Downloading %s (%s bytes)\n", fi.Title, cmdutil.Thousands(fi.Size))
				}
				if err := fetch(ctx, c, fi, path); err != nil {
					return err
				}
				saved++
			}
			if ios.IsStdoutTTY() {
				msg := fmt.Sprintf("Downloaded %d file(s) to %s", saved, dir)
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
	cmd.Flags().StringVar(&category, "category", "", "Download the files of a `category`, with or without its namespace prefix")
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of files to download from a category")
	cmd.Flags().BoolVar(&clobber, "clobber", false, "Overwrite existing files")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Keep existing files and skip those files")
	return cmd
}

// fetch downloads the file of fi to path through a temporary file next to
// it, so that a failed download leaves no partial file behind.
func fetch(ctx context.Context, c *api.Client, fi api.FileInfo, path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mw-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	sum, err := c.Fetch(ctx, fi.URL, tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", fi.Title, err)
	}
	if fi.SHA1 != "" && !strings.EqualFold(sum, fi.SHA1) {
		return fmt.Errorf("download %s: SHA-1 %s does not match the wiki's %s", fi.Title, sum, fi.SHA1)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s exists", path)
		}
		return err
	}
	return nil
}

// readNames returns the file names of the arguments; - reads them from in,
// one per line, skipping blank lines.
func readNames(args []string, in io.Reader) ([]string, error) {
	var names []string
	stdin := false
	for _, a := range args {
		if a != "-" {
			if strings.TrimSpace(a) == "" {
				return nil, cmdutil.FlagErrorf("empty file name")
			}
			names = append(names, a)
			continue
		}
		if stdin {
			return nil, cmdutil.FlagErrorf("- can be given only once")
		}
		stdin = true
		b, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if n := strings.TrimSpace(strings.TrimSuffix(line, "\r")); n != "" {
				names = append(names, n)
			}
		}
	}
	return names, nil
}
