// Command events runs the suite's internal event bus.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/cli"
)

func main() { os.Exit(run()) }

func run() int {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	go func() {
		select {
		case received := <-signals:
			name := "SIGTERM"
			if received == syscall.SIGINT {
				name = "SIGINT"
			}
			cancel(errors.New(name))
		case <-ctx.Done():
		}
	}()
	return cli.Run(ctx, cli.Process{Args: os.Args[1:], LookupEnv: os.LookupEnv, Unsetenv: os.Unsetenv, Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr, Sink: telemetry.NewSocketSink()})
}
