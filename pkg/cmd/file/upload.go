package file

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func newCmdUpload(f *cmdutil.Factory) *cobra.Command {
	var wiki, name, summary, text, textFile string
	var ignoreWarnings bool
	cmd := &cobra.Command{
		Use:   "upload <path>...",
		Short: "Upload files",
		Long: heredoc.Doc(`
			Upload files to the wiki, each under its own name unless --name gives one
			for a single file. A file larger than 5 MiB is sent in parts.

			--summary is the upload summary, shown in the file history and the log.
			--text, or --text-file, is the wikitext of the description page of a new
			file, such as its source and license; a new version of an existing file
			keeps its page.

			When the wiki warns, such as about a file that exists, one with the same
			content, or a name that was deleted, the file is not published and the
			warning is shown; --ignore-warnings uploads it anyway, such as a new
			version of an existing file. With several files, the others are still
			uploaded and the command exits with status 1. Uploading needs the upload
			right, such as the uploadfile grant of a token.
		`),
		Example: heredoc.Doc(`
			$ mw file upload photo.jpg --text '{{Information|description=A photo}}' -s 'New photo'
			$ mw file upload scan.png --name 'Report 2026 page 1.png' --text-file desc.wiki
			$ mw file upload logo.svg --ignore-warnings -s 'New version'
			$ mw file upload *.jpg --text-file desc.wiki
		`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if name != "" && len(args) > 1 {
				return cmdutil.FlagErrorf("--name is for a single file")
			}
			if cmd.Flags().Changed("name") && strings.TrimSpace(name) == "" {
				return cmdutil.FlagErrorf("--name needs a value")
			}
			if cmd.Flags().Changed("text-file") && textFile == "" {
				return cmdutil.FlagErrorf("--text-file needs a file, or - for standard input")
			}
			if cmd.Flags().Changed("text") && cmd.Flags().Changed("text-file") {
				return cmdutil.FlagErrorf("specify only one of --text and --text-file")
			}
			ios := f.IOStreams
			if textFile != "" {
				var b []byte
				var err error
				if textFile == "-" {
					b, err = io.ReadAll(ios.In)
				} else {
					b, err = os.ReadFile(textFile)
				}
				if err != nil {
					return err
				}
				text = string(b)
			}
			type upload struct {
				path, name string
				size       int64
			}
			var uploads []upload
			for _, p := range args {
				st, err := os.Stat(p)
				if err != nil {
					return err
				}
				if !st.Mode().IsRegular() {
					return cmdutil.FlagErrorf("%s is not a file", p)
				}
				n := filepath.Base(p)
				if name != "" {
					n = name
				}
				uploads = append(uploads, upload{path: p, name: strings.TrimSpace(n), size: st.Size()})
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
			c, ok, err := cmdutil.Client(cmd.Context(), cfg, t)
			if err != nil {
				return err
			}
			if !ok {
				return cmdutil.NotLoggedIn(t.Name)
			}
			// The API takes the name without the namespace: File:, or the
			// wiki's own name for it, is dropped.
			tl := &titler{ctx: ctx, c: c}
			for i := range uploads {
				title, err := tl.title(uploads[i].name)
				if err != nil {
					return f.Hint(err)
				}
				_, n, _ := strings.Cut(title, ":")
				uploads[i].name = strings.TrimLeft(n, " _")
				if strings.TrimSpace(uploads[i].name) == "" {
					return cmdutil.FlagErrorf("--name needs a file name after %q", strings.TrimSpace(title))
				}
			}
			failed := false
			// chunk keeps the smaller parts a server asked for, for the next files.
			var chunk int64
			for _, u := range uploads {
				fh, err := os.Open(u.path)
				if err != nil {
					return err
				}
				opts := api.UploadOptions{Comment: summary, Text: text, IgnoreWarnings: ignoreWarnings, ChunkSize: chunk}
				// open is true while a progress line waits for its line break.
				open := false
				if ios.IsStdoutTTY() && u.size > api.DefaultChunkSize {
					opts.Progress = func(sent, total int64) {
						fmt.Fprintf(ios.ErrOut, "\r%s: %d%%", u.name, sent*100/total)
						open = sent < total
						if !open {
							fmt.Fprintln(ios.ErrOut)
						}
					}
				}
				opts.Smaller = func(smaller int64) {
					chunk = smaller
					if open {
						fmt.Fprintln(ios.ErrOut)
						open = false
					}
					fmt.Fprintf(ios.ErrOut, "The server refused a request that large; sending %s in parts of %s\n", u.name, sizeText(smaller))
				}
				res, err := c.Upload(ctx, u.name, fh, u.size, opts)
				fh.Close()
				if open {
					fmt.Fprintln(ios.ErrOut)
				}
				if err := cmdutil.LoginError(err, t); err != nil {
					return err
				}
				var warn api.UploadWarnings
				if errors.As(err, &warn) {
					fmt.Fprintf(ios.ErrOut, "%s Not uploaded %s: %v\n", ios.WarningIcon(), u.path, warn)
					fmt.Fprintln(ios.ErrOut, "  Run again with --ignore-warnings to upload it anyway.")
					failed = true
					continue
				}
				if err != nil {
					return fmt.Errorf("upload %s: %w", u.path, err)
				}
				fmt.Fprintf(ios.ErrOut, "%s Uploaded File:%s on %s\n", ios.SuccessIcon(), res.FileName, t.Name)
				if res.URL != "" {
					fmt.Fprintln(ios.Out, res.URL)
				}
			}
			if failed {
				return cmdutil.ErrSilent
			}
			return nil
		},
	}
	cmdutil.AddTargetFlags(cmd, &wiki)
	cmd.Flags().StringVar(&name, "name", "", "The `name` to upload a single file under, with or without File:")
	cmd.Flags().StringVarP(&summary, "summary", "s", "", "Upload summary `text`, shown in the file history and the log")
	cmd.Flags().StringVar(&text, "text", "", "The `wikitext` of the description page of a new file")
	cmd.Flags().StringVar(&textFile, "text-file", "", "Read the description page wikitext from `file` (use \"-\" to read from standard input)")
	cmd.Flags().BoolVar(&ignoreWarnings, "ignore-warnings", false, "Upload even when the wiki warns, such as about an existing file")
	return cmd
}

// sizeText shows a size in bytes as KiB or MiB.
func sizeText(n int64) string {
	if n >= 1<<20 && n%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KiB", n>>10)
}
