// Command sites serves the platform's static sites.
package main

import (
	"context"
	"os"

	"github.com/ikigenba/ikigenba/sites/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), cli.Process{
		Args: os.Args[1:], LookupEnv: os.LookupEnv, Environ: os.Environ,
		Unsetenv: os.Unsetenv, Pid: os.Getpid(), Stdout: os.Stdout, Stderr: os.Stderr,
	}))
}
