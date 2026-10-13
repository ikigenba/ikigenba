// Command candidates serves candidate evidence over the inherited socket.
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
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/pages"
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
			if s == syscall.SIGTERM {
				cancel(errors.New("SIGTERM"))
			} else {
				cancel(errors.New("SIGINT"))
			}
		case <-ctx.Done():
		}
	}()
	code := cli.Run(ctx, cli.Process{Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv, Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr, Version: v, Inherit: func(fd uintptr) (net.Listener, error) {
		f := os.NewFile(fd, "listener")
		defer func() { _ = f.Close() }()
		return net.FileListener(f)
	}, Now: time.Now, Rand: rand.Reader, After: time.After, Banner: page.New(pages.ServiceName, id).Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: v, Telemetry: w})
	}})
	signal.Stop(signals)
	cancel(context.Canceled)
	os.Exit(code)
}
