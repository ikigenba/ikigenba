package cli

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/db"
	cron "github.com/ikigenba/ikigenba/cron"
)

// Usage is the published command text.
const Usage = "Usage: cron [command]\n\nKeep triggers that emit events on the suite's event bus on a schedule,\nwith MCP tools at /mcp and a page of triggers at /, on the socket\nsystemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest is the published command text.
const Manifest = "app = \"cron\"\ndescription = \"Triggers that emit events on a schedule\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/cron.db\"\n\n[resources]\nmemory_max = \"128M\"\n"

// NginxConf is the published command text.
const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"

// Command exit codes distinguish success, failure and usage errors.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

func dispatchCommand(ctx context.Context, p Process) (int, bool) {
	if len(p.Args) == 0 {
		return 0, false
	}
	if len(p.Args) == 1 {
		switch p.Args[0] {
		case "--version":
			_, _ = io.WriteString(p.Stdout, p.Version+"\n")
			return ExitSuccess, true
		case "manifest":
			_, _ = io.WriteString(p.Stdout, Manifest)
			return ExitSuccess, true
		case "--help":
			_, _ = io.WriteString(p.Stdout, Usage)
			return ExitSuccess, true
		}
	}
	if len(p.Args) == 2 && p.Args[0] == "db" && p.Args[1] == "status" {
		err := db.Status(context.WithoutCancel(ctx), db.Config{Path: filepath.Join(p.Dir, "state", "cron.db"), Migrations: cron.Migrations()}, p.Stdout)
		if err != nil {
			_, _ = p.Stderr.Write([]byte("cron: " + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"))
			return ExitServerFailed, true
		}
		return ExitSuccess, true
	}
	arg := p.Args[0]
	if arg == "db" && len(p.Args) > 1 {
		arg = p.Args[1]
		if arg == "status" {
			arg = p.Args[2]
		}
	} else if (arg == "--version" || arg == "manifest" || arg == "--help") && len(p.Args) > 1 {
		arg = p.Args[1]
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	_, _ = p.Stderr.Write([]byte("cron: unknown " + kind + " '" + arg + "'\n\nsee 'cron --help' for usage\n"))
	return ExitUsage, true
}
