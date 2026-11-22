package cmdutil

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/kballard/go-shellquote"
)

// BrowserEnv names the browser command for --web; BROWSER is read after it.
const BrowserEnv = "MW_BROWSER"

// OpenBrowser opens url with MW_BROWSER, BROWSER or the system opener.
func OpenBrowser(url string) error {
	name, args, err := browserCommand(os.Getenv(BrowserEnv), os.Getenv("BROWSER"), runtime.GOOS, url)
	if err != nil {
		return err
	}
	return exec.Command(name, args...).Start()
}

// browserCommand returns the program and arguments that open url. A browser
// setting that names an existing file is that program, spaces included, as in
// C:\Program Files\...; otherwise it is split like a command line of the
// platform, so quoted paths and extra arguments work.
func browserCommand(mwBrowser, browser, goos, url string) (string, []string, error) {
	for _, env := range []struct{ name, value string }{{BrowserEnv, mwBrowser}, {"BROWSER", browser}} {
		v := strings.TrimSpace(env.value)
		if v == "" {
			continue
		}
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return v, []string{url}, nil
		}
		words, err := splitCommand(v, goos)
		if err != nil {
			return "", nil, fmt.Errorf("parse %s: %w", env.name, err)
		}
		if len(words) == 0 {
			continue
		}
		return words[0], append(words[1:], url), nil
	}
	switch goos {
	case "darwin":
		return "open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	}
	return "xdg-open", []string{url}, nil
}

// splitCommand splits a command line: with the Windows rules on Windows, where
// backslashes in paths are kept, and with POSIX shell rules elsewhere.
func splitCommand(s, goos string) ([]string, error) {
	if goos == "windows" {
		return splitWindows(s)
	}
	return shellquote.Split(s)
}

// splitWindows follows CommandLineToArgvW: spaces and tabs separate words,
// double quotes group them, and backslashes are literal unless they come
// before a double quote, where 2n backslashes give n and a quote that groups,
// and 2n+1 give n and a literal quote.
func splitWindows(s string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord, quoted := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(s) && s[i] == '\\' {
				n++
				i++
			}
			if i < len(s) && s[i] == '"' {
				word.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					word.WriteByte('"')
				} else {
					quoted = !quoted
				}
			} else {
				word.WriteString(strings.Repeat(`\`, n))
				i--
			}
			inWord = true
		case c == '"':
			quoted = !quoted
			inWord = true
		case (c == ' ' || c == '\t') && !quoted:
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	if quoted {
		return nil, fmt.Errorf("unterminated quote in %q", s)
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}
