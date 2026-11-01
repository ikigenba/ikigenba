// Command turns pushes content-free session turns to a space.
package main

import (
	"context"
	"os"

	"github.com/ikigenba/ikigenba/turns/internal/cli"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, seam.Process()))
}
