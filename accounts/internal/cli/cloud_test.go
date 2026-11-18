package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
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
	r := startIntegration(t, integrationOptions{dir: dir, env: awsEnvironment(), cloud: func(_ context.Context, region string) (cloud.Cloud, error) {
		calls.Add(1)
		if region != " chosen region " {
			return nil, fmt.Errorf("region %q", region)
		}
		if _, err := os.Stat(filepath.Join(dir, "state", "accounts.db")); err != nil {
			return nil, err
		}
		return watchedCloud{Cloud: testMemoryCloud(t), alloc: func(_ context.Context, tags cloud.Tags) (cloud.Address, error) {
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
			for _, call := range append(changeCalls(validKey()), readCalls()...) {
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
		base := testMemoryCloud(t)
		run := startIntegration(t, integrationOptions{env: awsEnvironment(), now: clock.now, after: clock.after, cloud: func(context.Context, string) (cloud.Cloud, error) {
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

// R-E18S-PNZ9 R-EKR6-TZUD
func TestRunSecrecy(t *testing.T) {
	const bodyMarker = "GHIJKLMNGHIJKLMNGHIJKLMN"
	const emailMarker = "OPQRSTUVOPQRSTUVOPQRSTUV"
	const commentMarker = "WXYZ1234WXYZ1234WXYZ1234"
	body := strings.Split(validKey(), " ")[1]
	encoded := body[:28] + bodyMarker + body[52:]
	key := "ssh-ed25519 " + encoded + " " + commentMarker
	if _, err := valid.ParseKey(key); err != nil {
		t.Fatal("invalid secrecy fixture", err)
	}
	clock := newDrivenClock()
	run := startIntegration(t, integrationOptions{now: clock.now, after: clock.after, sleep: clock.sleep, email: emailMarker + "@fixture.test"})
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
	for _, call := range readCalls() {
		_, err := run.callAs(call.name, call.args, identity.Caller{UserID: "owner", Email: run.email})
		if err != nil {
			t.Fatal(call.name, err)
		}
	}
	for _, user := range []string{"", "owner"} {
		for _, path := range []string{"/", "/hosts/host01", "/tools", "/about"} {
			run.request("GET", path, user, "")
		}
	}
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	for _, event := range run.capture.Events() {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{bodyMarker, emailMarker, commentMarker} {
			if strings.Contains(strings.ToLower(string(data)), strings.ToLower(marker)) {
				t.Fatalf("marker in event %s", event.Name)
			}
		}
	}
}

func TestCloudOperationCannotHoldServerDrain(t *testing.T) {
	entered, release, released := make(chan context.Context, 1), make(chan struct{}), make(chan struct{})
	defer close(release)
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
	run.call("create_host", struct{}{}, nil)
	var cloudCtx context.Context
	select {
	case cloudCtx = <-entered:
	case <-time.After(time.Second):
		t.Fatal("cloud allocation did not start")
	}
	start := time.Now()
	if run.stop() != cli.ExitSuccess || time.Since(start) > 3*time.Second || cloudCtx.Err() == nil {
		t.Fatal("cloud call held the drain", run.err.String())
	}
	if run.err.Len() != 0 {
		t.Fatal(run.err.String())
	}
	assertLifecycle(t, run.capture.Events())
	select {
	case <-released:
		t.Fatal("fake returned before the test released it")
	default:
	}
}
