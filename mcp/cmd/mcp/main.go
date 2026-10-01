// Command mcp serves the platform MCP gateway on its inherited socket.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/mcp/internal/cli"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	code := cli.Run(ctx, cli.Process{
		Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv,
		Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr,
		Banner: page.New(gateway.ServiceName, cli.Version).Banner,
		MCP:    gateway.NewServer(cli.Version, os.Stderr),
	})
	stop()
	os.Exit(code)
}
