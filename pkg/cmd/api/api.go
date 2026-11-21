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
	"github.com/spf13/cobra"
	mwapi "github.com/zetaoss/mwcli/api"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
)

type options struct {
	hostname  string
	apiURL    string
	method    string
	rawFields []string
	fields    []string
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
		`),
		Example: heredoc.Doc(`
			$ mw api action=query meta=userinfo uiprop=rights
			$ mw api -h zetawiki.com action=parse page=대문 prop=wikitext
			$ mw api -X POST action=purge titles=Sandbox
			$ mw api -X POST action=edit title=Sandbox -F text=@page.wiki summary=update
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.rawFields = append(args, opts.rawFields...)
			return run(f, opts)
		},
	}
	cmdutil.AddTargetFlags(cmd, &opts.hostname, &opts.apiURL)
	cmd.Flags().StringVarP(&opts.method, "method", "X", "GET", "The HTTP method for the request")
	cmd.Flags().StringArrayVarP(&opts.rawFields, "raw-field", "f", nil, "Add a string parameter in `key=value` format")
	cmd.Flags().StringArrayVarP(&opts.fields, "field", "F", nil, "Add a parameter in `key=value` format; @file reads the value from a file")
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

	cfg, err := f.Config()
	if err != nil {
		return err
	}
	t, err := cmdutil.Resolve(cfg, opts.hostname, opts.apiURL)
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
	body, err := c.Raw(method, params)
	if err != nil {
		return err
	}
	out := body
	if ios.IsStdoutTTY() && params.Get("format") == "json" {
		var buf bytes.Buffer
		if json.Indent(&buf, body, "", "  ") == nil {
			out = buf.Bytes()
		}
	}
	ios.Out.Write(out)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		fmt.Fprintln(ios.Out)
	}
	if params.Get("format") == "json" {
		if err := mwapi.CheckError(body); err != nil {
			return err
		}
	}
	return nil
}
