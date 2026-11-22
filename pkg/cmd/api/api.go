// Package api implements `mw api`.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	apiURL    string
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
			return run(f, opts)
		},
	}
	cmdutil.AddTargetFlags(cmd, &opts.wiki, &opts.apiURL)
	cmd.Flags().StringVarP(&opts.method, "method", "X", "GET", "The HTTP method for the request")
	cmd.Flags().StringArrayVarP(&opts.rawFields, "raw-field", "f", nil, "Add a string parameter in `key=value` format")
	cmd.Flags().StringArrayVarP(&opts.fields, "field", "F", nil, "Add a parameter in `key=value` format; @file reads the value from a file")
	cmd.Flags().BoolVar(&opts.paginate, "paginate", false, "Make additional requests following continue until the result is complete")
	cmd.Flags().BoolVar(&opts.slurp, "slurp", false, "Use with \"--paginate\" to print an array of all responses")
	cmd.Flags().StringVarP(&opts.jq, "jq", "q", "", "Filter each JSON response using a jq `expression`")
	return cmd
}

func run(f *cmdutil.Factory, opts *options) error {
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
	t, err := f.Resolve(cfg, opts.wiki, opts.apiURL)
	if err != nil {
		return err
	}
	c, _, err := cmdutil.Client(cfg, t)
	if err != nil {
		return err
	}
	if method != http.MethodGet && !params.Has("token") {
		tok, err := c.TokenOf("csrf")
		if err != nil {
			return err
		}
		params.Set("token", tok)
	}
	// Each request is the original one with the continue values of the last
	// response: values a later response drops must not be sent again.
	base := url.Values{}
	for k, v := range params {
		base[k] = v
	}
	var slurped []json.RawMessage
	for {
		body, err := c.Raw(method, params)
		if err != nil {
			return err
		}
		var apiErr error
		if isJSON {
			apiErr = mwapi.CheckError(body)
		}
		switch {
		case opts.slurp && apiErr == nil:
			slurped = append(slurped, json.RawMessage(bytes.TrimSpace(body)))
		case opts.jq != "" && apiErr == nil:
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
		if apiErr != nil {
			return apiErr
		}
		if !opts.paginate {
			return nil
		}
		next, err := continueValues(body)
		if err != nil {
			return err
		}
		if next == nil {
			break
		}
		params = url.Values{}
		for k, v := range base {
			params[k] = v
		}
		for k, v := range next {
			params.Set(k, v)
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

// continueValues returns the parameters of the next request from the continue
// object of a response, or nil when the result is complete. Values are strings
// or, like sroffset, numbers.
func continueValues(body []byte) (map[string]string, error) {
	var resp struct {
		Continue map[string]json.RawMessage `json:"continue"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode API response: %w", err)
	}
	if len(resp.Continue) == 0 {
		return nil, nil
	}
	next := map[string]string{}
	for k, raw := range resp.Continue {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			s = string(raw)
		}
		next[k] = s
	}
	return next, nil
}
