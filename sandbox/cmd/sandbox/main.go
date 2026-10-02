// Command sandbox runs the local development sandbox.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikigenba/ikigenba/sandbox/internal/cli"
	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	dir, err := os.Getwd()
	if err != nil {
		dir = ""
	}
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, seam.Deps{
		Dir: dir, EUID: os.Geteuid(), Getenv: os.Getenv, Exec: seam.Exec, Stream: seam.Stream,
	})
	stop()
	os.Exit(code)
}
