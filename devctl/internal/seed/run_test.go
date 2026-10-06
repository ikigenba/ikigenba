package seed_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cli"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/seed"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

var _ func(context.Context, []string, io.Writer, seam.Deps) error = seed.Run

const wantHelp = `Usage: devctl seed <space> <source>

Give <space> the data of <source>, a golden set or another space. Each app's
newest snapshot in <source> is copied under <space>'s own seed/ prefix and
put back there with 'opsctl restore <app> --from <uri>', which replaces the
app's etc/, state/ and database, and writes its etc/env from <space>'s own
secrets. An app on <space> that <source> holds no snapshot of is left alone.

<source> is a golden set when one has that name, and otherwise the space with
that label. A space's full domain always names the space.
`

func TestSeedPublicErrorsAndGrammar(t *testing.T) {
	// R-T9FE-TD6G R-TANB-74X5 R-TBV7-KWNU R-TEB0-CG58 R-TFIW-Q7VX
	usage := &seed.UsageError{Message: "bad", Help: "devctl seed --help"}
	if usage.Error() != "bad" || usage.ExitCode() != 2 || usage.Detail() != "see 'devctl seed --help' for usage" {
		t.Fatal(usage)
	}
	invalid := &seed.NotASourceError{Operand: "crm.sbx1"}
	missing := &seed.NoSourceError{Operand: "gone"}
	if invalid.Error() != "'crm.sbx1' is not a golden set or a space" || invalid.ExitCode() != 2 || missing.Error() != "no golden set or space snapshots for 'gone'" || missing.ExitCode() != 1 {
		t.Fatal(invalid, missing)
	}
	deps := seam.Deps{Dir: t.TempDir(), EUID: 1000, Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("unexpected process")
		return seam.Result{}, nil
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud")
		return cloud.Clients{}, nil
	}}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"sbx2", "demo", "-h"}, {"--unknown", "--help"}} {
		var stdout, stderr bytes.Buffer
		if status := cli.Run(context.Background(), append([]string{"seed"}, args...), strings.NewReader(""), &stdout, &stderr, deps); status != 0 || stdout.String() != wantHelp || stderr.Len() != 0 {
			t.Fatalf("help %q: %d %q %q", args, status, stdout.String(), stderr.String())
		}
	}
	for _, tc := range []struct {
		args    []string
		message string
	}{{nil, "seed needs <space> and <source>"}, {[]string{"sbx2"}, "seed needs <space> and <source>"}, {[]string{"sbx2", "demo", "extra"}, "seed takes only <space> and <source>"}, {[]string{"sbx2", "--at=x"}, "unknown option '--at=x'"}} {
		var stdout, stderr bytes.Buffer
		want := "devctl: " + tc.message + "\n\nsee 'devctl seed --help' for usage\n"
		if status := cli.Run(context.Background(), append([]string{"seed"}, tc.args...), strings.NewReader(""), &stdout, &stderr, deps); status != 2 || stdout.Len() != 0 || stderr.String() != want {
			t.Fatalf("usage: %d %q %q", status, stdout.String(), stderr.String())
		}
	}
}

func TestSeedValidationOrderAndTargetFailures(t *testing.T) {
	// R-TZ1A-UJR1 R-THYP-HRDB
	for _, operand := range []string{"crm.sbx1", "Demo", "ikigenba.dev", "crm.sbx1.ikigenba.dev", "golden.ikigenba.dev"} {
		h := newHarness(t)
		var out, diag bytes.Buffer
		want := "devctl: '" + operand + "' is not a golden set or a space\n"
		if operand == "golden.ikigenba.dev" {
			want = "devctl: 'golden' is not a usable space label: golden/ holds the golden sets\n"
		}
		if status := cli.Run(context.Background(), []string{"seed", "sbx2", operand}, strings.NewReader(""), &out, &diag, h.deps()); status != 2 || out.Len() != 0 || diag.String() != want || h.opens != 0 || len(h.events) != 1 {
			t.Fatalf("%s: %d %q %q %v", operand, status, out.String(), diag.String(), h.events)
		}
	}
	for _, tc := range []struct {
		target string
		state  cloud.InstanceState
		want   string
		code   int
	}{{"gone", cloud.StateRunning, "devctl: no space at 'gone.ikigenba.dev'\n", 1}, {"sbx2", cloud.StateStopped, "devctl: 'sbx2.ikigenba.dev' is stopped\n", 1}, {"crm.sbx1", cloud.StateRunning, "devctl: 'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'\n", 2}} {
		h := newHarness(t)
		h.state = tc.state
		var out, diag bytes.Buffer
		if status := cli.Run(context.Background(), []string{"seed", tc.target, "Demo"}, strings.NewReader(""), &out, &diag, h.deps()); tc.target == "crm.sbx1" {
			if status != tc.code || diag.String() != tc.want || h.opens != 0 {
				t.Fatalf("target before source: %d %q", status, diag.String())
			}
		}
		// A valid source permits lookup; an invalid source prevents cloud access.
		out.Reset()
		diag.Reset()
		h = newHarness(t)
		h.state = tc.state
		if status := cli.Run(context.Background(), []string{"seed", tc.target, "demo"}, strings.NewReader(""), &out, &diag, h.deps()); status != tc.code || out.Len() != 0 || diag.String() != tc.want || h.s3Calls() != 0 || h.ssh != 0 {
			t.Fatalf("target: %d %q %q %v", status, out.String(), diag.String(), h.events)
		}
	}
	for _, phase := range []string{"root", "connect", "identity", "lookup"} {
		h := newHarness(t)
		sentinel := errors.New(phase)
		h.failurePhase = phase
		h.failure = sentinel
		if phase == "root" {
			if err := os.WriteFile(filepath.Join(h.root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev"}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var out bytes.Buffer
		err := seed.Run(context.Background(), []string{"sbx2", "demo"}, &out, h.deps())
		if phase == "root" {
			var rootErr *checkout.RootFileError
			if !errors.As(err, &rootErr) || rootErr.Detail != "missing 'region'" {
				t.Fatal(err)
			}
		} else if reflect.ValueOf(err).Pointer() != reflect.ValueOf(sentinel).Pointer() {
			t.Fatalf("error identity %v", err)
		}
		if out.Len() != 0 || h.s3Calls() != 0 || h.ssh != 0 {
			t.Fatalf("side effects %v", h.events)
		}
	}
	h := newHarness(t)
	h.state = cloud.StateStopped
	var stopped *space.NotRunningError
	if err := seed.Run(context.Background(), []string{"sbx2", "demo"}, io.Discard, h.deps()); !errors.As(err, &stopped) || stopped.Domain != "sbx2.ikigenba.dev" || stopped.State != cloud.StateStopped {
		t.Fatal(err)
	}
	h = newHarness(t)
	var invalid *spaceref.ReservedLabelError
	if err := seed.Run(context.Background(), []string{"sbx2", "golden.ikigenba.dev"}, io.Discard, h.deps()); !errors.As(err, &invalid) || invalid.Label != "golden" {
		t.Fatal(err)
	}
}

func TestSeedSourceResolution(t *testing.T) {
	// R-U7KL-IXXW R-TKEI-9AUP
	for _, tc := range []struct {
		operand  string
		golden   []string
		space    []string
		prefixes []string
		detail   string
	}{
		{"demo", []string{"crm/a", "dashboard/b"}, []string{"crm/z", "dashboard/z"}, []string{"golden/demo/", "sbx2/seed/"}, "golden set demo, 2 apps"},
		{"demo.ikigenba.dev", []string{"crm/a"}, []string{"crm/z", "dashboard/z"}, []string{"demo/snapshots/", "sbx2/seed/"}, "space demo.ikigenba.dev, 2 apps"},
		{"sbx1", []string{"notes.txt", "crm/"}, []string{"crm/x/y", "Bad_1/a", "crm/b", "dashboard/c"}, []string{"golden/sbx1/", "sbx1/snapshots/", "sbx2/seed/"}, "space sbx1.ikigenba.dev, 2 apps"},
		{"sbx1.ikigenba.dev", nil, []string{"crm/b", "dashboard/c"}, []string{"sbx1/snapshots/", "sbx2/seed/"}, "space sbx1.ikigenba.dev, 2 apps"},
	} {
		h := newHarness(t)
		label := strings.TrimSuffix(tc.operand, ".ikigenba.dev")
		h.add("golden/"+label+"/", tc.golden...)
		h.add(label+"/snapshots/", tc.space...)
		var out bytes.Buffer
		if err := seed.Run(context.Background(), []string{"sbx2", tc.operand}, &out, h.deps()); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), "source: ok ("+tc.detail+")\n") || !reflect.DeepEqual(h.prefixes, tc.prefixes) || len(h.copies) != 2 {
			t.Fatalf("%s: %q %v %v", tc.operand, out.String(), h.prefixes, h.copies)
		}
	}
	for _, operand := range []string{"gone", "golden"} {
		h := newHarness(t)
		var out, diag bytes.Buffer
		if status := cli.Run(context.Background(), []string{"seed", "sbx2", operand}, strings.NewReader(""), &out, &diag, h.deps()); status != 1 || out.Len() != 0 || diag.String() != "devctl: no golden set or space snapshots for '"+operand+"'\n" || len(h.copies) != 0 || h.ssh != 0 {
			t.Fatalf("missing: %d %q %q", status, out.String(), diag.String())
		}
		want := []string{"golden/" + operand + "/"}
		if operand != "golden" {
			want = append(want, operand+"/snapshots/")
		}
		if !reflect.DeepEqual(h.prefixes, want) {
			t.Fatal(h.prefixes)
		}
	}
}

func TestSeedCopiesNewestInOrderAndOnlyTargetMutates(t *testing.T) {
	// R-U8SH-WPOL R-TMUB-0UC3 R-U3WW-DMPT R-TQI0-65K6
	for _, source := range []string{"sbx1", "sbx2"} {
		h := newHarness(t)
		h.add(source+"/snapshots/", "dashboard/2026-09-12T14:22:51Z.tar.zst", "crm/2026-09-12T14:22:51Z.tar.zst", "crm/2026-09-14T09:10:02Z.tar.zst", "crm/2026-09-13T00:00:00Z.tar.zst")
		h.add("golden/other/", "crm/a")
		h.add("sbx2/seed/", "gmail/2026-09-01T00:00:00Z.tar.zst")
		original := append([]cloud.Object(nil), h.objects...)
		var out bytes.Buffer
		if err := seed.Run(context.Background(), []string{"sbx2", source}, &out, h.deps()); err != nil {
			t.Fatal(err)
		}
		want := "source: ok (space " + source + ".ikigenba.dev, 2 apps)\ncopy: ok (crm -> ikigenba.dev/sbx2/seed/crm/2026-09-14T09:10:02Z.tar.zst)\ncopy: ok (dashboard -> ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)\nrestore: ok (opsctl restore crm --from s3://ikigenba.dev/sbx2/seed/crm/2026-09-14T09:10:02Z.tar.zst)\nrestore: ok (opsctl restore dashboard --from s3://ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)\nclean: ok (3 objects deleted)\n"
		if out.String() != want || h.lookups != 1 || h.opens != 1 || h.ssh != 2 || len(h.deleted) != 3 {
			t.Fatalf("output %q events %v", out.String(), h.events)
		}
		if !reflect.DeepEqual(h.copies, [][2]string{{source + "/snapshots/crm/2026-09-14T09:10:02Z.tar.zst", "sbx2/seed/crm/2026-09-14T09:10:02Z.tar.zst"}, {source + "/snapshots/dashboard/2026-09-12T14:22:51Z.tar.zst", "sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst"}}) {
			t.Fatal(h.copies)
		}
		for _, object := range original {
			if !strings.HasPrefix(object.Key, "sbx2/seed/") && !containsObject(h.objects, object) {
				t.Fatalf("modified %s", object.Key)
			}
		}
		if h.events[4] != "copy" || h.events[5] != "ssh" {
			t.Fatalf("all copies precede restores: %v", h.events)
		}
	}
}

func TestSeedCopyRestoreAndCleanupFailures(t *testing.T) {
	// R-U8SH-WPOL R-TMUB-0UC3 R-U3WW-DMPT
	for _, phase := range []string{"copy1", "copy2", "restore2", "restore3", "clean-list", "delete", "empty-clean"} {
		h := newHarness(t)
		h.add("golden/demo/", "crm/2026-09-12T14:22:51Z.tar.zst", "dashboard/2026-09-12T14:22:51Z.tar.zst", "gmail/2026-09-12T14:22:51Z.tar.zst")
		sentinel := &cloud.Error{Service: "s3", Operation: "CopyObject", Code: "SlowDown"}
		h.failure = sentinel
		h.failurePhase = phase
		var out, diag bytes.Buffer
		status := cli.Run(context.Background(), []string{"seed", "sbx2", "demo"}, strings.NewReader(""), &out, &diag, h.deps())
		switch phase {
		case "copy1", "copy2":
			n := 1
			wantOut := "source: ok (golden set demo, 3 apps)\n"
			if phase == "copy2" {
				n = 2
				wantOut += "copy: ok (crm -> ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)\n"
			}
			if status != 1 || len(h.copies) != n || h.ssh != 0 || out.String() != wantOut || diag.String() != "devctl: s3 CopyObject: SlowDown\n" || h.events[len(h.events)-1] != "copy" {
				t.Fatalf("copy: %d %q %q %v", status, out.String(), diag.String(), h.events)
			}
		case "restore2":
			if status != 1 || h.ssh != 2 || h.deleteCalls != 0 || len(h.seedObjects()) != 3 || strings.Count(out.String(), "restore: ok") != 1 || h.events[len(h.events)-1] != "ssh" {
				t.Fatalf("restore stops: %d %q %v", status, out.String(), h.events)
			}
		case "restore3":
			wantOut := "source: ok (golden set demo, 3 apps)\n" +
				"copy: ok (crm -> ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)\n" +
				"copy: ok (dashboard -> ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)\n" +
				"copy: ok (gmail -> ikigenba.dev/sbx2/seed/gmail/2026-09-12T14:22:51Z.tar.zst)\n" +
				"restore: ok (opsctl restore crm --from s3://ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)\n" +
				"restore: ok (opsctl restore dashboard --from s3://ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)\n"
			want := "devctl: restore: ssh ec2-user@18.224.31.9 sudo opsctl restore gmail --from s3://ikigenba.dev/sbx2/seed/gmail/2026-09-12T14:22:51Z.tar.zst: exit status 1\n\n> partial\n> output\n> failure\n> detail\n"
			if status != 1 || h.ssh != 3 || h.deleteCalls != 0 || len(h.seedObjects()) != 3 || out.String() != wantOut || diag.String() != want {
				t.Fatalf("restore: %d %q %q %v", status, out.String(), diag.String(), h.events)
			}
		case "clean-list", "delete":
			if status != 1 || strings.Count(out.String(), "restore: ok") != 3 || strings.Contains(out.String(), "clean:") || len(h.seedObjects()) != 3 {
				t.Fatalf("clean: %d %q %v", status, out.String(), h.events)
			}
		case "empty-clean":
			if status != 0 || !strings.HasSuffix(out.String(), "clean: ok (nothing to delete)\n") || h.deleteCalls != 0 {
				t.Fatalf("empty %d %q", status, out.String())
			}
		}
	}
	// Golden two-app cleanup, with and without stale copies.
	for _, stale := range []bool{false, true} {
		h := newHarness(t)
		h.add("golden/demo/", "crm/a", "dashboard/b")
		if stale {
			h.add("sbx2/seed/", "gmail/old")
		}
		var out bytes.Buffer
		if err := seed.Run(context.Background(), []string{"sbx2", "demo"}, &out, h.deps()); err != nil {
			t.Fatal(err)
		}
		n := 2
		if stale {
			n = 3
		}
		if len(h.deleted) != n || len(h.seedObjects()) != 0 || h.deleteCalls != 1 || !strings.HasSuffix(out.String(), "clean: ok ("+strconv.Itoa(n)+" objects deleted)\n") {
			t.Fatal(h.deleted)
		}
	}
	h := newHarness(t)
	h.add("golden/demo/", "crm/a", "dashboard/b")
	h.failurePhase = "delete"
	h.failure = &cloud.Error{Service: "s3", Operation: "DeleteObjects", Code: "AccessDenied"}
	var failedOut, failedDiag bytes.Buffer
	if status := cli.Run(context.Background(), []string{"seed", "sbx2", "demo"}, strings.NewReader(""), &failedOut, &failedDiag, h.deps()); status != 1 || strings.Count(failedOut.String(), "restore: ok") != 2 || strings.Contains(failedOut.String(), "clean:") || len(h.seedObjects()) != 2 {
		t.Fatalf("two-app cleanup failure: %d %q %q", status, failedOut.String(), failedDiag.String())
	}
	// Cloud failures are returned unchanged at each storage stage.
	for _, phase := range []string{"golden-list", "space-list", "copy1", "clean-list", "delete"} {
		h := newHarness(t)
		h.add("golden/demo/", "crm/a")
		if phase == "space-list" {
			h.objects = nil
		}
		h.failurePhase = phase
		h.failure = errors.New(phase + " failure")
		var out bytes.Buffer
		err := seed.Run(context.Background(), []string{"sbx2", "demo"}, &out, h.deps())
		if reflect.ValueOf(err).Pointer() != reflect.ValueOf(h.failure).Pointer() {
			t.Fatalf("%s identity: %v", phase, err)
		}
		if phase == "golden-list" || phase == "space-list" {
			if out.Len() != 0 || len(h.copies) != 0 || h.ssh != 0 {
				t.Fatalf("listing side effects %q %v", out.String(), h.events)
			}
		}
	}
	h = newHarness(t)
	h.add("golden/demo/", "crm/a", "dashboard/b", "gmail/c")
	h.failurePhase = "restore3"
	err := seed.Run(context.Background(), []string{"sbx2", "demo"}, io.Discard, h.deps())
	var remoteErr *host.CommandError
	if !errors.As(err, &remoteErr) || remoteErr.Step != "restore" {
		t.Fatal(err)
	}
}

type harness struct {
	t                                *testing.T
	root                             string
	state                            cloud.InstanceState
	objects                          []cloud.Object
	events, prefixes                 []string
	copies                           [][2]string
	deleted                          []string
	opens, lookups, ssh, deleteCalls int
	failurePhase                     string
	failure                          error
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, root: root, state: cloud.StateRunning}
}
func (h *harness) add(prefix string, keys ...string) {
	for _, key := range keys {
		h.objects = append(h.objects, cloud.Object{Key: prefix + key})
	}
}
func (h *harness) deps() seam.Deps {
	return seam.Deps{Dir: h.root, EUID: 1000, Now: func() time.Time { h.t.Fatal("clock used"); return time.Time{} }, After: func(time.Duration) <-chan time.Time { h.t.Fatal("wait used"); return nil }, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		h.events = append(h.events, cmd.Path)
		switch cmd.Path {
		case "git":
			if !reflect.DeepEqual(cmd.Args, []string{"rev-parse", "--show-toplevel"}) {
				h.t.Fatal(cmd)
			}
			return seam.Result{Stdout: []byte(h.root + "\n")}, nil
		case "ssh":
			h.ssh++
			app := []string{"crm", "dashboard", "gmail"}[h.ssh-1]
			key := ""
			for _, copyPair := range h.copies {
				if strings.Contains(copyPair[1], "/"+app+"/") {
					key = copyPair[1]
				}
			}
			want := "'sudo' 'opsctl' 'restore' '" + app + "' '--from' 's3://ikigenba.dev/" + key + "'"
			if cmd.Args[len(cmd.Args)-2] != "ec2-user@18.224.31.9" || cmd.Args[len(cmd.Args)-1] != want {
				h.t.Fatal(cmd)
			}
			if (h.failurePhase == "restore3" && h.ssh == 3) || (h.failurePhase == "restore2" && h.ssh == 2) {
				return seam.Result{ExitCode: 1, Stdout: []byte("partial\noutput\n"), Stderr: []byte("failure\ndetail\n")}, nil
			}
			return seam.Result{Stdout: []byte("discard me"), Stderr: []byte("discard me too")}, nil
		default:
			h.t.Fatal(cmd)
			return seam.Result{}, nil
		}
	}, Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
		h.opens++
		if profile != "ikigenba.dev" || region != "us-east-2" {
			h.t.Fatal(profile, region)
		}
		if h.failurePhase == "connect" {
			return cloud.Clients{}, h.failure
		}
		return cloud.Clients{STS: fakeSTS{h: h}, EC2: fakeEC2{h: h}, S3: fakeS3{h: h}}, nil
	}}
}

type fakeSTS struct {
	cloud.STS
	h *harness
}

func (f fakeSTS) CallerAccountID(context.Context) (string, error) {
	if f.h.failurePhase == "identity" {
		return "", f.h.failure
	}
	return "123456789012", nil
}

type fakeEC2 struct {
	cloud.EC2
	h *harness
}

func (f fakeEC2) ListSpaceInstances(_ context.Context, root string) ([]cloud.Instance, error) {
	f.h.lookups++
	if root != "ikigenba.dev" {
		f.h.t.Fatal(root)
	}
	if f.h.failurePhase == "lookup" {
		return nil, f.h.failure
	}
	return []cloud.Instance{{ID: "i-target", Space: "sbx2.ikigenba.dev", State: f.h.state, Address: "18.224.31.9"}}, nil
}

type fakeS3 struct{ h *harness }

func (f fakeS3) ListObjects(_ context.Context, bucket, prefix string) ([]cloud.Object, error) {
	h := f.h
	h.checkBucket(bucket)
	h.events = append(h.events, "list")
	h.prefixes = append(h.prefixes, prefix)
	if (h.failurePhase == "golden-list" && prefix == "golden/demo/") || (h.failurePhase == "space-list" && prefix == "demo/snapshots/") {
		return nil, h.failure
	}
	if prefix == "sbx2/seed/" {
		if h.failurePhase == "clean-list" {
			return nil, h.failure
		}
		if h.failurePhase == "empty-clean" {
			return nil, nil
		}
	}
	var objects []cloud.Object
	for _, object := range h.objects {
		if strings.HasPrefix(object.Key, prefix) {
			objects = append(objects, object)
		}
	}
	return objects, nil
}
func (f fakeS3) CopyObject(_ context.Context, bucket, source, key string) error {
	h := f.h
	h.checkBucket(bucket)
	h.events = append(h.events, "copy")
	h.copies = append(h.copies, [2]string{source, key})
	if !strings.HasPrefix(key, "sbx2/seed/") {
		h.t.Fatal(key)
	}
	if (h.failurePhase == "copy1" && len(h.copies) == 1) || (h.failurePhase == "copy2" && len(h.copies) == 2) {
		return h.failure
	}
	h.objects = append(h.objects, cloud.Object{Key: key})
	return nil
}
func (f fakeS3) PutObject(context.Context, string, string, io.Reader, int64) error {
	f.h.t.Fatal("PutObject called")
	return nil
}
func (f fakeS3) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	h := f.h
	h.checkBucket(bucket)
	h.events = append(h.events, "delete")
	h.deleteCalls++
	h.deleted = append([]string(nil), keys...)
	for _, key := range keys {
		if !strings.HasPrefix(key, "sbx2/seed/") {
			h.t.Fatal(key)
		}
	}
	if h.failurePhase == "delete" {
		return h.failure
	}
	var remaining []cloud.Object
	for _, object := range h.objects {
		deleted := false
		for _, key := range keys {
			if object.Key == key {
				deleted = true
			}
		}
		if !deleted {
			remaining = append(remaining, object)
		}
	}
	h.objects = remaining
	return nil
}
func (h *harness) checkBucket(bucket string) {
	if bucket != "ikigenba.dev" {
		h.t.Fatal(bucket)
	}
}
func (h *harness) s3Calls() int {
	n := 0
	for _, event := range h.events {
		if event == "list" || event == "copy" || event == "delete" {
			n++
		}
	}
	return n
}
func (h *harness) seedObjects() []cloud.Object {
	var result []cloud.Object
	for _, object := range h.objects {
		if strings.HasPrefix(object.Key, "sbx2/seed/") {
			result = append(result, object)
		}
	}
	return result
}
func containsObject(objects []cloud.Object, want cloud.Object) bool {
	for _, object := range objects {
		if object == want {
			return true
		}
	}
	return false
}
