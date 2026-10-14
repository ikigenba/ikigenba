// Command scrapes serves the platform's public-record scraper service.
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
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
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
		Inherit: func(fd uintptr) (net.Listener, error) {
			f := os.NewFile(fd, "inherited-listener")
			defer func() { _ = f.Close() }()
			return net.FileListener(f)
		},
		Now: time.Now, Rand: rand.Reader, After: time.After, Pressure: cli.PressureFile,
		Banner: page.New(pages.ServiceName, id).Banner,
		MCP: func(w *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: v, Telemetry: w})
		},
	})
	cancel(nil)
	signal.Stop(signals)
	os.Exit(code)
}
