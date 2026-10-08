package cli_test

import (
	"context"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantUninstallUsage = `Usage: opsctl uninstall APP

Take APP off the host: stop ikigenba-APP.socket and ikigenba-APP.service,
socket first so no request starts the service again, disable both, remove
both units (which ends a disabled APP's disabled state: a later install is a
first install and comes up enabled), then remove /opt/APP/,
/etc/opt/ikigenba/APP/ with its environment file, and
/var/opt/ikigenba/APP/cache/. /var/opt/ikigenba/APP/state/ is kept untouched,
so APP is still a service the host backs up, and a later install lands over
its data, which every install leaves untouched. Removing state/ is a decision
made by hand, never here.

A state/ or cache/ still under /opt/APP/ is first moved to
/var/opt/ikigenba/APP/, once APP is stopped, as 'opsctl install' moves it; a
state/ in both places fails the uninstall with nothing removed.

The nginx configuration, /var/lib/ikigenba/services.json, and
/etc/litestream.yml are regenerated from every app left on the host, so APP's
name stops answering, APP leaves the service launcher, and a database APP
declared stops being replicated once litestream has shipped what it holds.

The parameter /<host.name>/APP is not touched: it is devctl's.

On a host that runs releases, one where /opt/ikigenba/current exists,
uninstall refuses and changes nothing: an app leaves such a host when
'opsctl activate' puts a release without it in place.

Configuration keys:
  host.name  the fully-qualified name this host answers at
`

const wantRestartUsage = `Usage: opsctl restart APP

Restart ikigenba-APP.service and report the service as 'opsctl activate'
reports each app. The socket is never restarted: it keeps listening, so
requests that arrive during the restart wait and are answered by the new
process. Nothing on disk changes: the binary, the environment file, and the
units are what the last activate wrote, so a secret pushed since then is not
picked up here, nor a timing setting changed since then ('opsctl init'
applies those). A service that is inactive or failed is started, and so is
its socket if it was stopped. A disabled app is not started: it stays
disabled until 'opsctl enable'.
`

const wantStatusUsage = `Usage: opsctl status

Print one line per service on this host, in name order: its name, the short
commit of the release it runs, that release's label or '-' when it has none,
the state of its service unit, the state of its socket unit, and the journal
mode of the database its manifest declares.
A service is an app the current release (/opt/ikigenba/current) holds, or any
/var/opt/ikigenba/<name>/ with a state/ directory. A service with only its
kept state is data only, and every field after its name is '-'. '-' means
opsctl could not ask, or there was nothing to ask.

On a host not yet running releases, a service is any /opt/<name>/ with an
etc/ directory or any /var/opt/ikigenba/<name>/ with a state/ directory, and
the second field is the version its own binary reports and the third is
always '-'.

A service that is inactive behind an active socket is idle, not down: its
socket starts it again when the next request arrives. The socket's field reads
'disabled' when its unit is disabled, as 'opsctl disable' leaves it: neither
unit starts until 'opsctl enable'.

A declared database must stay in WAL mode: litestream cannot replicate one in
any other mode, so a service reporting anything but 'wal' is a service whose
data is not reaching S3.

The exit code is 0 whatever the report says. A failed unit and an unreplicable
database are facts about the host, not failures of this command.
`

const wantDisableUsage = `Usage: opsctl disable APP

Stop ikigenba-APP.socket and ikigenba-APP.service, socket first so no request
starts the service again, and disable both, so neither starts at boot or on a
request. The nginx configuration and /run/ikigenba/services.json are then
regenerated, so APP's names answer 503 and the service launcher shows APP
disabled until it is enabled. Nothing on disk under /opt/ikigenba/ or
/var/opt/ikigenba/APP/ changes.
'opsctl enable APP' undoes it.

auth, the authenticator every other app is checked against, is never
disabled.

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none
`

const wantEnableUsage = `Usage: opsctl enable APP

Enable ikigenba-APP.socket and ikigenba-APP.service and start the socket,
regenerate the nginx configuration and /run/ikigenba/services.json so APP's
names reach it again and the service launcher shows it enabled, then start
the service and report it as 'opsctl activate' reports each app.
Nothing on disk under /opt/ikigenba/ changes.

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none
`

func TestUninstallHelpIsExactAndHostIndependent(t *testing.T) {
	// R-EQLH-T9TQ
	assertLifecycleHelp(t, "uninstall", wantUninstallUsage)
}

func TestRestartHelpIsExactAndHostIndependent(t *testing.T) {
	// R-ERTE-71KF
	assertLifecycleHelp(t, "restart", wantRestartUsage)
}

func TestStatusHelpIsExactAndHostIndependent(t *testing.T) {
	// R-ET1A-KTB4
	assertLifecycleHelp(t, "status", wantStatusUsage)
}

func TestDisableHelpIsExactAndHostIndependent(t *testing.T) {
	// R-EU96-YL1T
	assertLifecycleHelp(t, "disable", wantDisableUsage)
}

func TestEnableHelpIsExactAndHostIndependent(t *testing.T) {
	// R-EVH3-CCSI
	assertLifecycleHelp(t, "enable", wantEnableUsage)
}

func assertLifecycleHelp(t *testing.T, command, want string) {
	t.Helper()
	for _, euid := range []int{0, 1000} {
		for _, option := range []string{"--help", "-h"} {
			deps, assertUnused := unusedLifecycleDeps(t, euid)
			stdout, stderr, code := invoke([]string{command, option}, deps)
			assertUnused()
			if code != 0 || stdout != want || stderr != "" {
				t.Errorf("%s %s as euid %d = exit %d stdout %q stderr %q", command, option, euid, code, stdout, stderr)
			}
		}
	}
}

func TestLifecycleArityPrecedesHostAccess(t *testing.T) {
	// R-VFKQ-1FOH
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"uninstall"}, "opsctl: uninstall needs APP\n\nsee 'opsctl uninstall --help' for usage\n"},
		{[]string{"uninstall", "one", "two"}, "opsctl: uninstall takes one APP\n\nsee 'opsctl uninstall --help' for usage\n"},
		{[]string{"restart"}, "opsctl: restart needs APP\n\nsee 'opsctl restart --help' for usage\n"},
		{[]string{"restart", "one", "two"}, "opsctl: restart takes one APP\n\nsee 'opsctl restart --help' for usage\n"},
		{[]string{"disable"}, "opsctl: disable needs APP\n\nsee 'opsctl disable --help' for usage\n"},
		{[]string{"disable", "one", "two"}, "opsctl: disable takes one APP\n\nsee 'opsctl disable --help' for usage\n"},
		{[]string{"enable"}, "opsctl: enable needs APP\n\nsee 'opsctl enable --help' for usage\n"},
		{[]string{"enable", "one", "two"}, "opsctl: enable takes one APP\n\nsee 'opsctl enable --help' for usage\n"},
		{[]string{"status", "extra"}, "opsctl: status takes no arguments\n\nsee 'opsctl status --help' for usage\n"},
	}
	for _, test := range tests {
		deps, assertUnused := unusedLifecycleDeps(t, 1000)
		stdout, stderr, code := invoke(test.args, deps)
		assertUnused()
		if code != 2 || stdout != "" || stderr != test.want {
			t.Errorf("%q = exit %d stdout %q stderr %q, want exit 2, empty stdout, stderr %q", test.args, code, stdout, stderr, test.want)
		}
	}
}

func unusedLifecycleDeps(t *testing.T, euid int) (cli.Deps, func()) {
	t.Helper()
	root := t.TempDir()
	assertNoAccess := observeFilesystemAccess(t, root)
	used := false
	deps := cli.Deps{
		Root: root,
		EUID: euid,
		Execute: func(context.Context, host.Command) (host.Result, error) {
			used = true
			return host.Result{}, nil
		},
		Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			used = true
			return nil, nil
		}},
	}
	return deps, func() {
		t.Helper()
		assertNoAccess()
		if used {
			t.Error("grammar or help accessed a process or cloud boundary")
		}
	}
}
