package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spacecreate"
	"github.com/ikigenba/ikigenba/devctl/internal/spacereplace"
)

var _ func(context.Context, []string, string, io.Writer, seam.Deps) error = spacereplace.Run

const replaceOldID = "i-0a1b2c3d4e5f60718"
const replaceNewID = "i-0e5d4c3b2a1f09876"
const replaceOldIP = "18.220.10.5"
const replaceNewIP = "3.141.27.86"
const replaceLaunchIP = "3.22.140.61"
const replaceZone = "Z09565073GHK8BYWQ1A78"
const replaceActivation = "opaque one\nopaque two\nopaque three\nopaque four\nopaque five\nopaque six\nopaque seven\nopaque eight\n"
const expectedReplaceUsage = `Usage: devctl space replace <space> [--release <sha|tag>] [--no-backup]

Put the space on a new instance at its address: build the release, have the
current host take its final backups with opsctl retire, disassociate the
space's Elastic IP, terminate the instance, launch a new one and associate
the same Elastic IP with it, wait until its name resolves, copy and unpack
the release on the host, restore the newest host backup, run init, restore
each app's data, and activate the release. A space with no Elastic IP is
first given one, and its records pointed at it, before opsctl retire;
otherwise the records are not touched, and the apex never is. Completed
steps remain on failure; replace again, with --no-backup when the space's
instance is a half-built host.

Options:
  --release <sha|tag>     the release to deploy; the newest release-<N> tag otherwise
  --no-backup             skip the final backup; restore from the newest backups already kept
`

// Unimplemented cloud operations panic through the embedded interfaces. Replace
// has no permission to use those operations, including any SSM operation.
type replaceFake struct {
	cloud.EC2
	cloud.IAM
	cloud.Route53
	cloud.S3
	t                  *testing.T
	h                  *commandHarness
	ctx                context.Context
	tmp                string
	tags, sha, resolve string
	instances          []cloud.Instance
	addresses          []cloud.Address
	apex               bool
	backup             bool
	onlyAccounts       bool
	hostObjects        []cloud.Object
	allCommands        []seam.Cmd
	oldState           cloud.InstanceState
	events             []string
	commands           []seam.Cmd
	prefixes           []string
	changes            [][]cloud.RecordChange
	restoreDone        bool
	knownLaunch        string
	fail               string
	forbidOld          bool
	legacyResolved     bool
	resolveBad         bool
	unreachable        bool
	domain             string
	oldID              string
	newID              string
	failRemote         string
	failRestored       bool
	cause              error
	compileApp         string
	version            string
}

func newReplaceFake(t *testing.T) *replaceFake {
	h := newCommandHarness(t)
	for _, app := range createApps {
		h.addAppWithSecrets(app)
	}
	return &replaceFake{t: t, h: h, ctx: t.Context(), tmp: t.TempDir(), tags: "release-86\nrelease-87\nrelease-88-rc1\n", sha: createSHA, backup: true, oldState: cloud.StateRunning, domain: "staging.ikigenba.dev", oldID: replaceOldID, newID: replaceNewID,
		instances: []cloud.Instance{{ID: replaceOldID, Space: "staging.ikigenba.dev", State: cloud.StateRunning, Address: replaceOldIP}},
		addresses: []cloud.Address{{AllocationID: "eipalloc-0123456789abcdef0", IP: replaceOldIP, AssociationID: "old-association", InstanceID: replaceOldID, Space: "staging.ikigenba.dev"}}, cause: errors.New("replace sentinel")}
}
func (f *replaceFake) call(ctx context.Context, name string) error {
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
func (f *replaceFake) deps() seam.Deps {
	return seam.Deps{Dir: f.h.checkoutRoot, EUID: 1, Getenv: func(key string) string {
		if key == "TMPDIR" {
			return f.tmp
		}
		return ""
	}, Now: func() time.Time { return time.Date(2026, 11, 8, 18, 30, 0, 0, time.UTC) }, After: immediateD07After, LookupHost: func(domain string) ([]string, error) {
		if domain != f.domain {
			f.t.Fatal(domain)
		}
		if f.resolveBad {
			return []string{"203.0.113.8"}, nil
		}
		if len(f.addresses) == 0 {
			if !slices.Contains(f.events, "insync") {
				f.t.Fatal("resolved before records synchronized", f.events)
			}
			f.legacyResolved = true
			return []string{replaceNewIP}, nil
		}
		return []string{f.addresses[0].IP}, nil
	},
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
			_, err := io.WriteString(w, replaceActivation)
			return seam.Result{}, err
		}}
}
func (f *replaceFake) checkTransport(c seam.Cmd) {
	f.t.Helper()
	f.commands = append(f.commands, c)
	if c.Dir != f.h.checkoutRoot {
		f.t.Fatal(c)
	}
	joined := strings.Join(c.Args, " ")
	if !strings.Contains(joined, "ec2-user@"+f.domain) {
		f.t.Fatal("host must be reached by name", c)
	}
	for _, ip := range []string{replaceOldIP, replaceLaunchIP, replaceNewIP} {
		if strings.Contains(joined, ip) {
			f.t.Fatal("address in transport", c)
		}
	}
	known := ""
	for _, arg := range c.Args {
		if strings.HasPrefix(arg, "UserKnownHostsFile=") {
			known = strings.TrimPrefix(arg, "UserKnownHostsFile=")
		}
	}
	beforeHost := !slices.Contains(f.events, "launch")
	if beforeHost && f.forbidOld {
		f.t.Fatal("contacted old host despite no backup", c)
	}
	if beforeHost && len(f.addresses) == 0 && !f.legacyResolved {
		f.t.Fatal("contacted legacy host before resolution", c)
	}
	if beforeHost || f.restoreDone {
		if known != "" {
			f.t.Fatal("unexpected known hosts override", c)
		}
		return
	}
	if known == "" || filepath.Base(known) != "known_hosts.launch" || filepath.Dir(filepath.Dir(known)) != f.tmp || !strings.HasPrefix(filepath.Base(filepath.Dir(known)), "replace.") {
		f.t.Fatal("launch key isolation", c)
	}
	f.knownLaunch = known
	if err := os.WriteFile(known, []byte("arbitrary trusted key\n"), 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *replaceFake) exec(ctx context.Context, c seam.Cmd) (seam.Result, error) {
	f.allCommands = append(f.allCommands, c)
	if c.Path == "secret-tool" {
		f.t.Fatal("replace accessed secrets")
	}
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
	if strings.Contains(remote, "'opsctl'") && remote != "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'opsctl' 'retire'" {
		f.t.Fatal("unreleased opsctl", c)
	}
	if f.unreachable && slices.Contains(f.events, "launch") {
		return seam.Result{ExitCode: 255}, nil
	}
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
	case remote == "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'opsctl' 'retire'":
		stage = "retire"
	case strings.HasSuffix(remote, "'host' 'backup'"):
		stage = "backup"
	case strings.HasSuffix(remote, "'host' 'restore'"):
		stage = "restore"
	case strings.HasSuffix(remote, "'init'"):
		stage = "init"
	}
	if stage != "" {
		if err := f.call(ctx, stage); err != nil {
			return seam.Result{ExitCode: 1, Stdout: []byte("backup report one\nbackup report two\n")}, nil
		}
		if stage == "restore" {
			f.restoreDone = true
		}
	}
	return seam.Result{Stdout: []byte("opaque captured output\n")}, nil
}
func (f *replaceFake) CallerAccountID(ctx context.Context) (string, error) {
	return "295229566359", f.call(ctx, "sts")
}
func (f *replaceFake) Zone(ctx context.Context, root string) (cloud.Zone, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return cloud.Zone{ID: replaceZone, Name: root}, f.call(ctx, "zone")
}
func (f *replaceFake) LaunchTemplate(ctx context.Context, root string) (string, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return "lt-current", f.call(ctx, "template")
}
func (f *replaceFake) PermissionsBoundary(ctx context.Context, root string) (string, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return "boundary", f.call(ctx, "boundary")
}
func (f *replaceFake) ListSpaceInstances(ctx context.Context, root string) ([]cloud.Instance, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return f.instances, f.call(ctx, "instances")
}
func (f *replaceFake) ListSpaceAddresses(ctx context.Context, root string) ([]cloud.Address, error) {
	if root != "ikigenba.dev" {
		f.t.Fatal(root)
	}
	return f.addresses, f.call(ctx, "addresses")
}
func (f *replaceFake) StartInstance(ctx context.Context, id string) error {
	if id != f.oldID {
		f.t.Fatal(id)
	}
	f.oldState = cloud.StateRunning
	return f.call(ctx, "start")
}
func (f *replaceFake) TerminateInstance(ctx context.Context, id string) error {
	if id != f.oldID {
		f.t.Fatal(id)
	}
	if err := f.call(ctx, "terminate"); err != nil {
		return err
	}
	f.oldState = cloud.StateTerminated
	return nil
}
func (f *replaceFake) DisassociateAddress(ctx context.Context, id string) error {
	if id != f.addresses[0].AssociationID {
		f.t.Fatal(id)
	}
	return f.call(ctx, "disassociate")
}
func (f *replaceFake) DescribeInstance(ctx context.Context, id string) (cloud.Instance, error) {
	state, address := cloud.StateRunning, replaceLaunchIP
	if id == f.oldID {
		state, address = f.oldState, replaceOldIP
	} else if id != f.newID {
		f.t.Fatal(id)
	}
	return cloud.Instance{ID: id, State: state, Address: address}, f.call(ctx, "describe "+id)
}
func (f *replaceFake) InstanceChecksPassed(ctx context.Context, id string) (bool, error) {
	return true, f.call(ctx, "checks "+id)
}
func (f *replaceFake) RunInstance(ctx context.Context, spec cloud.LaunchSpec) (cloud.Instance, error) {
	for _, old := range f.instances {
		if old.State != cloud.StateTerminated && f.oldState != cloud.StateTerminated {
			f.t.Fatal("two active instances", f.events)
		}
	}
	if spec != (cloud.LaunchSpec{LaunchTemplateID: "lt-current", InstanceProfile: f.domain, Domain: "ikigenba.dev", Space: f.domain}) {
		f.t.Fatal(spec)
	}
	return cloud.Instance{ID: f.newID}, f.call(ctx, "launch")
}
func (f *replaceFake) AllocateAddress(ctx context.Context, root, domain string) (cloud.Address, error) {
	if root != "ikigenba.dev" || domain != f.domain {
		f.t.Fatal(root, domain)
	}
	return cloud.Address{AllocationID: "new-allocation", IP: replaceNewIP}, f.call(ctx, "allocate")
}
func (f *replaceFake) AssociateAddress(ctx context.Context, allocation, id string) error {
	want := "new-allocation"
	if len(f.addresses) > 0 {
		want = f.addresses[0].AllocationID
	}
	if allocation != want || (id != f.newID && id != f.oldID) {
		f.t.Fatal(allocation, id)
	}
	return f.call(ctx, "associate "+id)
}
func (f *replaceFake) ListObjects(ctx context.Context, bucket, prefix string, visit func([]cloud.Object) bool) error {
	if bucket != "ikigenba.dev" {
		f.t.Fatal(bucket)
	}
	f.prefixes = append(f.prefixes, prefix)
	if err := f.call(ctx, "objects "+prefix); err != nil {
		return err
	}
	if strings.HasSuffix(prefix, "/host/") && f.hostObjects != nil {
		visit(f.hostObjects)
		return nil
	}
	if !f.backup {
		return nil
	}
	objects := []cloud.Object{{Key: prefix + "2026-11-07T18:29:41Z.tar.zst", Modified: time.Unix(1, 0)}, {Key: prefix + "2026-11-08T18:29:41Z.tar.zst", Modified: time.Unix(2, 0)}}
	if strings.HasSuffix(prefix, "/host/") {
		accounts := cloud.Object{Key: prefix + "2026-11-08T18:29:41.222933531Z.accounts.tar.zst", Modified: time.Unix(3, 0)}
		if f.onlyAccounts {
			visit([]cloud.Object{accounts})
			return nil
		}
		objects = []cloud.Object{
			{Key: prefix + "2026-11-08T18:29:41.222933531Z.tar.zst", Modified: time.Unix(1, 0)},
			{Key: prefix + "2026-11-07T18:29:41.74850415Z.tar.zst", Modified: time.Unix(2, 0)},
			{Key: prefix + "2026-11-08T18:29:41.2229335Z.tar.zst", Modified: time.Unix(2, 0)},
			accounts,
		}
	}
	visit(objects[:1])
	visit(objects[1:])
	return nil
}
func (f *replaceFake) ChangeRecords(ctx context.Context, zone string, changes []cloud.RecordChange) (string, error) {
	if f.restoreDone || zone != replaceZone {
		f.t.Fatal("DNS changed before restore", zone)
	}
	f.changes = append(f.changes, changes)
	return "change", f.call(ctx, "records")
}
func (f *replaceFake) ChangeStatus(ctx context.Context, id string) (cloud.ChangeStatus, error) {
	if id != "change" {
		f.t.Fatal(id)
	}
	return cloud.ChangeInsync, f.call(ctx, "insync")
}
func (f *replaceFake) run(args ...string) runResult {
	var out, diagnostic bytes.Buffer
	code := Run(f.ctx, append([]string{"space", "replace"}, args...), strings.NewReader(""), &out, &diagnostic, f.deps())
	return runResult{code: code, stdout: out.String(), stderr: diagnostic.String()}
}
func (f *replaceFake) clean() {
	f.t.Helper()
	entries, err := os.ReadDir(f.tmp)
	if err != nil || len(entries) != 0 {
		f.t.Fatalf("temporary directory retained: %v %v", entries, err)
	}
}

func replaceInitial() string {
	return "account: ok (ikigenba.dev, us-east-2, 295229566359)\ndomain: ok (zone ikigenba.dev " + replaceZone + ")\nbuild: ok (release-87, dist/" + createSHA + ".tar.xz)\n"
}
func expectedReplace(stopped, noBackup, noAddress, gone bool) string {
	s := replaceInitial()
	if stopped && !noBackup {
		s += "start: ok (" + replaceOldID + " running, status checks passed)\n"
	}
	address := replaceOldIP
	if noAddress {
		address = replaceNewIP
		s += "address: ok (elastic ip " + address + " associated)\nrecords: ok (created staging.ikigenba.dev, *.staging.ikigenba.dev -> " + address + ", INSYNC)\nresolve: ok (staging.ikigenba.dev -> " + address + ")\n"
	}
	switch {
	case gone:
		s += "retire: ok (already gone)\n"
	case noBackup:
		s += "retire: skipped (--no-backup)\n"
	default:
		s += "retire: ok (opsctl retire)\n"
	}
	if !noAddress {
		if gone {
			s += "address: ok (not associated)\n"
		} else {
			s += "address: ok (elastic ip " + address + " disassociated from " + replaceOldID + ")\n"
		}
	}
	s += "instance: ok ("
	if !gone {
		s += replaceOldID + " terminated, "
	}
	s += replaceNewID + " running, " + replaceLaunchIP + ")\naddress: ok (elastic ip " + address + " associated)\nresolve: ok (staging.ikigenba.dev -> " + address + ")\nhost: ok (status checks passed, cloud-init done)\ncopy: ok (" + createSHA + ".tar.xz -> staging.ikigenba.dev)\nunpack: ok (" + release.Folder(createSHA) + ")\nopsctl: ok (5 keys set)\nrestore: ok (host/2026-11-08T18:29:41.222933531Z.tar.zst, 5 keys set again)\ninit: ok\n"
	for _, app := range createApps {
		s += "restore: ok (opsctl restore " + app + ")\n"
	}
	return s + replaceActivation + "staging.ikigenba.dev " + replaceNewID + " " + address + "\n"
}

func TestReplaceSuccessfulFlows(t *testing.T) {
	// R-3EJQ-KEIX R-2MPN-0OSX R-NG9R-FKNH R-NHHN-TCE6 R-NIPK-744V R-NJXG-KVVK
	// R-NMD9-CFCY R-NOT2-3YUC R-OW9L-JELI R-2YWM-UE7V R-304J-85YK R-31CF-LXP9
	// R-33S8-DH6N R-3504-R8XC R-NXCC-SD17 R-3681-50O1 R-NZS5-JWIL R-NQ0Y-HQL1
	// R-39VQ-ABW4 R-4DWT-NR12 R-4F4Q-1IRR R-4HKI-T295
	for _, tc := range []struct {
		name                                     string
		stopped, noBackup, noAddress, gone, full bool
	}{
		{name: "running"}, {name: "domain", full: true}, {name: "stopped", stopped: true},
		{name: "no-backup", noBackup: true}, {name: "stopped-no-backup", stopped: true, noBackup: true},
		{name: "legacy", noAddress: true}, {name: "stopped-legacy", stopped: true, noAddress: true},
		{name: "legacy-no-backup", noBackup: true, noAddress: true}, {name: "address-only", gone: true},
		{name: "address-only-no-backup", gone: true, noBackup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReplaceFake(t)
			f.forbidOld = tc.noBackup || tc.gone
			if tc.stopped {
				f.instances[0].State = cloud.StateStopped
				f.oldState = cloud.StateStopped
			}
			if tc.noAddress {
				f.addresses = nil
			}
			if tc.gone {
				f.instances[0].State = cloud.StateTerminated
				f.addresses[0].AssociationID = ""
				f.addresses[0].InstanceID = ""
			}
			operand := "staging"
			if tc.full {
				operand = f.domain
			}
			args := []string{operand}
			if tc.noBackup {
				args = append(args, "--no-backup")
			}
			got := f.run(args...)
			assertResult(t, got, 0, expectedReplace(tc.stopped, tc.noBackup, tc.noAddress, tc.gone), "")
			f.clean()
			if f.version != strings.TrimSpace(invokeWithDeps(f.deps(), "--version").stdout) {
				t.Fatal("version not forwarded", f.version)
			}
			if f.knownLaunch == "" {
				t.Fatal("no launch key file")
			}
			wantPrefix := []string{"tags", "resolve", "open", "sts", "zone", "template", "boundary", "instances", "addresses"}
			if !slices.Equal(f.events[:len(wantPrefix)], wantPrefix) {
				t.Fatal("preflight order", f.events)
			}
			if !tc.gone {
				want := []string{"disassociate", "terminate", "describe " + replaceOldID, "launch"}
				if tc.noAddress {
					want = want[1:]
				}
				pos := slices.Index(f.events, want[0])
				if pos < 0 || !slices.Equal(f.events[pos:pos+len(want)], want) {
					t.Fatal("serial replacement", f.events)
				}
			} else if slices.Contains(f.events, "terminate") || slices.Contains(f.events, "disassociate") {
				t.Fatal("retired absent instance", f.events)
			}
			if tc.noBackup || tc.gone {
				if slices.Contains(f.events, "retire") || slices.Contains(f.events, "start") {
					t.Fatal(f.events)
				}
			}
			if tc.noAddress {
				if len(f.changes) != 1 || len(f.changes[0]) != 2 {
					t.Fatal(f.changes)
				}
				for i, name := range space.RecordNames(f.domain) {
					c := f.changes[0][i]
					if c.Action != cloud.ChangeUpsert || c.Record.Name != name || c.Record.Type != "A" || !slices.Equal(c.Record.Values, []string{replaceNewIP}) {
						t.Fatal(c)
					}
				}
			} else if len(f.changes) != 0 || slices.Contains(f.events, "allocate") {
				t.Fatal("changed lifetime address", f.events, f.changes)
			}
			var config []string
			for _, c := range f.commands {
				remote := c.Args[len(c.Args)-1]
				if c.Path == "ssh" && strings.Contains(remote, "'config' 'set'") {
					config = append(config, remote)
				}
				if strings.Contains(remote, "'host.apex") || strings.Contains(remote, "'acme.email") || strings.Contains(remote, "'backup.period") || strings.Contains(remote, "'curl'") || strings.Contains(remote, "'bash'") {
					t.Fatal(c)
				}
			}
			var wantConfig []string
			for range 2 {
				for _, key := range []string{"host.name=" + f.domain, "dns.provider=route53", "dns.zones=ikigenba.dev:" + replaceZone, "aws.region=us-east-2", "backup.s3_uri=s3://ikigenba.dev/staging/"} {
					wantConfig = append(wantConfig, "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' '"+release.Opsctl(f.sha)+"' 'config' 'set' '"+key+"'")
				}
			}
			if !slices.Equal(config, wantConfig) {
				t.Fatal(config, wantConfig)
			}
		})
	}
}

func TestReplaceUsageBeforeExternalAccess(t *testing.T) {
	// R-2LHQ-MX28 R-3I7F-PPR0 R-3JFC-3HHP
	for _, tc := range []struct {
		args    []string
		message string
		help    bool
	}{
		{message: "space replace needs <space>"}, {args: []string{"sbx1", "sbx2"}, message: "space replace takes only <space>"},
		{args: []string{"sbx1", "--acme-email", "ops@example.test"}, message: "unknown option '--acme-email'"},
		{args: []string{"sbx1", "--apex", "crm"}, message: "unknown option '--apex'"},
		{args: []string{"sbx1", "--no-backup=true"}, message: "unknown option '--no-backup=true'"},
		{args: []string{"sbx1", "--release"}, message: "option '--release' requires a value"},
		{args: []string{"sbx1", "--release="}, message: "option '--release' requires a value"},
		{args: []string{"--release"}, message: "option '--release' requires a value"},
		{args: []string{"sbx1", "--release", "-x"}, message: "option '--release' requires a value"},
		{args: []string{"--help"}, help: true}, {args: []string{"-h"}, help: true}, {args: []string{"sbx1", "--release", "--help"}, help: true},
	} {
		calls := 0
		deps := noExternalDeps(&calls)
		deps.Dir = t.TempDir()
		got := invokeWithDeps(deps, append([]string{"space", "replace"}, tc.args...)...)
		if tc.help {
			assertResult(t, got, 0, expectedReplaceUsage, "")
		} else {
			assertResult(t, got, 2, "", "devctl: "+tc.message+"\n\nsee 'devctl space --help' for usage\n")
		}
		if calls != 0 {
			t.Fatal(calls)
		}
	}
	var out bytes.Buffer
	err := spacereplace.Run(t.Context(), nil, "test-version", &out, seam.Deps{})
	var usage *space.UsageError
	if !errors.As(err, &usage) || usage.Message != "space replace needs <space>" || usage.Help != "devctl space --help" {
		t.Fatal(err)
	}
}

func TestReplaceReleaseResolution(t *testing.T) {
	// R-3FRM-Y69M R-3KN8-H98E
	for _, tc := range []struct {
		args                           []string
		tags, sha, ref, label, message string
	}{
		{args: []string{"staging"}, ref: "refs/tags/release-87^{commit}", label: "release-87"},
		{args: []string{"--release", "r1", "staging", "--no-backup", "--release", "9e1c7a3", "--no-backup"}, ref: "9e1c7a3^{commit}", sha: "9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c"},
		{args: []string{"staging", "--release=release-88-rc1"}, ref: "refs/tags/release-88-rc1^{commit}", label: "release-88-rc1"},
		{args: []string{"staging"}, tags: "r1\nr2\nr3\nr4\nr5\nr6\nr7\n", message: "no release-<N> tag in this checkout; name one with --release <sha|tag>"},
		{args: []string{"staging", "--release", "main"}, message: "'main' is not a commit"},
		{args: []string{"crm.sbx1"}, message: "'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'"},
	} {
		f := newReplaceFake(t)
		if tc.tags != "" {
			f.tags = tc.tags
		}
		if tc.sha != "" {
			f.sha = tc.sha
		}
		got := f.run(tc.args...)
		if tc.message != "" {
			assertResult(t, got, 2, "", "devctl: "+tc.message+"\n")
			if slices.Contains(f.events, "open") {
				t.Fatal("opened cloud", f.events)
			}
		} else {
			if got.code != 0 || got.stderr != "" || f.resolve != tc.ref {
				t.Fatal(got, f.resolve, tc.ref)
			}
			build := "build: ok ("
			if tc.label != "" {
				build += tc.label + ", "
			}
			build += "dist/" + f.sha + ".tar.xz)\n"
			if !strings.Contains(got.stdout, build) {
				t.Fatal(got.stdout, build)
			}
			if slices.Contains(tc.args, "--no-backup") && !strings.Contains(got.stdout, "retire: skipped (--no-backup)\n") {
				t.Fatal(got.stdout)
			}
		}
		f.clean()
	}
}

func TestReplaceRequiresHostBackup(t *testing.T) {
	// R-47TB-QWBL
	for _, key := range []string{"", "staging/host/2026-11-08T18:29:41.222933531Z.accounts.tar.zst"} {
		f := newReplaceFake(t)
		f.hostObjects = []cloud.Object{}
		if key != "" {
			f.hostObjects = append(f.hostObjects, cloud.Object{Key: key})
		}
		got := f.run("staging", "--no-backup")
		if got.code != 1 || !strings.HasSuffix(got.stdout, "opsctl: ok (5 keys set)\n") || got.stderr != "devctl: restore: no host backup under s3://ikigenba.dev/staging/host/\n" || f.events[len(f.events)-1] != "objects staging/host/" {
			t.Fatal(got, f.events)
		}
		for _, c := range f.commands {
			if strings.HasSuffix(c.Args[len(c.Args)-1], "'init'") {
				t.Fatal(c)
			}
		}
		f.clean()
		f = newReplaceFake(t)
		f.hostObjects = []cloud.Object{}
		var out bytes.Buffer
		err := spacereplace.Run(f.ctx, []string{"staging", "--no-backup"}, "test-version", &out, f.deps())
		var missing *spacecreate.NoHostBackupError
		if !errors.As(err, &missing) || errors.Unwrap(err) != nil || missing.Bucket != "ikigenba.dev" || missing.Prefix != "staging/host/" {
			t.Fatal(err)
		}
	}
}

func TestReplacePreflightErrorsDoNothing(t *testing.T) {
	// R-NR8U-VIBQ R-NSGR-9A2F
	for _, failure := range []string{"open", "sts", "zone", "template", "boundary", "instances", "addresses"} {
		t.Run(failure, func(t *testing.T) {
			f := newReplaceFake(t)
			f.fail = failure
			var out bytes.Buffer
			err := spacereplace.Run(f.ctx, []string{"staging"}, "test-version", &out, f.deps())
			if !errors.Is(err, f.cause) || errors.Unwrap(err) != nil || out.Len() != 0 || len(f.commands) != 0 || f.events[len(f.events)-1] != failure {
				t.Fatal(err, out.String(), f.events)
			}
			for _, c := range f.allCommands {
				if c.Path != "git" || c.Args[0] == "worktree" {
					t.Fatal("effects before checks", c)
				}
			}
			f.clean()
		})
	}
	for _, tc := range []struct {
		name                            string
		state                           cloud.InstanceState
		gone, multiple, addressMultiple bool
		code                            int
		message                         string
	}{
		{name: "gone", gone: true, code: 1, message: "no space at 'staging.ikigenba.dev'"},
		{name: "multiple instances", multiple: true, code: 2, message: "'staging.ikigenba.dev' has more than one active instance (" + replaceOldID + ", " + replaceNewID + ")"},
		{name: "multiple addresses", addressMultiple: true, code: 2, message: "'staging.ikigenba.dev' has more than one Elastic IP (18.190.33.61, 18.220.10.5)"},
		{name: "stopping", state: cloud.StateStopping, code: 1, message: "'staging.ikigenba.dev' is stopping"},
		{name: "pending", state: cloud.StatePending, code: 1, message: "'staging.ikigenba.dev' is pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReplaceFake(t)
			if tc.gone {
				f.instances = nil
				f.addresses = nil
			}
			if tc.multiple {
				f.instances = append(f.instances, cloud.Instance{ID: replaceNewID, Space: f.domain, State: cloud.StateRunning})
			}
			if tc.addressMultiple {
				f.addresses = append(f.addresses, cloud.Address{AllocationID: "other", Space: f.domain, IP: "18.190.33.61"})
			}
			if tc.state != "" {
				f.instances[0].State = tc.state
			}
			got := f.run("staging")
			assertResult(t, got, tc.code, "", "devctl: "+tc.message+"\n")
			if len(f.commands) != 0 || slices.Contains(f.events, "launch") {
				t.Fatal(f.events)
			}
			f.clean()
		})
	}
	f := newReplaceFake(t)
	f.instances[0].State = cloud.StateStopping
	got := f.run("staging", "--no-backup")
	assertResult(t, got, 0, expectedReplace(false, true, false, false), "")
}

func TestReplaceTempDirectoryLifecycle(t *testing.T) {
	// R-NTON-N1T4
	for _, failure := range []string{"", "launch", "restore"} {
		f := newReplaceFake(t)
		f.fail = failure
		got := f.run("staging")
		if (failure == "") != (got.code == 0) {
			t.Fatal(failure, got)
		}
		f.clean()
	}
	f := newReplaceFake(t)
	f.tmp = filepath.Join(t.TempDir(), "a b")
	got := f.run("staging")
	assertResult(t, got, 2, "", "devctl: temporary directory '"+f.tmp+"' holds whitespace\n")
	if _, err := os.Stat(f.tmp); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(f.commands) != 0 || slices.Contains(f.events, "launch") {
		t.Fatal(f.events)
	}
	f = newReplaceFake(t)
	f.tmp = filepath.Join(t.TempDir(), "missing", "child")
	var out bytes.Buffer
	err := spacereplace.Run(f.ctx, []string{"staging"}, "test-version", &out, f.deps())
	var path *os.PathError
	if !errors.As(err, &path) || errors.Unwrap(err) == nil || out.Len() != 0 {
		t.Fatal(err, out.String())
	}
}

func TestReplaceBuildFailurePreservesSpace(t *testing.T) {
	// R-2MPN-0OSX
	f := newReplaceFake(t)
	f.compileApp = "dashboard"
	f.h.addAppWithSecrets("dashboard")
	got := f.run("staging")
	assertResult(t, got, 1, "account: ok (ikigenba.dev, us-east-2, 295229566359)\ndomain: ok (zone ikigenba.dev "+replaceZone+")\n", "devctl: build dashboard: exit status 1\n\n> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\n> cmd/dashboard/main.go:41:2: undefined: render\n")
	if len(f.commands) != 0 || slices.Contains(f.events, "launch") || slices.Contains(f.events, "terminate") {
		t.Fatal(f.events)
	}
	f.clean()
}

func TestReplaceRetireFailurePreservesSpace(t *testing.T) {
	// R-NNL5-Q73N R-NMD9-CFCY
	f := newReplaceFake(t)
	f.fail = "retire"
	got := f.run("staging")
	assertResult(t, got, 1, replaceInitial(), "devctl: retire: ssh ec2-user@staging.ikigenba.dev sudo systemd-run --quiet --wait --pipe --collect opsctl retire: exit status 1\n\n> backup report one\n> backup report two\n")
	if len(f.commands) != 1 || f.events[len(f.events)-1] != "retire" {
		t.Fatal(f.events, f.commands)
	}
	f.clean()
}

func TestReplaceLaunchFailureLeavesAddress(t *testing.T) {
	// R-32KB-ZPFY
	f := newReplaceFake(t)
	f.fail = "launch"
	f.cause = &cloud.Error{Service: "ec2", Operation: "RunInstances", Code: "InsufficientInstanceCapacity"}
	got := f.run("staging")
	assertResult(t, got, 1, replaceInitial()+"retire: ok (opsctl retire)\naddress: ok (elastic ip "+replaceOldIP+" disassociated from "+replaceOldID+")\n", "devctl: ec2 RunInstances: InsufficientInstanceCapacity\n")
	if f.events[len(f.events)-1] != "launch" || strings.Count(strings.Join(f.events, "\n"), "terminate") != 1 {
		t.Fatal(f.events)
	}
	f.clean()
}

func TestReplaceCloudFailuresReturnUnchanged(t *testing.T) {
	// R-NG9R-FKNH R-NHHN-TCE6 R-NIPK-744V R-OW9L-JELI R-2YWM-UE7V
	// R-304J-85YK R-31CF-LXP9 R-33S8-DH6N R-NQ0Y-HQL1
	for _, tc := range []struct {
		failure               string
		stopped, legacy, gone bool
		last                  string
	}{
		{failure: "start", stopped: true, last: "build"}, {failure: "checks " + replaceOldID, stopped: true, last: "build"},
		{failure: "describe " + replaceOldID, stopped: true, last: "build"},
		{failure: "allocate", legacy: true, last: "build"}, {failure: "associate " + replaceOldID, legacy: true, last: "build"},
		{failure: "records", legacy: true, last: "address"}, {failure: "insync", legacy: true, last: "address"},
		{failure: "disassociate", last: "retire"}, {failure: "terminate", last: "address"},
		{failure: "describe " + replaceOldID, last: "address"}, {failure: "launch", last: "address"},
		{failure: "describe " + replaceNewID, last: "address"}, {failure: "associate " + replaceNewID, last: "instance"},
		{failure: "launch", gone: true, last: "address"}, {failure: "describe " + replaceNewID, gone: true, last: "address"},
	} {
		t.Run(tc.failure+tc.last, func(t *testing.T) {
			f := newReplaceFake(t)
			f.fail = tc.failure
			if tc.stopped {
				f.instances[0].State = cloud.StateStopped
				f.oldState = cloud.StateStopped
			}
			if tc.legacy {
				f.addresses = nil
			}
			if tc.gone {
				f.instances = nil
				f.addresses[0].AssociationID = ""
			}
			var out bytes.Buffer
			err := spacereplace.Run(f.ctx, []string{"staging"}, "test-version", &out, f.deps())
			if !errors.Is(err, f.cause) || errors.Unwrap(err) != nil || f.events[len(f.events)-1] != tc.failure {
				t.Fatal(err, f.events)
			}
			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			if !strings.HasPrefix(lines[len(lines)-1], tc.last+": ok (") {
				t.Fatal(out.String())
			}
			f.clean()
		})
	}
}

func TestReplaceResolverFailureStopsBeforeSSH(t *testing.T) {
	// R-3504-R8XC R-NJXG-KVVK
	for _, legacy := range []bool{false, true} {
		f := newReplaceFake(t)
		f.resolveBad = true
		if legacy {
			f.addresses = nil
		}
		got := f.run("staging")
		address := replaceOldIP
		if legacy {
			address = replaceNewIP
		}
		if got.code != 1 || got.stderr != "devctl: resolve: staging.ikigenba.dev does not resolve to "+address+"\n" {
			t.Fatal(got)
		}
		if legacy {
			if len(f.commands) != 0 || slices.Contains(f.events, "terminate") {
				t.Fatal(f.events, f.commands)
			}
			if !strings.HasSuffix(got.stdout, ", INSYNC)\n") {
				t.Fatal(got.stdout)
			}
		} else if len(f.commands) != 1 || !strings.HasSuffix(got.stdout, "address: ok (elastic ip "+address+" associated)\n") {
			t.Fatal(got, f.commands)
		}
		f.clean()
	}
}

func TestReplaceNewHostUnreachableAndRetry(t *testing.T) {
	// R-4GCM-FAIG
	f := newReplaceFake(t)
	f.unreachable = true
	got := f.run("staging")
	if got.code != 1 || got.stderr != "devctl: ssh ec2-user@staging.ikigenba.dev: connection timed out\n" || !strings.HasSuffix(got.stdout, "resolve: ok (staging.ikigenba.dev -> "+replaceOldIP+")\n") {
		t.Fatal(got)
	}
	if f.events[len(f.events)-1] != "checks "+replaceNewID {
		t.Fatal(f.events)
	}
	for _, c := range f.commands {
		if c.Path == "scp" {
			t.Fatal(c)
		}
	}
	f.clean()
	f = newReplaceFake(t)
	f.oldID = replaceNewID
	f.newID = "i-07a8b9c0d1e2f3a4b"
	f.instances[0].ID = f.oldID
	f.addresses[0].InstanceID = f.oldID
	got = f.run("staging", "--no-backup")
	want := strings.ReplaceAll(expectedReplace(false, true, false, false), replaceNewID, f.newID)
	want = strings.ReplaceAll(want, replaceOldID, f.oldID)
	assertResult(t, got, 0, want, "")
	f.clean()
}

func TestReplaceNewHostFailuresStopWithoutUndo(t *testing.T) {
	// R-NXCC-SD17 R-3681-50O1 R-NQ0Y-HQL1
	for _, failure := range []string{"checks " + replaceNewID, "objects staging/host/", "restore", "init", "objects staging/auth/", "activate"} {
		t.Run(failure, func(t *testing.T) {
			f := newReplaceFake(t)
			f.fail = failure
			got := f.run("staging")
			if got.code != 1 || got.stderr == "" || strings.HasSuffix(got.stdout, "staging.ikigenba.dev "+replaceNewID+" "+replaceOldIP+"\n") {
				t.Fatal(got)
			}
			if f.events[len(f.events)-1] != failure {
				t.Fatal(f.events)
			}
			if strings.Count(strings.Join(f.events, "\n"), "terminate") != 1 || strings.Count(strings.Join(f.events, "\n"), "disassociate") != 1 || strings.Count(strings.Join(f.events, "\n"), "launch") != 1 {
				t.Fatal("undid completed effects", f.events)
			}
			if failure == "activate" && !strings.HasPrefix(got.stderr, "devctl: activate: ssh ec2-user@staging.ikigenba.dev sudo systemd-run --quiet --wait --pipe --collect "+release.Opsctl(f.sha)+" activate "+f.sha+" release-87: exit status 1\n") {
				t.Fatal(got.stderr)
			}
			f.clean()
		})
	}
}

func TestReplaceAddressAssociationAndApexStayWithSpace(t *testing.T) {
	// R-OW9L-JELI R-4HKI-T295 R-39VQ-ABW4
	for _, foreign := range []bool{false, true} {
		f := newReplaceFake(t)
		f.domain = "sbx1.ikigenba.dev"
		f.oldID = "i-0c9e94542d98846a8"
		f.instances[0].ID = f.oldID
		f.instances[0].Space = f.domain
		f.addresses[0].Space = f.domain
		f.addresses[0].IP = "18.118.7.42"
		f.addresses[0].InstanceID = f.oldID
		// The root's A record points at this lifetime address. Any attempt to read
		// or change it panics through Route53's unimplemented methods.
		f.apex = true
		if foreign {
			f.instances = nil
			f.addresses[0].InstanceID = "i-0f1e2d3c4b5a69788"
			f.forbidOld = true
		}
		got := f.run("sbx1")
		if got.code != 0 || got.stderr != "" {
			t.Fatal(got)
		}
		if !strings.Contains(got.stdout, "address: ok (elastic ip 18.118.7.42 disassociated from "+f.addresses[0].InstanceID+")\n") || !strings.HasSuffix(got.stdout, "sbx1.ikigenba.dev "+f.newID+" 18.118.7.42\n") {
			t.Fatal(got.stdout)
		}
		if len(f.changes) != 0 {
			t.Fatal(f.changes)
		}
		for _, prefix := range []string{"apex:", "role:", "record:", "previous:"} {
			if strings.Contains(got.stdout, prefix) {
				t.Fatal(got.stdout)
			}
		}
		if foreign && (slices.Contains(f.events, "terminate") || !strings.Contains(got.stdout, "retire: ok (already gone)\n")) {
			t.Fatal(got.stdout, f.events)
		}
		f.clean()
	}
}

func (f *replaceFake) FindRecord(context.Context, string, string, string) (cloud.Record, bool, error) {
	f.t.Fatal("replace must not read the root record", f.apex)
	return cloud.Record{}, false, nil
}
