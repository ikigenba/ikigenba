// Package main provides the dummy executable.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/dummy/internal/cli"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

func main() {
	kit := page.New(panel.ServiceName, cli.Version)
	srv := mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Version: cli.Version, Stderr: os.Stderr})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	exit := cli.Run(ctx, cli.Process{
		Args:      os.Args[1:],
		LookupEnv: os.LookupEnv,
		Unsetenv:  os.Unsetenv,
		Pid:       os.Getpid(),
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Banner:    kit.Banner,
		MCP:       srv,
	})
	stop()
	os.Exit(exit)
}
