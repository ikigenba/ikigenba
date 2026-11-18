package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func TestRollbackPrehandoffAndMetadata(t *testing.T) {
	// R-ASY4-HQ9Q R-WR68-8PLN R-WOQF-H649 R-WPYB-UXUY R-SE5G-RQ11
	f := newTransitionFixture(t)
	code, out, err := f.run("rollback", "operand")
	if code != 2 || out != "" || err != "opsctl: rollback takes no arguments\n\nsee 'opsctl rollback --help' for usage\n" {
		t.Fatalf("%d %q %q", code, out, err)
	}
	f.deps.Executable = func() (string, error) { t.Fatal("exe checked without previous"); return "", nil }
	code, out, err = f.run("rollback")
	if code != 1 || out != "" || err != "opsctl: no previous release to roll back to\n" {
		t.Fatalf("%d %q %q", code, out, err)
	}
	f.write("opt/ikigenba/previous", "wrong", 0o644)
	code, out, err = f.run("rollback")
	if code != 1 || out != "" || err != "opsctl: /opt/ikigenba/previous does not name a release\n" {
		t.Fatalf("%d %q %q", code, out, err)
	}
	for _, mode := range []string{"exe error", "handoff", "own missing metadata", "own mismatch metadata"} {
		t.Run(mode, func(t *testing.T) {
			f := newTransitionFixture(t)
			f.addRelease(transitionOld, "dummy")
			f.link("previous", transitionOld)
			calls := 0
			target := filepath.Join(f.root, "opt/ikigenba/previous/opsctl/bin/opsctl")
			f.deps.Exec = func(argv0 string, argv, env []string) error {
				calls++
				if argv0 != target || !reflect.DeepEqual(argv, []string{target, "rollback"}) || !reflect.DeepEqual(env, os.Environ()) {
					t.Fatalf("exec %q %v", argv0, argv)
				}
				return errors.New("exec fixture failure")
			}
			want := "opsctl: exec fixture failure\n"
			switch mode {
			case "exe error":
				f.deps.Executable = func() (string, error) { return "", errors.New("exe failure") }
				want = "opsctl: exe failure\n"
			case "own missing metadata", "own mismatch metadata":
				f.deps.Executable = func() (string, error) {
					return filepath.Join(f.root, "opt/ikigenba/releases", transitionOld, "opsctl/bin/opsctl"), nil
				}
				if mode == "own missing metadata" {
					if e := os.Remove(filepath.Join(f.root, "opt/ikigenba/releases", transitionOld, "release.json")); e != nil {
						t.Fatal(e)
					}
					want = "opsctl: /opt/ikigenba/releases/" + transitionOld + "/release.json is missing; unpack the release again\n"
				} else {
					f.write("opt/ikigenba/releases/"+transitionOld+"/release.json", `{"sha":"wrong"}`, 0o644)
					want = "opsctl: /opt/ikigenba/releases/" + transitionOld + "/release.json names wrong; unpack the release again\n"
				}
			}
			code, out, err := f.run("rollback")
			if code != 1 || out != "" || err != want || len(f.commands) != 0 {
				t.Fatalf("%d %q %q commands %v", code, out, err, f.commands)
			}
			expected := 0
			if mode == "handoff" {
				expected = 1
			}
			if calls != expected {
				t.Fatalf("exec calls %d", calls)
			}
		})
	}
}
func TestRollbackTransitionPreservesLabelRemovesPrevious(t *testing.T) {
	// R-981R-U0H9 R-BJRW-WOL0 R-BONI-FRJS
	f := newTransitionFixture(t)
	f.addRelease(transitionOld, "dummy")
	f.write("opt/ikigenba/releases/"+transitionOld+"/label", "old-label\n", 0o644)
	f.link("current", transitionSHA)
	f.link("previous", transitionOld)
	f.deps.Executable = func() (string, error) {
		return filepath.Join(f.root, "opt/ikigenba/releases", transitionOld, "opsctl/bin/opsctl"), nil
	}
	code, out, err := f.run("rollback")
	if code != 0 || err != "" {
		t.Fatalf("%d %s %s", code, out, err)
	}
	steps := []string{"release", "manifests", "secrets", "resources", "hardening", "env", "units", "links", "systemd", "nginx", "services", "litestream", "service", "retention"}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(steps) {
		t.Fatal(out)
	}
	for i, step := range steps {
		if !strings.HasPrefix(lines[i], step+": ok (") {
			t.Fatalf("step %d %q", i, lines[i])
		}
	}
	if f.read("opt/ikigenba/releases/"+transitionOld+"/label") != "old-label\n" || !strings.Contains(f.read("etc/opt/ikigenba/dummy/env"), "IKIGENBA_RELEASE=old-label\n") || !strings.Contains(out, "release: ok (bbbbbbb, old-label)\n") || !strings.Contains(out, "links: ok (current bbbbbbb, previous none)\n") {
		t.Fatal(out)
	}
	f.missing("opt/ikigenba/previous")
	f.missing("opt/ikigenba/releases/" + transitionSHA)
	code, out, err = f.run("rollback")
	if code != 1 || out != "" || err != "opsctl: no previous release to roll back to\n" {
		t.Fatalf("second %d %q %q", code, out, err)
	}
}

func TestReleaseCommandHelpIsInert(t *testing.T) {
	// R-3DQG-9P3D R-3G69-18KR
	for _, tc := range []struct{ command, help string }{
		{"activate", `Usage: opsctl activate SHA [LABEL]

Make the release unpacked at /opt/ikigenba/releases/SHA/ the one this host
runs. SHA is the full 40-character commit sha. LABEL, one token of letters,
digits, '.', '_', '/' and '-', is written to releases/SHA/label and given to
every app as IKIGENBA_RELEASE; without LABEL the label file is removed.
Must be run by the opsctl inside that release,
/opt/ikigenba/releases/SHA/opsctl/bin/opsctl.

Every check runs before anything outside the release changes: the release
tree, the host's layout, every app's manifest, its secrets in
/<host.name>/<app>, and the slices' room for the release. Then the host's
rules are put in place as 'opsctl init' puts them: sshd, the shell group,
fail2ban, unattended security updates, the login sessions' slices, the
suite's directories closed to others, and the accounts sudo rule. Then every
app's environment file and units are written, previous is pointed at what
current named and current at the release, nginx, /run/ikigenba/services.json
and /etc/litestream.yml are regenerated, and every app is restarted one at a
time, core apps first, each active before the next. A disabled app stays
disabled and is not started. An app the host runs that the release lacks is
stopped and its units removed; its data under /var/opt/ikigenba/APP/ is
kept. The first failure stops the run. After a run that succeeds, every
release neither current nor previous names is removed.

On a host whose apps were installed one by one, every service is snapshotted
and the apps' units and /opt/APP/ removed before the release's are written.
Activating the release current already names redoes everything but moving
current and previous.

Configuration keys:
  aws.region              the region this host's parameters live in; passed to every app as AWS_REGION
  host.name               the fully-qualified name this host answers at
  host.apex               the app that answers at the parent of host.name; unset means none
  backup.s3_uri           the prefix the cutover's snapshots are written under
  apps.drain_seconds      how long an app may drain when stopped (default 5)
  apps.stop_seconds       how long systemd waits for an app to stop (default 10)
  hosts.domain            the domain hosts are named under; passed to every app
  hosts.zone_id           the hosted zone id of hosts.domain; passed to every app
  hosts.launch_template   the launch template hosts are launched from; passed to every app
  hosts.instance_profile  the instance profile hosts run with; passed to every app
`},
		{"rollback", `Usage: opsctl rollback

Make the release previous names the one this host runs again. The host's
rules are put in place as 'opsctl activate' puts them, current is pointed at
the release and previous removed, then every app's environment file and
units are written from it, nginx, /run/ikigenba/services.json and
/etc/litestream.yml are regenerated, and every app is restarted as 'opsctl
activate' does. The release keeps the label it last had. Run by the opsctl
inside that release; any other opsctl hands the command to
/opt/ikigenba/previous/opsctl/bin/opsctl. After a run that succeeds the
release rolled away from is removed, so there is nothing for a second
rollback to go back to.

Configuration keys:
  aws.region              the region this host's parameters live in; passed to every app as AWS_REGION
  host.name               the fully-qualified name this host answers at
  host.apex               the app that answers at the parent of host.name; unset means none
  apps.drain_seconds      how long an app may drain when stopped (default 5)
  apps.stop_seconds       how long systemd waits for an app to stop (default 10)
  hosts.domain            the domain hosts are named under; passed to every app
  hosts.zone_id           the hosted zone id of hosts.domain; passed to every app
  hosts.launch_template   the launch template hosts are launched from; passed to every app
  hosts.instance_profile  the instance profile hosts run with; passed to every app
`},
	} {
		for _, flag := range []string{"-h", "--help"} {
			for _, uid := range []int{0, 1001} {
				f := newTransitionFixture(t)
				f.deps.EUID = uid
				f.deps.Root = filepath.Join(f.root, "missing-root")
				f.deps.Executable = func() (string, error) { t.Fatal("help executable access"); return "", nil }
				f.deps.Cloud.Open = func(context.Context, string) (cloud.Client, error) {
					t.Fatal("help cloud access")
					return nil, nil
				}
				code, out, err := f.run(tc.command, flag)
				if code != 0 || out != tc.help || err != "" || len(f.commands) != 0 {
					t.Fatalf("%d %q %q", code, out, err)
				}
			}
		}
	}
}

func TestRollbackRepairsOpsctlLink(t *testing.T) {
	// R-BONI-FRJS
	for _, kind := range []string{"missing", "file", "wrong symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransitionFixture(t)
			f.addRelease(transitionOld, "dummy")
			f.link("current", transitionSHA)
			f.link("previous", transitionOld)
			f.deps.Executable = func() (string, error) {
				return filepath.Join(f.root, "opt/ikigenba/releases", transitionOld, "opsctl/bin/opsctl"), nil
			}
			f.seedOpsctlLink(kind)
			code, out, stderr := f.run("rollback")
			if code != 0 || stderr != "" || !strings.Contains(out, "links: ok (current bbbbbbb, previous none)\n") {
				t.Fatalf("rollback: %d\n%s\n%s", code, out, stderr)
			}
			f.requireReleaseLink("usr/local/bin/opsctl", release.CurrentOpsctl)
			f.requireReleaseLink("opt/ikigenba/current", "releases/"+transitionOld)
			f.missing("opt/ikigenba/previous")
		})
	}
}
