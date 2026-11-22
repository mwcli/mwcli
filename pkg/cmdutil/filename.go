package cmdutil

import (
	"fmt"
	"strings"
)

// windowsReserved are base names Windows does not allow for files.
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SafeFileName writes each character of name that a file name cannot hold on
// Windows, macOS or Linux, and % itself, as %XX, so that name can be read
// back: / \ : * ? " < > |, control characters, a leading dot, a final dot or
// space, and the first letter of a name Windows reserves, such as CON.
func SafeFileName(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|%`, r):
			fmt.Fprintf(&b, "%%%02X", r)
		case (r == '.' || r == ' ') && i == len(name)-1:
			// Windows drops a final dot or space.
			fmt.Fprintf(&b, "%%%02X", r)
		case r == '.' && i == 0:
			// A leading dot hides the file, and . or .. are not files.
			b.WriteString("%2E")
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if base, _, _ := strings.Cut(s, "."); windowsReserved[strings.ToUpper(base)] {
		s = fmt.Sprintf("%%%02X", s[0]) + s[1:]
	}
	return s
}
