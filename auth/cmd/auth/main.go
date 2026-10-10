// Command auth serves the platform auth service.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/auth/internal/cli"
)

func main() {
	id := version.Read()
	kit := page.New("auth", id)
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-signals
		reason := "SIGTERM"
		if sig == syscall.SIGINT {
			reason = "SIGINT"
		}
		cancel(errors.New(reason))
	}()
	code := cli.Run(ctx, cli.Process{
		Args:       os.Args[1:],
		LookupEnv:  os.LookupEnv,
		Unsetenv:   os.Unsetenv,
		Pid:        os.Getpid(),
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Version:    id.String(),
		Now:        time.Now,
		Rand:       rand.Reader,
		OIDCIssuer: "https://accounts.google.com",
		Banner:     kit.Banner,
		Sink:       telemetry.NewSocketSink(),
	})
	signal.Stop(signals)
	os.Exit(code)
}
