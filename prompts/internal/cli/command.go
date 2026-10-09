// Package cli implements the prompts command and its process seam.
package cli

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	prompts "github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
)

// Usage is the command contract declared by the design.
const Usage = "Usage: prompts [command]\n\nRun prompts as agent sessions over the suite's models, with MCP tools at\n/mcp and pages for prompts and their runs at /, on the socket systemd\npasses in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n  agent       run one prompt run (the app starts it)\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest is the command contract declared by the design.
const Manifest = "app = \"prompts\"\ndescription = \"Prompts run by an agent over the suite's models\"\ndefault = false\nmcp = true\nguests = false\nsecrets = [\"ANTHROPIC_API_KEY\", \"OPENAI_API_KEY\", \"GEMINI_API_KEY\", \"XAI_API_KEY\", \"OPENROUTER_API_KEY\"]\n\n[env]\nPROMPT_SECONDS = \"600\"\nOUTPUT_MAX_BYTES = \"1048576\"\nRUN_MAX_TOOL_CALLS = \"50\"\nRUN_MEMORY_MAX_BYTES = \"134217728\"\nRUNS_MEMORY_MAX_BYTES = \"536870912\"\nRUNS_CPU_PERCENT = \"100\"\nRUN_PIDS_MAX = \"64\"\nRUN_MAX_ACTIVE = \"4\"\nRUN_MAX_QUEUED = \"10\"\nRUN_KEEP_DAYS = \"15\"\nRUN_KEEP_COUNT = \"10\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/prompts.db\"\n\n[resources]\nslice = \"apps\"\nmemory_max = \"896M\"\ngo_memory_limit = \"128M\"\ndelegate = true\n"

// NginxConf is the command contract declared by the design.
const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"

// Exit codes shared by commands and the server.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Process supplies the program's operating-system dependencies.
type Process struct {
	Args        []string
	LookupEnv   func(string) (string, bool)
	Unsetenv    func(string) error
	Pid         int
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Version     string
	Inherit     func(uintptr) (net.Listener, error)
	Now         func() time.Time
	Sleep       func(context.Context, time.Duration)
	ScriptAfter func(time.Duration) <-chan time.Time
	Rand        io.Reader
	Dir         string
	Cgroup      string
	Sink        telemetry.Sink
	Banner      func(page.User) page.Banner
	MCP         func(*telemetry.Writer) *mcp.Server
	BaseURL     string
}

// Run dispatches a command or serves until ctx is cancelled.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) == 0 {
		return serve(ctx, p)
	}
	if len(p.Args) == 1 {
		switch p.Args[0] {
		case "--version":
			write(p.Stdout, p.Version+"\n")
			return ExitSuccess
		case "manifest":
			write(p.Stdout, Manifest)
			return ExitSuccess
		case "--help":
			write(p.Stdout, Usage)
			return ExitSuccess
		case agent.Command:
			input := io.NopCloser(strings.NewReader(""))
			if p.Stdin != nil {
				if closer, ok := p.Stdin.(io.ReadCloser); ok {
					input = closer
				} else {
					input = io.NopCloser(p.Stdin)
				}
			}
			return agent.Run(ctx, agent.Process{Stdin: input, Stdout: p.Stdout, Stderr: p.Stderr, LookupEnv: p.LookupEnv, Now: p.Now})
		}
	}
	if len(p.Args) == 2 && p.Args[0] == "db" && p.Args[1] == "status" {
		err := db.Status(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "prompts.db"), Migrations: prompts.Migrations()}, p.Stdout)
		if err != nil {
			write(p.Stderr, "prompts: "+strings.ReplaceAll(err.Error(), "\n", " ")+"\n")
			return ExitServerFailed
		}
		return ExitSuccess
	}
	arg := p.Args[0]
	switch arg {
	case "--version", "manifest", "--help", agent.Command:
		arg = p.Args[1]
	case "db":
		if len(p.Args) > 1 {
			arg = p.Args[1]
			if arg == "status" {
				arg = p.Args[2]
			}
		}
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	write(p.Stderr, "prompts: unknown "+kind+" '"+arg+"'\n\nsee 'prompts --help' for usage\n")
	return ExitUsage
}

func write(w io.Writer, text string) {
	if w != nil {
		_, _ = io.WriteString(w, text)
	}
}
