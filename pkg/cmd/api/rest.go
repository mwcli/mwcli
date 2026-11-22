package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/mwcli/mwcli/pkg/cmdutil"
)

// runREST makes a request to path under the wiki's rest.php.
func runREST(ctx context.Context, f *cmdutil.Factory, path string, opts *options) error {
	ios := f.IOStreams
	switch {
	case opts.paginate || opts.slurp:
		return cmdutil.FlagErrorf("`--paginate` and `--slurp` are only for the Action API")
	}
	if opts.jq != "" {
		if _, err := cmdutil.CompileJQ(opts.jq); err != nil {
			return err
		}
	}
	method := strings.ToUpper(opts.method)
	inQuery := method == http.MethodGet || method == http.MethodHead || method == http.MethodDelete || opts.input != ""
	query := url.Values{}
	fields := map[string]any{}
	add := func(kv string, typed bool) error {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return cmdutil.FlagErrorf("parameter %q is not in key=value format", kv)
		}
		var value any = v
		if typed {
			var err error
			if value, err = typedValue(v, ios.In); err != nil {
				return err
			}
		}
		if inQuery {
			query.Add(k, fmt.Sprint(value))
			return nil
		}
		if _, ok := fields[k]; ok {
			return cmdutil.FlagErrorf("parameter %q given twice", k)
		}
		fields[k] = value
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
	if _, err := c.RESTURL(path); err != nil {
		return cmdutil.FlagErrorf("%v", err)
	}
	var body []byte
	contentType := "application/json"
	switch {
	case opts.input == "-":
		if body, err = io.ReadAll(ios.In); err != nil {
			return err
		}
	case opts.input != "":
		if body, err = os.ReadFile(opts.input); err != nil {
			return err
		}
	case !inQuery:
		if _, ok := fields["token"]; !ok && c.SessionAuth() {
			tok, err := c.TokenOf(ctx, "csrf")
			if err != nil {
				return err
			}
			fields["token"] = tok
		}
		if body, err = json.Marshal(fields); err != nil {
			return err
		}
	}
	resp, err := c.REST(ctx, method, path, query, body, contentType)
	if err != nil {
		return err
	}
	isJSON := strings.Contains(resp.Header.Get("Content-Type"), "json")
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	switch {
	case !ok || opts.jq == "":
		if len(resp.Body) > 0 {
			if err := writeBody(ios.Out, resp.Body, isJSON && ios.IsStdoutTTY()); err != nil {
				return err
			}
		}
	case !isJSON:
		return fmt.Errorf("the response is not JSON (%s), so --jq cannot filter it", resp.Header.Get("Content-Type"))
	default:
		v, err := cmdutil.DecodeJSON(resp.Body)
		if err != nil {
			return fmt.Errorf("decode API response: %w", err)
		}
		if err := cmdutil.WriteJQ(ios.Out, opts.jq, v, ios.IsStdoutTTY()); err != nil {
			return err
		}
	}
	if !ok {
		return fmt.Errorf("MediaWiki returned %s", resp.Status)
	}
	return nil
}

// typedValue is the JSON value of a -F parameter: the text of a file for
// @file, true, false, null or a number, else the string itself.
func typedValue(v string, stdin io.Reader) (any, error) {
	switch {
	case v == "@-":
		b, err := io.ReadAll(stdin)
		return string(b), err
	case strings.HasPrefix(v, "@"):
		b, err := os.ReadFile(v[1:])
		return string(b), err
	case v == "true":
		return true, nil
	case v == "false":
		return false, nil
	case v == "null":
		return nil, nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, nil
	}
	return v, nil
}
