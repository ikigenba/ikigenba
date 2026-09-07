// Command opsctl is the operator CLI for the Ikigenba platform.
package main

import (
	"os"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.Deps{
		Root: "/",
		EUID: os.Geteuid(),
	}))
}
