package cli

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const installUsage = `Usage: opsctl install URI

Install the app at URI, an s3:// object holding an <app>-<tag>.tar.xz built by
devctl. The app name, its port, and the secrets it needs are read from
etc/manifest.toml inside it; the secret values are read from the parameter
/ikigenba/<host.name>/<app>.

Nothing under /opt/<app>/state/ or /opt/<app>/cache/ is touched, so installing
over a running app keeps its data. Safe to re-run.

The nginx configuration and /etc/litestream.yml are regenerated from every app
on the host, so an app that declares a [database] is replicated from the
moment it is installed. litestream.service is restarted only when its
configuration changed.

Configuration keys:
  aws.region  the region this host's parameters and artifacts live in
  host.name   the fully-qualified name this host answers at
  backup.s3_uri  the prefix this host backs up to
  backup.service_db_seconds  how often a declared database is snapshotted whole
  backup.service_wal_seconds  how often a declared database's committed changes are shipped
`

func runInstall(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		return writeOut(stdout, installUsage)
	}
	if len(args) == 0 {
		return writeInstallUsageError(stderr, "install needs URI")
	}
	if len(args) != 1 {
		return writeInstallUsageError(stderr, "install takes one URI")
	}
	if !validInstallURI(args[0]) {
		return writeInstallUsageError(stderr, "install takes an s3:// URI")
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}

	err := apps.Install(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}, deps.Cloud, config.Store{Root: deps.Root}, args[0], apps.InstallHooks{})
	if err != nil {
		writeDiagnostic(stderr, err)
		var failure *apps.InstallError
		if errors.As(err, &failure) {
			return exitCode(failure.Code)
		}
		return exitFail
	}
	return exitOK
}

func validInstallURI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "s3" && parsed.Host != "" &&
		strings.TrimPrefix(parsed.EscapedPath(), "/") != "" && parsed.RawQuery == "" && !parsed.ForceQuery &&
		parsed.Fragment == "" && !strings.Contains(value, "#") &&
		parsed.User == nil && !parsed.OmitHost && parsed.Opaque == ""
}

func writeInstallUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl install --help' for usage\n")
	return exitUsage
}
