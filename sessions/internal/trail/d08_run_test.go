package trail_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
	"github.com/ikigenba/ikigenba/sessions/internal/trail"
)

type d08Random struct {
	mu       sync.Mutex
	n        uint64
	constant bool
}

func (r *d08Random) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(b)
	if r.constant {
		for i := range b {
			b[i] = 0x5a
		}
	} else {
		r.n++
		if len(b) >= 8 {
			binary.LittleEndian.PutUint64(b, r.n)
		} else {
			for i := range b {
				b[i] = byte((r.n >> uint(8*i)) & 0xff)
			}
		}
	}
	return len(b), nil
}

type d08Request struct {
	method, path, user, tool string
	status                   int
}
type d08RunRig struct {
	client   *mcp.Client
	http     *http.Client
	endpoint string
	capture  *telemetry.Capture
	writer   *telemetry.Writer
	cancel   context.CancelFunc
	done     chan int
	requests map[string]d08Request
	serial   int
	stopped  bool
}

func d08StartRun(t *testing.T, dir string, rnd io.Reader) *d08RunRig {
	t.Helper()
	t.Setenv(services.Variable, "")
	socketDir, err := os.MkdirTemp("", "d08-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(socketDir); err != nil {
			t.Error(err)
		}
	})
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(socketDir, "ready"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notify.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	capture := &telemetry.Capture{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	writers := make(chan *telemetry.Writer, 1)
	env := map[string]string{"LISTEN_PID": "77", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String(), "DRAIN_SECONDS": "3", "IKIGENBA_SERVICES": ""}
	p := cli.Process{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Unsetenv: func(k string) error { delete(env, k); return nil }, Pid: 77, Stdout: io.Discard, Stderr: io.Discard, Version: "trail-test", Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Now: func() time.Time { return trailNow }, Sleep: func(context.Context, time.Duration) {}, Rand: rnd, Dir: dir, Sink: capture, Banner: func(u page.User) page.Banner {
		return page.Banner{Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, MCP: func(w *telemetry.Writer) *mcp.Server {
		writers <- w
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "trail-test", Telemetry: w})
	}}
	go func() { done <- cli.Run(ctx, p) }()
	httpClient := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r := &d08RunRig{http: httpClient, endpoint: "http://" + ln.Addr().String(), capture: capture, cancel: cancel, done: done, requests: map[string]d08Request{}}
	t.Cleanup(func() {
		if !r.stopped {
			r.stop(t)
		}
	})
	if err := notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64)
	n, _, err := notify.ReadFromUnix(b)
	if err != nil {
		select {
		case code := <-done:
			t.Fatalf("Run returned %d before READY", code)
		default:
			t.Fatal(err)
		}
	}
	if string(b[:n]) != "READY=1" {
		t.Fatalf("notification=%q", b[:n])
	}
	r.writer = <-writers
	r.client = mcp.NewClient(mcp.ClientConfig{Endpoint: r.endpoint + "/mcp", HTTPClient: httpClient, Name: "test", Version: "test"})
	return r
}
func (r *d08RunRig) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	select {
	case code := <-r.done:
		if code != cli.ExitSuccess {
			t.Errorf("Run exit=%d", code)
		}
	case <-time.After(5 * time.Second):
		t.Error("Run did not stop")
	}
	r.stopped = true
}
func (r *d08RunRig) call(t *testing.T, user, email, name string, args any, wantError bool, withID bool) {
	t.Helper()
	r.serial++
	q := ""
	if withID {
		q = fmt.Sprintf("run-call-%d", r.serial)
	}
	result, err := r.client.CallTool(context.Background(), identity.Caller{UserID: user, Email: email, RequestID: q}, name, trailJSON(t, args))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError() != wantError {
		t.Fatalf("%s error=%t want %t result=%+v", name, result.IsError(), wantError, result)
	}
	trailFlush(t, r.writer)
	if withID {
		r.requests[q] = d08Request{method: "POST", path: "/mcp", user: user, tool: name, status: 200}
	}
}
func (r *d08RunRig) request(t *testing.T, method, path, user, email string) {
	t.Helper()
	r.serial++
	q := fmt.Sprintf("run-page-%d", r.serial)
	req, err := http.NewRequest(method, r.endpoint+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-Id", q)
	if user != "" {
		req.Header.Set("X-User-Id", user)
		req.Header.Set("X-User-Email", email)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	response, err := r.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read response: %v %v", err, closeErr)
	}
	r.requests[q] = d08Request{method: method, path: strings.Split(path, "?")[0], user: user, status: response.StatusCode}
	trailFlush(t, r.writer)
}

// R-QUIJ-0TPK R-QY68-64XN R-QZE4-JWOC
func d08AssertRun(t *testing.T, r *d08RunRig) {
	t.Helper()
	events := r.capture.Events()
	allowed := map[string]bool{"service.started": true, "service.stopping": true, "request.started": true, "request.finished": true, "tool.called": true, trail.EventChangeFinished: true, trail.EventRefreshFinished: true}
	changing := map[string]bool{tools.ToolPushTurns: true, tools.ToolAddRule: true, tools.ToolRemoveRule: true, tools.ToolSetPrice: true, tools.ToolRemovePrice: true, tools.ToolSetAwayLimit: true}
	for _, e := range events {
		if !allowed[e.Name] {
			t.Errorf("unexpected event name %q", e.Name)
		}
	}
	for q, request := range r.requests {
		var group []telemetry.Event
		for _, e := range events {
			if e.RequestID == q {
				group = append(group, e)
				if e.User != request.user {
					t.Errorf("%s user=%q want %q", q, e.User, request.user)
				}
			}
		}
		if len(group) < 2 {
			t.Fatalf("%s events=%+v", q, group)
		}
		starts, finishes, changes, toolsCount := 0, 0, 0, 0
		changeIndex, toolIndex := -1, -1
		for i, e := range group {
			switch e.Name {
			case "request.started":
				starts++
				if i != 0 || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"method": request.method, "path": request.path}) {
					t.Errorf("%s start=%+v at %d", q, e, i)
				}
			case "request.finished":
				finishes++
				if i != len(group)-1 || e.Attrs["status"] != int64(request.status) {
					t.Errorf("%s finish=%+v at %d", q, e, i)
				}
			case trail.EventChangeFinished:
				changes++
				changeIndex = i
				if e.Attrs["tool"] != request.tool {
					t.Errorf("%s wrong change tool %+v", q, e)
				}
			case "tool.called":
				toolsCount++
				toolIndex = i
			}
		}
		wantChanges := 0
		if changing[request.tool] {
			wantChanges = 1
		}
		if starts != 1 || finishes != 1 || changes != wantChanges {
			t.Errorf("%s pair/change counts %d/%d/%d want changes %d", q, starts, finishes, changes, wantChanges)
		}
		if wantChanges == 1 && (toolsCount != 1 || changeIndex >= toolIndex) {
			t.Errorf("%s change/tool ordering %d/%d count %d", q, changeIndex, toolIndex, toolsCount)
		}
	}
}

// R-VGUN-Z27V
func TestRunRefreshOrderAndRoots(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			dir := t.TempDir()
			want := int64(0)
			if pending {
				d, s := trailDatabase(t, filepath.Join(dir, "state", "sessions.db"))
				entries := make([]store.PushSession, 3)
				for i := range entries {
					entries[i] = store.PushSession{Tool: "codex", ID: fmt.Sprint(i), Host: "host", Directory: "/work", Turns: []store.PushTurn{{Moment: trailNow.Format(time.RFC3339Nano), Whose: "agent"}}}
				}
				if _, err := s.Push(context.Background(), "owner", "owner@invalid.test", entries); err != nil {
					t.Fatal(err)
				}
				if err := d.Write(context.Background(), func(tx *sql.Tx) error {
					_, err := tx.Exec("UPDATE settings SET away_minutes=25, refresh_after_tool='', refresh_after_id='' WHERE id=1")
					return err
				}); err != nil {
					t.Fatal(err)
				}
				if err := d.Close(); err != nil {
					t.Fatal(err)
				}
				want = 3
			}
			r := d08StartRun(t, dir, &d08Random{})
			r.request(t, "GET", "/tools?x=1", "", "")
			r.stop(t)
			found, started := 0, -1
			for i, e := range r.capture.Events() {
				switch e.Name {
				case "service.started":
					started = i
				case trail.EventRefreshFinished:
					found++
					if started < 0 || i <= started || e.RequestID != "" || e.User != "" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"roots": want, "result": trail.ResultOK}) {
						t.Fatalf("refresh=%+v at %d started %d", e, i, started)
					}
				case "request.started":
					if found != 1 {
						t.Fatalf("request before refresh: %+v", r.capture.Events())
					}
				}
			}
			if found != 1 {
				t.Fatalf("refresh count=%d", found)
			}
			d08AssertRun(t, r)
		})
	}
}

// R-R1TX-BG5Q
func TestRunMintsChangeRequestID(t *testing.T) {
	ids := make([]string, 2)
	for i := range ids {
		r := d08StartRun(t, t.TempDir(), &d08Random{constant: true})
		r.call(t, "owner", "private@invalid.test", tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 30}, false, false)
		r.stop(t)
		var started, changed string
		count := 0
		for _, e := range r.capture.Events() {
			if e.Name == "request.started" {
				started = e.RequestID
			}
			if e.Name == trail.EventChangeFinished {
				changed = e.RequestID
				count++
			}
		}
		if started == "" || changed != started || count != 1 {
			t.Fatalf("minted ids started %q changed %q count %d", started, changed, count)
		}
		ids[i] = changed
	}
	if ids[0] != ids[1] {
		t.Fatalf("same Rand minted different ids: %v", ids)
	}
}

type d08ZeroReader struct{}

func (*d08ZeroReader) Read(b []byte) (int, error) { clear(b); return len(b), nil }

type d08Markers struct {
	rng    io.Reader
	values []string
}

func (m *d08Markers) next() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 24)
	if _, err := io.ReadFull(m.rng, b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	for i := 0; i < 24; i += 8 {
		b[i] = "GHIJKLMNOPQRSTUVWXYZ"[int(b[i])%20]
	}
	s := string(b)
	m.values = append(m.values, s)
	return s
}
func (m *d08Markers) entry(n int) tools.PushSession {
	s := trailEntry(m.next(), n)
	s.Host = m.next()
	s.Directory = "/private/" + m.next()
	s.Command = &tools.Command{Name: m.next(), Argument: m.next()}
	for i := range s.Turns {
		s.Turns[i].Model = m.next()
	}
	return s
}
func d08AssertNoMarkers(t *testing.T, events []telemetry.Event, markers []string) {
	t.Helper()
	for i, m := range markers {
		if len(m) != 24 {
			t.Fatalf("marker length=%d", len(m))
		}
		for _, c := range m {
			if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				t.Fatalf("bad marker %q", m)
			}
		}
		for start := 0; start <= len(m)-8; start++ {
			hasLetter := false
			for _, c := range m[start : start+8] {
				hasLetter = hasLetter || c >= 'G' && c <= 'Z'
			}
			if !hasLetter {
				t.Fatalf("marker window lacks letter: %q", m)
			}
		}
		for j, other := range markers {
			if i != j && strings.Contains(m, other) {
				t.Fatalf("marker %q holds marker %q", m, other)
			}
		}
	}

	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToUpper(string(b))
		for _, m := range markers {
			if strings.Contains(text, strings.ToUpper(m)) {
				t.Errorf("event leaked marker %q: %s", m, b)
			}
		}
	}
}

// R-I8ZH-K0MD R-IA7D-XSD2
// The same run also proves R-QUIJ-0TPK, R-QY68-64XN and R-QZE4-JWOC for all tools and pages.
func TestRunSecrecyAndCompleteTrail(t *testing.T) {
	block, err := aes.NewCipher([]byte("trail-marker-key"))
	if err != nil {
		t.Fatal(err)
	}
	markers := &d08Markers{rng: &cipher.StreamReader{S: cipher.NewCTR(block, make([]byte, aes.BlockSize)), R: &d08ZeroReader{}}}
	email := markers.next() + "@invalid.test"
	r := d08StartRun(t, t.TempDir(), &d08Random{})
	a, b := markers.entry(2), markers.entry(1)
	b.Parent = &tools.SessionRef{Tool: a.Tool, ID: a.ID}
	r.call(t, "owner", email, tools.ToolPushTurns, trailPush(a, b), false, true)
	relative := markers.entry(1)
	relative.Directory = markers.next()
	r.call(t, "owner", email, tools.ToolPushTurns, trailPush(relative), true, true)
	own := markers.entry(1)
	own.Parent = &tools.SessionRef{Tool: own.Tool, ID: own.ID}
	r.call(t, "owner", email, tools.ToolPushTurns, trailPush(own), true, true)
	r.call(t, "second-owner", email, tools.ToolPushTurns, trailPush(a), true, true)
	for _, tc := range []struct {
		name      string
		good, bad any
	}{
		{tools.ToolAddRule, tools.AddRuleArgs{Host: "configuration", Directory: "/configuration/rule", Kind: "build"}, tools.AddRuleArgs{Host: "configuration", Directory: "/configuration/rule", Kind: "build"}},
		{tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "configuration", Directory: "/configuration/rule"}, tools.RemoveRuleArgs{Host: "configuration", Directory: "/configuration/rule"}},
		{tools.ToolSetPrice, tools.SetPriceArgs{Model: "configured", Effective: "2026-01-01", Input: "1", Cached: "1", Output: "1", Reasoning: "1"}, tools.SetPriceArgs{Model: "configured", Effective: "invalid", Input: "1", Cached: "1", Output: "1", Reasoning: "1"}},
		{tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "configured", Effective: "2026-01-01"}, tools.RemovePriceArgs{Model: "configured", Effective: "2026-01-01"}},
		{tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 30}, tools.SetAwayLimitArgs{Minutes: 0}},
	} {
		r.call(t, "owner", email, tc.name, tc.good, false, true)
		r.call(t, "second-owner", email, tc.name, tc.bad, true, true)
	}
	person, host, command := markers.next(), markers.next(), markers.next()
	for _, tc := range []struct {
		name string
		args any
	}{
		{tools.ToolGetLatestTurns, tools.LatestTurnsArgs{Sessions: []tools.SessionRef{{Tool: a.Tool, ID: a.ID}, {Tool: "codex", ID: markers.next()}}}},
		{tools.ToolListSessions, tools.ListSessionsArgs{Person: &person, Host: &host, Command: &command}},
		{tools.ToolTotalSessions, tools.TotalSessionsArgs{Period: "week", From: "2026-10-26", Group: "kind", Person: &person, Host: &host, Command: &command}},
		{tools.ToolListRules, tools.NoArgs{}}, {tools.ToolListPrices, tools.NoArgs{}}, {tools.ToolGetAwayLimit, tools.NoArgs{}},
	} {
		r.call(t, "owner", email, tc.name, tc.args, false, true)
	}
	for _, user := range []string{"", "owner"} {
		for _, path := range []string{"/", "/tools?x=1", "/about", "/missing"} {
			r.request(t, "GET", path, user, email)
		}
	}
	r.request(t, "POST", "/mcp", "", "")
	r.request(t, "DELETE", "/missing", "owner", email)
	r.stop(t)
	d08AssertRun(t, r)
	d08AssertNoMarkers(t, r.capture.Events(), markers.values)
	// A store failure is reached at the tools seam, with marker-bearing pushed values and email.
	rig := newTrailToolRig(t)
	rig.d.SetFailing(true)
	for i, tc := range []struct {
		name string
		args any
	}{
		{tools.ToolPushTurns, trailPush(markers.entry(3), markers.entry(2))},
		{tools.ToolAddRule, tools.AddRuleArgs{Host: "configuration", Directory: "/configuration/rule", Kind: "build"}},
		{tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "configuration", Directory: "/configuration/rule"}},
		{tools.ToolSetPrice, tools.SetPriceArgs{Model: "configured", Effective: "2026-01-01", Input: "1", Cached: "1", Output: "1", Reasoning: "1"}},
		{tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "configured", Effective: "2026-01-01"}},
		{tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 30}},
	} {
		result, err := rig.client.CallTool(context.Background(), identity.Caller{UserID: "owner", Email: email, RequestID: fmt.Sprintf("failed-%d", i)}, tc.name, trailJSON(t, tc.args))
		if err != nil || !result.IsError() {
			t.Fatalf("failed %s: %+v %v", tc.name, result, err)
		}
	}
	trailFlush(t, rig.w)
	d08AssertNoMarkers(t, rig.capture.Events(), markers.values)
}
