// Package version implements `mw version`.
package version

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zetaoss/mwcli/internal/build"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
)

// Format returns the text printed by `mw version` and `mw --version`.
func Format(version, date string) string {
	version = strings.TrimPrefix(version, "v")
	dateStr := ""
	if date != "" {
		dateStr = fmt.Sprintf(" (%s)", date)
	}
	return fmt.Sprintf("mw version %s%s\n%s\n", version, dateStr, releaseURL(version))
}

func releaseURL(version string) string {
	if version == "DEV" || strings.Contains(version, "-") || strings.Contains(version, "+") {
		return "https://github.com/zetaoss/mwcli/releases/latest"
	}
	return "https://github.com/zetaoss/mwcli/releases/tag/v" + version
}

// NewCmdVersion returns `mw version`.
func NewCmdVersion(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:    "version",
		Short:  "Show mw version",
		Hidden: true,
		Args:   cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprint(f.IOStreams.Out, Format(build.Version, build.Date))
		},
	}
}
