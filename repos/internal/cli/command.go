package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/repos"
)

// Run serves when Args is empty, otherwise handles a command or usage error.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) == 0 {
		return serve(ctx, p)
	}
	if len(p.Args) == 2 && p.Args[0] == "db" && p.Args[1] == "status" {
		err := db.Status(context.Background(), db.Config{Path: filepath.Join(p.Dir, "state", "repos.db"), Migrations: repos.Migrations()}, p.Stdout)
		if err != nil {
			_, _ = p.Stderr.Write([]byte("repos: " + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"))
			return ExitServerFailed
		}
		return ExitSuccess
	}
	var product string
	known := true
	switch p.Args[0] {
	case "--version":
		product = Version + "\n"
	case "manifest":
		product = Manifest
	case "--help":
		product = Usage
	default:
		known = false
	}
	if known && len(p.Args) == 1 {
		_, _ = p.Stdout.Write([]byte(product))
		return ExitSuccess
	}
	arg := p.Args[0]
	if p.Args[0] == "db" && len(p.Args) > 1 {
		arg = p.Args[1]
		if arg == "status" {
			arg = p.Args[2]
		}
	} else if known {
		arg = p.Args[1]
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	_, _ = p.Stderr.Write([]byte("repos: unknown " + kind + " '" + arg + "'\n\nsee 'repos --help' for usage\n"))
	return ExitUsage
}
