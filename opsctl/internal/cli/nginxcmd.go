package cli

import (
	"context"
	"errors"
	"io"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
	"github.com/ikigenba/ikigenba/opsctl/internal/services"
)

const nginxUsage = `Usage: opsctl nginx <subcommand>

Generate /etc/nginx/conf.d/ikigenba.conf from the configuration store, the
services under /opt, and which apps systemd reports disabled. The file is generated, never edited; opsctl writes no
other file under /etc/nginx.

Subcommands:
  show   print the configuration opsctl would write
  apply  write the file, test it, and reload nginx

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none

A service is any /opt/<name>/ with an etc/ directory, or any
/var/opt/ikigenba/<name>/ with a state/ directory. One with an
/opt/<name>/etc/manifest.toml naming its app answers at <name>.<host.name>,
proxied to its socket /run/ikigenba/<name>.sock, and the one whose manifest
sets default answers at <host.name> as well. Its own etc/nginx.conf, if it ships one, is
included in its server block. An app whose socket unit systemd reports
disabled keeps its names, and its block answers 503. Every proxied request
carries X-Request-Id set to nginx's own request id, which also ends its
access-log line. The app
host.apex names also answers at the parent of host.name; until that app is
routed, the parent answers 404. A routed app named auth is the authenticator:
every other app's block then requires a valid session, checked against auth's
/check, while auth's own name is not gated. Under /mcp, a request without a
valid credential, or with one auth refuses, is answered 401 naming the MCP
gateway's protected-resource metadata instead of being sent to sign in or
refused; a git smart HTTP request without a credential is answered 401 too,
with a Basic challenge so git asks for the token. An app whose manifest sets
guests admits a request without a credential elsewhere, checked against
auth's /check/open.
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
	if _, err := services.Write(context.Background(), env, hostName); err != nil {
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
