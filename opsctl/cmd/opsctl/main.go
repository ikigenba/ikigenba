// Command opsctl is the operator CLI for the Ikigenba platform.
package main

import (
	"net"
	"os"
	"os/exec"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns/route53"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.Deps{
		Root:       "/",
		EUID:       os.Geteuid(),
		Getenv:     os.Getenv,
		DNS:        dns.Env{Open: route53.Open},
		LookPath:   exec.LookPath,
		LookupHost: net.DefaultResolver.LookupHost,
	}))
}
