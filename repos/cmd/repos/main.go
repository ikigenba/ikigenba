// Command repos serves the suite's repositories on its inherited socket.
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
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

func main() {
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case sig := <-signals:
			name := "SIGTERM"
			if sig == syscall.SIGINT {
				name = "SIGINT"
			}
			cancel(errors.New(name))
		case <-ctx.Done():
		}
	}()
	code := cli.Run(ctx, cli.Process{
		Args: os.Args[1:], LookupEnv: os.LookupEnv, Environ: os.Environ,
		Unsetenv: os.Unsetenv, Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr,
		Banner: page.New(web.ServiceName, cli.Version).Banner,
		MCP: func(w *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: cli.Version, Telemetry: w})
		},
	})
	signal.Stop(signals)
	cancel(nil)
	os.Exit(code)
}
