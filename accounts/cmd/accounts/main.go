// Command accounts manages the host’s SSH accounts.
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

	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
)

func main() {
	id := version.Read()
	v := id.String()
	kit := page.New(pages.ServiceName, id)
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
	code := cli.Run(ctx, cli.Process{Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv, Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr, Version: v, Inherit: inherit, Now: time.Now, Rand: rand.Reader, Banner: kit.Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: v, Telemetry: w})
	}})
	signal.Stop(signals)
	cancel(nil)
	os.Exit(code)
}
func inherit(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "inherited-listener")
	defer func() { _ = f.Close() }()
	return net.FileListener(f)
}
