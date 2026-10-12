// Command gmail serves the connector on a systemd-inherited listening socket.
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
	"github.com/ikigenba/ikigenba/gmail/internal/cli"
	"github.com/ikigenba/ikigenba/gmail/internal/pages"
)

func main() {
	id := version.Read()
	v := id.String()
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case s := <-signals:
			name := "SIGTERM"
			if s == syscall.SIGINT {
				name = "SIGINT"
			}
			cancel(errors.New(name))
		case <-ctx.Done():
		}
	}()
	code := cli.Run(ctx, cli.Process{
		Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv,
		Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr, Version: v,
		Banner: page.New(pages.ServiceName, id).Banner, Inherit: inherit,
		Now: time.Now, Rand: rand.Reader, After: time.After,
		MCP: func(w *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: v, Telemetry: w})
		},
	})
	signal.Stop(signals)
	cancel(nil)
	os.Exit(code)
}

func inherit(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "systemd-listener")
	defer func() { _ = f.Close() }()
	return net.FileListener(f)
}
