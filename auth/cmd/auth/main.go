// Command auth serves the platform auth service.
package main

import (
	"crypto/rand"
	"os"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.Process{
		Args:       os.Args[1:],
		Getenv:     os.Getenv,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Now:        time.Now,
		Rand:       rand.Reader,
		OIDCIssuer: "https://accounts.google.com",
		DBSource:   "state/auth.db",
	}))
}
