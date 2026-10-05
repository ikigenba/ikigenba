// Package cli implements scripts' command and service entry point.
package cli

import (
	"fmt"
	"io"
	"strings"
)

// Version is the release version reported by the service.
var Version = "v0.1.0"

// Exit codes distinguish a successful command, a server failure and incorrect usage.
const (
	ExitSuccess = iota
	ExitServerFailed
	ExitUsage
)

// Usage describes the command's public interface.
const Usage = `Usage: scripts [command]

Run Python scripts from the suite's repositories, with MCP tools at /mcp
and pages for scripts and their runs at /, on the socket systemd passes in.
With no command, serve.

Commands:
  manifest   print the app manifest

Options:
  --help      print this help
  --version   print the version

Exit codes:
  0  success
  1  the server failed
  2  usage error
`

// Manifest describes the service to the host's deployment tools.
const Manifest = `app = "scripts"
description = "Python scripts run from the suite's repositories"
default = false
mcp = true
secrets = []

[env]
REPOS_DIR = "../repos/state/repos"
TREE_MAX_BYTES = "268435456"
OUTPUT_MAX_BYTES = "1048576"
OPERATION_SECONDS = "600"
SCRIPT_SECONDS = "600"
RUN_KEEP_DAYS = "15"
RUN_KEEP_COUNT = "10"

[database]
engine = "sqlite"
path = "state/scripts.db"

[resources]
cpu_weight = 50
memory_max = "1G"
io_weight = 50
`

func command(args []string, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 {
		return false, ExitSuccess
	}
	var text string
	switch args[0] {
	case "--version":
		text = Version + "\n"
	case "manifest":
		text = Manifest
	case "--help":
		text = Usage
	}
	if text != "" && len(args) == 1 {
		_, _ = io.WriteString(stdout, text)
		return true, ExitSuccess
	}
	arg := args[0]
	if text != "" {
		arg = args[1]
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	diagnostic := fmt.Sprintf("scripts: unknown %s '%s'\n\nsee 'scripts --help' for usage\n", kind, arg)
	_, _ = stderr.Write([]byte(diagnostic))
	return true, ExitUsage
}
