package cli

import (
	"context"
	"errors"
	"io"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

const nginxUsage = `Usage: opsctl nginx <subcommand>

Generate /etc/nginx/conf.d/ikigenba.conf from the configuration store and the
services under /opt. The file is generated, never edited; opsctl writes no
other file under /etc/nginx.

Subcommands:
  show   print the configuration opsctl would write
  apply  write the file, test it, and reload nginx

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none

A service is any /opt/<name>/ with an etc/ or state/ directory. One with an
etc/manifest.toml naming a port answers at <name>.<host.name>, and the one
whose manifest sets default answers at <host.name> as well. Its own
etc/nginx.conf, if it ships one, is included in its server block. The app
host.apex names also answers at the parent of host.name; until that app is
routed, the parent answers 404.
`

func runNginx(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	invocation := parseNginxInvocation(args, stdout, stderr, deps)
	if invocation.done {
		return invocation.code
	}
	hostName, apexApp, code := nginxConfig(config.Store{Root: deps.Root}, stderr, deps)
	if code != exitOK {
		return code
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	return executeNginx(invocation.subcommand, stdout, stderr, env, hostName, apexApp)
}

type nginxInvocation struct {
	subcommand string
	code       exitCode
	done       bool
}

func parseNginxInvocation(args []string, stdout, stderr io.Writer, deps Deps) nginxInvocation {
	if isCommandHelp(args) {
		return nginxInvocation{code: writeOut(stdout, nginxUsage), done: true}
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return nginxInvocation{code: code, done: true}
	}
	if len(args) == 0 {
		return nginxInvocation{code: writeNginxUsageError(stderr, "no nginx subcommand given"), done: true}
	}
	if args[0] != "show" && args[0] != "apply" {
		return nginxInvocation{code: writeNginxUsageError(stderr, "unknown nginx subcommand '"+diagnosticArg(args[0])+"'"), done: true}
	}
	if len(args) != 1 {
		return nginxInvocation{code: writeNginxUsageError(stderr, "nginx "+diagnosticArg(args[0])+" takes no arguments"), done: true}
	}
	return nginxInvocation{subcommand: args[0]}
}

func executeNginx(subcommand string, stdout, stderr io.Writer, env host.Env, hostName, apexApp string) exitCode {
	if subcommand == "show" {
		candidate, err := nginx.Render(context.Background(), env, hostName, apexApp)
		if err != nil {
			writeDiagnostic(stderr, err)
			return exitFail
		}
		if _, err := stdout.Write(candidate); err != nil {
			return exitFail
		}
		return exitOK
	}
	if err := nginx.Apply(context.Background(), env, hostName, apexApp); err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	return exitOK
}

type nginxConfigReader interface {
	Get(string) (string, error)
}

func nginxConfig(store nginxConfigReader, stderr io.Writer, deps Deps) (string, string, exitCode) {
	hostName, err := store.Get("host.name")
	hostName = host.NormalizeName(hostName)
	if (err == nil || errors.Is(err, config.ErrNotSet)) && hostName == "" {
		_, _ = io.WriteString(stderr, "opsctl: host.name not set\n")
		return "", "", exitFail
	}
	if err != nil {
		return "", "", configActionErr(stderr, "get", deps, err)
	}
	apexApp, err := store.Get("host.apex")
	if errors.Is(err, config.ErrNotSet) {
		apexApp = ""
	} else if err != nil {
		return "", "", configActionErr(stderr, "get", deps, err)
	}
	if apexApp != "" {
		if _, err := host.Apex(hostName); err != nil {
			writeDiagnostic(stderr, err)
			return "", "", exitFail
		}
	}
	return hostName, apexApp, exitOK
}

func writeNginxUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl nginx --help' for usage\n")
	return exitUsage
}
