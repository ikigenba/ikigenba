// Command agent-repl starts an interactive agent session.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/ikigenba/ikigenba/agent-repl/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	home, err := os.UserHomeDir()
	if err != nil {
		return environmentError("find home directory", err)
	}
	root, err := os.Getwd()
	if err != nil {
		return environmentError("find working directory", err)
	}

	signals := make(chan os.Signal, 1)
	interrupts := make(chan struct{})
	signal.Notify(signals, syscall.SIGINT)
	defer signal.Stop(signals)
	go forwardInterrupts(signals, interrupts)

	return cli.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.Deps{
		Home:       home,
		Getenv:     os.Getenv,
		Now:        time.Now,
		LogID:      uuid.NewString(),
		Root:       root,
		Interrupts: interrupts,
	})
}

func forwardInterrupts(signals <-chan os.Signal, interrupts chan<- struct{}) {
	for range signals {
		interrupts <- struct{}{}
	}
}

func environmentError(action string, err error) int {
	_, _ = fmt.Fprintf(os.Stderr, "error: %s: %v\n", action, err)
	return 1
}
