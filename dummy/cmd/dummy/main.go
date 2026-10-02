// Package main provides the dummy executable.
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
	"github.com/ikigenba/ikigenba/dummy/internal/cli"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

func main() {
	kit := page.New(panel.ServiceName, cli.Version)
	gate := cli.NewGate(telemetry.NewSocketSink())
	writer := telemetry.New(telemetry.Config{Service: panel.ServiceName, Version: cli.Version, Sink: gate, Stderr: os.Stderr})
	srv := mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Version: cli.Version, Telemetry: writer})
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signals
		reason := "SIGTERM"
		if sig == os.Interrupt {
			reason = "SIGINT"
		}
		cancel(errors.New(reason))
	}()
	exit := cli.Run(ctx, cli.Process{
		Args:      os.Args[1:],
		LookupEnv: os.LookupEnv,
		Unsetenv:  os.Unsetenv,
		Pid:       os.Getpid(),
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Banner:    kit.Banner,
		MCP:       srv,
		Telemetry: writer,
		Gate:      gate,
	})
	signal.Stop(signals)
	os.Exit(exit)
}
