// Package main wires the agent-monitor process to the CLI run seam.
package main

import (
	"os"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

func main() {
	code := cli.Run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(int(code))
}
