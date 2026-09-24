// Package main wires the agent-monitor process to the CLI run seam.
package main

import (
	"os"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

func main() {
	code := cli.Run(os.Args[1:], cli.System{Home: os.Getenv("HOME"), Root: os.DirFS("/")}, os.Stdout, os.Stderr)
	os.Exit(int(code))
}
