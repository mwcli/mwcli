// Package api implements `mw api`.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc"
	mwapi "github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

type options struct {
	wiki      string
	method    string
	rawFields []string
	fields    []string
	paginate  bool
	slurp     bool
	jq        string
}

// NewCmdApi returns `mw api`.
func NewCmdApi(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "api [<key>=<value>...]",
		Short: "Make an authenticated MediaWiki Action API request",
		Long: heredoc.Doc(`
			Make a request to the wiki's api.php and print the response.

			Parameters are given as <key>=<value> arguments or with -f/--raw-field.
			-F/--field reads the value from a file when it starts with "@" ("@-" for
			standard input). format=json and formatversion=2 are added unless given.

			With a method other than GET and no token parameter, a CSRF token is fetched
			and added. The exit status is 1 when the response reports an error.

			--paginate follows the continue values of each response with more requests
			until the result is complete, and prints each response; --slurp prints
			them as one JSON array instead. --jq filters each response with a jq
			expression. These need format=json.
		`),
		Example: heredoc.Doc(`
			$ mw api action=query meta=userinfo uiprop=rights
			$ mw api --wiki mediawiki.org action=parse page=MediaWiki prop=wikitext
			$ mw api -X POST action=purge titles=Sandbox
			$ mw api -X POST action=edit title=Sandbox -F text=@page.wiki summary=update

			# every member of a category, one title per line
			$ mw api action=query list=categorymembers cmtitle=Category:Physics cmlimit=max \
			    --paginate --jq '.query.categorymembers[].title'
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.rawFields = append(args, opts.rawFields...)
			return run(cmd.Context(), f, opts)
		},
	}
	cmdutil.AddTargetFlags(cmd, &opts.wiki)
	cmd.Flags().StringVarP(&opts.method, "method", "X", "GET", "The HTTP method for the request")
	cmd.Flags().StringArrayVarP(&opts.rawFields, "raw-field", "f", nil, "Add a string parameter in `key=value` format")
	cmd.Flags().StringArrayVarP(&opts.fields, "field", "F", nil, "Add a parameter in `key=value` format; @file reads the value from a file")
	cmd.Flags().BoolVar(&opts.paginate, "paginate", false, "Make additional requests following continue until the result is complete")
	cmd.Flags().BoolVar(&opts.slurp, "slurp", false, "Use with \"--paginate\" to print an array of all responses")
	cmd.Flags().StringVarP(&opts.jq, "jq", "q", "", "Filter each JSON response using a jq `expression`")
	return cmd
}

func run(ctx context.Context, f *cmdutil.Factory, opts *options) error {
	ios := f.IOStreams
	params := url.Values{}
	add := func(kv string, typed bool) error {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return cmdutil.FlagErrorf("parameter %q is not in key=value format", kv)
		}
		if typed && strings.HasPrefix(v, "@") {
			var b []byte
			var err error
			if v == "@-" {
				b, err = io.ReadAll(ios.In)
			} else {
				b, err = os.ReadFile(v[1:])
			}
			if err != nil {
				return err
			}
			v = string(b)
		}
		params.Add(k, v)
		return nil
	}
	for _, kv := range opts.rawFields {
		if err := add(kv, false); err != nil {
			return err
		}
	}
	for _, kv := range opts.fields {
		if err := add(kv, true); err != nil {
			return err
		}
	}
	if !params.Has("action") {
		return cmdutil.FlagErrorf("the action parameter is required, e.g. action=query")
	}
	if !params.Has("format") {
		params.Set("format", "json")
	}
	if !params.Has("formatversion") && params.Get("format") == "json" {
		params.Set("formatversion", "2")
	}
	method := strings.ToUpper(opts.method)
	isJSON := params.Get("format") == "json"
	switch {
	case opts.slurp && !opts.paginate:
		return cmdutil.FlagErrorf("`--slurp` requires `--paginate`")
	case opts.slurp && opts.jq != "":
		return cmdutil.FlagErrorf("the `--slurp` option is not supported with `--jq`")
	case (opts.paginate || opts.jq != "") && !isJSON:
		return cmdutil.FlagErrorf("`--paginate` and `--jq` need format=json")
	}
	if opts.jq != "" {
		if _, err := cmdutil.CompileJQ(opts.jq); err != nil {
			return err
		}
	}

	cfg, err := f.Config()
	if err != nil {
		return err
	}
	t, err := f.Resolve(ctx, cfg, opts.wiki)
	if err != nil {
		return err
	}
	c, _, err := cmdutil.Client(cfg, t)
	if err != nil {
		return err
	}
	if method != http.MethodGet && !params.Has("token") {
		tok, err := c.TokenOf(ctx, "csrf")
		if err != nil {
			return err
		}
		params.Set("token", tok)
	}
	// One response, or with --paginate each one that follows continue.
	var responses iter.Seq2[[]byte, error] = func(yield func([]byte, error) bool) {
		body, err := c.Raw(ctx, method, params)
		if err == nil && isJSON {
			err = mwapi.CheckError(body)
		}
		yield(body, err)
	}
	if opts.paginate {
		responses = c.Paginate(ctx, method, params)
	}
	var slurped []json.RawMessage
	for body, err := range responses {
		if err != nil {
			// An API error is printed as the wiki sent it, then reported.
			if body != nil {
				if werr := writeBody(ios.Out, body, isJSON && ios.IsStdoutTTY()); werr != nil {
					return werr
				}
			}
			return err
		}
		switch {
		case opts.slurp:
			slurped = append(slurped, json.RawMessage(bytes.TrimSpace(body)))
		case opts.jq != "":
			v, err := cmdutil.DecodeJSON(body)
			if err != nil {
				return fmt.Errorf("decode API response: %w", err)
			}
			if err := cmdutil.WriteJQ(ios.Out, opts.jq, v, ios.IsStdoutTTY()); err != nil {
				return err
			}
		default:
			if err := writeBody(ios.Out, body, isJSON && ios.IsStdoutTTY()); err != nil {
				return err
			}
		}
	}
	if opts.slurp {
		b, err := json.Marshal(slurped)
		if err != nil {
			return err
		}
		return writeBody(ios.Out, b, ios.IsStdoutTTY())
	}
	return nil
}

func writeBody(w io.Writer, body []byte, indent bool) error {
	out := body
	if indent {
		var buf bytes.Buffer
		if json.Indent(&buf, body, "", "  ") == nil {
			out = buf.Bytes()
		}
	}
	if _, err := w.Write(out); err != nil {
		return err
	}
	if len(out) > 0 && out[len(out)-1] != '\n' {
		_, err := fmt.Fprintln(w)
		return err
	}
	return nil
}
