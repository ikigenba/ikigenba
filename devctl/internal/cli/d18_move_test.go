package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spacemove"
)

var _ func(context.Context, []string, string, io.Writer, seam.Deps) error = spacemove.Run

const moveOldID = "i-0a1b2c3d4e5f60718"
const moveNewID = "i-0e5d4c3b2a1f09876"
const moveOldIP = "18.220.10.5"
const moveNewIP = "3.141.27.86"
const moveLaunchIP = "3.22.140.61"
const moveZone = "Z09565073GHK8BYWQ1A78"
const moveTime = "2026-11-08T18:30:00Z"
const moveActivation = "opaque one\nopaque two\nopaque three\nopaque four\nopaque five\nopaque six\nopaque seven\nopaque eight\n"
const expectedMoveUsage = `Usage: devctl space move <space> [--release <sha|tag>]

Move the space to a new instance launched from the current launch template:
build the release, have the current host take its final backup with opsctl
retire, stop it and tag it and its Elastic IP Retired, launch the new instance
with a new Elastic IP, copy and unpack the release there, restore the host
backup, point the space's records at the new address, restore each app's data,
and activate the release. The retired instance stays, stopped, as the way
back until space destroy removes it. Completed steps remain on failure.

Options:
  --release <sha|tag>     the release to deploy; the newest release-<N> tag otherwise
`

// Unimplemented cloud operations panic through the embedded interfaces. A move
// has no permission to use those operations, including any SSM operation.
type moveFake struct {
	cloud.EC2
	cloud.IAM
	cloud.Route53
	cloud.S3
	t                          *testing.T
	h                          *commandHarness
	ctx                        context.Context
	tmp                        string
	tags, sha, resolve         string
	instances                  []cloud.Instance
	addresses                  []cloud.Address
	apex, apexOther            bool
	backup                     bool
	absentApp                  string
	oldState                   cloud.InstanceState
	events                     []string
	commands                   []seam.Cmd
	prefixes                   []string
	changes                    [][]cloud.RecordChange
	restoreDone                bool
	knownLaunch, knownRestored string
	fail                       string
	failRemote                 string
	failRestored               bool
	cause                      error
	compileApp                 string
	version                    string
}

func newMoveFake(t *testing.T) *moveFake {
	h := newCommandHarness(t)
	for _, app := range createApps {
		h.addAppWithSecrets(app)
	}
	return &moveFake{t: t, h: h, ctx: t.Context(), tmp: t.TempDir(), tags: "release-86\nrelease-87\nrelease-88-rc1\n", sha: createSHA, backup: true, oldState: cloud.StateRunning,
		instances: []cloud.Instance{{ID: moveOldID, Space: "staging.ikigenba.dev", State: cloud.StateRunning, Address: moveOldIP}},
		addresses: []cloud.Address{{AllocationID: "eipalloc-0123456789abcdef0", IP: moveOldIP, Space: "staging.ikigenba.dev"}}, cause: errors.New("move sentinel")}
}
func (f *moveFake) call(ctx context.Context, name string) error {
	f.t.Helper()
	if ctx != f.ctx {
		f.t.Fatalf("context changed at %s", name)
	}
	if name == "open" || name == "sts" || name == "zone" || name == "template" || name == "boundary" || name == "instances" || name == "addresses" || name == "apex" {
		entries, err := os.ReadDir(f.tmp)
		if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
			f.t.Fatalf("temporary work started before preflight completed: %v %v", entries, err)
		}
	}
	f.events = append(f.events, name)
	if name == f.fail {
		return f.cause
	}
	return nil
}
func (f *moveFake) deps() seam.Deps {
	return seam.Deps{Dir: f.h.checkoutRoot, EUID: 1, Getenv: func(key string) string {
		if key == "TMPDIR" {
			return f.tmp
		}
		return ""
	}, Now: func() time.Time { return time.Date(2026, 11, 8, 18, 30, 0, 0, time.UTC) }, After: immediateD07After,
		Cloud: func(ctx context.Context, profile, region string) (cloud.Clients, error) {
			if profile != "ikigenba.dev" || region != "us-east-2" {
				f.t.Fatal(profile, region)
			}
			if err := f.call(ctx, "open"); err != nil {
				return cloud.Clients{}, err
			}
			return cloud.Clients{EC2: f, IAM: f, Route53: f, S3: f, STS: f}, nil
		}, Exec: f.exec, Stream: func(ctx context.Context, c seam.Cmd, w io.Writer) (seam.Result, error) {
			f.checkTransport(c)
			if err := f.call(ctx, "activate"); err != nil {
				return seam.Result{ExitCode: 1, Stderr: []byte("arbitrary activation failure")}, nil
			}
			_, err := io.WriteString(w, moveActivation)
			return seam.Result{}, err
		}}
}
func (f *moveFake) checkTransport(c seam.Cmd) {
	f.t.Helper()
	f.commands = append(f.commands, c)
	if c.Dir != f.h.checkoutRoot {
		f.t.Fatal(c)
	}
	joined := strings.Join(c.Args, " ")
	if strings.Contains(joined, moveLaunchIP) {
		f.t.Fatal("launch address used after allocation", c)
	}
	known := ""
	for _, arg := range c.Args {
		if strings.HasPrefix(arg, "UserKnownHostsFile=") {
			known = strings.TrimPrefix(arg, "UserKnownHostsFile=")
		}
	}
	if strings.Contains(joined, "ec2-user@"+moveOldIP) {
		if known != "" {
			f.t.Fatal("old host used temporary known hosts")
		}
		return
	}
	if !strings.Contains(joined, "ec2-user@"+moveNewIP) || known == "" {
		f.t.Fatal("new host lacks isolated known hosts", c)
	}
	if filepath.Dir(filepath.Dir(known)) != f.tmp || !strings.HasPrefix(filepath.Base(filepath.Dir(known)), "move.") {
		f.t.Fatal(known)
	}
	want := "known_hosts.launch"
	if f.restoreDone {
		want = "known_hosts"
	}
	if filepath.Base(known) != want {
		f.t.Fatal("wrong key phase", known, f.restoreDone)
	}
	if f.restoreDone && f.knownRestored == "" {
		if _, err := os.Stat(known); !os.IsNotExist(err) {
			f.t.Fatal("restored key file already exists", err)
		}
		f.knownRestored = known
	} else if !f.restoreDone {
		f.knownLaunch = known
	}
	if err := os.WriteFile(known, []byte("arbitrary trusted key\n"), 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *moveFake) exec(ctx context.Context, c seam.Cmd) (seam.Result, error) {
	if c.Path == "git" && c.Args[0] == "tag" {
		f.events = append(f.events, "tags")
		return seam.Result{Stdout: []byte(f.tags)}, nil
	}
	if c.Path == "git" && len(c.Args) > 1 && c.Args[1] == "--verify" {
		f.resolve = c.Args[len(c.Args)-1]
		f.events = append(f.events, "resolve")
		if f.resolve == "refs/tags/main^{commit}" {
			return seam.Result{ExitCode: 1}, nil
		}
		return seam.Result{Stdout: []byte(f.sha + "\n")}, nil
	}
	if c.Path == "go" && f.compileApp != "" && filepath.Base(c.Dir) == f.compileApp {
		return seam.Result{ExitCode: 1, Stderr: []byte("# github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\ncmd/dashboard/main.go:41:2: undefined: render\n")}, nil
	}
	if c.Path == "tar" {
		data, err := os.ReadFile(filepath.Join(c.Args[3], f.sha, "release.json"))
		if err != nil {
			f.t.Fatal(err)
		}
		var meta struct{ Devctl string }
		if err := json.Unmarshal(data, &meta); err != nil {
			f.t.Fatal(err)
		}
		f.version = meta.Devctl
	}
	if result, handled, err := fakeCreateBuild(f.t, f.h.checkoutRoot, c); handled {
		return result, err
	}
	if c.Path != "ssh" && c.Path != "scp" {
		f.t.Fatalf("unexpected process %#v", c)
	}
	f.checkTransport(c)
	if c.Path == "scp" {
		if c.Args[len(c.Args)-2] != filepath.Join(f.h.checkoutRoot, "dist", f.sha+".tar.xz") {
			f.t.Fatal(c)
		}
		return seam.Result{}, nil
	}
	remote := c.Args[len(c.Args)-1]
	if f.failRemote != "" && strings.Contains(remote, f.failRemote) && f.restoreDone == f.failRestored {
		return seam.Result{ExitCode: 1, Stderr: []byte("remote failure")}, nil
	}
	if strings.Contains(remote, "'test' '-e'") {
		return seam.Result{ExitCode: 1}, nil
	}
	if strings.Contains(remote, "'mktemp' '-d'") {
		return seam.Result{Stdout: []byte("/opt/ikigenba/releases/.unpack.fixture\n")}, nil
	}
	if remote == "'mktemp'" {
		return seam.Result{Stdout: []byte("/tmp/arbitrary-upload\n")}, nil
	}
	stage := ""
	switch {
	case remote == "'sudo' 'opsctl' 'retire'":
		stage = "retire"
	case strings.HasSuffix(remote, "'host' 'restore'"):
		stage = "restore"
	case strings.HasSuffix(remote, "'init'"):
		stage = "init"
	}
	if stage != "" {
		if err := f.call(ctx, stage); err != nil {
			return seam.Result{ExitCode: 1, Stderr: []byte("opsctl: backup failed")}, nil
		}
		if stage == "restore" {
			f.restoreDone = true
		}
	}
	return seam.Result{Stdout: []byte("opaque captured output\n")}, nil
}
func (f *moveFake) CallerAccountID(ctx context.Context) (string, error) {
	return "295229566359", f.call(ctx, "sts")
}
func (f *moveFake) Zone(ctx context.Context, root string) (cloud.Zone, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return cloud.Zone{ID: moveZone, Name: root}, f.call(ctx, "zone")
}
func (f *moveFake) LaunchTemplate(ctx context.Context, root string) (string, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return "lt-current", f.call(ctx, "template")
}
func (f *moveFake) PermissionsBoundary(ctx context.Context, root string) (string, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return "boundary", f.call(ctx, "boundary")
}
func (f *moveFake) ListSpaceInstances(ctx context.Context, root string) ([]cloud.Instance, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return f.instances, f.call(ctx, "instances")
}
func (f *moveFake) ListSpaceAddresses(ctx context.Context, root string) ([]cloud.Address, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return f.addresses, f.call(ctx, "addresses")
}
func (f *moveFake) FindRecord(ctx context.Context, zone, root, kind string) (cloud.Record, bool, error) {
	if zone != moveZone || root != "ikigenba.dev" || kind != "A" {
		f.t.Fatal(zone, root, kind)
	}
	address := moveOldIP
	if f.apexOther {
		address = "203.0.113.9"
	}
	return cloud.Record{Name: root, Type: kind, Values: []string{address}}, f.apex || f.apexOther, f.call(ctx, "apex")
}
func (f *moveFake) StartInstance(ctx context.Context, id string) error {
	if id != moveOldID {
		f.t.Fatal(id)
	}
	f.oldState = cloud.StateRunning
	return f.call(ctx, "start")
}
func (f *moveFake) StopInstance(ctx context.Context, id string) error {
	if id != moveOldID {
		f.t.Fatal(id)
	}
	f.oldState = cloud.StateStopped
	return f.call(ctx, "stop")
}
func (f *moveFake) DescribeInstance(ctx context.Context, id string) (cloud.Instance, error) {
	state, address := cloud.StateRunning, moveLaunchIP
	if id == moveOldID {
		state, address = f.oldState, moveOldIP
	} else if id != moveNewID {
		f.t.Fatal(id)
	}
	return cloud.Instance{ID: id, State: state, Address: address}, f.call(ctx, "describe "+id)
}
func (f *moveFake) InstanceChecksPassed(ctx context.Context, id string) (bool, error) {
	return true, f.call(ctx, "checks "+id)
}
func (f *moveFake) TagRetired(ctx context.Context, ids []string, value string) error {
	want := []string{moveOldID}
	if len(f.addresses) > 0 {
		want = append(want, "eipalloc-0123456789abcdef0")
	}
	if !reflect.DeepEqual(ids, want) || value != moveTime {
		f.t.Fatal(ids, value)
	}
	return f.call(ctx, "tag")
}
func (f *moveFake) RunInstance(ctx context.Context, spec cloud.LaunchSpec) (cloud.Instance, error) {
	if spec != (cloud.LaunchSpec{LaunchTemplateID: "lt-current", InstanceProfile: "staging.ikigenba.dev", Domain: "ikigenba.dev", Space: "staging.ikigenba.dev"}) {
		f.t.Fatal(spec)
	}
	return cloud.Instance{ID: moveNewID}, f.call(ctx, "launch")
}
func (f *moveFake) AllocateAddress(ctx context.Context, root, domain string) (cloud.Address, error) {
	if root != "ikigenba.dev" || domain != "staging.ikigenba.dev" {
		f.t.Fatal(root, domain)
	}
	return cloud.Address{AllocationID: "new-allocation", IP: moveNewIP}, f.call(ctx, "allocate")
}
func (f *moveFake) AssociateAddress(ctx context.Context, allocation, id string) error {
	if allocation != "new-allocation" || id != moveNewID {
		f.t.Fatal(allocation, id)
	}
	return f.call(ctx, "associate")
}
func (f *moveFake) ListObjects(ctx context.Context, bucket, prefix string) ([]cloud.Object, error) {
	if bucket != "ikigenba.dev" {
		f.t.Fatal(bucket)
	}
	f.prefixes = append(f.prefixes, prefix)
	if err := f.call(ctx, "objects "+prefix); err != nil {
		return nil, err
	}
	if !f.backup || prefix == "staging/"+f.absentApp+"/" {
		return nil, nil
	}
	return []cloud.Object{{Key: prefix + "2026-11-07T18:29:41Z.tar.zst", Modified: time.Unix(1, 0)}, {Key: prefix + "2026-11-08T18:29:41Z.tar.zst", Modified: time.Unix(2, 0)}}, nil
}
func (f *moveFake) ChangeRecords(ctx context.Context, zone string, changes []cloud.RecordChange) (string, error) {
	if !f.restoreDone || zone != moveZone {
		f.t.Fatal("DNS changed before restore", zone)
	}
	f.changes = append(f.changes, changes)
	return "change", f.call(ctx, "records")
}
func (f *moveFake) ChangeStatus(ctx context.Context, id string) (cloud.ChangeStatus, error) {
	if id != "change" {
		f.t.Fatal(id)
	}
	return cloud.ChangeInsync, f.call(ctx, "insync")
}
func (f *moveFake) run(args ...string) runResult {
	var out, diagnostic bytes.Buffer
	code := Run(f.ctx, append([]string{"space", "move"}, args...), strings.NewReader(""), &out, &diagnostic, f.deps())
	return runResult{code: code, stdout: out.String(), stderr: diagnostic.String()}
}
func (f *moveFake) clean() {
	f.t.Helper()
	entries, err := os.ReadDir(f.tmp)
	if err != nil || len(entries) != 0 {
		f.t.Fatalf("temporary directory retained: %v %v", entries, err)
	}
}

func moveInitial() string {
	return "account: ok (ikigenba.dev, us-east-2, 295229566359)\ndomain: ok (zone ikigenba.dev " + moveZone + ")\nbuild: ok (release-87, dist/" + createSHA + ".tar.xz)\n"
}
func expectedMove(stopped, apex, noAddress bool, absentApp string) string {
	s := moveInitial()
	if stopped {
		s += "start: ok (" + moveOldID + " running, status checks passed)\n"
	}
	s += "retire: ok (opsctl retire)\nstop: ok (" + moveOldID + " stopped)\ntag: ok (" + moveOldID
	if !noAddress {
		s += " and elastic ip " + moveOldIP
	}
	s += " Retired=" + moveTime + ")\n"
	s += "instance: ok (" + moveNewID + " running, " + moveLaunchIP + ")\naddress: ok (elastic ip " + moveNewIP + " associated)\nhost: ok (status checks passed, cloud-init done)\ncopy: ok (" + createSHA + ".tar.xz -> " + moveNewIP + ")\nunpack: ok (" + release.Folder(createSHA) + ")\nopsctl: ok (5 keys set)\nrestore: ok (host/2026-11-08T18:29:41Z.tar.zst, 5 keys set again)\nrecords: ok (updated staging.ikigenba.dev, *.staging.ikigenba.dev -> " + moveNewIP + ", INSYNC)\ninit: ok\n"
	for _, app := range createApps {
		detail := "opsctl restore " + app
		if app == absentApp {
			detail = "no backup of " + app
		}
		s += "restore: ok (" + detail + ")\n"
	}
	s += moveActivation
	if apex {
		s += "apex: ok (ikigenba.dev -> " + moveNewIP + ", INSYNC)\n"
	}
	return s + "staging.ikigenba.dev " + moveNewID + " " + moveNewIP + "\n"
}

func TestMoveSuccessfulFlows(t *testing.T) {
	// R-K6AR-U5BF R-G0YK-GUGN R-VQ53-X7P1 R-KKXK-FE7R R-KM5G-T5YG
	// R-KNDD-6XP5 R-KOL9-KPFU R-KPT5-YH6J R-KS8Y-Q0NX R-KTGV-3SEM
	// R-KUOR-HK5B R-KVWN-VBW0 R-KX4K-93MP R-KZKD-0N43 R-L0S9-EEUS
	// R-L205-S6LH R-L382-5YC6 R-L4FY-JQ2V R-L5NU-XHTK R-KIHR-NUQD
	for _, tc := range []struct {
		name, operand                       string
		stopped, apex, apexOther, noAddress bool
		absentApp                           string
	}{
		{name: "label", operand: "staging"}, {name: "domain", operand: "staging.ikigenba.dev"}, {name: "stopped", operand: "staging", stopped: true}, {name: "apex", operand: "staging", apex: true}, {name: "another apex holder", operand: "staging", apexOther: true}, {name: "no address or app backup", operand: "staging", noAddress: true, absentApp: "sites"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMoveFake(t)
			f.apex, f.apexOther = tc.apex, tc.apexOther
			if tc.apexOther {
				f.addresses = append(f.addresses, cloud.Address{Space: "other.ikigenba.dev", IP: "203.0.113.9"})
			}
			f.absentApp = tc.absentApp
			if tc.stopped {
				f.instances[0].State = cloud.StateStopped
				f.oldState = cloud.StateStopped
			}
			if tc.noAddress {
				f.addresses = nil
			}
			result := f.run(tc.operand)
			assertResult(t, result, 0, expectedMove(tc.stopped, tc.apex, tc.noAddress, tc.absentApp), "")
			f.clean()
			if f.version != strings.TrimSpace(invokeWithDeps(f.deps(), "--version").stdout) {
				t.Fatal("version was not forwarded", f.version)
			}
			if f.knownLaunch == "" || f.knownRestored == "" || f.knownLaunch == f.knownRestored {
				t.Fatal("missing key phases")
			}
			wantEvents := []string{"tags", "resolve", "open", "sts", "zone", "template", "boundary", "instances", "addresses", "apex"}
			if tc.apex || tc.apexOther {
				wantEvents = append(wantEvents, "addresses")
			}
			if tc.stopped {
				wantEvents = append(wantEvents, "start", "describe "+moveOldID, "checks "+moveOldID)
			}
			wantEvents = append(wantEvents, "retire", "stop", "describe "+moveOldID, "tag", "launch", "describe "+moveNewID, "allocate", "associate", "checks "+moveNewID, "objects staging/host/", "restore", "records", "insync", "init")
			for _, app := range createApps {
				wantEvents = append(wantEvents, "objects staging/"+app+"/")
			}
			wantEvents = append(wantEvents, "activate")
			if tc.apex {
				wantEvents = append(wantEvents, "records", "insync")
			}
			if !reflect.DeepEqual(f.events, wantEvents) {
				t.Fatalf("events %v want %v", f.events, wantEvents)
			}
			wantChanges := []cloud.RecordChange{}
			for _, name := range space.RecordNames("staging.ikigenba.dev") {
				wantChanges = append(wantChanges, cloud.RecordChange{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: name, Type: "A", TTL: space.RecordTTL, Values: []string{moveNewIP}}})
			}
			if !reflect.DeepEqual(f.changes[0], wantChanges) || len(f.changes) != 1+boolInt(tc.apex) {
				t.Fatal(f.changes)
			}
			if tc.apex && !reflect.DeepEqual(f.changes[1], []cloud.RecordChange{{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "ikigenba.dev", Type: "A", TTL: space.RecordTTL, Values: []string{moveNewIP}}}}) {
				t.Fatal(f.changes)
			}
			var config, ops []string
			for _, c := range f.commands {
				if c.Path != "ssh" {
					continue
				}
				r := c.Args[len(c.Args)-1]
				if strings.Contains(r, "'config' 'set'") {
					config = append(config, r)
				}
				if strings.Contains(r, "'opsctl'") || strings.Contains(r, "opsctl/bin/opsctl'") {
					ops = append(ops, r)
				}
				if strings.Contains(r, "'config' 'del'") || strings.Contains(r, "'curl'") || strings.Contains(r, "'bash'") {
					t.Fatal(r)
				}
			}
			program := "'sudo' '" + release.Opsctl(createSHA) + "'"
			var five []string
			for _, key := range []string{"host.name=staging.ikigenba.dev", "dns.provider=route53", "dns.zones=ikigenba.dev:" + moveZone, "aws.region=us-east-2", "backup.s3_uri=s3://ikigenba.dev/staging/"} {
				five = append(five, program+" 'config' 'set' '"+key+"'")
			}
			if !reflect.DeepEqual(config, append(append([]string{}, five...), five...)) {
				t.Fatal(config)
			}
			wantOps := append([]string{"'sudo' 'opsctl' 'retire'"}, five...)
			wantOps = append(wantOps, program+" 'host' 'restore'")
			wantOps = append(wantOps, five...)
			wantOps = append(wantOps, program+" 'init'")
			for _, app := range createApps {
				if app != tc.absentApp {
					wantOps = append(wantOps, program+" 'restore' '"+app+"'")
				}
			}
			wantOps = append(wantOps, program+" 'activate' '"+createSHA+"' 'release-87'")
			if !reflect.DeepEqual(ops, wantOps) {
				t.Fatalf("ops %v want %v", ops, wantOps)
			}
		})
	}
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestMoveUsageBeforeAnyExternalAccess(t *testing.T) {
	// R-K9YG-ZGJI R-KB6D-D8A7 R-KCE9-R00W R-KDM6-4RRL R-VK1M-0CZK
	for _, tc := range []struct {
		args    []string
		message string
		help    bool
	}{
		{args: nil, message: "space move needs <space>"}, {args: []string{"sbx1", "sbx2"}, message: "space move takes only <space>"},
		{args: []string{"sbx1", "--acme-email", "ops@example.test"}, message: "unknown option '--acme-email'"},
		{args: []string{"sbx1", "--release"}, message: "option '--release' requires a value"},
		{args: []string{"sbx1", "--release="}, message: "option '--release' requires a value"},
		{args: []string{"--release"}, message: "option '--release' requires a value"},
		{args: []string{"sbx1", "--release", "-x"}, message: "option '--release' requires a value"},
		{args: []string{"--help"}, help: true}, {args: []string{"-h"}, help: true}, {args: []string{"sbx1", "--release", "--help"}, help: true},
	} {
		external := 0
		deps := noExternalDeps(&external)
		deps.Dir = t.TempDir()
		got := invokeWithDeps(deps, append([]string{"space", "move"}, tc.args...)...)
		if tc.help {
			assertResult(t, got, 0, expectedMoveUsage, "")
		} else {
			assertResult(t, got, 2, "", "devctl: "+tc.message+"\n\nsee 'devctl space --help' for usage\n")
		}
		if external != 0 {
			t.Fatal(external)
		}
	}
}

func TestMoveReleaseAndPreflightFailures(t *testing.T) {
	// R-K9YG-ZGJI R-KEU2-IJIA R-KG1Y-WB8Z R-KH9V-A2ZO R-KIHR-NUQD
	for _, tc := range []struct {
		name    string
		setup   func(*moveFake)
		args    []string
		message string
		code    int
	}{
		{name: "old tags", setup: func(f *moveFake) { f.tags = "r1\nr2\nr3\nr4\nr5\nr6\nr7\n" }, message: "no release-<N> tag in this checkout; name one with --release <sha|tag>", code: 2},
		{name: "branch", args: []string{"staging", "--release", "main"}, message: "'main' is not a commit", code: 2},
		{name: "invalid space", args: []string{"crm.sbx1"}, message: "'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'", code: 2},
		{name: "zone", setup: func(f *moveFake) {
			f.fail = "zone"
			f.cause = &cloud.NotFoundError{Kind: "hosted zone", Name: "ikigenba.dev"}
		}, message: "no hosted zone 'ikigenba.dev'", code: 1},
		{name: "template", setup: func(f *moveFake) {
			f.fail = "template"
			f.cause = &cloud.NotFoundError{Kind: "launch template", Name: "ikigenba.dev"}
		}, message: "no launch template 'ikigenba.dev'", code: 1},
		{name: "boundary", setup: func(f *moveFake) {
			f.fail = "boundary"
			f.cause = &cloud.NotFoundError{Kind: "permissions boundary", Name: "ikigenba.dev"}
		}, message: "no permissions boundary 'ikigenba.dev'", code: 1},
		{name: "only retired", setup: func(f *moveFake) { f.instances[0].Retired = moveTime }, message: "no space at 'staging.ikigenba.dev'", code: 1},
		{name: "duplicate", setup: func(f *moveFake) {
			f.instances = append(f.instances, cloud.Instance{ID: "i-0f1e2d3c4b5a69788", Space: "staging.ikigenba.dev", State: cloud.StateRunning})
		}, message: "'staging.ikigenba.dev' has more than one active instance (" + moveOldID + ", i-0f1e2d3c4b5a69788)", code: 2},
		{name: "stopping", setup: func(f *moveFake) { f.instances[0].State = cloud.StateStopping }, message: "'staging.ikigenba.dev' is stopping", code: 1},
		{name: "pending", setup: func(f *moveFake) { f.instances[0].State = cloud.StatePending }, message: "'staging.ikigenba.dev' is pending", code: 1},
		{name: "whitespace", setup: func(f *moveFake) { f.tmp = filepath.Join(f.tmp, "a b") }, code: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMoveFake(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			args := tc.args
			if args == nil {
				args = []string{"staging"}
			}
			got := f.run(args...)
			message := tc.message
			if tc.name == "whitespace" {
				message = "temporary directory '" + f.tmp + "' holds whitespace"
			}
			assertResult(t, got, tc.code, "", "devctl: "+message+"\n")
			if len(f.commands) != 0 {
				t.Fatal(f.commands)
			}
			for _, event := range f.events {
				if event == "start" || event == "stop" || event == "tag" || event == "launch" {
					t.Fatal(f.events)
				}
			}
			if tc.name == "old tags" || tc.name == "branch" || tc.name == "invalid space" {
				for _, event := range f.events {
					if event == "open" {
						t.Fatal("connected before local validation")
					}
				}
			}
			if tc.name != "whitespace" {
				f.clean()
			}
		})
	}
	// Last release wins, including an explicitly named legacy tag; a SHA has no label.
	f := newMoveFake(t)
	f.sha = "9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c"
	got := f.run("--release", "r1", "staging", "--release", "9e1c7a3")
	if got.code != 0 || got.stderr != "" || f.resolve != "9e1c7a3^{commit}" || !strings.Contains(got.stdout, "build: ok (dist/"+f.sha+".tar.xz)\n") {
		t.Fatalf("%#v %s", got, f.resolve)
	}
	for _, event := range f.events {
		if event == "tags" {
			t.Fatal("explicit release listed tags")
		}
	}
	f.clean()
}

func TestMoveStopsAtFailureAndKeepsCompletedSteps(t *testing.T) {
	// R-VQ53-X7P1 R-KM5G-T5YG R-KNDD-6XP5 R-KOL9-KPFU R-KPT5-YH6J
	// R-KS8Y-Q0NX R-KTGV-3SEM R-KX4K-93MP R-KYCG-MVDE R-KZKD-0N43
	// R-L0S9-EEUS R-L205-S6LH R-L4FY-JQ2V R-KIHR-NUQD R-L5NU-XHTK
	for _, stage := range []string{"retire", "stop", "describe " + moveOldID, "tag", "launch", "allocate", "associate", "checks " + moveNewID, "restore", "records", "init", "activate"} {
		t.Run(stage, func(t *testing.T) {
			f := newMoveFake(t)
			f.fail = stage
			if stage == "launch" {
				f.cause = &cloud.Error{Service: "ec2", Operation: "RunInstances", Code: "InsufficientInstanceCapacity"}
			}
			got := f.run("staging")
			if got.code != 1 || got.stderr == "" {
				t.Fatal(got)
			}
			f.clean()
			full := expectedMove(false, false, false, "")
			markers := map[string]string{
				"retire": "retire: ok", "stop": "stop: ok", "describe " + moveOldID: "stop: ok",
				"tag": "tag: ok", "launch": "instance: ok", "allocate": "address: ok", "associate": "address: ok",
				"checks " + moveNewID: "host: ok", "restore": "restore: ok (host/", "records": "records: ok",
				"init": "init: ok", "activate": moveActivation,
			}
			want, _, found := strings.Cut(full, markers[stage])
			if !found {
				t.Fatal("missing step marker", stage)
			}
			if got.stdout != want {
				t.Fatalf("completed output %q want %q", got.stdout, want)
			}
			if f.events[len(f.events)-1] != stage {
				t.Fatal("continued after failure", f.events)
			}
			if stage == "retire" {
				assertResult(t, got, 1, moveInitial(), "devctl: retire: ssh ec2-user@"+moveOldIP+" sudo opsctl retire: exit status 1\n\n> opsctl: backup failed\n")
			}
			if stage == "launch" {
				assertResult(t, got, 1, moveInitial()+"retire: ok (opsctl retire)\nstop: ok ("+moveOldID+" stopped)\ntag: ok ("+moveOldID+" and elastic ip "+moveOldIP+" Retired="+moveTime+")\n", "devctl: ec2 RunInstances: InsufficientInstanceCapacity\n")
			}
			if stage == "activate" && (!strings.HasPrefix(got.stderr, "devctl: activate: ssh ec2-user@"+moveNewIP+" sudo "+release.Opsctl(createSHA)+" activate "+createSHA+" release-87: exit status 1\n") || strings.Contains(got.stdout, "apex: ok")) {
				t.Fatal(got)
			}
		})
	}
	f := newMoveFake(t)
	f.backup = false
	got := f.run("staging")
	if got.code != 1 || !strings.HasSuffix(got.stdout, "opsctl: ok (5 keys set)\n") || got.stderr != "devctl: restore: no host backup under s3://ikigenba.dev/staging/host/\n" || f.events[len(f.events)-1] != "objects staging/host/" {
		t.Fatal(got, f.events)
	}
	last := f.commands[len(f.commands)-1]
	if last.Args[len(last.Args)-1] != "'sudo' '"+release.Opsctl(createSHA)+"' 'config' 'set' 'backup.s3_uri=s3://ikigenba.dev/staging/'" {
		t.Fatal("process after empty host listing", last)
	}
	f.clean()
	f = newMoveFake(t)
	f.compileApp = "dashboard"
	f.h.addAppWithSecrets("dashboard")
	got = f.run("staging")
	want := strings.SplitAfter(moveInitial(), "build: ok")[0]
	want = strings.TrimSuffix(want, "build: ok")
	assertResult(t, got, 1, want, "devctl: build dashboard: exit status 1\n\n> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\n> cmd/dashboard/main.go:41:2: undefined: render\n")
	if len(f.commands) != 0 || f.events[len(f.events)-1] != "apex" {
		t.Fatal(f.commands, f.events)
	}
	f.clean()
}

func TestMoveTypedErrorsAndWrappedTemporaryFailure(t *testing.T) {
	// R-K7IO-7X24 R-KYCG-MVDE R-KIHR-NUQD R-KH9V-A2ZO
	_ = spacemove.NoHostBackupError(struct{ Bucket, Prefix string }{})
	e := &spacemove.NoHostBackupError{Bucket: "ikigenba.dev", Prefix: "sbx1/host/"}
	if e.Error() != "restore: no host backup under s3://ikigenba.dev/sbx1/host/" || e.ExitCode() != 1 {
		t.Fatal(e)
	}
	f := newMoveFake(t)
	f.tmp = filepath.Join(f.tmp, "absent")
	var out bytes.Buffer
	err := spacemove.Run(f.ctx, []string{"staging"}, "test-version", &out, f.deps())
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || out.Len() != 0 || len(f.commands) != 0 {
		t.Fatalf("%v %s", err, out.String())
	}
	f = newMoveFake(t)
	f.instances[0].State = cloud.StatePending
	err = spacemove.Run(f.ctx, []string{"staging"}, "test-version", &out, f.deps())
	var stateErr *space.NotRunningError
	if !errors.As(err, &stateErr) || stateErr.Domain != "staging.ikigenba.dev" || stateErr.State != cloud.StatePending {
		t.Fatal(err)
	}
	f = newMoveFake(t)
	f.backup = false
	err = spacemove.Run(f.ctx, []string{"staging"}, "test-version", &out, f.deps())
	var backupErr *spacemove.NoHostBackupError
	if !errors.As(err, &backupErr) || backupErr.Bucket != "ikigenba.dev" || backupErr.Prefix != "staging/host/" {
		t.Fatal(err)
	}
	f.clean()

}

func TestMoveCloudFailuresReturnUnchanged(t *testing.T) {
	// R-KEU2-IJIA R-KG1Y-WB8Z R-KKXK-FE7R R-KNDD-6XP5 R-KOL9-KPFU
	// R-KPT5-YH6J R-KS8Y-Q0NX R-KTGV-3SEM R-KX4K-93MP R-KZKD-0N43 R-L0S9-EEUS
	for _, stage := range []string{"open", "sts", "zone", "template", "boundary", "instances", "addresses", "apex", "start", "describe " + moveOldID, "checks " + moveOldID, "stop", "tag", "launch", "describe " + moveNewID, "allocate", "associate", "checks " + moveNewID, "objects staging/host/", "records", "insync", "objects staging/auth/", "objects staging/sites/"} {
		t.Run(stage, func(t *testing.T) {
			f := newMoveFake(t)
			f.fail = stage
			if stage == "start" || stage == "checks "+moveOldID {
				f.instances[0].State, f.oldState = cloud.StateStopped, cloud.StateStopped
			}
			var out bytes.Buffer
			err := spacemove.Run(f.ctx, []string{"staging"}, "test-version", &out, f.deps())
			if reflect.ValueOf(err) != reflect.ValueOf(f.cause) || f.events[len(f.events)-1] != stage {
				t.Fatalf("error %v events %v", err, f.events)
			}
			f.clean()
		})
	}
}

func TestMoveRemoteFailuresOmitFailedStep(t *testing.T) {
	// R-KTGV-3SEM R-KVWN-VBW0 R-KX4K-93MP R-L0S9-EEUS R-L4FY-JQ2V R-KIHR-NUQD
	for _, tc := range []struct {
		name, remote, marker string
		restored             bool
	}{
		{name: "cloud init", remote: "'cloud-init' 'status' '--wait'", marker: "host: ok"},
		{name: "configure launch", remote: "'config' 'set' 'dns.provider=route53'", marker: "opsctl: ok"},
		{name: "configure restored", remote: "'config' 'set' 'dns.provider=route53'", marker: "restore: ok (host/", restored: true},
		{name: "app restore", remote: "'restore' 'sites'", marker: "restore: ok (opsctl restore sites)", restored: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMoveFake(t)
			f.failRemote, f.failRestored = tc.remote, tc.restored
			got := f.run("staging")
			full := expectedMove(false, false, false, "")
			want, _, found := strings.Cut(full, tc.marker)
			if !found {
				t.Fatal("missing step marker", tc.marker)
			}
			last := f.commands[len(f.commands)-1]
			if got.code != 1 || !strings.Contains(got.stderr, "> remote failure\n") || got.stdout != want || !strings.Contains(last.Args[len(last.Args)-1], tc.remote) {
				t.Fatal(got, last)
			}
			f.clean()
		})
	}
}
