// Package main wires real process dependencies into the dory CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/ikigenba/ikigenba/dory/internal/cli"
)

func main() {
	home, _ := os.UserHomeDir()
	root, _ := os.Getwd()

	signals := make(chan os.Signal, 1)
	interrupts := make(chan struct{})
	signal.Notify(signals, syscall.SIGINT)
	go func() {
		for range signals {
			interrupts <- struct{}{}
		}
	}()

	code := cli.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.Deps{
		Home:       home,
		Getenv:     os.Getenv,
		Now:        time.Now,
		SessionID:  uuid.NewString(),
		Root:       root,
		Interrupts: interrupts,
	})
	signal.Stop(signals)
	os.Exit(code)
}
