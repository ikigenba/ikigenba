package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	// R-B6D0-P7FD R-BJRW-WOL0 R-BONI-FRJS
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
	steps := []string{"release", "manifests", "secrets", "resources", "env", "units", "links", "systemd", "nginx", "services", "litestream", "service", "retention"}
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
	// R-ARQ8-3YJ1 R-AU60-VI0F
	for _, tc := range []struct{ command, help string }{
		{"activate", "Usage: opsctl activate SHA [LABEL]\n\nMake the release unpacked at /opt/ikigenba/releases/SHA/ the one this host\nruns. SHA is the full 40-character commit sha. LABEL, one token of letters,\ndigits, '.', '_', '/' and '-', is written to releases/SHA/label and given to\nevery app as IKIGENBA_RELEASE; without LABEL the label file is removed.\nMust be run by the opsctl inside that release,\n/opt/ikigenba/releases/SHA/opsctl/bin/opsctl.\n\nEvery check runs before anything outside the release changes: the release\ntree, the host's layout, every app's manifest, its secrets in\n/<host.name>/<app>, and the slices' room for the release. Then every app's\nenvironment file and units are written, previous is pointed at what current\nnamed and current at the release, nginx, /run/ikigenba/services.json and\n/etc/litestream.yml are regenerated, and every app is restarted one at a\ntime, core apps first, each active before the next. A disabled app stays\ndisabled and is not started. An app the host runs that the release lacks is\nstopped and its units removed; its data under /var/opt/ikigenba/APP/ is kept.\nThe first failure stops the run. After a run that succeeds, every release\nneither current nor previous names is removed.\n\nOn a host whose apps were installed one by one, every service is snapshotted\nand the apps' units and /opt/APP/ removed before the release's are written.\nActivating the release current already names redoes everything but moving\ncurrent and previous.\n\nConfiguration keys:\n  aws.region          the region this host's parameters live in\n  host.name           the fully-qualified name this host answers at\n  host.apex           the app that answers at the parent of host.name; unset means none\n  backup.s3_uri       the prefix the cutover's snapshots are written under\n  apps.drain_seconds  how long an app may drain when stopped (default 5)\n  apps.stop_seconds   how long systemd waits for an app to stop (default 10)\n"},
		{"rollback", "Usage: opsctl rollback\n\nMake the release previous names the one this host runs again. current is\npointed at it and previous removed, then every app's environment file and\nunits are written from it, nginx, /run/ikigenba/services.json and\n/etc/litestream.yml are regenerated, and every app is restarted as 'opsctl\nactivate' does. The release keeps the label it last had. Run by the opsctl\ninside that release; any other opsctl hands the command to\n/opt/ikigenba/previous/opsctl/bin/opsctl. After a run that succeeds the\nrelease rolled away from is removed, so there is nothing for a second\nrollback to go back to.\n\nConfiguration keys:\n  aws.region          the region this host's parameters live in\n  host.name           the fully-qualified name this host answers at\n  host.apex           the app that answers at the parent of host.name; unset means none\n  apps.drain_seconds  how long an app may drain when stopped (default 5)\n  apps.stop_seconds   how long systemd waits for an app to stop (default 10)\n"},
	} {
		for _, flag := range []string{"-h", "--help"} {
			f := newTransitionFixture(t)
			f.deps.EUID = 1001
			f.deps.Root = filepath.Join(f.root, "missing-root")
			f.deps.Executable = func() (string, error) { t.Fatal("help executable access"); return "", nil }
			code, out, err := f.run(tc.command, flag)
			if code != 0 || out != tc.help || err != "" || len(f.commands) != 0 || f.secretsCalls != 0 {
				t.Fatalf("%d %q %q", code, out, err)
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
