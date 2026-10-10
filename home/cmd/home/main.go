// Command home serves the space's front door on its inherited socket.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/home/internal/cli"
	"github.com/ikigenba/ikigenba/home/internal/pages"
)

func main() { os.Exit(run()) }

func run() int {
	id := version.Read()
	kit := page.New(pages.ServiceName, id)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	go func() {
		select {
		case sig := <-signals:
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
		Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr,
		Version: id.String(), Banner: kit.Banner,
	})
}
