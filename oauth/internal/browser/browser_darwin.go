//go:build darwin

package browser

import "os/exec"

var newCommand commandFactory = func(_ string, args ...string) command {
	// The authorize URL is intentionally the sole argument to a fixed executable.
	//nolint:gosec // G204 treats the required user-derived URL argument as a command name.
	return exec.Command("open", args[0])
}

// New returns the macOS browser launcher.
func New() Launcher {
	return platformLauncher{commandName: "open"}
}
