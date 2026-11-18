package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/daemonapi"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/web"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type simClock struct {
	elapsed atomic.Int64
}

func (clock *simClock) advance(d time.Duration) { clock.elapsed.Add(int64(d)) }

func (clock *simClock) now() time.Time {
	return time.Date(2026, 10, 1, 3, 4, 5, 0, time.UTC).Add(time.Duration(clock.elapsed.Load()))
}

func sandboxCloud(t *testing.T, clock *simClock) (*memory.Cloud, cloud.Instance) {
	t.Helper()
	hosts := settings.SandboxHosts
	c, err := memory.New(memory.Config{
		Path: filepath.Join(t.TempDir(), "sandbox-cloud.json"),
		Now:  clock.now, Rand: rand.NewChaCha8([32]byte{13}), Hosts: hosts,
	})
	must(t, err)
	return c, launchSandboxHost(t, c, "host01")
}

func launchSandboxHost(t *testing.T, c *memory.Cloud, name string) cloud.Instance {
	t.Helper()
	hosts := settings.SandboxHosts
	i, err := c.RunInstance(t.Context(), cloud.Launch{
		Template: hosts.LaunchTemplate, InstanceProfile: hosts.InstanceProfile, Type: "t4g.medium", RootGB: 20,
		Tags: cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagHost: name},
	})
	must(t, err)
	return i
}

func watchTimer(t *testing.T, watchers chan<- chan time.Time) func(time.Duration) <-chan time.Time {
	t.Helper()
	return func(d time.Duration) <-chan time.Time {
		if d != WatchInterval {
			t.Errorf("watch delay = %v, want %v", d, WatchInterval)
		}
		tick := make(chan time.Time, 1)
		watchers <- tick
		return tick
	}
}

func TestWatchAndRestartLoop(t *testing.T) {
	// R-QARJ-4MFY R-VB9N-YMHD
	const (
		_ int64  = MaxBanFileBytes
		_ uint64 = MaxBanFileBytes
	)
	if WatchInterval != 2*time.Second || ApplyDelay != 3*time.Second || MaxBanFileBytes != 64<<10 {
		t.Fatalf("WatchInterval = %v, ApplyDelay = %v, MaxBanFileBytes = %d", WatchInterval, ApplyDelay, MaxBanFileBytes)
	}
	clock := &simClock{}
	c, i := sandboxCloud(t, clock)
	started, canceled := make(chan string, 8), make(chan string, 8)
	watches := make(chan chan time.Time, 8)
	expectedBinding := settings.SandboxBinding
	verify := hostauth.NewVerifier(hostauth.VerifierConfig{
		Region: settings.SandboxHosts.Region, Role: settings.SandboxHosts.InstanceProfile,
		Now: clock.now, STS: c, Binding: func() (string, error) { return expectedBinding, nil },
	})
	var served atomic.Uint32
	binary := []byte{0x7f, 'E', 'L', 'F', 1, 2, 3}
	digest, err := protocol.Digest(bytes.NewReader(binary))
	must(t, err)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != expectedBinding {
			t.Errorf("Host = %s, want %s", r.Host, expectedBinding)
		}
		if r.URL.Path == protocol.BinaryPath {
			_, err := w.Write(binary)
			if err != nil {
				t.Error(err)
			}
			return
		}
		id, err := verify.Verify(r.Context(), r.Header.Get(hostauth.HeaderName))
		if err != nil {
			t.Error(err)
		}
		if r.URL.Path != protocol.StatePath || r.URL.Query().Get(protocol.QueryDigest) != "sandbox-digest" {
			t.Errorf("first state request = %s", r.URL)
		}
		started <- id
		if served.Add(1) == 1 {
			// A successful update makes the real daemon return. Later looks must relaunch it.
			if err := json.NewEncoder(w).Encode(protocol.DesiredState{Protocol: protocol.Version, Host: "host01", Version: 1, Daemon: digest}); err != nil {
				t.Error(err)
			}
			return
		}
		<-r.Context().Done()
		canceled <- id
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := New(Config{c, h, expectedBinding, func() string { return "sandbox-digest" }, clock.now,
		func(ctx context.Context, _ time.Duration) { <-ctx.Done() }, watchTimer(t, watches), "",
	})
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	tick := await(t, watches)
	select {
	case id := <-started:
		t.Fatalf("pending instance %s got a daemon", id)
	default:
	}
	clock.advance(memory.BootDelay)
	tick <- clock.now()
	if id := await(t, started); id != i.ID {
		t.Fatalf("daemon instance = %s, want %s", id, i.ID)
	}
	clock.advance(time.Second)
	tick = driveUntilStarted(t, clock, watches, started, i.ID)
	// Keep the original loop blocked on state while a further look runs.
	tick <- clock.now()
	tick = await(t, watches)
	if n := served.Load(); n != 2 {
		t.Fatalf("look started a duplicate loop; %d state requests", n)
	}
	must(t, c.StopInstance(t.Context(), i.ID))
	clock.advance(memory.StopDelay)
	tick <- clock.now()
	if id := await(t, canceled); id != i.ID {
		t.Fatalf("canceled daemon = %s, want %s", id, i.ID)
	}
	must(t, c.StartInstance(t.Context(), i.ID))
	clock.advance(memory.BootDelay)
	driveUntilStarted(t, clock, watches, started, i.ID)
	cancel()
	must(t, await(t, done))
}

func driveUntilStarted(t *testing.T, clock *simClock, watches <-chan chan time.Time, started <-chan string, wantID string) chan time.Time {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case id := <-started:
			if id != wantID {
				t.Fatalf("daemon instance = %s, want %s", id, wantID)
			}
			return await(t, watches)
		case tick := <-watches:
			tick <- clock.now()
		case <-timer.C:
			t.Fatal("running instance did not start a daemon")
		}
	}
}

func TestNoStartWhenCanceledAndWaitForLoops(t *testing.T) {
	// R-VDPG-Q5YR
	clock := &simClock{}
	c, _ := sandboxCloud(t, clock)
	clock.advance(memory.BootDelay)
	watches := make(chan chan time.Time, 4)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	s := New(Config{Cloud: c, Handler: http.NotFoundHandler(), Binding: settings.SandboxBinding,
		Now: clock.now, After: watchTimer(t, watches),
		Digest: func() string {
			entered <- struct{}{}
			<-release
			return ""
		},
	})
	defer close(release)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	must(t, s.Run(ctx))
	select {
	case <-entered:
		t.Fatal("canceled Run started a loop")
	default:
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	await(t, entered)
	cancel()
	select {
	case <-done:
		t.Fatal("Run returned while a loop was still blocked")
	default:
	}
	release <- struct{}{}
	must(t, await(t, done))
}

func TestDesiredChangeReachesApplied(t *testing.T) {
	// R-VEXD-3XPG and the real-loop wiring of R-VB9N-YMHD.
	t.Setenv(services.Variable, "")
	ctx := t.Context()
	clock := &simClock{}
	c, instance := sandboxCloud(t, clock)
	otherInstance := launchSandboxHost(t, c, "host02")
	clock.advance(memory.BootDelay)
	hosts := settings.SandboxHosts
	dir := t.TempDir()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(dir, "accounts.db"), Migrations: accounts.Migrations(), Now: clock.now})
	must(t, err)
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: clock.now, Rand: rand.NewChaCha8([32]byte{26})})
	caller := identity.Caller{UserID: "owner"}
	recordHost := func(i cloud.Instance) store.Host {
		v, err := c.CreateVolume(ctx, cloud.VolumeSpec{AZ: i.AZ, SizeGB: 50, Tags: cloud.Tags{cloud.TagKind: cloud.KindHostData}})
		must(t, err)
		h, op, err := st.CreateHost(ctx, store.HostSpec{Type: i.Type, RootGB: 20, DataGB: 50}, caller)
		must(t, err)
		must(t, st.Finish(ctx, op.ID, store.Update{InstanceID: i.ID, RootVolume: i.RootVolume, DataVolume: v.ID, AZ: i.AZ}))
		h, err = st.Host(ctx, h.Name)
		must(t, err)
		return h
	}
	host, other := recordHost(instance), recordHost(otherInstance)
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "accounts", Sink: capture, Now: clock.now,
		Rand: rand.NewChaCha8([32]byte{48}), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {},
	})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	binding := settings.SandboxBinding
	verifier := hostauth.NewVerifier(hostauth.VerifierConfig{
		Region: hosts.Region, Role: hosts.InstanceProfile, STS: c, Now: clock.now,
		Binding: func() (string, error) { return binding, nil },
	})
	polls := make(chan chan time.Time, 4)
	binary := filepath.Join(dir, "accountsd")
	must(t, os.WriteFile(binary, []byte("sandbox daemon"), 0600))
	ds, err := daemonapi.New(ctx, daemonapi.Config{
		Store: st, Verifier: verifier, Binary: binary, Telemetry: w, Now: clock.now, Configured: true,
		After: func(d time.Duration) <-chan time.Time {
			if d != protocol.LongPoll {
				t.Errorf("long poll duration = %v", d)
			}
			tick := make(chan time.Time, 1)
			polls <- tick
			return tick
		},
	})
	must(t, err)
	t.Cleanup(ds.Stop)
	cfg, err := settings.Read(func(key string) (string, bool) { return "", key == "IKIGENBA_SANDBOX" })
	must(t, err)
	handler := web.Handler(web.Config{
		Banner: func(page.User) page.Banner { return page.Banner{Service: "accounts"} },
		MCP:    mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: w}),
		Store:  st, Cloud: c, Hosts: hosts, Configured: true, Wake: func() {},
		Daemon: ds, Telemetry: w, Settings: cfg,
	})
	reports := make(chan struct{}, 4)
	observed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
		switch r.URL.Path {
		case protocol.BinaryPath:
			t.Error("simulator tried to update its served daemon")
		case protocol.ReportPath:
			reports <- struct{}{}
		}
	})
	readReport := func(name string) store.Report {
		r, err := st.Report(ctx, name)
		must(t, err)
		if r == nil {
			t.Fatalf("host %s has no report", name)
		}
		return *r
	}
	type pause struct {
		proceed chan struct{}
	}
	applies := make(chan pause, 4)
	watches := make(chan chan time.Time, 4)
	banFile := filepath.Join(dir, "drop")
	const initialBan = "192.0.2.76"
	must(t, os.WriteFile(banFile, []byte(host.Name+" "+initialBan+"\n"), 0600))
	s := New(Config{Cloud: c, Handler: observed, Binding: binding, Digest: ds.Digest, Now: clock.now, After: watchTimer(t, watches), BanFile: banFile,
		Sleep: func(ctx context.Context, d time.Duration) {
			if d != ApplyDelay {
				clock.advance(d)
				return
			}
			p := pause{proceed: make(chan struct{})}
			applies <- p
			select {
			case <-ctx.Done():
			case <-p.proceed:
			}
		},
	})
	run, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- s.Run(run) }()
	defer func() {
		cancel()
		must(t, await(t, done))
	}()
	finishApply := func(p pause) time.Time {
		clock.advance(ApplyDelay)
		appliedAt := clock.now()
		close(p.proceed)
		return appliedAt
	}
	// R-IV91-69U5. Collect both initial reports after the first look has run.
	watch := await(t, watches)
	assertNoBanFile(t, banFile)
	finishApply(await(t, applies))
	await(t, reports)
	firstPoll := await(t, polls)
	firstTargetReport, err := st.Report(ctx, host.Name)
	must(t, err)
	finishApply(await(t, applies))
	await(t, reports)
	secondPoll := await(t, polls)
	otherPoll := firstPoll
	if firstTargetReport != nil {
		otherPoll = secondPoll
	}
	initial := readReport(host.Name).Body
	if len(initial.Bans) != 1 || initial.Bans[0].Address != initialBan {
		t.Fatalf("first report for %s has bans %+v, want %s", host.Name, initial.Bans, initialBan)
	}
	if r := readReport(other.Name).Body; r.InstanceID != otherInstance.ID || len(r.Bans) != 0 {
		t.Fatalf("first report for %s has instance %s and bans %+v, want instance %s and no bans", other.Name, r.InstanceID, r.Bans, otherInstance.ID)
	}
	if initial.Applied != host.Version || initial.InstanceID != instance.ID ||
		initial.Type != instance.Type || initial.AMI != memory.AMI || initial.OS != machine.MemoryOS ||
		initial.DaemonVersion != ds.Digest() {
		t.Fatalf("initial report = %+v", initial)
	}
	account, _, err := st.PlaceAccount(ctx, "team", host.Name, caller)
	must(t, err)
	nextApply := await(t, applies)
	pending, err := st.Host(ctx, host.Name)
	must(t, err)
	if pending.Applied != host.Version || pending.Version != account.Version || pending.Applied == pending.Version {
		t.Fatalf("host should show pending during apply delay: %+v", pending)
	}
	finishApply(nextApply)
	await(t, reports)
	final := readReport(host.Name).Body
	synced, err := st.Host(ctx, host.Name)
	must(t, err)
	if synced.Applied != account.Version || final.Applied != account.Version {
		t.Fatalf("applied host = %d, report = %d, expected %d", synced.Applied, final.Applied, account.Version)
	}

	await(t, polls)
	nextLook := func() {
		watch <- clock.now()
		watch = await(t, watches)
	}
	nextReport := func(name string, timer *chan time.Time) protocol.Report {
		*timer <- clock.now()
		appliedAt := finishApply(await(t, applies))
		await(t, reports)
		*timer = await(t, polls)
		r := readReport(name)
		if !r.Received.Equal(appliedAt) {
			t.Fatalf("host %s last reported at %v, want report at %v", name, r.Received, appliedAt)
		}
		return r.Body
	}

	// R-IV91-69U5
	const allowed = "198.51.100.0/24"
	const banned = "192.0.2.77"
	clock.advance(machine.MemoryBanTime)
	_, err = st.Allow(ctx, host.Name, allowed)
	must(t, err)
	finishApply(await(t, applies))
	await(t, reports)
	before := readReport(host.Name).Body
	poll := await(t, polls)
	if !slices.Equal(before.Allowlist, []string{allowed}) || len(before.Bans) != 0 {
		t.Fatalf("before the drop, allowlist = %v, bans = %+v", before.Allowlist, before.Bans)
	}
	must(t, os.WriteFile(banFile, []byte(host.Name+" "+banned+"\n"), 0600))
	expires := clock.now().Add(machine.MemoryBanTime)
	nextLook()
	assertNoBanFile(t, banFile)
	if r := nextReport(host.Name, &poll); len(r.Bans) != 1 || r.Bans[0].Address != banned {
		t.Fatalf("first report after the drop has bans %+v, want %s", r.Bans, banned)
	}
	if r := nextReport(other.Name, &otherPoll); len(r.Bans) != 0 {
		t.Fatalf("first report for %s after the drop for %s has bans %+v", other.Name, host.Name, r.Bans)
	}
	clock.advance(expires.Add(-3 * ApplyDelay).Sub(clock.now()))
	if r := nextReport(host.Name, &poll); len(r.Bans) != 1 || r.Bans[0].Address != banned {
		t.Fatalf("report before expiry has bans %+v, want %s", r.Bans, banned)
	}
	clock.advance(expires.Add(-ApplyDelay).Sub(clock.now()))
	if r := nextReport(host.Name, &poll); len(r.Bans) != 0 {
		t.Fatalf("report at expiry has bans %+v", r.Bans)
	}
	const covered = "198.51.100.77"
	must(t, os.WriteFile(banFile, []byte(host.Name+" "+covered+"\n"), 0600))
	nextLook()
	assertNoBanFile(t, banFile)
	if r := nextReport(host.Name, &poll); len(r.Bans) != 0 {
		t.Fatalf("drop for allowlisted address %s caused bans %+v", covered, r.Bans)
	}

	// R-IYWQ-BL28
	for range 2 {
		nextLook()
		assertNoBanFile(t, banFile)
	}
	must(t, os.Mkdir(banFile, 0700))
	nextLook()
	info, err := os.Lstat(banFile)
	if err != nil || !info.IsDir() {
		t.Fatalf("look changed the directory at its drop path: %v, %v", info, err)
	}
	for name, timer := range map[string]*chan time.Time{host.Name: &poll, other.Name: &otherPoll} {
		if r := nextReport(name, timer); len(r.Bans) != 0 {
			t.Fatalf("directory caused bans for %s: %+v", name, r.Bans)
		}
	}
	must(t, os.Remove(banFile))
	lines := "host01\nhost01 192.0.2.77 x\nnohost 192.0.2.78\nhost01 192.0.2.300\nhost01 \t::ffff:192.0.2.79\n"
	const clipped = "host01 192.0.2.8"
	padding := strings.Repeat("x", MaxBanFileBytes-len(lines)-len(clipped)-1)
	must(t, os.WriteFile(banFile, []byte(lines+padding+"\n"+clipped+"0\n"), 0600))
	nextLook()
	assertNoBanFile(t, banFile)
	if r := nextReport(host.Name, &poll); len(r.Bans) != 1 || r.Bans[0].Address != "192.0.2.79" {
		t.Fatalf("report after reading the drop file = %+v", r.Bans)
	}
	if r := nextReport(other.Name, &otherPoll); len(r.Bans) != 0 {
		t.Fatalf("mixed drop file caused bans for %s: %+v", other.Name, r.Bans)
	}
	const finalLine = "host01 192.0.2.90"
	padding = strings.Repeat("x", MaxBanFileBytes-len(finalLine)-1)
	must(t, os.WriteFile(banFile, []byte(padding+"\n"+finalLine), 0600))
	nextLook()
	assertNoBanFile(t, banFile)
	if r := nextReport(host.Name, &poll); len(r.Bans) != 2 || !slices.ContainsFunc(r.Bans, func(b protocol.Ban) bool { return b.Address == "192.0.2.90" }) {
		t.Fatalf("report after a complete final line at the byte cap = %+v", r.Bans)
	}
}

func assertNoBanFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected drop path to be absent: %v", err)
	}
}
