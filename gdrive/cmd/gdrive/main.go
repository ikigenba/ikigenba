// Command gdrive serves the Google Drive connector.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/gdrive/internal/cli"
	"github.com/ikigenba/ikigenba/gdrive/internal/pages"
)

func main() { os.Exit(run()) }
func run() int {
	id := version.Read()
	v := id.String()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	go func() {
		select {
		case s := <-signals:
			name := "SIGINT"
			if s == syscall.SIGTERM {
				name = "SIGTERM"
			}
			cancel(fmt.Errorf("%s", name))
		case <-ctx.Done():
		}
	}()
	return cli.Run(ctx, cli.Process{Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv, Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr, Version: v, Banner: page.New(pages.ServiceName, id).Banner, After: time.After, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: v, Telemetry: w})
	}})
}
