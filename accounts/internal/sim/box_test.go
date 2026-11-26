package sim

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

const (
	boxRequest  = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	boxResponse = `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`
)

type sandboxBox struct {
	t       *testing.T
	ctx     context.Context
	clock   *simClock
	cloud   *memory.Cloud
	hosts   []cloud.Instance
	states  map[string]chan protocol.DesiredState
	reports chan protocol.Report
	watches chan chan time.Time
	tick    chan time.Time
	banFile string
}

func newSandboxBox(t *testing.T, dir string, hosts int, forward http.HandlerFunc) *sandboxBox {
	t.Helper()
	return newSandbox(t, dir, "", hosts, forward)
}

func newSandbox(t *testing.T, boxDir, seedFile string, hosts int, forward http.HandlerFunc) *sandboxBox {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	clock := &simClock{}
	c, first := sandboxCloud(t, clock)
	b := &sandboxBox{
		t: t, ctx: ctx, clock: clock, cloud: c, hosts: []cloud.Instance{first},
		states: make(map[string]chan protocol.DesiredState), reports: make(chan protocol.Report, hosts),
		watches: make(chan chan time.Time, 1),
		banFile: filepath.Join(t.TempDir(), "sandbox-bans"),
	}
	for n := 2; n <= hosts; n++ {
		b.hosts = append(b.hosts, launchSandboxHost(t, c, fmt.Sprintf("host%02d", n)))
	}
	for _, host := range b.hosts {
		b.states[host.ID] = make(chan protocol.DesiredState, 1)
	}
	clock.advance(memory.BootDelay)
	binding := settings.SandboxBinding
	verifier := hostauth.NewVerifier(hostauth.VerifierConfig{
		Region: settings.SandboxHosts.Region, Role: settings.SandboxHosts.InstanceProfile,
		STS: c, Now: clock.now, Binding: func() (string, error) { return binding, nil },
	})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case protocol.StatePath:
			instanceID, err := verifier.Verify(r.Context(), r.Header.Get(hostauth.HeaderName))
			if err != nil {
				if r.Context().Err() == nil {
					t.Error(err)
				}
				<-r.Context().Done()
				return
			}
			select {
			case state := <-b.states[instanceID]:
				if err := json.NewEncoder(w).Encode(state); err != nil && r.Context().Err() == nil {
					t.Error(err)
				}
			case <-r.Context().Done():
			}
		case protocol.ReportPath:
			report, err := protocol.DecodeReport(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			b.reports <- report
		case protocol.BoxPath:
			if forward != nil {
				forward(w, r)
				return
			}
			fallthrough
		default:
			t.Errorf("unexpected request to %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	watch := watchTimer(t, b.watches)
	after := func(d time.Duration) <-chan time.Time {
		if d == daemon.BoxTimeout {
			return make(chan time.Time)
		}
		return watch(d)
	}
	s := New(Config{
		Cloud: c, Handler: h, Binding: binding, BoxDir: boxDir, SeedFile: seedFile, After: after, BanFile: b.banFile,
		Digest: func() string { return "same" }, Now: clock.now,
		Sleep: func(_ context.Context, d time.Duration) { clock.advance(d) },
	})
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		must(t, await(t, done))
	})
	b.tick = await(t, b.watches)
	return b
}

func (b *sandboxBox) apply(host int, version int64, users ...protocol.User) protocol.Report {
	b.t.Helper()
	instance := b.hosts[host-1]
	return b.applyState(host, protocol.DesiredState{
		Protocol: protocol.Version, Host: instance.Tags[cloud.TagHost], Version: version, Daemon: "same",
		DataVolume: "vol-data", DataGB: 100, RootGB: 20,
		Accounts: []protocol.Account{{Slug: "team", GID: 4040, State: protocol.AccountActive}},
		Users:    users,
	})
}

func (b *sandboxBox) applyState(host int, state protocol.DesiredState) protocol.Report {
	b.t.Helper()
	instance := b.hosts[host-1]
	b.states[instance.ID] <- state
	r := await(b.t, b.reports)
	if r.InstanceID != instance.ID || r.Applied != state.Version {
		b.t.Fatalf("report = %+v, expected instance %s version %d", r, instance.ID, state.Version)
	}
	return r
}

func (b *sandboxBox) look() {
	b.t.Helper()
	b.tick <- b.clock.now()
	b.tick = await(b.t, b.watches)
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sim-box")
	must(t, err)
	t.Cleanup(func() { must(t, os.RemoveAll(dir)) })
	return dir
}

func activeBoxUser(name string, uid int64) protocol.User {
	return protocol.User{Name: name, Account: "team", UID: uid, State: protocol.UserActive}
}

func listLine(dir string, user protocol.User, host, status string) string {
	return fmt.Sprintf("%s\t%s\t%d\t%s\t%s\n", user.Name, host, user.UID, status, filepath.Join(dir, user.Name))
}

func expectListing(t *testing.T, dir, want string) {
	t.Helper()
	path := filepath.Join(dir, SocketList)
	info, err := os.Lstat(path)
	must(t, err)
	if !info.Mode().IsRegular() {
		t.Fatalf("listing is not regular: %v", info.Mode())
	}
	got, err := os.ReadFile(filepath.Clean(path))
	must(t, err)
	if string(got) != want {
		t.Fatalf("socket listing = %q, want %q", got, want)
	}
}

func expectSocket(t *testing.T, path string) {
	t.Helper()
	s, err := os.Lstat(path)
	must(t, err)
	if s.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%s has mode %v, expected a socket", path, s.Mode())
	}
}

func expectAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s absent, stat error = %v", path, err)
	}
}

func dialBox(t *testing.T, path string) net.Conn {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", path)
	must(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func sendBoxRequest(t *testing.T, conn net.Conn) {
	t.Helper()
	_, err := io.WriteString(conn, boxRequest+"\n")
	must(t, err)
}

func readBoxAnswer(t *testing.T, conn net.Conn) boxmcp.Answer {
	t.Helper()
	type result struct {
		line []byte
		err  error
	}
	answers := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		answers <- result{line: line, err: err}
	}()
	got := await(t, answers)
	must(t, got.err)
	var answer boxmcp.Answer
	must(t, json.Unmarshal(got.line, &answer))
	return answer
}

func TestBoxSocketsFollowAppliedUsers(t *testing.T) {
	// R-Y48R-6BVL R-2FY1-20FT R-BG44-VSUM R-BHC1-9KLB R-Y0L2-10NI
	base := shortTempDir(t)
	dir := filepath.Join(base, "box")
	t.Cleanup(func() { expectAbsent(t, dir) })
	workingDir, err := filepath.Abs(".")
	must(t, err)
	relativeDir, err := filepath.Rel(workingDir, dir)
	must(t, err)
	b := newSandboxBox(t, relativeDir, 2, nil)
	expectAbsent(t, dir)
	alice := activeBoxUser("alice", 4242)
	bob := activeBoxUser("bob", 4343)
	locked := activeBoxUser("locked", 4444)
	locked.State = protocol.UserLocked
	b.apply(1, 1, bob, locked, alice)
	must(t, os.Mkdir(dir, 0o700))
	path := filepath.Join(dir, alice.Name)
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	must(t, err)
	stale.SetUnlinkOnClose(false)
	must(t, stale.Close())
	extra := filepath.Join(dir, "extra")
	stale, err = net.ListenUnix("unix", &net.UnixAddr{Name: extra, Net: "unix"})
	must(t, err)
	stale.SetUnlinkOnClose(false)
	must(t, stale.Close())
	bobPath := filepath.Join(dir, bob.Name)
	must(t, os.WriteFile(bobPath, []byte(bob.Name), 0o600))
	b.look()
	conn := dialBox(t, path)
	must(t, conn.Close())
	expectAbsent(t, extra)
	expectAbsent(t, filepath.Join(dir, locked.Name))
	bobContents, err := os.ReadFile(filepath.Clean(bobPath))
	must(t, err)
	if string(bobContents) != bob.Name {
		t.Fatalf("collision was modified: %q", bobContents)
	}
	const blocked = "192.0.2.77"
	must(t, os.WriteFile(b.banFile, []byte("host01 "+blocked+"\n"), 0o600))
	b.look()
	expectAbsent(t, b.banFile)
	if r := b.apply(1, 2, bob, locked, alice); len(r.Bans) != 1 || r.Bans[0].Address != blocked {
		t.Fatalf("report after failed bind = %+v, want ban of %s", r.Bans, blocked)
	}
	oldListing := listLine(dir, alice, "host01", StatusServed) + listLine(dir, bob, "host01", StatusFailed)
	expectListing(t, dir, oldListing)
	old, err := os.Open(filepath.Clean(filepath.Join(dir, SocketList)))
	must(t, err)
	t.Cleanup(func() { _ = old.Close() })
	must(t, os.Remove(bobPath))

	otherAlice := activeBoxUser(alice.Name, 5252)
	b.apply(2, 1, otherAlice)
	b.look()
	expectAbsent(t, path)
	expectSocket(t, bobPath)
	expectListing(t, dir, listLine(dir, alice, "host01", StatusFailed)+listLine(dir, otherAlice, "host02", StatusFailed)+listLine(dir, bob, "host01", StatusServed))
	oldContents, err := io.ReadAll(old)
	must(t, err)
	if string(oldContents) != oldListing {
		t.Fatalf("old descriptor has %q instead of the old listing", oldContents)
	}

	alice.State = protocol.UserLocked
	bob.State = protocol.UserDeleted
	b.apply(1, 3, alice, bob)
	b.look()
	expectAbsent(t, bobPath)
	expectSocket(t, path)
	expectListing(t, dir, listLine(dir, otherAlice, "host02", StatusServed))
	must(t, b.cloud.StopInstance(t.Context(), b.hosts[1].ID))
	b.look()
	expectAbsent(t, dir)

	bob.State = protocol.UserActive
	b.apply(1, 4, bob)
	b.look()
	expectSocket(t, bobPath)
	info, err := os.Lstat(dir)
	must(t, err)
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("box directory mode = %v, want 0700", info.Mode())
	}
}

func TestBoxRefusesInvalidDirectory(t *testing.T) {
	// R-BHC1-9KLB
	base := shortTempDir(t)
	file := filepath.Join(base, "not-a-directory")
	must(t, os.WriteFile(file, []byte("keep"), 0o600))
	b := newSandboxBox(t, file, 1, nil)
	b.apply(1, 1, activeBoxUser("alice", 4242))
	b.look()
	const blocked = "192.0.2.77"
	must(t, os.WriteFile(b.banFile, []byte("host01 "+blocked+"\n"), 0o600))
	b.look()
	expectAbsent(t, b.banFile)
	r := b.apply(1, 2, activeBoxUser("nora", 4343))
	if len(r.Bans) != 1 || r.Bans[0].Address != blocked {
		t.Fatalf("report after failed socket setup = %+v, want ban of %s", r.Bans, blocked)
	}
	got, err := os.ReadFile(filepath.Clean(file))
	must(t, err)
	if string(got) != "keep" {
		t.Fatalf("invalid box path changed to %q", got)
	}
	missing := filepath.Join(base, "missing")
	other := newSandboxBox(t, filepath.Join(missing, "box"), 1, nil)
	other.apply(1, 1, activeBoxUser("bob", 4343))
	other.look()
	expectAbsent(t, missing)
}

func TestNoBoxDirectoryConfigured(t *testing.T) {
	// R-Y48R-6BVL
	b := newSandboxBox(t, "", 1, nil)
	b.apply(1, 1, activeBoxUser("alice", 4242))
	b.look()
	entries, err := os.ReadDir(filepath.Dir(b.banFile))
	must(t, err)
	if len(entries) != 0 {
		t.Fatalf("box disabled, but the state directory has entries: %v", entries)
	}
}

func TestAbandonedBoxListingIsRemoved(t *testing.T) {
	// R-BG44-VSUM R-BHC1-9KLB R-Y0L2-10NI
	for _, phase := range []string{"look", "shutdown"} {
		t.Run(phase, func(t *testing.T) {
			dir := filepath.Join(shortTempDir(t), "box")
			must(t, os.Mkdir(dir, 0o700))
			other := filepath.Join(dir, "."+SocketList+".notes")
			must(t, os.WriteFile(other, []byte("keep"), 0o600))
			t.Cleanup(func() {
				expectAbsent(t, filepath.Join(dir, SocketList))
				expectAbsent(t, filepath.Join(dir, pendingSocketList))
				body, err := os.ReadFile(filepath.Clean(other))
				must(t, err)
				if string(body) != "keep" {
					t.Fatalf("other file changed to %q", body)
				}
			})
			abandon := func() {
				t.Helper()
				f, err := openBoxListing(dir)
				must(t, err)
				if filepath.Dir(f.Name()) != dir || !strings.HasPrefix(filepath.Base(f.Name()), ".") {
					t.Errorf("staging path must be a dot file in the box directory: %s", f.Name())
				}
				must(t, f.Close())
				must(t, os.WriteFile(filepath.Join(dir, SocketList), []byte("old listing\n"), 0o600))
			}
			if phase == "look" {
				abandon()
				newSandboxBox(t, dir, 1, nil)
				expectAbsent(t, filepath.Join(dir, SocketList))
				expectAbsent(t, filepath.Join(dir, pendingSocketList))
			} else {
				b := newSandboxBox(t, dir, 1, nil)
				b.apply(1, 1, activeBoxUser("alice", 4242))
				b.look()
				expectSocket(t, filepath.Join(dir, "alice"))
				abandon()
			}
		})
	}
}

func TestBoxSocketPathLimit(t *testing.T) {
	// R-2EQ4-O8P4
	base := shortTempDir(t)
	dir := filepath.Join(base, strings.Repeat("x", 80-len(base)-1))
	b := newSandboxBox(t, dir, 1, nil)
	a := activeBoxUser("alice", 4242)
	bb := activeBoxUser(strings.Repeat("b", 26), 4343)
	c := activeBoxUser(strings.Repeat("c", 27), 4444)
	d := activeBoxUser(strings.Repeat("d", 32), 4545)
	b.apply(1, 1, d, c, bb, a)
	b.look()
	expectListing(t, dir, listLine(dir, a, "host01", StatusServed)+listLine(dir, bb, "host01", StatusServed)+listLine(dir, c, "host01", StatusTooLong)+listLine(dir, d, "host01", StatusTooLong))
	expectSocket(t, filepath.Join(dir, a.Name))
	expectSocket(t, filepath.Join(dir, bb.Name))
	expectAbsent(t, filepath.Join(dir, c.Name))
	expectAbsent(t, filepath.Join(dir, d.Name))
}

func TestBoxForwardsUserUID(t *testing.T) {
	// R-XVPG-HXOQ
	dir := filepath.Join(shortTempDir(t), "box")
	type forward struct{ uid, body string }
	requests := make(chan forward, 2)
	b := newSandboxBox(t, dir, 1, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		requests <- forward{uid: r.URL.Query().Get(protocol.QueryUID), body: string(body)}
		_, _ = io.WriteString(w, boxResponse+"\n")
	})
	b.apply(1, 1, activeBoxUser("alice", 4242), activeBoxUser("bob", 4343))
	b.look()
	paths := []string{filepath.Join(dir, "alice"), filepath.Join(dir, "bob")}
	for i, uid := range []string{"4242", "4343"} {
		conn := dialBox(t, paths[i])
		sendBoxRequest(t, conn)
		got := await(t, requests)
		if got != (forward{uid, boxRequest}) {
			t.Fatalf("forwarded request = %+v, want UID %s and line %q", got, uid, boxRequest)
		}
		if a := readBoxAnswer(t, conn); string(a.Message) != boxResponse || a.Refusal != "" {
			t.Fatalf("answer = %+v, want %s", a, boxResponse)
		}
		must(t, conn.Close())
	}
}

func TestBoxSharesHostConnectionLimit(t *testing.T) {
	// R-2H5X-FS6I
	dir := filepath.Join(shortTempDir(t), "box")
	requests := make(chan string, daemon.MaxBoxConns+1)
	b := newSandboxBox(t, dir, 1, func(_ http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query().Get(protocol.QueryUID)
		<-r.Context().Done()
	})
	b.apply(1, 1, activeBoxUser("alice", 4242), activeBoxUser("bob", 4343))
	b.look()
	paths := []string{filepath.Join(dir, "alice"), filepath.Join(dir, "bob")}
	for i := range daemon.MaxBoxConns {
		conn := dialBox(t, paths[i%len(paths)])
		sendBoxRequest(t, conn)
		await(t, requests)
	}
	extra := dialBox(t, paths[1])
	if a := readBoxAnswer(t, extra); a.Refusal != daemon.CannotReach || len(a.Message) != 0 {
		t.Fatalf("answer for saturated host = %+v, want %q", a, daemon.CannotReach)
	}
	select {
	case extra := <-requests:
		t.Fatalf("saturated host forwarded %+v", extra)
	default:
	}
}

func TestClosingBoxListenerDiscardsPendingConnection(t *testing.T) {
	// R-2FY1-20FT R-2H5X-FS6I
	dir := shortTempDir(t)
	path := filepath.Join(dir, "alice")
	box := newHostListener(dir)
	t.Cleanup(func() { _ = box.Close() })
	if !box.listen(path, 4242) {
		t.Fatal("listen failed")
	}
	type result struct {
		wire string
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		var out strings.Builder
		err := boxmcp.Serve(t.Context(), strings.NewReader(boxRequest+"\n"), &out, boxmcp.Config{
			Socket: path, Timeout: boxmcp.AnswerTimeout,
		})
		resultCh <- result{out.String(), err}
	}()
	// Observe the accepted connection without handing it to the daemon.
	await(t, box.ready)
	must(t, box.Close())
	expectAbsent(t, path)
	if c, err := box.Accept(); c != nil || !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed listener returned %v, %v", c, err)
	}
	got := await(t, resultCh)
	must(t, got.err)
	var response struct {
		ID    int `json:"id"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	must(t, json.Unmarshal([]byte(got.wire), &response))
	if response.ID != 1 || response.Error.Message != boxmcp.NotAnswering {
		t.Fatalf("closed pending client received %q, want %q", got.wire, boxmcp.NotAnswering)
	}
}

func TestBoxLookMemoryBound(t *testing.T) {
	// R-P4EZ-66LH
	dir := filepath.Join(shortTempDir(t), "box")
	b := newSandboxBox(t, dir, 3, nil)
	for host := 1; host <= 3; host++ {
		users := make([]protocol.User, valid.MaxUsersPerHost)
		for i := range users {
			users[i] = activeBoxUser(fmt.Sprintf("user%d_%03d", host, i), int64(host*1000+i))
		}
		b.apply(host, 1, users...)
	}
	var before, after, warmed runtime.MemStats
	for i := 1; i <= 50; i++ {
		runtime.ReadMemStats(&before)
		b.look()
		runtime.ReadMemStats(&after)
		if delta := after.TotalAlloc - before.TotalAlloc; delta >= 16<<20 {
			t.Fatalf("look %d allocated %d bytes, want under 16 MiB", i, delta)
		}
		if i == 1 {
			body, err := os.ReadFile(filepath.Clean(filepath.Join(dir, SocketList)))
			must(t, err)
			if got := strings.Count(string(body), "\t"+StatusServed+"\t"); got != 3*valid.MaxUsersPerHost {
				t.Fatalf("listing serves %d of %d users", got, 3*valid.MaxUsersPerHost)
			}
		}
		if i == 2 {
			runtime.GC()
			runtime.ReadMemStats(&warmed)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if after.HeapInuse >= warmed.HeapInuse+1<<20 {
		t.Fatalf("heap grew from %d to %d bytes, want growth under 1 MiB", warmed.HeapInuse, after.HeapInuse)
	}
}
