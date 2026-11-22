package cmdutil

import (
	"strings"
	"time"
)

// ShortTime shows an API timestamp to the minute, as 2026-11-22 08:41.
func ShortTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// OneLine folds all white space into single spaces, for a terminal.
func OneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// tsvReplacer turns each line break or tab, which would split a line or a
// column, into one space and leaves the rest of the text as it is.
var tsvReplacer = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

// TSVField makes s safe as one column of a tab-separated line.
func TSVField(s string) string { return tsvReplacer.Replace(s) }
