package cmdutil

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// BrowserEnv names the browser command for --web; BROWSER is read after it.
const BrowserEnv = "MW_BROWSER"

// OpenBrowser opens url with MW_BROWSER, BROWSER or the system opener.
func OpenBrowser(url string) error {
	for _, env := range []string{BrowserEnv, "BROWSER"} {
		if b := strings.TrimSpace(os.Getenv(env)); b != "" {
			args := strings.Fields(b)
			return exec.Command(args[0], append(args[1:], url)...).Start()
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
