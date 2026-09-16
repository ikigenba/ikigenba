package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const certUsage = `Usage: opsctl cert <subcommand>

Obtain and inspect the one certificate this host serves: host.name and
*.host.name, proved over DNS-01 through 'opsctl dns acme-auth'.

Subcommands:
  show    print the certificate's names, issuer, and expiry
  obtain  obtain the certificate, or renew it if it is due

Configuration keys:
  acme.email  the address the CA sends expiry warnings to
  host.name   the fully-qualified name this host answers at

Renewal is certbot's: 'certbot renew' re-runs the same hooks and reloads
nginx, with no further configuration. 'opsctl init' writes the timer that
runs it twice a day, ikigenba-renew-certificate.timer.
`

func runCert(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		return writeOut(stdout, certUsage)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if len(args) == 0 {
		return writeCertUsageError(stderr, "no cert subcommand given")
	}

	switch args[0] {
	case "show":
		if len(args) != 1 {
			return writeCertUsageError(stderr, "cert show takes no arguments")
		}
	case "obtain":
		if len(args) != 1 {
			return writeCertUsageError(stderr, "cert obtain takes no arguments")
		}
	default:
		quoted := strconv.Quote(args[0])
		return writeCertUsageError(stderr, "unknown cert subcommand '"+quoted[1:len(quoted)-1]+"'")
	}

	store := config.Store{Root: deps.Root}
	hostName, code := certConfigValue(store, "host.name", stderr, deps)
	if code != exitOK {
		return code
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	if args[0] == "show" {
		return runCertShow(stdout, stderr, env, hostName)
	}
	return runCertObtain(stderr, store, deps, env, hostName)
}

func runCertShow(stdout, stderr io.Writer, env host.Env, hostName string) exitCode {
	info, err := cert.Inspect(env, hostName)
	if err != nil {
		return certOperationErr(stderr, err)
	}
	return writeOut(stdout, fmt.Sprintf("names: %s\nissuer: %s\nexpires: %s\n",
		strings.Join(info.Names, ", "), info.Issuer, info.Expires.UTC().Format(time.RFC3339)))
}

func runCertObtain(stderr io.Writer, store config.Store, deps Deps, env host.Env, hostName string) exitCode {
	email, code := certConfigValue(store, "acme.email", stderr, deps)
	if code != exitOK {
		return code
	}
	if err := cert.Obtain(context.Background(), env, hostName, email); err != nil {
		return certOperationErr(stderr, err)
	}
	return exitOK
}

func certOperationErr(stderr io.Writer, err error) exitCode {
	writeDiagnostic(stderr, err)
	return exitFail
}

func certConfigValue(store config.Store, key string, stderr io.Writer, deps Deps) (string, exitCode) {
	value, err := store.Get(key)
	if err == nil && value != "" {
		return value, exitOK
	}
	if err == nil || errors.Is(err, config.ErrNotSet) {
		_, _ = io.WriteString(stderr, "opsctl: "+key+" not set\n")
		return "", exitFail
	}
	return "", configActionErr(stderr, "get", deps, err)
}

func writeCertUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl cert --help' for usage\n")
	return exitUsage
}
