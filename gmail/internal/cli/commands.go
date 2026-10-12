package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/db"
	app "github.com/ikigenba/ikigenba/gmail"
)

// Exit codes distinguish success, serving failure and usage errors.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage is the command help text.
const Usage = "Usage: gmail [command]\n\nGmail for agents: MCP tools at /mcp read mail, compose and send drafts,\nand record transfers that move attachments on the host, with a page of\ndrafts and transfers at /, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest declares the app to platform deployment tools.
const Manifest = "app = \"gmail\"\ndescription = \"Gmail messages read, attachments pulled into artifacts, drafts composed and sent\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[env]\nTRANSFER_MAX_BYTES = \"52428800\"\nTRANSFER_MAX_ACTIVE = \"2\"\nTRANSFER_MAX_QUEUED = \"10\"\nTRANSFER_KEEP_DAYS = \"15\"\nTRANSFER_KEEP_COUNT = \"100\"\nDRAFT_KEEP_DAYS = \"15\"\nDRAFT_KEEP_COUNT = \"100\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/gmail.db\"\n\n[resources]\nmemory_max = \"256M\"\n\n[home]\ngroup = \"core\"\n"

// NginxConf keeps the bus paths private at the public name.
const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"

func command(_ context.Context, p Process) (int, bool) {
	if len(p.Args) == 0 {
		return 0, false
	}
	if len(p.Args) == 1 {
		var output string
		switch p.Args[0] {
		case "--version":
			output = p.Version + "\n"
		case "--help":
			output = Usage
		case "manifest":
			output = Manifest
		default:
			return commandError(p, p.Args[0]), true
		}
		_, _ = p.Stdout.Write([]byte(output))
		return ExitSuccess, true
	}
	if len(p.Args) == 2 && p.Args[0] == "db" && p.Args[1] == "status" {
		err := db.Status(context.Background(), db.Config{Path: filepath.Join(p.Dir, "state", "gmail.db"), Migrations: app.Migrations()}, p.Stdout)
		if err != nil {
			return diagnostic(p, strings.ReplaceAll(err.Error(), "\n", " "), ExitServerFailed), true
		}
		return ExitSuccess, true
	}
	arg := p.Args[0]
	switch arg {
	case "db":
		arg = p.Args[1]
		if arg == "status" && len(p.Args) > 2 {
			arg = p.Args[2]
		}
	case "--version", "--help", "manifest":
		arg = p.Args[1]
	}
	return commandError(p, arg), true
}

func commandError(p Process, arg string) int {
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	return diagnostic(p, "unknown "+kind+" '"+arg+"'\n\nsee 'gmail --help' for usage", ExitUsage)
}
