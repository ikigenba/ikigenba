// Command auth serves the platform auth service.
package main

import (
	"context"
	"crypto/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/appkit"
	"github.com/ikigenba/ikigenba/auth/internal/cli"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

func main() {
	kit := appkit.New("auth", version.Version)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	code := cli.Run(ctx, cli.Process{
		Args:       os.Args[1:],
		LookupEnv:  os.LookupEnv,
		Unsetenv:   os.Unsetenv,
		Pid:        os.Getpid(),
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Now:        time.Now,
		Rand:       rand.Reader,
		OIDCIssuer: "https://accounts.google.com",
		DBSource:   "state/auth.db",
		Banner:     kit.Banner,
	})
	stop()
	os.Exit(code)
}
