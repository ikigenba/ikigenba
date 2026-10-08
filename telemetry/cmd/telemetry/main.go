// Package main wires telemetry to the host process and socket activation.
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
	"github.com/ikigenba/ikigenba/telemetry/internal/cli"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

func main() {
	v := version.Display()
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case received := <-signals:
			if received == syscall.SIGTERM {
				cancel(errors.New("SIGTERM"))
			} else {
				cancel(errors.New("SIGINT"))
			}
		case <-ctx.Done():
		}
	}()
	kit := page.New(web.ServiceName, v)
	code := cli.Run(ctx, cli.Process{
		Args: os.Args[1:], LookupEnv: os.LookupEnv, Pid: os.Getpid(),
		Stdout: os.Stdout, Stderr: os.Stderr, Unsetenv: os.Unsetenv,
		Version: v, Banner: kit.Banner,
		MCP: func(writer *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: v, Telemetry: writer})
		},
	})
	signal.Stop(signals)
	cancel(nil)
	os.Exit(code)
}
