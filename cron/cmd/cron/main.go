// Command cron serves scheduled triggers on a socket inherited from systemd.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/cron/internal/cli"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
)

func main() { os.Exit(runMain()) }

func runMain() int {
	display := version.Display()
	kit := page.New(pages.ServiceName, display)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	go func() {
		select {
		case sig := <-signals:
			if sig == syscall.SIGTERM {
				cancel(errors.New("SIGTERM"))
			} else {
				cancel(errors.New("SIGINT"))
			}
		case <-ctx.Done():
		}
	}()
	return cli.Run(ctx, cli.Process{Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv, Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr, Version: display, Banner: kit.Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: display, Telemetry: w})
	}})
}
