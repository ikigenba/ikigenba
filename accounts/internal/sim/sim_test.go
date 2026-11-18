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
	i, err := c.RunInstance(t.Context(), cloud.Launch{
		Template: hosts.LaunchTemplate, InstanceProfile: hosts.InstanceProfile, Type: "t4g.medium", RootGB: 20,
		Tags: cloud.Tags{cloud.TagKind: cloud.KindHost},
	})
	must(t, err)
	return c, i
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
	// R-V8TV-72ZZ R-VB9N-YMHD
	if WatchInterval != 2*time.Second || ApplyDelay != 3*time.Second {
		t.Fatalf("WatchInterval = %v, ApplyDelay = %v", WatchInterval, ApplyDelay)
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
	s := New(Config{Cloud: c, Handler: h, Binding: expectedBinding, Digest: func() string { return "sandbox-digest" },
		Now: clock.now, After: watchTimer(t, watches),
		Sleep: func(ctx context.Context, _ time.Duration) { <-ctx.Done() },
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
	clock.advance(memory.BootDelay)
	hosts := settings.SandboxHosts
	v, err := c.CreateVolume(ctx, cloud.VolumeSpec{AZ: instance.AZ, SizeGB: 50, Tags: cloud.Tags{cloud.TagKind: cloud.KindHostData}})
	must(t, err)
	dir := t.TempDir()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(dir, "accounts.db"), Migrations: accounts.Migrations(), Now: clock.now})
	must(t, err)
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: clock.now, Rand: rand.NewChaCha8([32]byte{26})})
	caller := identity.Caller{UserID: "owner"}
	host, op, err := st.CreateHost(ctx, store.HostSpec{Type: instance.Type, RootGB: 20, DataGB: 50}, caller)
	must(t, err)
	must(t, st.Finish(ctx, op.ID, store.Update{InstanceID: instance.ID, RootVolume: instance.RootVolume, DataVolume: v.ID, AZ: instance.AZ}))
	host, err = st.Host(ctx, host.Name)
	must(t, err)
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
	polls := make(chan struct{}, 4)
	binary := filepath.Join(dir, "accountsd")
	must(t, os.WriteFile(binary, []byte("sandbox daemon"), 0600))
	ds, err := daemonapi.New(ctx, daemonapi.Config{
		Store: st, Verifier: verifier, Binary: binary, Telemetry: w, Now: clock.now, Configured: true,
		After: func(d time.Duration) <-chan time.Time {
			if d != protocol.LongPoll {
				t.Errorf("long poll duration = %v", d)
			}
			polls <- struct{}{}
			return make(chan time.Time)
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
	reports := make(chan *store.Report, 4)
	observed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
		switch r.URL.Path {
		case protocol.BinaryPath:
			t.Error("simulator tried to update its served daemon")
		case protocol.ReportPath:
			report, err := st.Report(ctx, host.Name)
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
			reports <- report
		}
	})
	type pause struct {
		proceed chan struct{}
	}
	applies := make(chan pause, 4)
	watches := make(chan chan time.Time, 4)
	s := New(Config{Cloud: c, Handler: observed, Binding: binding, Digest: ds.Digest, Now: clock.now, After: watchTimer(t, watches),
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
	finishApply := func(p pause) {
		clock.advance(ApplyDelay)
		close(p.proceed)
	}
	finishApply(await(t, applies))
	initial := await(t, reports)
	if initial.Body.Applied != host.Version || initial.Body.InstanceID != instance.ID ||
		initial.Body.Type != instance.Type || initial.Body.AMI != memory.AMI || initial.Body.OS != machine.MemoryOS ||
		initial.Body.DaemonVersion != ds.Digest() {
		t.Fatalf("initial report = %+v", initial.Body)
	}
	await(t, polls)
	account, _, err := st.PlaceAccount(ctx, "team", host.Name, caller)
	must(t, err)
	nextApply := await(t, applies)
	pending, err := st.Host(ctx, host.Name)
	must(t, err)
	if pending.Applied != host.Version || pending.Version != account.Version || pending.Applied == pending.Version {
		t.Fatalf("host should show pending during apply delay: %+v", pending)
	}
	finishApply(nextApply)
	final := await(t, reports)
	synced, err := st.Host(ctx, host.Name)
	must(t, err)
	if synced.Applied != account.Version || final.Body.Applied != account.Version {
		t.Fatalf("applied host = %d, report = %d, expected %d", synced.Applied, final.Body.Applied, account.Version)
	}
}
