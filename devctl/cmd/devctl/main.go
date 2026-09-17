package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cli"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud/awssdk"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devctl: determine working directory: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	deps := seam.Deps{
		Dir:    dir,
		EUID:   os.Geteuid(),
		Getenv: os.Getenv,
		Cloud:  awssdk.Open,
		Exec:   seam.Exec,
		Stream: seam.Stream,
		Now:    time.Now,
		After:  time.After,
	}
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, deps)
	cancel()
	os.Exit(code)
}
