package golden_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cli"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/golden"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const help = `Usage: devctl golden <subcommand> [arguments]

Keep a space's data as a named golden set, one snapshot per app under
golden/<set>/ in the backup bucket, for 'devctl seed' to give to any space.
A golden set carries no secrets.

Subcommands:
  capture <space> <set>   snapshot every app on <space> and make the snapshots golden set <set>

Run 'devctl golden <subcommand> --help' for details.
`
const stamp = "2026-09-14T09:10:02Z.tar.zst"
const snapshotLine = "snapshot: ok (opsctl snapshot, 2 apps)\n"

type runSignature func(context.Context, []string, io.Writer, seam.Deps) error

func TestPublicContract(t *testing.T) {
	// R-SM9B-JQ39 R-SOP4-B9KN R-SPX0-P1BC R-SR4X-2T21 R-SSCT-GKSQ
	var _ runSignature = golden.Run
	usage := golden.UsageError(struct {
		Message string
		Help    string
	}{"bad", "devctl golden --help"})
	if usage.Error() != "bad" || usage.ExitCode() != 2 || usage.Detail() != "see 'devctl golden --help' for usage" {
		t.Fatal(usage)
	}
	noApps := golden.NoAppsError(struct{ Domain string }{"sbx1.ikigenba.dev"})
	if noApps.Error() != "'sbx1.ikigenba.dev' has no apps to capture" || noApps.ExitCode() != 1 {
		t.Fatal(noApps)
	}
	report := golden.ReportError(struct{ Line string }{"bad"})
	if report.Error() != "snapshot: unexpected line from opsctl snapshot: 'bad'" || report.ExitCode() != 1 {
		t.Fatal(report)
	}
	if golden.Prefix("demo") != "golden/demo/" {
		t.Fatal("prefix")
	}
}

func TestHelpUsageAndLabelBeforeCheckout(t *testing.T) {
	// R-SUSM-84A4 R-SW0I-LW0T R-SX8E-ZNRI
	deps := seam.Deps{EUID: 1000, Dir: t.TempDir(), Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("unexpected exec")
		return seam.Result{}, nil
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud")
		return cloud.Clients{}, nil
	}}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"capture", "--help"}, {"capture", "sbx1", "demo", "-h"}, {"--bad", "--help"}} {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), append([]string{"golden"}, args...), nil, &out, &stderr, deps)
		if code != 0 || stderr.Len() != 0 || out.String() != help {
			t.Fatalf("%q: %d %q %q", args, code, out.String(), stderr.String())
		}
	}
	for _, test := range []struct {
		args    []string
		message string
	}{
		{nil, "golden needs <subcommand>"}, {[]string{"other"}, "unknown subcommand 'other'"},
		{[]string{"capture"}, "golden capture needs <space> and <set>"}, {[]string{"capture", "sbx1"}, "golden capture needs <space> and <set>"},
		{[]string{"capture", "sbx1", "demo", "extra"}, "golden capture takes only <space> and <set>"},
		{[]string{"--bad"}, "unknown option '--bad'"}, {[]string{"capture", "sbx1", "demo", "-x"}, "unknown option '-x'"},
	} {
		err := golden.Run(context.Background(), test.args, io.Discard, deps)
		var usage *golden.UsageError
		if !errors.As(err, &usage) || usage.Message != test.message || usage.Help != "devctl golden --help" {
			t.Fatalf("%q: %v", test.args, err)
		}
	}
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"golden", "capture", "sbx1"}, nil, &out, &stderr, deps)
	if code != 2 || out.Len() != 0 || stderr.String() != "devctl: golden capture needs <space> and <set>\n\nsee 'devctl golden --help' for usage\n" {
		t.Fatalf("usage: %d %q %q", code, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"golden", "capture", "sbx1", "Demo_1"}, nil, &out, &stderr, deps)
	if code != 2 || out.Len() != 0 || stderr.String() != "devctl: 'Demo_1' is not a valid label\n" {
		t.Fatalf("label: %d %q %q", code, out.String(), stderr.String())
	}
	err := golden.Run(context.Background(), []string{"capture", "sbx1", "Demo_1"}, io.Discard, deps)
	var invalid *spaceref.InvalidLabelError
	if !errors.As(err, &invalid) || invalid.Operand != "Demo_1" {
		t.Fatal(err)
	}
}

func TestRootSessionLookupFailures(t *testing.T) {
	// R-SYGB-DFI7
	for _, name := range []string{"root", "parse", "open", "identity", "lookup", "missing", "stopped"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			want := errors.New(name)
			operand := "sbx1"
			switch name {
			case "root":
				if err := os.WriteFile(filepath.Join(h.root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "parse":
				operand = "Bad"
			case "open":
				h.openErr = want
			case "identity":
				h.stsErr = want
			case "lookup":
				h.lookupErr = want
			case "missing":
				operand = "gone"
			case "stopped":
				operand = "sbx2"
				h.domain = "sbx2.ikigenba.dev"
				h.state = cloud.StateStopped
			}
			var out bytes.Buffer
			err := golden.Run(context.Background(), []string{"capture", operand, "demo"}, &out, h.deps())
			if err == nil || out.Len() != 0 || len(h.s3Calls) != 0 || len(h.commands) != 1 {
				t.Fatalf("%v %q %#v %#v", err, out.String(), h.s3Calls, h.commands)
			}
			if name == "open" || name == "identity" || name == "lookup" {
				if reflect.ValueOf(err).Pointer() != reflect.ValueOf(want).Pointer() {
					t.Fatalf("identity lost: %v", err)
				}
			}
			if name == "root" {
				var e *checkout.RootFileError
				if !errors.As(err, &e) || e.Detail != "missing 'region'" {
					t.Fatal(err)
				}
			}
			if name == "parse" {
				var e *spaceref.InvalidLabelError
				if !errors.As(err, &e) || h.opens != 0 {
					t.Fatal(err)
				}
			}
			if name == "stopped" {
				var stopped *space.NotRunningError
				if !errors.As(err, &stopped) || stopped.Domain != "sbx2.ikigenba.dev" || stopped.State != cloud.StateStopped {
					t.Fatalf("stopped error = %#v", err)
				}
			}
			if name == "missing" || name == "stopped" {
				var stderr bytes.Buffer
				out.Reset()
				code := cli.Run(context.Background(), []string{"golden", "capture", operand, "demo"}, nil, &out, &stderr, h.deps())
				message := "devctl: no space at 'gone.ikigenba.dev'\n"
				if name == "stopped" {
					message = "devctl: 'sbx2.ikigenba.dev' is stopped\n"
				}
				if code != 1 || out.Len() != 0 || stderr.String() != message {
					t.Fatalf("%d %q %q", code, out.String(), stderr.String())
				}
			}
		})
	}
}

func TestStatusAndSnapshotRefusals(t *testing.T) {
	// R-TRPW-JXAV R-U2OZ-ZUZ4
	for _, status := range []string{"", " \t\n"} {
		h := newHarness(t)
		h.status = seam.Result{Stdout: []byte(status)}
		out, stderr, code := h.cli()
		if code != 1 || out != "" || stderr != "devctl: 'sbx1.ikigenba.dev' has no apps to capture\n" || len(h.commands) != 2 || len(h.s3Calls) != 0 {
			t.Fatalf("%d %q %q %#v", code, out, stderr, h.commands)
		}
	}
	for _, step := range []string{"status", "snapshot"} {
		h := newHarness(t)
		result := seam.Result{ExitCode: 1, Stdout: []byte("line1\nline2\n"), Stderr: []byte("err1\nerr2\n")}
		if step == "status" {
			h.status = result
		} else {
			h.snapshot = result
		}
		out, stderr, code := h.cli()
		prefix := ""
		if step == "snapshot" {
			prefix = "snapshot: "
		}
		want := "devctl: " + prefix + "ssh ec2-user@18.118.7.42 sudo opsctl " + step + ": exit status 1\n\n> line1\n> line2\n> err1\n> err2\n"
		if code != 1 || out != "" || stderr != want || len(h.s3Calls) != 0 {
			t.Fatalf("%d %q %q", code, out, stderr)
		}
		var e *host.CommandError
		err := golden.Run(context.Background(), []string{"capture", "sbx1", "demo"}, io.Discard, h.deps())
		wantStep := ""
		if step == "snapshot" {
			wantStep = "snapshot"
		}
		if !errors.As(err, &e) || e.Step != wantStep {
			t.Fatal(err)
		}
	}
}

func TestMalformedReports(t *testing.T) {
	// R-T0W4-4YZL
	valid := report("crm")
	for _, bad := range []string{"crm: failed: no replica under the prefix", strings.Replace(valid, "sbx1/", "sbx2/", 1), valid, "host: ok (s3://ikigenba.dev/sbx1/snapshots/host/f, detail)", "crm: ok (s3://ikigenba.dev/sbx1/snapshots/crm/, detail)", "crm: ok (s3://ikigenba.dev/sbx1/snapshots/crm/a/b, detail)", "crm: ok (s3://ikigenba.dev/sbx1/snapshots/crm/a b, detail)", "crm: ok (s3://ikigenba.dev/sbx1/snapshots/crm/a,b, detail)", "crm: ok (s3://ikigenba.dev/sbx1/snapshots/crm/f, )", " "} {
		h := newHarness(t)
		h.snapshot.Stdout = []byte(valid + "\n" + bad + "\n")
		var out bytes.Buffer
		err := golden.Run(context.Background(), []string{"capture", "sbx1", "demo"}, &out, h.deps())
		var e *golden.ReportError
		if !errors.As(err, &e) || e.Line != bad || out.Len() != 0 || len(h.s3Calls) != 0 {
			t.Fatalf("%q: %v %q %#v", bad, err, out.String(), h.s3Calls)
		}
	}
}

func TestCopiesPruneAndRestrictedOperations(t *testing.T) {
	// R-T3BW-WIGZ R-U6CP-5677 R-T6ZM-1TP2 R-U1H3-M38F
	for _, obsolete := range []bool{false, true} {
		h := newHarness(t)
		h.objects = []cloud.Object{{Key: key("crm")}, {Key: key("dashboard")}}
		old := []string{"golden/demo/crm/2026-09-12T14:22:51Z.tar.zst", "golden/demo/dashboard/2026-09-12T14:22:51Z.tar.zst", "golden/demo/gmail/2026-09-12T14:22:51Z.tar.zst"}
		if obsolete {
			for _, k := range old {
				h.objects = append(h.objects, cloud.Object{Key: k})
			}
		}
		out, stderr, code := h.cli()
		prune := "prune: ok (nothing to delete)\n"
		if obsolete {
			prune = "prune: ok (3 objects deleted)\n"
		}
		if code != 0 || stderr != "" || out != snapshotLine+copyLine("crm")+copyLine("dashboard")+prune {
			t.Fatalf("%d %q %q", code, out, stderr)
		}
		wantCalls := []string{"copy ikigenba.dev sbx1/snapshots/crm/" + stamp + " " + key("crm"), "copy ikigenba.dev sbx1/snapshots/dashboard/" + stamp + " " + key("dashboard"), "list ikigenba.dev golden/demo/"}
		if obsolete {
			wantCalls = append(wantCalls, "delete ikigenba.dev "+strings.Join(old, " "))
		}
		if !reflect.DeepEqual(h.s3Calls, wantCalls) {
			t.Fatalf("S3 %#v want %#v", h.s3Calls, wantCalls)
		}
		h.assertCommands(t)
	}
	for _, failure := range []string{"copy1", "copy2", "list", "delete"} {
		t.Run(failure, func(t *testing.T) {
			h := newHarness(t)
			want := &cloud.Error{Service: "s3", Operation: "CopyObject", Code: "SlowDown"}
			switch failure {
			case "copy1":
				h.copyFail = 1
				h.copyErr = want
				h.snapshot.Stdout = append(h.snapshot.Stdout, []byte(report("gmail")+"\n")...)
			case "copy2":
				h.copyFail = 2
				h.copyErr = want
			case "list":
				h.listErr = want
			case "delete":
				h.deleteErr = want
				h.objects = []cloud.Object{{Key: "golden/demo/old"}}
			}
			var out bytes.Buffer
			err := golden.Run(context.Background(), []string{"capture", "sbx1", "demo"}, &out, h.deps())
			if reflect.ValueOf(err).Pointer() != reflect.ValueOf(want).Pointer() || strings.Contains(out.String(), "prune:") {
				t.Fatalf("%v %q", err, out.String())
			}
			if failure == "copy1" && (len(h.s3Calls) != 1 || out.String() != "snapshot: ok (opsctl snapshot, 3 apps)\n") {
				t.Fatalf("%q %#v", out.String(), h.s3Calls)
			}
			if failure == "copy2" {
				h = newHarness(t)
				h.copyFail = 2
				h.copyErr = want
				stdout, stderr, code := h.cli()
				if code != 1 || stdout != snapshotLine+copyLine("crm") || stderr != "devctl: s3 CopyObject: SlowDown\n" || len(h.s3Calls) != 2 {
					t.Fatalf("%d %q %q %#v", code, stdout, stderr, h.s3Calls)
				}
			}
			if failure == "list" || failure == "delete" {
				h.assertCommands(t)
			}
		})
	}
}

func report(app string) string {
	return app + ": ok (s3://ikigenba.dev/sbx1/snapshots/" + app + "/" + stamp + ", arbitrary detail)"
}
func key(app string) string      { return "golden/demo/" + app + "/" + stamp }
func copyLine(app string) string { return "copy: ok (" + app + " -> ikigenba.dev/" + key(app) + ")\n" }

type harness struct {
	domain                                                  string
	t                                                       *testing.T
	root                                                    string
	commands                                                []seam.Cmd
	opens                                                   int
	openErr, stsErr, lookupErr, copyErr, listErr, deleteErr error
	state                                                   cloud.InstanceState
	status, snapshot                                        seam.Result
	objects                                                 []cloud.Object
	s3Calls                                                 []string
	copies, copyFail                                        int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "infra"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, root: root, domain: "sbx1.ikigenba.dev", state: cloud.StateRunning, status: seam.Result{Stdout: []byte("unparsed status")}, snapshot: seam.Result{Stdout: []byte(report("dashboard") + "\n\n" + report("crm") + "\n")}}
}
func (h *harness) deps() seam.Deps {
	return seam.Deps{Dir: h.root, EUID: 1000, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		h.commands = append(h.commands, cmd)
		switch cmd.Path {
		case "git":
			if !reflect.DeepEqual(cmd.Args, []string{"rev-parse", "--show-toplevel"}) {
				h.t.Fatalf("unexpected git %#v", cmd)
			}
			return seam.Result{Stdout: []byte(h.root + "\n")}, nil
		case "ssh":
			switch cmd.Args[len(cmd.Args)-1] {
			case "'sudo' 'opsctl' 'status'":
				return h.status, nil
			case "'sudo' 'opsctl' 'snapshot'":
				return h.snapshot, nil
			}
		}
		h.t.Fatalf("unexpected process %#v", cmd)
		return seam.Result{}, nil
	}, Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
		h.opens++
		if profile != "ikigenba.dev" || region != "us-east-2" {
			h.t.Fatalf("cloud %q %q", profile, region)
		}
		return cloud.Clients{EC2: fakeEC2{h: h}, STS: fakeSTS{h: h}, S3: fakeS3{h: h}}, h.openErr
	}, Now: func() time.Time { h.t.Fatal("clock used"); return time.Time{} }}
}
func (h *harness) cli() (string, string, int) {
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"golden", "capture", "sbx1", "demo"}, nil, &out, &stderr, h.deps())
	return out.String(), stderr.String(), code
}
func (h *harness) assertCommands(t *testing.T) {
	t.Helper()
	if h.opens != 1 || len(h.commands) != 3 {
		t.Fatalf("opens %d commands %#v", h.opens, h.commands)
	}
	for i, remote := range []string{"'sudo' 'opsctl' 'status'", "'sudo' 'opsctl' 'snapshot'"} {
		cmd := h.commands[i+1]
		if cmd.Path != "ssh" || cmd.Dir != h.root || !reflect.DeepEqual(cmd.Args, []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@18.118.7.42", remote}) {
			t.Fatalf("command %#v", cmd)
		}
	}
}

type fakeEC2 struct {
	cloud.EC2
	h *harness
}

func (f fakeEC2) ListSpaceInstances(_ context.Context, domain string) ([]cloud.Instance, error) {
	if domain != "ikigenba.dev" {
		f.h.t.Fatal(domain)
	}
	return []cloud.Instance{{ID: "i-test", Space: f.h.domain, Address: "18.118.7.42", State: f.h.state}}, f.h.lookupErr
}

type fakeSTS struct{ h *harness }

func (f fakeSTS) CallerAccountID(context.Context) (string, error) { return "123456789012", f.h.stsErr }

type fakeS3 struct {
	cloud.S3
	h *harness
}

func (f fakeS3) CopyObject(_ context.Context, bucket, source, key string) error {
	f.h.s3Calls = append(f.h.s3Calls, "copy "+bucket+" "+source+" "+key)
	f.h.copies++
	if !strings.HasPrefix(key, "golden/demo/") {
		f.h.t.Fatal(key)
	}
	if f.h.copies == f.h.copyFail {
		return f.h.copyErr
	}
	return nil
}
func (f fakeS3) ListObjects(_ context.Context, bucket, prefix string) ([]cloud.Object, error) {
	f.h.s3Calls = append(f.h.s3Calls, "list "+bucket+" "+prefix)
	return f.h.objects, f.h.listErr
}
func (f fakeS3) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	for _, key := range keys {
		if !strings.HasPrefix(key, "golden/demo/") {
			f.h.t.Fatal(key)
		}
	}
	f.h.s3Calls = append(f.h.s3Calls, "delete "+bucket+" "+strings.Join(keys, " "))
	return f.h.deleteErr
}
