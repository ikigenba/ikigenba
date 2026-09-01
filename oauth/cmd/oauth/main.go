// Package main wires the OAuth CLI to its production dependencies.
package main

import (
	"context"
	"crypto/rand"
	"net"
	"net/http"
	"os"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
	"github.com/ikigenba/ikigenba/oauth/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, cli.Deps{
		Launcher:   browser.New(),
		Entropy:    rand.Reader,
		HTTPClient: http.DefaultClient,
		Listen:     net.Listen,
	}))
}
