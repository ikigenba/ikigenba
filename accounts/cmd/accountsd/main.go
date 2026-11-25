// Command accountsd reconciles a host, places SSH sessions, and serves box tools.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
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
	switch filepath.Base(os.Args[0]) {
	case linux.SessionName:
		return helperStatus(linux.Session(ctx, linux.Real(), os.Getenv, os.Getppid()))
	case linux.MCPName:
		err := boxmcp.Run(ctx, os.Stdin, os.Stdout, os.Getenv)
		if errors.Is(err, context.Canceled) {
			return daemon.ExitSuccess
		}
		return helperStatus(err)
	}
	return daemon.Main(ctx, daemon.Process{
		Args:       os.Args[1:],
		LookupEnv:  os.LookupEnv,
		Stderr:     os.Stderr,
		Executable: os.Executable,
		Socket:     boxmcp.SocketPath,
	}, awshost.Open, linux.New(linux.Real()))
}

func helperStatus(err error) int {
	if err == nil {
		return daemon.ExitSuccess
	}
	detail := strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error())
	_, _ = fmt.Fprintf(os.Stderr, "%s: %s\n", daemon.UserAgent, detail)
	return daemon.ExitFailed
}
