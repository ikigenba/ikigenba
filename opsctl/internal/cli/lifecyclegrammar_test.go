package cli_test

import (
	"context"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantUninstallUsage = `Usage: opsctl uninstall APP

Take APP off the host: stop and disable ikigenba-APP.service and remove it,
then remove /opt/APP/bin/, etc/, share/, and cache/. /opt/APP/state/ is kept
untouched, so APP is still a service the host backs up, and a later install
lands over its data the way an install over a restore does. Removing state/ is
a decision made by hand, never here.

The nginx configuration and /etc/litestream.yml are regenerated from every app
left on the host, so APP's name stops answering, and a database APP declared
stops being replicated once litestream has shipped what it holds.

The parameter /<host.name>/APP is not touched: it is devctl's.

Configuration keys:
  host.name  the fully-qualified name this host answers at
`

const wantRestartUsage = `Usage: opsctl restart APP

Restart ikigenba-APP.service and report the service as the last line of
'opsctl install' does. Nothing on disk changes: the binary, the environment
file, and the unit are what the last install wrote, so a secret pushed since
then is not picked up here. A unit that is inactive or failed is started.
`

const wantStatusUsage = `Usage: opsctl status

Print one line per service on this host, in name order: its name, the version
its own binary reports, the state of its systemd unit, and the journal mode of
the database its manifest declares. A service is any /opt/<name>/ with an etc/
or state/ directory; '-' means opsctl could not ask, or there was nothing to
ask.

A declared database must stay in WAL mode: litestream cannot replicate one in
any other mode, so a service reporting anything but 'wal' is a service whose
data is not reaching S3.

The exit code is 0 whatever the report says. A failed unit and an unreplicable
database are facts about the host, not failures of this command.
`

func TestUninstallHelpIsExactAndHostIndependent(t *testing.T) {
	// R-J9A3-IC1D
	assertLifecycleHelp(t, "uninstall", wantUninstallUsage)
}

func TestRestartHelpIsExactAndHostIndependent(t *testing.T) {
	// R-LVYX-276T
	assertLifecycleHelp(t, "restart", wantRestartUsage)
}

func TestStatusHelpIsExactAndHostIndependent(t *testing.T) {
	// R-LX6T-FYXI
	assertLifecycleHelp(t, "status", wantStatusUsage)
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
	// R-LYEP-TQO7
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"uninstall"}, "opsctl: uninstall needs APP\n\nsee 'opsctl uninstall --help' for usage\n"},
		{[]string{"uninstall", "one", "two"}, "opsctl: uninstall takes one APP\n\nsee 'opsctl uninstall --help' for usage\n"},
		{[]string{"restart"}, "opsctl: restart needs APP\n\nsee 'opsctl restart --help' for usage\n"},
		{[]string{"restart", "one", "two"}, "opsctl: restart takes one APP\n\nsee 'opsctl restart --help' for usage\n"},
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
