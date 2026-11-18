// Command accountsd reconciles a host or places an SSH session in its slice.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud/awshost"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/linux"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	code := run(ctx)
	stop()
	os.Exit(code)
}

func run(ctx context.Context) int {
	if filepath.Base(os.Args[0]) == linux.SessionName {
		if err := linux.Session(ctx, linux.Real(), os.Getenv, os.Getppid()); err != nil {
			detail := strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error())
			_, _ = fmt.Fprintf(os.Stderr, "%s: %s\n", daemon.UserAgent, detail)
			return daemon.ExitFailed
		}
		return daemon.ExitSuccess
	}
	return daemon.Main(ctx, daemon.Process{
		Args:       os.Args[1:],
		LookupEnv:  os.LookupEnv,
		Stderr:     os.Stderr,
		Executable: os.Executable,
	}, awshost.Open, linux.New(linux.Real()))
}
