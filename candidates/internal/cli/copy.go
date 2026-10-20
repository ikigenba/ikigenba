// Package cli implements the command and its injected process boundary.
package cli

import "github.com/ikigenba/ikigenba/candidates/internal/pages"

// Usage is the complete command help.
const Usage = `Usage: candidates [command]

Candidate businesses for agents: MCP tools at /mcp keep the owner's prospect lists,
read each candidate with its evidence and record the owner's rulings,
with the prospect lists and the passes at /, on the socket systemd
passes in.
With no command, serve.

Commands:
  manifest    print the app manifest
  db status   print applied and pending migrations

Options:
  --help      print this help
  --version   print the version

Exit codes:
  0  success
  1  failure
  2  usage error
`

// Manifest is the host-readable service manifest.
const Manifest = `app = "candidates"
description = "` + pages.Description + `"
default = false
mcp = true
guests = false
secrets = []

[env]
CANDIDATES_KEEP_PASSES = "20"

[database]
engine = "sqlite"
path = "state/candidates.db"

[resources]
memory_max = "256M"

[home]
group = "application"
`

// NginxConf keeps event bus endpoints off the public host.
const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"

// Exit codes distinguish success, runtime failure, and usage errors.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)
