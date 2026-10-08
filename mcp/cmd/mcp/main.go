// Command mcp serves the platform MCP gateway on its inherited socket.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	appkitmcp "github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/mcp/internal/cli"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func main() {
	v := version.Display()
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
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
	code := cli.Run(ctx, cli.Process{
		Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv,
		Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr, Version: v,
		Banner: page.New(gateway.ServiceName, v).Banner,
		MCP:    func(w *telemetry.Writer) *appkitmcp.Server { return gateway.NewServer(v, w) },
	})
	signal.Stop(signals)
	cancel(nil)
	os.Exit(code)
}
