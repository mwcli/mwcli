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
// C:\Program Files\...; otherwise it is split like a shell command line, so
// quoted paths and extra arguments work.
func browserCommand(mwBrowser, browser, goos, url string) (string, []string, error) {
	for _, env := range []struct{ name, value string }{{BrowserEnv, mwBrowser}, {"BROWSER", browser}} {
		v := strings.TrimSpace(env.value)
		if v == "" {
			continue
		}
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return v, []string{url}, nil
		}
		words, err := shellquote.Split(v)
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
