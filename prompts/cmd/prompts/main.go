// Command prompts serves the prompt catalog or runs its agent role.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/prompts/internal/cli"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
)

func main() {
	v := version.Display()
	kit := page.New(pages.ServiceName, v)
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case s := <-signals:
			name := "SIGTERM"
			if s == syscall.SIGINT {
				name = "SIGINT"
			}
			cancel(errors.New(name))
		case <-ctx.Done():
		}
	}()
	code := cli.Run(ctx, cli.Process{
		Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv,
		Pid: os.Getpid(), Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Version: v, Banner: kit.Banner, ScriptAfter: time.After, Cgroup: cgroup(),
		MCP: func(w *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: v, Telemetry: w})
		},
	})
	signal.Stop(signals)
	cancel(nil)
	os.Exit(code)
}

func cgroup() string {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			return filepath.Join("/sys/fs/cgroup", path)
		}
	}
	return ""
}
