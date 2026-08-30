// Package main wires the idgen CLI to the operating system process.
package main

import (
	"os"
	"time"

	"github.com/ikigenba/ikigenba/idgen/internal/cli"
)

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, realClock{}))
}
