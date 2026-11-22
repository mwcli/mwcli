package cmdutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/spf13/cobra"
)

// JSONFlags are --json and --jq, as in gh.
type JSONFlags struct {
	fields []string
	jq     string
	known  []string
}

// AddJSONFlags adds --json FIELDS and --jq EXPR to cmd; known lists the fields.
func AddJSONFlags(cmd *cobra.Command, known []string) *JSONFlags {
	j := &JSONFlags{known: known}
	cmd.Flags().StringSliceVar(&j.fields, "json", nil, "Output JSON with the specified `fields`")
	cmd.Flags().StringVarP(&j.jq, "jq", "q", "", "Filter JSON output using a jq `expression`")
	// Like gh, a bare --json lists the fields instead of failing on the missing value.
	parent := cmd.FlagErrorFunc()
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "flag needs an argument: --json") {
			return j.fieldsError()
		}
		return parent(c, err)
	})
	return j
}

// Enabled reports whether JSON output was asked for.
func (j *JSONFlags) Enabled() bool { return len(j.fields) > 0 }

// Has reports whether field was asked for.
func (j *JSONFlags) Has(field string) bool { return slices.Contains(j.fields, field) }

// Validate checks the flags before the command does any work.
func (j *JSONFlags) Validate() error {
	if j.jq != "" && !j.Enabled() {
		return FlagErrorf("cannot use `--jq` without specifying `--json`")
	}
	if !j.Enabled() {
		return nil
	}
	for _, f := range j.fields {
		if !slices.Contains(j.known, f) {
			return FlagErrorf("Unknown JSON field: %q\nAvailable fields:\n  %s", f, strings.Join(j.known, "\n  "))
		}
	}
	return nil
}

func (j *JSONFlags) fieldsError() error {
	return FlagErrorf("Specify one or more comma-separated fields for `--json`:\n  %s", strings.Join(j.known, "\n  "))
}

// Write prints the requested fields of v, a struct whose JSON names are the
// known fields, filtered by --jq when given. Output is indented on a terminal.
func (j *JSONFlags) Write(w io.Writer, v any, tty bool) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var all map[string]any
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	out := map[string]any{}
	for _, f := range j.fields {
		out[f] = all[f]
	}
	if j.jq == "" {
		return writeJSON(w, out, tty)
	}
	q, err := gojq.Parse(j.jq)
	if err != nil {
		return FlagErrorf("invalid --jq expression: %v", err)
	}
	code, err := gojq.Compile(q)
	if err != nil {
		return FlagErrorf("invalid --jq expression: %v", err)
	}
	var generic any
	b, _ = json.Marshal(out)
	_ = json.Unmarshal(b, &generic)
	iter := code.Run(generic)
	for {
		r, ok := iter.Next()
		if !ok {
			return nil
		}
		if err, ok := r.(error); ok {
			return fmt.Errorf("jq: %w", err)
		}
		if s, ok := r.(string); ok {
			fmt.Fprintln(w, s)
			continue
		}
		if err := writeJSON(w, r, tty); err != nil {
			return err
		}
	}
}

func writeJSON(w io.Writer, v any, tty bool) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if tty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}
