// Command check-version checks the VERSION file against the git tags and prints
// GitHub Actions outputs:
//
//	version=X.Y.Z
//	tag=vX.Y.Z
//	tagged=true|false
//	prerelease=true|false
//
// VERSION must be a semantic version without a "v" prefix. Unless its tag
// already exists, it must be greater than every v* tag by SemVer precedence.
//
//	go run ./scripts/check-version
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/mod/semver"
)

func main() {
	b, err := os.ReadFile("VERSION")
	if err != nil {
		fail(err)
	}
	out, err := exec.Command("git", "tag", "-l", "v*").Output()
	if err != nil {
		fail(fmt.Errorf("list tags: %w", err))
	}
	version := strings.TrimSpace(string(b))
	tagged, err := check(version, strings.Fields(string(out)))
	if err != nil {
		fail(err)
	}
	fmt.Printf("version=%s\ntag=v%s\ntagged=%t\nprerelease=%t\n", version, version, tagged, isPrerelease(version))
}

// check reports whether the tag of version exists, or an error when version is
// invalid or not above the latest tag.
func check(version string, tags []string) (bool, error) {
	tag := "v" + version
	if strings.HasPrefix(version, "v") || !semver.IsValid(tag) || semver.Canonical(tag) != strings.SplitN(tag, "+", 2)[0] {
		return false, fmt.Errorf("VERSION %q is not a semantic version X.Y.Z", version)
	}
	latest := ""
	for _, t := range tags {
		if t == tag {
			return true, nil
		}
		if semver.IsValid(t) && (latest == "" || semver.Compare(t, latest) > 0) {
			latest = t
		}
	}
	if latest != "" && semver.Compare(tag, latest) <= 0 {
		return false, fmt.Errorf("VERSION %s is not greater than the latest tag %s", version, latest)
	}
	return false, nil
}

// isPrerelease reports whether version has a SemVer prerelease part, as in 0.3.0-rc.1.
func isPrerelease(version string) bool {
	return semver.Prerelease("v"+version) != ""
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "check-version:", err)
	os.Exit(1)
}
