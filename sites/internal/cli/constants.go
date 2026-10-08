// Package cli runs sites as a command and a socket-activated service.
package cli

// Usage is the complete command help text.
const Usage = "Usage: sites [command]\n\nServe static sites from the suite's repositories at /<slug>/, MCP tools at\n/mcp, and a landing page at /, on the socket systemd passes in. With no\ncommand, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest is the service deployment contract.
const Manifest = "app = \"sites\"\ndescription = \"Static sites from the suite's repositories\"\ndefault = false\nmcp = true\nguests = true\nsecrets = []\n\n[env]\nREPOS_DIR = \"../repos/state/repos\"\nSITE_MAX_BYTES = \"268435456\"\nOPERATION_SECONDS = \"600\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/sites.db\"\n\n[resources]\nmemory_max = \"128M\"\n"

// Exit codes distinguish success, server failure and usage error.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Version is the release version reported by sites.
var Version = "v0.4.3"
