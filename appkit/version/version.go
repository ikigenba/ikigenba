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

// Identity holds the release label and short commit supplied by the host.
type Identity struct {
	Release string
	Commit  string
}

// Read reads the host's current code identity, shortening its commit.
func Read() Identity {
	commit := os.Getenv(CommitVariable)
	release := os.Getenv(ReleaseVariable)
	if commit == "" {
		return Identity{Release: release}
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
	return Identity{Release: release, Commit: short}
}

// String formats the identity's values without altering them.
func (id Identity) String() string {
	if id.Commit == "" {
		return id.Release
	}
	if id.Release == "" {
		return id.Commit
	}
	return id.Release + " (" + id.Commit + ")"
}

// Display reads the host's current code identity and formats it for display.
func Display() string {
	return Read().String()
}
