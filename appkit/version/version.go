// Package version carries the code identity supplied by the host.
package version

import (
	"os"
	"strings"
)

// CommitVariable names the environment variable holding the built commit.
const CommitVariable = "IKIGENBA_COMMIT"

// ReleaseVariable names the environment variable holding the release label.
const ReleaseVariable = "IKIGENBA_RELEASE"

// Display reads the host's current code identity and formats it for display.
func Display() string {
	commit := os.Getenv(CommitVariable)
	release := os.Getenv(ReleaseVariable)
	if commit == "" {
		return release
	}

	dirty := strings.HasSuffix(commit, "-dirty")
	runes := []rune(strings.TrimSuffix(commit, "-dirty"))
	if len(runes) > 7 {
		runes = runes[:7]
	}
	short := string(runes)
	if dirty {
		short += "-dirty"
	}
	if release == "" {
		return short
	}
	return release + " (" + short + ")"
}
