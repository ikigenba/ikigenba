// Command sessions serves the business's agent session records.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
)

func main() {
	id := version.Read()
	os.Exit(run(id))
}

func run(id version.Identity) int {
	display := id.String()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	stops := make(chan os.Signal, 1)
	signal.Notify(stops, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(stops)
	go func() {
		select {
		case sig := <-stops:
			name := "SIGTERM"
			if sig == syscall.SIGINT {
				name = "SIGINT"
			}
			cancel(errors.New(name))
		case <-ctx.Done():
		}
	}()
	return cli.Run(ctx, cli.Process{
		Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv,
		Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr, Version: display,
		Inherit: inherit, Now: time.Now, Rand: rand.Reader,
		Banner: page.New(pages.ServiceName, id).Banner,
		MCP: func(w *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: display, Telemetry: w})
		},
	})
}

func inherit(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "inherited listener")
	defer func() { _ = f.Close() }()
	return net.FileListener(f)
}
