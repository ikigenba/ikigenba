// Command sites serves the platform's static sites.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
)

func main() {
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-signals
		reason := "SIGTERM"
		if sig == syscall.SIGINT {
			reason = "SIGINT"
		}
		cancel(errors.New(reason))
	}()
	v := version.Display()
	kit := page.New(pages.ServiceName, v)
	os.Exit(cli.Run(ctx, cli.Process{
		Args: os.Args[1:], Version: v, LookupEnv: os.LookupEnv, Environ: os.Environ,
		Unsetenv: os.Unsetenv, Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr,
		After: time.After, Banner: kit.Banner,
		MCP: func(w *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: v, Telemetry: w})
		},
	}))
}
