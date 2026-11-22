package cmdutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/spf13/cobra"
)

// JSONFlags are --json FIELDS and --jq EXPR.
type JSONFlags struct {
	fields []string
	jq     string
	known  []string
	cmd    *cobra.Command
}

// AddJSONFlags adds --json FIELDS and --jq EXPR to cmd; known lists the fields.
func AddJSONFlags(cmd *cobra.Command, known []string) *JSONFlags {
	j := &JSONFlags{known: known, cmd: cmd}
	cmd.Flags().StringSliceVar(&j.fields, "json", nil, "Output JSON with the specified `fields`")
	cmd.Flags().StringVarP(&j.jq, "jq", "q", "", "Filter JSON output using a jq `expression`")
	// A bare --json lists the fields instead of failing on the missing value.
	parent := cmd.FlagErrorFunc()
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "flag needs an argument: --json") {
			return j.fieldsError()
		}
		return parent(c, err)
	})
	return j
}

// Enabled reports whether JSON output was asked for, even with no fields
// (--json=""), so that an empty field list is an error and not plain output.
func (j *JSONFlags) Enabled() bool { return j.cmd.Flags().Changed("json") }

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
	if len(j.fields) == 0 {
		return j.fieldsError()
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

// Write prints the requested fields of v, filtered by --jq when given. v is a
// struct whose JSON names are the known fields, or a slice of them, printed as
// an array. Output is indented on a terminal.
func (j *JSONFlags) Write(w io.Writer, v any, tty bool) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var out any
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '[' || string(t) == "null" && isList(v) {
		var many []map[string]any
		if err := json.Unmarshal(b, &many); err != nil {
			return err
		}
		list := make([]any, 0, len(many))
		for _, m := range many {
			list = append(list, j.pick(m))
		}
		out = list
	} else {
		var one map[string]any
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		out = j.pick(one)
	}
	if j.jq == "" {
		return writeJSON(w, out, tty)
	}
	var generic any
	b, _ = json.Marshal(out)
	_ = json.Unmarshal(b, &generic)
	return WriteJQ(w, j.jq, generic, tty)
}

// CompileJQ parses a --jq expression.
func CompileJQ(expr string) (*gojq.Code, error) {
	q, err := gojq.Parse(expr)
	if err != nil {
		return nil, FlagErrorf("invalid --jq expression: %v", err)
	}
	code, err := gojq.Compile(q)
	if err != nil {
		return nil, FlagErrorf("invalid --jq expression: %v", err)
	}
	return code, nil
}

// WriteJQ prints the results of expr on v, a value decoded from JSON: strings
// as plain lines, other values as JSON, indented on a terminal.
func WriteJQ(w io.Writer, expr string, v any, tty bool) error {
	code, err := CompileJQ(expr)
	if err != nil {
		return err
	}
	iter := code.Run(v)
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

// isList reports whether v is a slice or array, so that a nil one prints as [].
func isList(v any) bool {
	k := reflect.ValueOf(v).Kind()
	return k == reflect.Slice || k == reflect.Array
}

func (j *JSONFlags) pick(all map[string]any) map[string]any {
	out := map[string]any{}
	for _, f := range j.fields {
		out[f] = all[f]
	}
	return out
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
