// Package build holds the version information of the mw binary.
package build

import (
	"runtime/debug"
	"strings"
)

// Version and Date are set at build time with
// -ldflags "-X github.com/mwcli/mwcli/internal/build.Version=... -X ...Date=...".
// Without them, the module version recorded by `go install ...@vX.Y.Z` is used.
var (
	Version = "DEV"
	Date    = ""
)

func init() {
	if len(Date) > 10 {
		Date = Date[:10]
	}
	if Version != "DEV" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		Version = strings.TrimPrefix(v, "v")
	}
	if Date == "" {
		for _, s := range info.Settings {
			if s.Key == "vcs.time" && len(s.Value) >= 10 {
				Date = s.Value[:10]
			}
		}
	}
}
