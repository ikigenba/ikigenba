package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
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

	env := host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}
	store := config.Store{Root: deps.Root}
	reported := false
	report := func(step, detail string, success bool) error {
		reported = true
		return writeInstallReport(stdout, step, detail, success)
	}
	err := apps.Install(context.Background(), env, deps.Cloud, store, args[0], apps.InstallHooks{
		Report: report,
		Configure: func(ctx context.Context, manifest apps.Manifest) error {
			return configureInstalledApp(ctx, env, store, manifest, report)
		},
	})
	if err != nil {
		var failure *apps.InstallError
		if errors.As(err, &failure) {
			message := failure.Message
			if reported && message != "artifact download failed" {
				message = "install failed"
			}
			writeDiagnostic(stderr, &apps.InstallError{Code: failure.Code, Message: message, Cause: failure.Cause})
			return exitCode(failure.Code)
		}
		writeDiagnostic(stderr, err)
		return exitFail
	}
	return exitOK
}

func writeInstallReport(output io.Writer, step, detail string, success bool) error {
	detail = safeInstallReportDetail(detail)
	if success {
		_, err := fmt.Fprintf(output, "%s: ok (%s)\n", step, detail)
		return err
	}
	_, err := fmt.Fprintf(output, "%s: failed: %s\n", step, detail)
	return err
}

func safeInstallReportDetail(detail string) string {
	detail = strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(detail)
	if strings.TrimSpace(detail) == "" {
		return strconv.Quote(detail)
	}
	for _, character := range detail {
		if character < ' ' || character == '\u007f' {
			return strconv.Quote(detail)
		}
	}
	return detail
}

func configureInstalledApp(
	ctx context.Context,
	env host.Env,
	store config.Store,
	manifest apps.Manifest,
	report func(string, string, bool) error,
) error {
	hostName, err := store.Get("host.name")
	if err != nil {
		return reportInstallConfigurationFailure(report, "nginx", err)
	}
	nginxDetail := manifest.App + "." + hostName
	if manifest.Default {
		nginxDetail += ", " + hostName
	}
	if err := nginx.Apply(ctx, env, hostName); err != nil {
		return reportInstallConfigurationFailure(report, "nginx", err)
	}
	if err := report("nginx", nginxDetail, true); err != nil {
		return err
	}

	changed, err := backup.Regenerate(ctx, env, store)
	if err != nil {
		return reportInstallConfigurationFailure(report, "litestream", err)
	}
	detail := "unchanged"
	if changed {
		detail = "updated"
		if manifest.Database != nil {
			detail = manifest.Database.Path
		}
		if err := executeInstallCLICommand(ctx, env, "restart litestream.service", "systemctl", "restart", "litestream.service"); err != nil {
			return reportInstallConfigurationFailure(report, "litestream", err)
		}
	}
	if err := report("litestream", detail, true); err != nil {
		return err
	}
	return nil
}

func reportInstallConfigurationFailure(report func(string, string, bool) error, step string, cause error) error {
	if reportErr := report(step, cause.Error(), false); reportErr != nil {
		return errors.Join(cause, reportErr)
	}
	return cause
}

func executeInstallCLICommand(ctx context.Context, env host.Env, label, name string, args ...string) error {
	result, err := env.Execute(ctx, host.Command{Name: name, Args: args})
	if err != nil {
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			return err
		}
		return &host.CommandError{Label: label, Result: result, Err: err}
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result}
	}
	return nil
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
