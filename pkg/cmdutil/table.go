package cmdutil

import (
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
)

// WriteTable prints rows with each column but the last padded to its widest
// cell, two spaces apart. Widths count display cells, so Korean, Japanese or
// Chinese text, two cells a character, stays aligned.
func WriteTable(w io.Writer, rows [][]string) error {
	widths := map[int]int{}
	for _, row := range rows {
		for i, cell := range row[:max(len(row)-1, 0)] {
			widths[i] = max(widths[i], runewidth.StringWidth(cell))
		}
	}
	var b strings.Builder
	for _, row := range rows {
		for i, cell := range row {
			if i == len(row)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(runewidth.FillRight(cell, widths[i]+2))
		}
		line := strings.TrimRight(b.String(), " ")
		b.Reset()
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// Truncate shortens s to at most n display cells, ending with "…" when cut.
func Truncate(s string, n int) string {
	return runewidth.Truncate(s, n, "…")
}
