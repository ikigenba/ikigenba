package cli_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/join"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/sim"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func awsEnvironment() map[string]string {
	return map[string]string{
		"AWS_REGION": " chosen region ", "IKIGENBA_HOSTS_ZONE_ID": "zone",
		"IKIGENBA_HOSTS_DOMAIN": "managed.test", "IKIGENBA_HOSTS_LAUNCH_TEMPLATE": "template",
		"IKIGENBA_HOSTS_INSTANCE_PROFILE": "profile",
	}
}

func testMemoryCloud(t *testing.T) *memory.Cloud {
	t.Helper()
	value, err := memory.New(memory.Config{Path: filepath.Join(t.TempDir(), "cloud.json"), Now: func() time.Time { return fixedTime }, Rand: &integrationRandom{}, Hosts: settings.SandboxHosts})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type watchedCloud struct {
	cloud.Cloud
	alloc func(context.Context, cloud.Tags) (cloud.Address, error)
	find  func(context.Context, string, string) ([]cloud.Record, error)
}

func (c watchedCloud) AllocateAddress(ctx context.Context, tags cloud.Tags) (cloud.Address, error) {
	return c.alloc(ctx, tags)
}

func (c watchedCloud) FindRecords(ctx context.Context, zone, name string) ([]cloud.Record, error) {
	return c.find(ctx, zone, name)
}

// R-YXQP-862N R-EW3T-E5OX
func TestAWSRegionResourcesAndWake(t *testing.T) {
	var calls atomic.Int32
	alloc := make(chan cloud.Tags, 1)
	records := make(chan [2]string, 1)
	dir := t.TempDir()
	env := awsEnvironment()
	hosts := settings.SandboxHosts
	hosts.Domain = env["IKIGENBA_HOSTS_DOMAIN"]
	base, err := memory.New(memory.Config{
		Path: filepath.Join(dir, "cloud-fixture.json"), Now: func() time.Time { return fixedTime }, Rand: &integrationRandom{}, Hosts: hosts,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := startIntegration(t, integrationOptions{dir: dir, env: env, cloud: func(_ context.Context, region string) (cloud.Cloud, error) {
		calls.Add(1)
		if region != " chosen region " {
			return nil, fmt.Errorf("region %q", region)
		}
		if _, err := os.Stat(filepath.Join(dir, "state", "accounts.db")); err != nil {
			return nil, err
		}
		return watchedCloud{Cloud: base, alloc: func(_ context.Context, tags cloud.Tags) (cloud.Address, error) {
			alloc <- tags
			return cloud.Address{}, &cloud.Error{Code: "RequestLimitExceeded"}
		}, find: func(_ context.Context, zone, name string) ([]cloud.Record, error) {
			records <- [2]string{zone, name}
			return nil, nil
		}}, nil
	}})
	r.call("create_host", struct{}{}, nil)
	select {
	case tags := <-alloc:
		if tags[cloud.TagKind] != cloud.KindHost || tags[cloud.TagHost] != "host01" {
			t.Fatal(tags)
		}
	case <-time.After(time.Second):
		t.Fatal("operation waited on the timer")
	}
	r.call("create_account", map[string]string{"account": "acme", "host": "host01"}, nil)
	select {
	case got := <-records:
		if got != [2]string{"zone", "acme.managed.test"} {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("cloud never received DNS preflight")
	}
	if r.stop() != 0 || calls.Load() != 1 {
		t.Fatal(calls.Load(), r.err.String())
	}
}

// R-Z06H-ZPK1 R-GQTO-5Y20 R-GS1K-JPSP
func TestUnavailableCloudModesStillServe(t *testing.T) {
	for _, mode := range []string{"partial", "aws-error", "sandbox-error"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			o := integrationOptions{dir: dir}
			var calls atomic.Int32
			o.cloud = func(context.Context, string) (cloud.Cloud, error) {
				calls.Add(1)
				return nil, errors.New("no credentials\ntry again")
			}
			want := ""
			switch mode {
			case "partial":
				o.env = map[string]string{"AWS_REGION": "region"}
			case "aws-error":
				o.env = awsEnvironment()
				want = "accounts: " + cli.NoCloud + ": no credentials try again\n"
			case "sandbox-error":
				if err := os.MkdirAll(filepath.Join(dir, "state", "sandbox-cloud.json"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			r := startIntegration(t, o)
			code, _, _ := r.request("GET", "/accountsd", "", "")
			if code != 404 {
				t.Fatal(code)
			}
			for _, call := range append(append(changeCalls(validKey()), readCalls()...), backupReadCalls()...) {
				r.refuse(call.name, call.args, tools.NotConfigured)
			}
			if r.stop() != 0 || r.out.Len() != 0 {
				t.Fatal(r.err.String())
			}
			if mode == "sandbox-error" {
				if got := r.err.String(); !strings.HasPrefix(got, "accounts: "+cli.NoCloud+": ") || strings.Count(got, "\n") != 1 {
					t.Fatal(got)
				}
			} else {
				if r.err.String() != want {
					t.Fatal(r.err.String())
				}
				if _, err := os.Stat(filepath.Join(dir, "state", "sandbox-cloud.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			}
			if mode == "aws-error" && calls.Load() != 1 || mode != "aws-error" && calls.Load() != 0 {
				t.Fatal(calls.Load())
			}
		})
	}
}

// R-Z1EE-DHAQ
func TestDaemonBinaryIsResolvedAtStartup(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "accounts")
	link := filepath.Join(dir, "accountsd")
	a, b := filepath.Join(dir, "release-a"), filepath.Join(dir, "release-b")
	for name, content := range map[string]string{a: "release a", b: "release b"} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	r := startIntegration(t, integrationOptions{executable: func() (string, error) {
		calls.Add(1)
		return executable, nil
	}})
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, link); err != nil {
		t.Fatal(err)
	}
	status, _, body := r.request("GET", "/accountsd", "", "")
	if status != 200 || body != "release a" || calls.Load() != 1 || r.stop() != 0 {
		t.Fatal(status, body, calls.Load())
	}
	for _, err := range []error{nil, errors.New("no executable")} {
		r := startIntegration(t, integrationOptions{executable: func() (string, error) {
			return filepath.Join(t.TempDir(), "accounts"), err
		}})
		status, _, _ := r.request("GET", "/accountsd", "", "")
		if status != 404 || r.stop() != 0 {
			t.Fatal(status, r.err.String())
		}
	}
}

// R-H1SR-LVQ9
func TestCloudPreflightCannotHoldServerDrain(t *testing.T) {
	entered, release := make(chan context.Context, 1), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	env := awsEnvironment()
	env["DRAIN_SECONDS"] = "1"
	base := testMemoryCloud(t)
	r := startIntegration(t, integrationOptions{env: env, cloud: func(context.Context, string) (cloud.Cloud, error) {
		return watchedCloud{Cloud: base, alloc: base.AllocateAddress, find: func(ctx context.Context, _, _ string) ([]cloud.Record, error) {
			entered <- ctx
			<-release
			return nil, nil
		}}, nil
	}})
	r.call("create_host", struct{}{}, nil)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = r.callAs("create_account", map[string]string{"account": "acme", "host": "host01"}, identity.Caller{UserID: "owner"})
	}()
	var requestCtx context.Context
	select {
	case requestCtx = <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("cloud preflight did not start")
	}
	start := time.Now()
	if r.stop() != 1 || time.Since(start) > 3*time.Second || requestCtx.Err() == nil {
		t.Fatal("drain failed to end in-flight request", r.err.String())
	}
	prior := r.err.String()
	if !strings.HasSuffix(prior, "accounts: stopped with 1 request unfinished\n") {
		t.Fatal(prior)
	}
	count := len(r.capture.Events())
	releaseOnce.Do(func() { close(release) })
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request did not finish")
	}
	r.writer.Emit(t.Context(), "change.finished", nil)
	if r.err.String() != prior || r.out.Len() != 0 || len(r.capture.Events()) != count {
		t.Fatal("late telemetry or output")
	}
}

type drivenClock struct {
	current atomic.Int64
	mu      sync.Mutex
	timers  []clockTimer
}

type clockTimer struct {
	at  time.Time
	out chan time.Time
}

func newDrivenClock() *drivenClock {
	c := &drivenClock{}
	c.current.Store(fixedTime.Unix())
	return c
}

func (c *drivenClock) now() time.Time { return time.Unix(c.current.Load(), 0).UTC() }

func (c *drivenClock) after(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := clockTimer{at: c.now().Add(d), out: make(chan time.Time, 1)}
	c.timers = append(c.timers, timer)
	return timer.out
}

func (c *drivenClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Unix(c.current.Add(int64(d/time.Second)), 0).UTC()
	pending := c.timers[:0]
	for _, timer := range c.timers {
		if timer.at.After(now) {
			pending = append(pending, timer)
		} else {
			timer.out <- now
		}
	}
	c.timers = pending
}

func driveUntil(t *testing.T, c *drivenClock, check func() bool) {
	t.Helper()
	for range 120 {
		if check() {
			return
		}
		c.advance(ops.PollInterval)
	}
	t.Fatal("state never converged on the test clock")
}

func (c *drivenClock) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-c.after(d):
	}
}

// R-YYYL-LXTC R-Z2MA-R91F R-GT9G-XHJE R-GY52-GKI6 R-GZCY-UC8V
// R-AX78-VHKU (sandbox), R-VN5D-PUG1.
func TestSandboxResumesAndSynchronizesHosts(t *testing.T) {
	for _, withURL := range []bool{true, false} {
		t.Run(fmt.Sprint(withURL), func(t *testing.T) {
			dir := t.TempDir()
			path := ""
			wantURL := "http://" + settings.SandboxBinding
			if withURL {
				path = filepath.Join(t.TempDir(), "services.json")
				integrationServices(t, path, map[string]string{"accounts": ""}, "")
				wantURL = "https://accounts.test"
			}
			clock := newDrivenClock()
			var opens atomic.Int32
			dormant := integrationOptions{dir: dir, path: path, now: clock.now, cloud: func(context.Context, string) (cloud.Cloud, error) {
				opens.Add(1)
				return nil, errors.New("real cloud selected in sandbox")
			}}
			first := startIntegration(t, dormant)
			var created tools.HostChange
			first.call("create_host", struct{}{}, &created)
			if created.State != "provisioning" || first.stop() != cli.ExitSuccess {
				t.Fatal(created, first.err.String())
			}
			dormant.after = clock.after
			dormant.sleep = clock.sleep
			dormant.env = map[string]string{"DRAIN_SECONDS": "1"}
			run := startIntegration(t, dormant)
			var host tools.HostRead
			driveUntil(t, clock, func() bool {
				run.call("host", map[string]string{"host": created.Host}, &host)
				return host.State == "running" && host.Health == "sync"
			})
			wantName := "host01." + settings.SandboxHosts.Domain
			var report protocol.Report
			if err := json.Unmarshal(host.Report, &report); err != nil {
				t.Fatal(err)
			}
			if host.DNSName != wantName || report.Hostname != wantName || host.EIP == "" {
				t.Fatalf("sandbox host DNS name = %q, reported hostname = %q, EIP = %q; want %q for both names and an EIP",
					host.DNSName, report.Hostname, host.EIP, wantName)
			}
			// Wait for the simulated daemon's next poll to begin before mutating.
			var account tools.AccountRead
			run.call("create_account", map[string]string{"account": "acme", "host": created.Host}, &account)
			if account.DNSName != "acme."+settings.SandboxHosts.Domain {
				t.Fatal(account)
			}
			run.call("add_user", map[string]string{"account": "acme", "user": "alice", "key": validKey()}, nil)
			driveUntil(t, clock, func() bool {
				var users tools.UserList
				run.call("users", map[string]string{"account": "acme"}, &users)
				return len(users.Users) == 1 && !users.Users[0].Pending
			})
			if run.stop() != cli.ExitSuccess || opens.Load() != 0 {
				t.Fatal(run.err.String(), opens.Load())
			}
			assertLifecycle(t, run.capture.Events())
			saved, err := memory.New(memory.Config{
				Path: filepath.Join(dir, "state", "sandbox-cloud.json"), Now: clock.now, Rand: &integrationRandom{}, Hosts: settings.SandboxHosts,
			})
			if err != nil {
				t.Fatal(err)
			}
			records, err := saved.FindRecords(t.Context(), settings.SandboxHosts.ZoneID, wantName)
			if err != nil || len(records) != 1 || records[0].Type != "A" || !slices.Equal(records[0].Values, []string{host.EIP}) {
				t.Fatalf("sandbox DNS records at %q = %+v (%v), want one A with %q", wantName, records, err, host.EIP)
			}
			var instances []cloud.Instance
			err = saved.FindInstances(t.Context(), cloud.Tags{cloud.TagKind: cloud.KindHost}, func(page []cloud.Instance) bool {
				instances = append(instances, page...)
				return true
			})
			if err != nil || len(instances) != 1 || instances[0].ID != host.InstanceID ||
				instances[0].Tags[cloud.TagHost] != host.Host || instances[0].Tags[cloud.TagAccountsURL] != wantURL {
				t.Fatal(instances, host, err)
			}
		})
	}

	t.Run("AWS without an accounts URL", func(t *testing.T) {
		clock := newDrivenClock()
		hosts := settings.SandboxHosts
		base, err := memory.New(memory.Config{
			Path: filepath.Join(t.TempDir(), "cloud.json"), Now: clock.now, Rand: &integrationRandom{}, Hosts: hosts,
		})
		if err != nil {
			t.Fatal(err)
		}
		run := startIntegration(t, integrationOptions{env: hostEnvironment(hosts), now: clock.now, after: clock.after, cloud: func(context.Context, string) (cloud.Cloud, error) {
			return base, nil
		}})
		run.call("create_host", struct{}{}, nil)
		driveUntil(t, clock, func() bool {
			var host tools.HostRead
			run.call("host", map[string]string{"host": "host01"}, &host)
			if host.State != "failed" {
				return false
			}
			if host.Reason != ops.ReasonNoAccountsURL {
				t.Fatal(host.Reason)
			}
			return true
		})
		if run.stop() != cli.ExitSuccess {
			t.Fatal(run.err.String())
		}
	})
}

// R-R5Y2-GACO R-H8X7-UWAT
func TestSandboxListsSeededAccountFolder(t *testing.T) {
	clock := newDrivenClock()
	looks := make(chan chan time.Time, 1)
	after := func(d time.Duration) <-chan time.Time {
		if d != sim.WatchInterval {
			return clock.after(d)
		}
		timer := make(chan time.Time, 1)
		looks <- timer
		return timer
	}
	waitForLook := func() chan time.Time {
		select {
		case timer := <-looks:
			return timer
		case <-time.After(5 * time.Second):
			t.Fatal("simulated host look did not finish")
			return nil
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "sandbox-seeds")
	watch := watchSeedDirectory(t, filepath.Dir(path))
	run := startIntegration(t, integrationOptions{dir: dir, now: clock.now, after: after, sleep: clock.sleep})
	firstLook := waitForLook()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("seed listing exists before creating a host: %v", err)
	}

	const hostName, accountName = "host01", "acme"
	var created tools.HostChange
	run.call(tools.ToolCreateHost, struct{}{}, &created)
	if created.Host != hostName {
		t.Fatalf("created host = %q, want %q", created.Host, hostName)
	}
	clock.advance(sim.WatchInterval)
	firstLook <- clock.now()
	driveUntil(t, clock, func() bool {
		var host tools.HostRead
		run.call(tools.ToolHost, tools.HostArgs{Host: hostName}, &host)
		if host.Health == "sync" {
			return true
		}
		select {
		case timer := <-looks:
			timer <- clock.now()
		default:
		}
		return false
	})
	nextLook := waitForLook()
	// The simulator can remove an empty listing in its first look. Check the
	// directory events too so creation followed by removal still fails.
	assertNoSeedCreation(t, watch, filepath.Base(path))
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("seed listing exists before creating an account: %v", err)
	}

	run.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: accountName, Host: hostName}, nil)
	driveUntil(t, clock, func() bool {
		var listed tools.AccountList
		run.call(tools.ToolAccounts, tools.AccountsArgs{}, &listed)
		return len(listed.Accounts) == 1 && listed.Accounts[0].Account == accountName &&
			listed.Accounts[0].Host == hostName && listed.Accounts[0].State == store.AccountActive
	})
	// Start a separate look after the MCP response and wait until it finishes.
	clock.advance(sim.WatchInterval)
	nextLook <- clock.now()
	waitForLook()

	var files []string
	err := fs.WalkDir(accounts.Seed(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	var want strings.Builder
	for _, file := range files {
		fmt.Fprintf(&want, "%s %s file %s\n", hostName, accountName, file)
	}
	fmt.Fprintf(&want, "%s %s commit %s %s@%s.%s %s\n", hostName, accountName, accountName,
		accountName, hostName, settings.SandboxHosts.Domain, machine.SeedMessage)
	got, err := os.ReadFile(filepath.Clean(path))
	if err != nil || string(got) != want.String() {
		t.Fatalf("seed listing after the look = %q, err = %v; want %q", got, err, want.String())
	}
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
}

func watchSeedDirectory(t *testing.T, dir string) int {
	t.Helper()
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(watch) })
	if _, err := syscall.InotifyAddWatch(watch, dir, syscall.IN_CREATE|syscall.IN_MOVED_TO); err != nil {
		t.Fatal(err)
	}
	return watch
}

func assertNoSeedCreation(t *testing.T, watch int, seedName string) {
	t.Helper()
	var events [4096]byte
	for {
		n, err := syscall.Read(watch, events[:])
		if errors.Is(err, syscall.EAGAIN) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		for remaining := events[:n]; len(remaining) > 0; {
			var event syscall.InotifyEvent
			size, err := binary.Decode(remaining, binary.NativeEndian, &event)
			if err != nil || len(remaining)-size < int(event.Len) || event.Mask&syscall.IN_Q_OVERFLOW != 0 {
				t.Fatalf("invalid seed directory event: mask %x, error %v", event.Mask, err)
			}
			end := size + int(event.Len)
			if name := strings.TrimRight(string(remaining[size:end]), "\x00"); name == seedName {
				t.Fatalf("seed listing was created before any account existed: %q (event mask %x)", name, event.Mask)
			}
			remaining = remaining[end:]
		}
	}
}

// R-L5PS-7HX9
func TestSandboxDeleteHostAndData(t *testing.T) {
	clock := newDrivenClock()
	run := startIntegration(t, integrationOptions{now: clock.now, after: clock.after, sleep: clock.sleep})
	const name = "host01"
	run.call(tools.ToolCreateHost, struct{}{}, nil)
	waitForRunningHost(t, clock, run, name)
	args := tools.HostArgs{Host: name}
	run.call(tools.ToolCloseHost, args, nil)
	driveUntil(t, clock, func() bool {
		var host tools.HostRead
		run.call(tools.ToolHost, args, &host)
		return host.State == store.HostRunning && host.Closed != ""
	})
	deleteSandboxHost(t, clock, run, name, false)
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	saved, err := memory.New(memory.Config{
		Path: filepath.Join(run.dir, "state", "sandbox-cloud.json"), Now: clock.now,
		Rand: &integrationRandom{}, Hosts: settings.SandboxHosts,
	})
	if err != nil {
		t.Fatal(err)
	}
	tags := cloud.Tags{cloud.TagHost: name}
	checkNoCloudPages(t, "instances", tags, saved.FindInstances)
	checkNoCloudPages(t, "addresses", tags, saved.FindAddresses)
	checkNoCloudPages(t, "volumes", tags, saved.FindVolumes)
	records, err := saved.FindRecords(t.Context(), settings.SandboxHosts.ZoneID, name+"."+settings.SandboxHosts.Domain)
	if err != nil || len(records) != 0 {
		t.Fatalf("sandbox DNS records after teardown = %v, %v", records, err)
	}
}

func checkNoCloudPages[T any](t *testing.T, resource string, tags cloud.Tags, find func(context.Context, cloud.Tags, func([]T) bool) error) {
	t.Helper()
	err := find(t.Context(), tags, func(page []T) bool {
		t.Errorf("sandbox %s query delivered a page after teardown: %+v", resource, page)
		return false
	})
	if err != nil {
		t.Errorf("sandbox %s query failed after teardown: %v", resource, err)
	}
}

func waitForRunningHost(t *testing.T, clock *drivenClock, run *integrationRun, name string) {
	t.Helper()
	driveUntil(t, clock, func() bool {
		var host tools.HostRead
		run.call(tools.ToolHost, tools.HostArgs{Host: name}, &host)
		return host.State == store.HostRunning
	})
}

func deleteSandboxHost(t *testing.T, clock *drivenClock, run *integrationRun, name string, keep bool) tools.HostChange {
	t.Helper()
	var deletion tools.HostChange
	run.call(tools.ToolDeleteHost, tools.DeleteHostArgs{Host: name, KeepData: &keep}, &deletion)
	driveUntil(t, clock, func() bool {
		var hosts tools.HostList
		run.call(tools.ToolHosts, tools.HostsArgs{}, &hosts)
		return !slices.ContainsFunc(hosts.Hosts, func(host tools.Host) bool { return host.Host == name })
	})
	return deletion
}

func hostEnvironment(hosts settings.Hosts) map[string]string {
	return map[string]string{
		"AWS_REGION": hosts.Region, "IKIGENBA_HOSTS_ZONE_ID": hosts.ZoneID, "IKIGENBA_HOSTS_DOMAIN": hosts.Domain,
		"IKIGENBA_HOSTS_LAUNCH_TEMPLATE": hosts.LaunchTemplate, "IKIGENBA_HOSTS_INSTANCE_PROFILE": hosts.InstanceProfile,
	}
}

// R-JFIG-JA0H
func TestSandboxBanDropReachesHostReport(t *testing.T) {
	clock := newDrivenClock()
	run := startIntegration(t, integrationOptions{dir: integrationSocketDir(t), now: clock.now, after: clock.after, sleep: clock.sleep})
	var created tools.HostChange
	run.call(tools.ToolCreateHost, struct{}{}, &created)
	if created.Host != "host01" {
		t.Fatalf("new host = %q", created.Host)
	}
	hostArgs := tools.HostArgs{Host: created.Host}
	driveUntil(t, clock, func() bool {
		var host tools.HostRead
		run.call(tools.ToolHost, hostArgs, &host)
		return host.State == "running" && host.Health == "sync"
	})
	path := filepath.Join(run.dir, "state", "sandbox-bans")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("before the drop the expected path must be absent: %v", err)
	}
	const address = "192.0.2.77"
	if err := os.WriteFile(path, []byte(created.Host+" "+address+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	driveUntil(t, clock, func() bool {
		var host tools.HostRead
		run.call(tools.ToolHost, hostArgs, &host)
		var report protocol.Report
		if err := json.Unmarshal(host.Report, &report); err != nil {
			t.Fatal(err)
		}
		return len(report.Bans) == 1 && report.Bans[0].Address == address
	})
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("after the report the expected path must be absent: %v", err)
	}
	boxDir := filepath.Join(run.dir, "state", "box")
	socket := filepath.Join(boxDir, "daffy")
	if len(socket) > sim.MaxSocketPath {
		t.Fatalf("fixture socket path is too long: %q", socket)
	}
	if _, err := os.Lstat(boxDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("box path before adding a user: %v", err)
	}
	run.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "acme", Host: created.Host}, nil)
	run.call(tools.ToolAddUser, tools.UserKeyArgs{Account: "acme", User: "daffy", Key: validKey()}, nil)
	driveUntil(t, clock, func() bool {
		var users tools.UserList
		run.call(tools.ToolUsers, tools.AccountArgs{Account: "acme"}, &users)
		info, err := os.Lstat(socket)
		return len(users.Users) == 1 && !users.Users[0].Pending && err == nil && info.Mode()&os.ModeSocket != 0
	})
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
}

const (
	bodyMarker    = "GHIJKLMNGHIJKLMNGHIJKLMN"
	emailMarker   = "OPQRSTUVOPQRSTUVOPQRSTUV"
	commentMarker = "WXYZ1234WXYZ1234WXYZ1234"
)

func secrecyKey(t *testing.T, variant string) string {
	t.Helper()
	body := strings.Split(validKey(), " ")[1]
	encoded := body[:28] + bodyMarker + variant + body[52+len(variant):]
	key := "ssh-ed25519 " + encoded + " " + commentMarker
	if _, err := valid.ParseKey(key); err != nil {
		t.Fatal("invalid secrecy fixture", err)
	}
	return key
}

type secrecyRandom struct {
	integrationRandom
}

func (r *secrecyRandom) Read(p []byte) (int, error) {
	n, err := r.integrationRandom.Read(p)
	if len(p) == 32 {
		// Preserve two counter bytes to vary the token. Crockford base32
		// encodes the surrounding one bits as Z (G in the padded last digit),
		// so each eight-character run contains a character absent from hex IDs.
		for i := range p {
			if i != 16 && i != 17 {
				p[i] = 0xff
			}
		}
	}
	return n, err
}

// R-E18S-PNZ9 R-EKR6-TZUD R-IF26-P336 R-171J-ZR8S R-AL27-ZL79 R-1U30-4KIA R-EOQ0-QXWL
func TestRunSecrecy(t *testing.T) {
	key := secrecyKey(t, "")
	clock := newDrivenClock()
	run := startIntegration(t, integrationOptions{now: clock.now, after: clock.after, sleep: clock.sleep, email: emailMarker + "@fixture.test",
		random: &secrecyRandom{}})
	run.call("create_host", struct{}{}, nil)
	driveUntil(t, clock, func() bool {
		var host tools.HostRead
		run.call("host", map[string]string{"host": "host01"}, &host)
		return host.State == "running" && host.Health == "sync"
	})
	for _, call := range changeCalls(key)[1:8] {
		_, err := run.callAs(call.name, call.args, identity.Caller{UserID: "owner", Email: run.email, RequestID: call.name})
		if err != nil {
			t.Fatal(call.name, err)
		}
	}
	for _, call := range addressCalls() {
		result, err := run.callAs(call.name, call.args, identity.Caller{UserID: "owner", Email: run.email})
		if err != nil || result.IsError() {
			t.Fatal(call.name, result, err)
		}
	}
	for _, call := range readCalls() {
		_, err := run.callAs(call.name, call.args, identity.Caller{UserID: "owner", Email: run.email})
		if err != nil {
			t.Fatal(call.name, err)
		}
	}
	var admin, ordinary tools.InviteMade
	run.call(tools.ToolCreateInvite, map[string]any{"account": "acme", "admin": true}, &admin)
	run.call(tools.ToolCreateInvite, tools.AccountArgs{Account: "acme"}, &ordinary)
	var invites tools.InviteList
	run.call(tools.ToolListInvites, tools.AccountArgs{Account: "acme"}, &invites)
	if len(invites.Invites) != 2 {
		t.Fatalf("live invites = %+v", invites.Invites)
	}
	run.call(tools.ToolRevokeInvite, tools.InviteArgs{Invite: ordinary.Invite}, nil)
	path := invitePath(t, admin)
	for _, viewer := range []string{"", "owner"} {
		if status, _, _ := run.request(http.MethodGet, path, viewer, ""); status != http.StatusOK {
			t.Fatalf("join GET status = %d", status)
		}
	}
	if status, _ := run.enroll(path, "owner", "", "daffy", key); status != http.StatusOK {
		t.Fatalf("join POST status = %d", status)
	}
	for _, admin := range []bool{true, false} {
		run.call(tools.ToolSetAdmin, tools.SetAdminArgs{Account: "acme", User: "alice", Admin: admin}, nil)
	}
	for _, user := range []string{"", "owner"} {
		for _, path := range []string{"/", "/hosts/host01", "/tools", "/about"} {
			run.request("GET", path, user, "")
		}
	}
	deletion, kept := runSecretVolumeDelete(t, clock, run)
	backupChanges := runSecretBackupChanges(t, clock, run)
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	var allowObserved, enrollmentObserved bool
	events := run.capture.Events()
	assertTrailSecrecy(t, events, admin, ordinary)
	for _, event := range events {
		if event.Name == "change.finished" && event.Attrs["tool"] == tools.ToolAllowIP &&
			event.Attrs["host"] == "host01" && event.Attrs["ip"] == "192.0.2.0/24" {
			allowObserved = true
		}
		if event.Name == "invite.used" && event.Attrs["invite"] == admin.Invite && event.Attrs["user"] == "daffy" {
			enrollmentObserved = true
		}
	}
	if !allowObserved {
		t.Fatal("change.finished for the allowlist addition was missing")
	}
	if !enrollmentObserved {
		t.Fatal("the join POST did not record enrollment through the admin invite")
	}
	assertSecretVolumeEvents(t, events, deletion, kept)
	assertSecretBackupEvents(t, events, backupChanges)
}

func runSecretBackupChanges(t *testing.T, clock *drivenClock, run *integrationRun) map[string]string {
	t.Helper()
	const host = "host01"
	hostArgs := tools.HostArgs{Host: host}
	run.call(tools.ToolBackupStatus, tools.BackupStatusArgs{}, nil)
	run.call(tools.ToolBackupStatus, tools.BackupStatusArgs{Host: &hostArgs.Host}, nil)
	run.call(tools.ToolRestorePoints, tools.AccountArgs{Account: "acme"}, nil)
	operations := make(map[string]string)
	for _, step := range []struct {
		tool  string
		args  any
		ready func(tools.HostRead) bool
	}{
		{tools.ToolCloseHost, hostArgs, func(h tools.HostRead) bool { return h.Closed != "" }},
		{tools.ToolOpenHost, hostArgs, func(h tools.HostRead) bool { return h.State == store.HostRunning && h.Closed == "" }},
		{tools.ToolReplaceHost, tools.ReplaceHostArgs{Host: host, From: store.ReplaceVolume}, func(h tools.HostRead) bool {
			return h.State == store.HostRunning && h.Replace == ""
		}},
	} {
		var started tools.HostChange
		run.call(step.tool, step.args, &started)
		if started.Operation == "" {
			t.Fatalf("%s did not start an operation", step.tool)
		}
		operations[step.tool] = started.Operation
		driveUntil(t, clock, func() bool {
			var current tools.HostRead
			run.call(tools.ToolHost, hostArgs, &current)
			return step.ready(current)
		})
	}
	run.call(tools.ToolDeleteAccountBackups, tools.AccountArgs{Account: "absent"}, nil)
	return operations
}

func assertSecretBackupEvents(t *testing.T, events []telemetry.Event, operations map[string]string) {
	t.Helper()
	for tool, operation := range operations {
		for _, name := range []string{trail.EventOperationStarted, trail.EventOperationFinished, trail.EventHostChanged} {
			if !slices.ContainsFunc(events, func(e telemetry.Event) bool {
				return e.Name == name && e.Attrs["operation"] == operation
			}) {
				t.Errorf("%s did not exercise %s for operation %s", tool, name, operation)
			}
		}
	}
	for _, tool := range []string{tools.ToolCloseHost, tools.ToolOpenHost, tools.ToolReplaceHost, tools.ToolDeleteAccountBackups} {
		if !slices.ContainsFunc(events, func(e telemetry.Event) bool {
			return e.Name == trail.EventChangeFinished && e.Attrs["tool"] == tool && e.User == "owner"
		}) {
			t.Errorf("%s did not exercise %s", tool, trail.EventChangeFinished)
		}
	}
}

func runSecretVolumeDelete(t *testing.T, clock *drivenClock, run *integrationRun) (tools.HostChange, tools.KeptVolume) {
	t.Helper()
	var created tools.HostChange
	run.call(tools.ToolCreateHost, struct{}{}, &created)
	waitForRunningHost(t, clock, run, created.Host)
	deletion := deleteSandboxHost(t, clock, run, created.Host, true)
	var kept tools.KeptVolumeList
	run.call(tools.ToolKeptVolumes, tools.KeptVolumesArgs{}, &kept)
	index := slices.IndexFunc(kept.KeptVolumes, func(volume tools.KeptVolume) bool { return volume.Host == created.Host })
	if index == -1 {
		t.Fatalf("no kept volume for %s: %+v", created.Host, kept.KeptVolumes)
	}
	volume := kept.KeptVolumes[index]
	run.call(tools.ToolDeleteKeptVolume, tools.KeptVolumeArgs{Volume: volume.Volume}, nil)
	driveUntil(t, clock, func() bool {
		var current tools.KeptVolumeList
		run.call(tools.ToolKeptVolumes, tools.KeptVolumesArgs{}, &current)
		return !slices.ContainsFunc(current.KeptVolumes, func(entry tools.KeptVolume) bool { return entry.Volume == volume.Volume })
	})
	return deletion, volume
}

func assertSecretVolumeEvents(t *testing.T, events []telemetry.Event, deletion tools.HostChange, kept tools.KeptVolume) {
	t.Helper()
	found := map[string]bool{}
	for _, e := range events {
		switch e.Name {
		case "operation.started", "operation.finished":
			if e.Attrs["operation"] == deletion.Operation && e.Attrs["data"] == store.OnDeleteKeep {
				found[e.Name] = true
			}
		case "change.finished":
			if e.Attrs["tool"] == tools.ToolDeleteKeptVolume && e.Attrs["host"] == kept.Host {
				found[e.Name] = true
			}
		case "volume.changed":
			if e.Attrs["volume"] == kept.Volume {
				if to, ok := e.Attrs["to"].(string); ok {
					found[e.Name+" "+to] = true
				}
			}
		}
	}
	for _, name := range []string{"operation.started", "operation.finished", "change.finished", "volume.changed deleting", "volume.changed deleted"} {
		if !found[name] {
			t.Errorf("kept volume teardown did not exercise %s", name)
		}
	}
}

func assertTrailSecrecy(t *testing.T, events []telemetry.Event, invites ...tools.InviteMade) {
	t.Helper()
	forbidden := []string{strings.ToLower(bodyMarker), strings.ToLower(emailMarker), strings.ToLower(commentMarker)}
	var ids []string
	for _, invite := range invites {
		ids = append(ids, strings.ToLower(invite.Invite))
	}
	requests := make(map[string]bool)
	allowed := eventNames()
	for _, event := range events {
		if !slices.Contains(allowed, event.Name) {
			t.Fatal("unknown event", event.Name)
		}
		if event.Name == "request.started" {
			if event.RequestID == "" || requests[event.RequestID] {
				t.Fatalf("fixture request ids are not unique: %q", event.RequestID)
			}
			requests[event.RequestID] = true
			ids = append(ids, strings.ToLower(event.RequestID))
		}
	}
	for _, invite := range invites {
		link, err := url.Parse(invite.Link)
		if err != nil {
			t.Fatal("invalid invite link in secrecy fixture")
		}
		query := link.Query()
		token := query.Get(join.TokenParam)
		if link.Path != join.Path || len(query[join.TokenParam]) != 1 || !store.ValidToken(token) {
			t.Fatal("missing or invalid invite token in secrecy fixture")
		}
		token = strings.ToLower(token)
		for start := 0; start+8 <= len(token); start++ {
			fragment := token[start : start+8]
			for _, id := range ids {
				if strings.Contains(id, fragment) {
					t.Fatal("fixture token overlaps an invite or request id")
				}
			}
			forbidden = append(forbidden, fragment)
		}
	}
	for i, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		fields := []string{event.Time.String(), event.Service, event.Name, event.RequestID, event.User, string(data)}
		for key, value := range event.Attrs {
			fields = append(fields, key, fmt.Sprint(value))
		}
		for _, field := range fields {
			folded := strings.ToLower(field)
			for _, secret := range forbidden {
				if strings.Contains(folded, secret) {
					t.Fatalf("secret appeared in captured event %d", i)
				}
			}
		}
	}
}

// R-KQMY-Y1KI
func TestRunWithoutCloudLeavesOperationUnchanged(t *testing.T) {
	dir := t.TempDir()
	sandbox := startIntegration(t, integrationOptions{dir: dir})
	var created tools.HostChange
	sandbox.call("create_host", struct{}{}, &created)
	if sandbox.stop() != cli.ExitSuccess {
		t.Fatal(sandbox.err.String())
	}
	before, err := reopenRunStore(t, dir).Operation(t.Context(), created.Operation)
	if err != nil || before.Status != "running" || created.State != "provisioning" {
		t.Fatal(created, before, err)
	}
	clock := newDrivenClock()
	clock.advance(time.Hour)
	run := startIntegration(t, integrationOptions{dir: dir, noSandbox: true, now: clock.now, after: clock.after})
	run.request("GET", "/", "owner", "")
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	reopened := reopenRunStore(t, dir)
	host, err := reopened.Host(t.Context(), created.Host)
	if err != nil || host.State != "provisioning" {
		t.Fatal(host, err)
	}
	after, err := reopened.Operation(t.Context(), created.Operation)
	if err != nil || after != before {
		t.Fatal(before, after, err)
	}
	for _, event := range run.capture.Events() {
		if event.Name == "host.changed" || event.Name == "operation.finished" {
			t.Fatal(event)
		}
	}
}

// R-KRUV-BTB7
func TestCloudOperationCannotHoldServerDrain(t *testing.T) {
	entered, release, released := make(chan context.Context, 1), make(chan struct{}), make(chan struct{})
	releaseCall := sync.OnceFunc(func() { close(release) })
	defer releaseCall()
	env := awsEnvironment()
	env["DRAIN_SECONDS"] = "1"
	base := testMemoryCloud(t)
	run := startIntegration(t, integrationOptions{env: env, cloud: func(context.Context, string) (cloud.Cloud, error) {
		return watchedCloud{Cloud: base, find: base.FindRecords, alloc: func(ctx context.Context, _ cloud.Tags) (cloud.Address, error) {
			defer close(released)
			entered <- ctx
			<-release
			return cloud.Address{}, ctx.Err()
		}}, nil
	}})
	var created tools.HostChange
	run.call("create_host", struct{}{}, &created)
	var cloudCtx context.Context
	select {
	case cloudCtx = <-entered:
	case <-time.After(time.Second):
		t.Fatal("cloud allocation did not start")
	}
	start := time.Now()
	run.cancel(errors.New("integration stop"))
	if cloudCtx.Err() == nil {
		t.Fatal("engine cloud context remained active after cancellation")
	}
	if run.stop() != cli.ExitSuccess || time.Since(start) > 3*time.Second {
		t.Fatal("cloud call held the drain", run.err.String())
	}
	host, err := reopenRunStore(t, run.dir).Host(t.Context(), created.Host)
	if err != nil || host.State != "provisioning" {
		t.Fatal(host, err)
	}
	select {
	case <-released:
		t.Fatal("fake returned before the test released it")
	default:
	}
	releaseCall()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("cloud call did not return after release")
	}
}

func reopenRunStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	now := func() time.Time { return fixedTime }
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(dir, "state", "accounts.db"), Migrations: accounts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return store.New(d, store.Config{Now: now, Rand: &integrationRandom{}})
}
