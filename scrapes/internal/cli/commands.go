package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/db"
	app "github.com/ikigenba/ikigenba/scrapes"
)

// Exit codes distinguish success, serving failure and usage errors.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage is the command help text.
const Usage = "Usage: scrapes [command]\n\nPublic records for agents: MCP tools at /mcp start scrapers and read\ntheir runs and rows, with a page of runs at /, and each run's rows\nstreamed to siblings, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest declares the app to platform deployment tools.
const Manifest = "app = \"scrapes\"\ndescription = \"Public records about businesses in range, scraped per source as rows per run\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[env]\nSCRAPE_KEEP_RUNS = \"2\"\nSCRAPE_WRITE_BUDGET = \"4096000\"\nSCRAPE_PRESSURE_LIMIT = \"10\"\nFMCSA_STATES = \"MN,WI\"\nFMCSA_MIN_POWER_UNITS = \"5\"\nFMCSA_STATUS = \"A\"\nPPP_FILES = \"public_150k_plus_240930.csv\"\nPPP_STATES = \"MN,WI\"\nMN_PARCELS_COUNTIES = \"Carlton,Chisago,Isanti,Mille Lacs,Aitkin,St. Louis,Anoka,Washington,Sherburne,Stearns,Benton,Morrison,Crow Wing\"\nFORM5500_YEARS = \"2025,2024,2023,2022,2021,2020,2019\"\nFORM5500_STATES = \"MN,WI\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/scrapes.db\"\n\n[resources]\nmemory_max = \"256M\"\n\n[home]\ngroup = \"application\"\n"

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
		err := db.Status(context.Background(), db.Config{Path: filepath.Join(p.Dir, "state", "scrapes.db"), Migrations: app.Migrations()}, p.Stdout)
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
	return diagnostic(p, "unknown "+kind+" '"+arg+"'\n\nsee 'scrapes --help' for usage", ExitUsage)
}
