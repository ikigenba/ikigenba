package daemon_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// R-JQDI-OGZ0
var _ struct {
	BaseURL   string
	HTTP      *http.Client
	Presigner cloud.Presigner
	Metadata  cloud.Metadata
	Machine   machine.Machine
	Digest    string
	Now       func() time.Time
	Sleep     func(context.Context, time.Duration)
	Stderr    io.Writer
	Box       net.Listener
	PeerUID   func(net.Conn) (int64, error)
	After     func(time.Duration) <-chan time.Time
} = daemon.Config{}

var _ func(context.Context, daemon.Config) error = daemon.Run

// R-JSTB-G0GE R-JU17-TS73
func TestBoxConstants(t *testing.T) {
	if daemon.BoxTimeout != 15*time.Second || daemon.MaxBoxConns != 8 || daemon.SocketMode != 0o666 {
		t.Error("box constants differ from the socket contract")
	}
	if _, ok := any(daemon.SocketMode).(fs.FileMode); !ok {
		t.Errorf("SocketMode type = %T", daemon.SocketMode)
	}
	for _, copyText := range []string{daemon.NotActiveUser, daemon.CannotReach, daemon.DiagSocket, daemon.DiagForward} {
		if copyText == "" {
			t.Error("empty box diagnostic or refusal")
		}
	}
	if daemon.NotActiveUser == daemon.CannotReach {
		t.Error("box refusals must differ")
	}
}

const testBoxUID = 20037

type boxAccept struct {
	conn net.Conn
	err  error
}

type boxListener struct {
	input  chan boxAccept
	closed chan struct{}
	once   sync.Once
	calls  atomic.Int64
}

func (l *boxListener) Accept() (net.Conn, error) {
	l.calls.Add(1)
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	case event := <-l.input:
		return event.conn, event.err
	}
}

func (l *boxListener) Addr() net.Addr { return nil }

func (l *boxListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

type boxScenario struct {
	cfg         daemon.Config
	listener    *boxListener
	polls       chan *http.Request
	states      chan reply
	timers      chan chan time.Time
	diagnostics bytes.Buffer
	peers       atomic.Int64
	signs       atomic.Int64
	now         atomic.Int64
	onSign      func(context.Context, int64) error
	onBox       func(*http.Request) (*http.Response, error)
	onBinary    func() *http.Response
	cancel      context.CancelFunc
	done        chan struct{}
	result      error
}

func newBoxScenario(t *testing.T) *boxScenario {
	t.Helper()
	s := &boxScenario{
		listener: &boxListener{input: make(chan boxAccept), closed: make(chan struct{})},
		polls:    make(chan *http.Request), states: make(chan reply), timers: make(chan chan time.Time, 1),
		done: make(chan struct{}),
	}
	s.now.Store(time.Date(2026, 11, 19, 12, 0, 0, 250000000, time.UTC).UnixNano())
	clock := func() time.Time { return time.Unix(0, s.now.Load()) }
	sleep := func(_ context.Context, d time.Duration) { s.now.Add(int64(d)) }
	s.cfg = daemon.Config{
		BaseURL: "https://accounts.fake",
		Digest:  "own-binary",
		Now:     clock, Sleep: sleep, Stderr: &s.diagnostics,
		Machine:  machine.NewMemory(machine.MemoryConfig{Now: clock, Sleep: sleep}),
		Metadata: metadataFunc(func(context.Context) (cloud.InstanceFacts, error) { return cloud.InstanceFacts{}, nil }),
		Presigner: presignerFunc(func(ctx context.Context, binding string, _ time.Duration) (string, error) {
			if binding != "accounts.fake" {
				t.Errorf("signing binding = %q", binding)
			}
			number := s.signs.Add(1)
			if s.onSign != nil {
				if err := s.onSign(ctx, number); err != nil {
					return "", err
				}
			}
			return "https://identity.fake/?X-Amz-Date=" + clock().UTC().Format("20060102T150405Z"), nil
		}),
		Box: s.listener,
		PeerUID: func(net.Conn) (int64, error) {
			s.peers.Add(1)
			return testBoxUID, nil
		},
		After: func(d time.Duration) <-chan time.Time {
			if d != daemon.BoxTimeout {
				t.Errorf("timer duration = %v, want BoxTimeout", d)
			}
			timer := make(chan time.Time, 1)
			s.timers <- timer
			return timer
		},
	}
	seenHeaders := make(map[string]bool)
	var headers sync.Mutex
	s.cfg.HTTP = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Body != nil {
			defer func() { _ = req.Body.Close() }()
		}
		if req.URL.Path != protocol.BinaryPath {
			value := req.Header.Get(hostauth.HeaderName)
			decoded, err := base64.RawURLEncoding.DecodeString(value)
			if err != nil || !strings.HasPrefix(string(decoded), "https://identity.fake/?X-Amz-Date=") {
				t.Errorf("identity header = %q", value)
			}
			headers.Lock()
			if seenHeaders[value] {
				t.Errorf("identity header reused: %q", value)
			}
			seenHeaders[value] = true
			headers.Unlock()
		}
		switch req.URL.Path {
		case protocol.StatePath:
			select {
			case s.polls <- req:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			select {
			case value := <-s.states:
				return boxHTTPReply(value.status, value.body), nil
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		case protocol.ReportPath:
			return boxHTTPReply(http.StatusNoContent, ""), nil
		case protocol.BoxPath:
			if s.onBox != nil {
				return s.onBox(req)
			}
		case protocol.BinaryPath:
			if s.onBinary != nil {
				return s.onBinary(), nil
			}
		}
		t.Errorf("unexpected request %s %s", req.Method, req.URL)
		return boxHTTPReply(http.StatusBadRequest, ""), nil
	})}
	return s
}

func boxHTTPReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func (s *boxScenario) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	s.cancel = cancel
	go func() {
		s.result = daemon.Run(ctx, s.cfg)
		close(s.done)
	}()
	t.Cleanup(func() {
		cancel()
		waitBox(t, s.done)
	})
}

func waitBox[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the box scenario")
		var zero T
		return zero
	}
}

func sendBox[T any](t *testing.T, ch chan<- T, v T) {
	t.Helper()
	select {
	case ch <- v:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out advancing the box scenario")
	}
}

func activeBoxState(version int64, digest string) protocol.DesiredState {
	return protocol.DesiredState{
		Protocol: protocol.Version, Host: "box01", Version: version, Daemon: digest, DataVolume: "vol-fake",
		Accounts: []protocol.Account{{Slug: "client", GID: testBoxUID, State: protocol.AccountActive}},
		Users:    []protocol.User{{Name: "worker", Account: "client", UID: testBoxUID, State: protocol.UserActive}},
	}
}

func (s *boxScenario) state(t *testing.T, value protocol.DesiredState) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sendBox(t, s.states, reply{http.StatusOK, string(raw)})
}

func (s *boxScenario) prime(t *testing.T) {
	t.Helper()
	waitBox(t, s.polls)
	s.state(t, activeBoxState(1, ""))
	waitBox(t, s.polls)
}

func (s *boxScenario) dial(t *testing.T, wrap func(net.Conn) net.Conn) (net.Conn, chan time.Time) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	accepted := server
	if wrap != nil {
		accepted = wrap(server)
	}
	sendBox(t, s.listener.input, boxAccept{conn: accepted})
	return client, waitBox(t, s.timers)
}

func socketLine(t *testing.T, client net.Conn, line string) {
	t.Helper()
	if _, err := io.WriteString(client, line); err != nil {
		t.Fatal(err)
	}
}

func boxRequest(method, id string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":%q}`, id, method)
}

func expectBoxAnswer(t *testing.T, conn net.Conn, answer boxmcp.Answer) {
	t.Helper()
	var want bytes.Buffer
	if err := boxmcp.WriteAnswer(&want, answer); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(conn)
	if err != nil || !bytes.Equal(raw, want.Bytes()) {
		t.Fatalf("box answer = %q, %v, want %q", raw, err, &want)
	}
}

// R-K04P-QMWK
func TestBoxIsOptionalAndRequiresItsSeams(t *testing.T) {
	for _, missing := range []string{"both", "uid", "timer", "listener"} {
		t.Run(missing, func(t *testing.T) {
			s := newBoxScenario(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.cfg.HTTP = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				if missing != "listener" {
					t.Error("request started with an incomplete box config")
				}
				cancel()
				return nil, context.Canceled
			})}
			invalid := &boxListener{input: make(chan boxAccept), closed: make(chan struct{})}
			s.cfg.Box = invalid
			switch missing {
			case "both":
				s.cfg.PeerUID, s.cfg.After = nil, nil
			case "uid":
				s.cfg.PeerUID = nil
			case "timer":
				s.cfg.After = nil
			case "listener":
				s.cfg.Box = nil
				s.cfg.PeerUID = func(net.Conn) (int64, error) {
					t.Error("nil listener consulted PeerUID")
					return 0, nil
				}
				s.cfg.After = func(time.Duration) <-chan time.Time {
					t.Error("nil listener consulted After")
					return nil
				}
			}
			if missing != "listener" {
				// Give Accept a ready error that would produce a diagnostic
				// if Run started the accept loop before validating.
				invalid.input = make(chan boxAccept, 1)
				invalid.input <- boxAccept{err: net.ErrClosed}
			}
			err := daemon.Run(ctx, s.cfg)
			if err == nil || (missing == "listener") != errors.Is(err, context.Canceled) ||
				(missing != "listener" && len(invalid.input) != 1) {
				t.Errorf("Run error = %v, pending accepts = %d", err, len(invalid.input))
			}
		})
	}
}

// R-JYWT-CV5V R-3RC8-SPZ4 R-ZD3Z-LKAQ R-ZGRO-QVIT
func TestForwardPassesVerbatimWhileStatePollIsHeld(t *testing.T) {
	s := newBoxScenario(t)
	request := " \t" + strings.TrimSuffix(boxRequest(protocol.MethodToolsCall, `"raw-id"`), "}") +
		`, "params":{"arguments":["nested } brace", [true, null, 1]],"name":"quote\" and slash\\"}}` + " "
	message := `{"jsonrpc":"2.0", "id":"raw-id", "result":{"text":"<&>"}}`
	s.onBox = func(req *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(req.Body)
		wantURI := protocol.BoxPath + "?" + protocol.QueryUID + "=" + fmt.Sprint(testBoxUID)
		if err != nil || string(raw) != request || req.Method != http.MethodPost || req.URL.RequestURI() != wantURI {
			t.Errorf("forward = %s %s %q, %v", req.Method, req.URL.RequestURI(), raw, err)
		}
		if len(req.Header) != 3 || req.Header.Get("User-Agent") != daemon.UserAgent ||
			req.Header.Get("Content-Type") != protocol.ContentJSON || req.Header.Get(hostauth.HeaderName) == "" {
			t.Errorf("forward headers = %v", req.Header)
		}
		return boxHTTPReply(http.StatusOK, message+"\n\n"), nil
	}
	s.start(t)
	s.prime(t)
	client, _ := s.dial(t, nil)
	socketLine(t, client, request+"\nbytes after newline are ignored")
	expectBoxAnswer(t, client, boxmcp.Answer{Message: json.RawMessage(message)})
}

// R-ZEBV-ZC1F R-PT7L-W7VD
func TestBoxUsesLatestSuccessfulStateBeforeForwarding(t *testing.T) {
	s := newBoxScenario(t)
	memory := s.cfg.Machine
	s.cfg.Machine = machineProbe{Machine: memory, apply: func(ctx context.Context, st protocol.DesiredState) error {
		if st.Version == 2 {
			return errors.New("apply failed")
		}
		return memory.Apply(ctx, st)
	}}
	var uid atomic.Int64
	uid.Store(testBoxUID)
	s.cfg.PeerUID = func(net.Conn) (int64, error) {
		s.peers.Add(1)
		value := uid.Load()
		if value == -1 {
			return 0, errors.New("unavailable peer credential")
		}
		return value, nil
	}
	var forwarded atomic.Int64
	message := `{"result":{}}`
	s.onBox = func(*http.Request) (*http.Response, error) {
		forwarded.Add(1)
		return boxHTTPReply(http.StatusOK, message), nil
	}
	s.start(t)
	waitBox(t, s.polls)
	line := boxRequest(protocol.MethodToolsList, "19")
	send := func(line, refusal string) {
		t.Helper()
		client, _ := s.dial(t, nil)
		socketLine(t, client, line+"\n")
		if refusal == "" {
			expectBoxAnswer(t, client, boxmcp.Answer{Message: json.RawMessage(message)})
		} else {
			expectBoxAnswer(t, client, boxmcp.Answer{Refusal: refusal})
		}
	}

	send(line, daemon.CannotReach)
	st := activeBoxState(1, "")
	st.Users = append(st.Users,
		protocol.User{Name: "root", Account: "client", UID: 0, State: protocol.UserActive},
		protocol.User{Name: "locked", Account: "client", UID: testBoxUID + 1, State: protocol.UserLocked},
		protocol.User{Name: "removed", Account: "gone", UID: testBoxUID + 2, State: protocol.UserActive},
		protocol.User{Name: "deleted", Account: "client", UID: testBoxUID + 3, State: protocol.UserDeleted},
		protocol.User{Name: "missing", Account: "unknown", UID: testBoxUID + 4, State: protocol.UserActive},
	)
	st.Accounts = append(st.Accounts, protocol.Account{Slug: "gone", GID: testBoxUID + 2, State: protocol.AccountRemoved})
	s.state(t, st)
	waitBox(t, s.polls)
	for _, peer := range []int64{-1, 0, testBoxUID + 1, testBoxUID + 2, testBoxUID + 3, testBoxUID + 4, testBoxUID + 5} {
		uid.Store(peer)
		send(line, daemon.NotActiveUser)
	}
	uid.Store(testBoxUID)
	for _, invalid := range []string{
		boxRequest(protocol.MethodToolsList, "1.5"), boxRequest(protocol.MethodToolsList, "1e3"),
		boxRequest(protocol.MethodToolsList, "null"), boxRequest("initialize", "1"),
		strings.Replace(line, "method", "METHOD", 1), strings.Replace(line, "method", `m\u0065thod`, 1),
		strings.TrimSuffix(line, "}") + `,"metho\u0064":"initialize"}`,
		strings.TrimSuffix(boxRequest(protocol.MethodToolsCall, "19"), "}") + `,"method":"` + protocol.MethodToolsList + `"}`,
		strings.TrimSuffix(line, "}") + `,"\u0069d":20}`,
		strings.TrimSuffix(line, "}") + `,"jsonrp\u0063":"2.0"}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`, strings.Replace(line, "2.0", "1.0", 1),
		"[" + line + "]", line + line, "broken",
	} {
		send(invalid, daemon.CannotReach)
	}
	send(line, "")
	sendBox(t, s.states, reply{http.StatusOK, "invalid state"})
	waitBox(t, s.polls)
	send(line, "")
	st.Version = 2
	st.Users[0].State = protocol.UserLocked
	s.state(t, st)
	waitBox(t, s.polls)
	send(line, daemon.NotActiveUser)
	if got := forwarded.Load(); got != 2 {
		t.Errorf("sent %d forwards, want exactly the two valid requests", got)
	}
}

// R-ZGRO-QVIT
func TestBoxStopsReadingAtTheRequestLimit(t *testing.T) {
	s := newBoxScenario(t)
	s.start(t)
	s.prime(t)
	client, _ := s.dial(t, nil)
	wrote := make(chan int, 1)
	go func() {
		n, _ := io.WriteString(client, strings.Repeat("x", 2*protocol.MaxBoxRequest))
		wrote <- n
	}()
	expectBoxAnswer(t, client, boxmcp.Answer{Refusal: daemon.CannotReach})
	if n := waitBox(t, wrote); n != protocol.MaxBoxRequest+1 {
		t.Errorf("wrote %d bytes before refusal, want %d", n, protocol.MaxBoxRequest+1)
	}
}

// R-ZBW3-7SK1 R-ZD3Z-LKAQ
func TestBoxForwardRefusesBadAnswersAndBoundsReads(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		err    error
		refuse string
	}{
		{name: "forbidden", status: http.StatusForbidden, refuse: daemon.NotActiveUser},
		{name: "old server", status: http.StatusNotFound, refuse: daemon.CannotReach},
		{name: "unavailable", status: http.StatusServiceUnavailable, refuse: daemon.CannotReach},
		{name: "connection lost", err: errors.New("connection\r\nclosed"), refuse: daemon.CannotReach},
		{name: "array", status: http.StatusOK, body: "[]", refuse: daemon.CannotReach},
		{name: "prefix space", status: http.StatusOK, body: " {}", refuse: daemon.CannotReach},
		{name: "suffix space", status: http.StatusOK, body: "{} ", refuse: daemon.CannotReach},
		{name: "carriage return", status: http.StatusOK, body: "{\r}", refuse: daemon.CannotReach},
		{name: "internal newline", status: http.StatusOK, body: "{\n}", refuse: daemon.CannotReach},
		{name: "two objects", status: http.StatusOK, body: "{}{}", refuse: daemon.CannotReach},
		{name: "no body", status: http.StatusOK, refuse: daemon.CannotReach},
		{name: "limit plus one", status: http.StatusOK, body: "{" + strings.Repeat(" ", protocol.MaxBoxAnswer-1) + "}", refuse: daemon.CannotReach},
		{name: "beyond read limit", status: http.StatusOK, body: "{" + strings.Repeat(" ", protocol.MaxBoxAnswer+100) + "}", refuse: daemon.CannotReach},
		{name: "exact limit", status: http.StatusOK, body: "{" + strings.Repeat(" ", protocol.MaxBoxAnswer-2) + "}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newBoxScenario(t)
			body := &countingAnswer{Reader: strings.NewReader(tc.body)}
			s.onBox = func(*http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			}
			s.start(t)
			s.prime(t)
			client, _ := s.dial(t, nil)
			socketLine(t, client, boxRequest(protocol.MethodToolsList, "1")+"\n")
			answer := boxmcp.Answer{Refusal: tc.refuse}
			if tc.refuse == "" {
				answer.Message = json.RawMessage(tc.body)
			}
			expectBoxAnswer(t, client, answer)
			s.cancel()
			waitBox(t, s.done)
			if tc.status == http.StatusOK &&
				(!body.closed || body.count != min(protocol.MaxBoxAnswer+1, len(tc.body))) {
				t.Errorf("body read = %d bytes, closed %t", body.count, body.closed)
			}
			if tc.refuse == daemon.CannotReach {
				if got, prefix := s.diagnostics.String(), "accountsd: "+daemon.DiagForward+": "; !strings.HasPrefix(got, prefix) ||
					strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, "\n") || strings.ContainsRune(got, '\r') {
					t.Errorf("forward stderr = %q", got)
				}
			} else if s.diagnostics.Len() != 0 {
				t.Errorf("forward stderr = %q", &s.diagnostics)
			}
		})
	}
}

type countingAnswer struct {
	io.Reader
	count  int
	closed bool
}

func (r *countingAnswer) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.count += n
	return n, err
}

func (r *countingAnswer) Close() error {
	r.closed = true
	return nil
}

// R-ZEBV-ZC1F
func TestBoxRejectsExtraClientWhileAllSlotsAreHeld(t *testing.T) {
	s := newBoxScenario(t)
	held := make(chan *http.Request, daemon.MaxBoxConns+1)
	release := make(chan struct{})
	message := `{"result":{}}`
	s.onBox = func(req *http.Request) (*http.Response, error) {
		held <- req
		select {
		case <-release:
			return boxHTTPReply(http.StatusOK, message), nil
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	s.start(t)
	s.prime(t)
	clients := make([]net.Conn, daemon.MaxBoxConns)
	for i := range clients {
		clients[i], _ = s.dial(t, nil)
		socketLine(t, clients[i], boxRequest(protocol.MethodToolsList, "1")+"\n")
	}
	for range daemon.MaxBoxConns {
		waitBox(t, held)
	}
	extra, _ := s.dial(t, nil)
	expectBoxAnswer(t, extra, boxmcp.Answer{Refusal: daemon.CannotReach})
	if peers := s.peers.Load(); peers != daemon.MaxBoxConns || len(held) != 0 {
		t.Errorf("uid lookups = %d, extra requests = %d", peers, len(held))
	}
	close(release)
	for _, client := range clients {
		expectBoxAnswer(t, client, boxmcp.Answer{Message: json.RawMessage(message)})
	}
}

// R-3SK5-6HPT R-3TS1-K9GI
func TestBoxTimeoutInterruptsSigningWaitAndNeverSendsRequest(t *testing.T) {
	s := newBoxScenario(t)
	signing, release := make(chan struct{}), make(chan struct{})
	s.onSign = func(ctx context.Context, n int64) error {
		if n == 3 {
			close(signing)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	s.start(t)
	waitBox(t, s.polls)
	s.state(t, activeBoxState(1, ""))
	waitBox(t, signing)
	client, timer := s.dial(t, nil)
	socketLine(t, client, boxRequest(protocol.MethodToolsList, "1")+"\n")
	timer <- time.Time{}
	expectBoxAnswer(t, client, boxmcp.Answer{Refusal: daemon.CannotReach})
	if got := s.signs.Load(); got != 3 {
		t.Errorf("signed %d times while the previous signing was blocked, want three", got)
	}
	close(release)
	waitBox(t, s.polls)
	s.cancel()
	waitBox(t, s.done)
	if got := s.signs.Load(); got != 3 {
		t.Errorf("expired forward signed after the gate opened, signing count = %d", got)
	}
}

// R-3SK5-6HPT R-3TS1-K9GI
func TestBoxTimeoutInterruptsWaitForNextSigningSecond(t *testing.T) {
	s := newBoxScenario(t)
	var block atomic.Bool
	sleep := s.cfg.Sleep
	waiting := make(chan time.Duration, 1)
	s.cfg.Sleep = func(ctx context.Context, d time.Duration) {
		if block.Swap(false) {
			waiting <- d
			<-ctx.Done()
			return
		}
		sleep(ctx, d)
	}
	s.start(t)
	s.prime(t)
	block.Store(true)
	client, timer := s.dial(t, nil)
	socketLine(t, client, boxRequest(protocol.MethodToolsList, "1")+"\n")
	if d := waitBox(t, waiting); d != time.Second {
		t.Errorf("signing pause = %v, want one second", d)
	}
	timer <- time.Time{}
	expectBoxAnswer(t, client, boxmcp.Answer{Refusal: daemon.CannotReach})
	if got := s.signs.Load(); got != 3 {
		t.Errorf("signed %d times, want just the loop's first three", got)
	}
}

// R-3TS1-K9GI R-3RC8-SPZ4 R-KCBP-KCBI
func TestBoxTimeoutAndCancellationInterruptPendingReadOrForward(t *testing.T) {
	for _, phase := range []string{"reading", "forward timeout", "forward cancellation"} {
		t.Run(phase, func(t *testing.T) {
			s := newBoxScenario(t)
			forward := make(chan *http.Request, 1)
			s.onBox = func(req *http.Request) (*http.Response, error) {
				forward <- req
				<-req.Context().Done()
				return nil, req.Context().Err()
			}
			s.start(t)
			s.prime(t)
			client, timer := s.dial(t, nil)
			var request *http.Request
			if phase == "reading" {
				socketLine(t, client, "{")
			} else {
				socketLine(t, client, boxRequest(protocol.MethodToolsList, "1")+"\n")
				request = waitBox(t, forward)
			}
			if phase == "forward cancellation" {
				s.cancel()
			} else {
				timer <- time.Time{}
			}
			expectBoxAnswer(t, client, boxmcp.Answer{Refusal: daemon.CannotReach})
			if request != nil && !errors.Is(request.Context().Err(), context.Canceled) {
				t.Errorf("forward context = %v, want canceled", request.Context().Err())
			}
			s.cancel()
			waitBox(t, s.done)
			waitBox(t, s.listener.closed)
			if !errors.Is(s.result, context.Canceled) {
				t.Errorf("Run = %v, want canceled", s.result)
			}
			if phase != "reading" && !strings.HasPrefix(s.diagnostics.String(), "accountsd: "+daemon.DiagForward+": ") {
				t.Errorf("forward cancellation stderr = %q", &s.diagnostics)
			}
		})
	}
}

type observedWrite struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *observedWrite) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(p)
}

// R-3TS1-K9GI
func TestBoxTimeoutClosesConnectionWhoseAnswerCannotFinish(t *testing.T) {
	s := newBoxScenario(t)
	s.onBox = func(*http.Request) (*http.Response, error) {
		return boxHTTPReply(http.StatusOK, "{}"), nil
	}
	s.start(t)
	s.prime(t)
	started := make(chan struct{})
	client, timer := s.dial(t, func(conn net.Conn) net.Conn { return &observedWrite{Conn: conn, started: started} })
	socketLine(t, client, boxRequest(protocol.MethodToolsList, "1")+"\n")
	waitBox(t, started)
	timer <- time.Time{}
	// A pipe write does not finish until we read it. Wait for the daemon's
	// end to close without receiving the pending write.
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write([]byte("ignored"))
		writeDone <- err
	}()
	if err := waitBox(t, writeDone); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("connection after write began and timed out: %v", err)
	}
	if data, err := io.ReadAll(client); len(data) != 0 || err != nil {
		t.Errorf("closed connection returned %q, %v", data, err)
	}
}

type notifyingWriter struct {
	io.Writer
	writes chan struct{}
}

func (w notifyingWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	select {
	case w.writes <- struct{}{}:
	default:
	}
	return n, err
}

// R-KB3T-6KKT
func TestBoxAcceptFailureIsReportedAndRetriedOrStopsAccepting(t *testing.T) {
	s := newBoxScenario(t)
	writes := make(chan struct{}, 2)
	s.cfg.Stderr = notifyingWriter{Writer: s.cfg.Stderr, writes: writes}
	pauses := make(chan time.Duration, 2)
	sleep := s.cfg.Sleep
	s.cfg.Sleep = func(ctx context.Context, d time.Duration) {
		if d >= daemon.MinBackoff {
			pauses <- d
		}
		sleep(ctx, d)
	}
	s.start(t)
	s.prime(t)
	sendBox(t, s.listener.input, boxAccept{err: errors.New("temporary accept error")})
	waitBox(t, writes)
	if d := waitBox(t, pauses); d != daemon.MinBackoff {
		t.Errorf("accept backoff = %v, want %v", d, daemon.MinBackoff)
	}
	client, _ := s.dial(t, nil)
	socketLine(t, client, "not JSON\n")
	expectBoxAnswer(t, client, boxmcp.Answer{Refusal: daemon.CannotReach})
	sendBox(t, s.listener.input, boxAccept{err: fmt.Errorf("external close: %w", net.ErrClosed)})
	waitBox(t, writes)
	s.state(t, activeBoxState(2, ""))
	req := waitBox(t, s.polls)
	if got := req.URL.Query().Get(protocol.QueryAfter); got != "2" {
		t.Errorf("loop did not continue after external socket closure: after=%q", got)
	}
	s.cancel()
	waitBox(t, s.done)
	if got := s.diagnostics.String(); strings.Count(got, "accountsd: "+daemon.DiagSocket+": ") != 2 || strings.Count(got, "\n") != 2 {
		t.Errorf("accept stderr = %q", got)
	}
	if calls := s.listener.calls.Load(); len(pauses) != 0 || calls != 3 {
		t.Errorf("accept called %d times, extra sleeps %d after permanent closure", calls, len(pauses))
	}
}

// R-KCBP-KCBI
func TestBoxSelfUpdateLetsAcceptedForwardDrainBeforeRunReturns(t *testing.T) {
	s := newBoxScenario(t)
	held := make(chan *http.Request, 1)
	release := make(chan struct{})
	s.onBox = func(req *http.Request) (*http.Response, error) {
		held <- req
		select {
		case <-release:
			return boxHTTPReply(http.StatusOK, "{}"), nil
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	const binary = "\x7fELFnew"
	digest, err := protocol.Digest(strings.NewReader(binary))
	if err != nil {
		t.Fatal(err)
	}
	s.onBinary = func() *http.Response { return boxHTTPReply(http.StatusOK, binary) }
	s.start(t)
	s.prime(t)
	client, _ := s.dial(t, nil)
	socketLine(t, client, boxRequest(protocol.MethodToolsList, "1")+"\n")
	req := waitBox(t, held)
	s.state(t, activeBoxState(2, digest))
	waitBox(t, s.listener.closed)
	select {
	case <-s.done:
		t.Fatal("Run returned before the accepted forward was answered")
	default:
	}
	if err := req.Context().Err(); err != nil {
		t.Fatalf("self-update canceled the accepted forward: %v", err)
	}
	close(release)
	expectBoxAnswer(t, client, boxmcp.Answer{Message: json.RawMessage("{}")})
	waitBox(t, s.done)
	if s.result != nil || s.diagnostics.Len() != 0 {
		t.Errorf("Run = %v, stderr = %q", s.result, &s.diagnostics)
	}
}

// R-3RC8-SPZ4 R-ZBW3-7SK1
func TestBoxForwardNeverFollowsRedirect(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := newBoxScenario(t)
			var posts atomic.Int64
			s.onBox = func(req *http.Request) (*http.Response, error) {
				posts.Add(1)
				if req.URL.Query().Has("redirect") {
					t.Error("forward followed the redirect and sent another POST")
					return boxHTTPReply(http.StatusOK, "{}"), nil
				}
				resp := boxHTTPReply(status, "")
				resp.Header = http.Header{"Location": {protocol.BoxPath + "?redirect=1"}}
				return resp, nil
			}
			s.cfg.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error {
				t.Error("the injected client redirect policy ran for a box forward")
				return nil
			}
			s.start(t)
			s.prime(t)
			client, _ := s.dial(t, nil)
			socketLine(t, client, boxRequest(protocol.MethodToolsCall, "1")+"\n")
			expectBoxAnswer(t, client, boxmcp.Answer{Refusal: daemon.CannotReach})
			if got := posts.Load(); got != 1 {
				t.Errorf("box POSTs = %d, want one", got)
			}
		})
	}
}

// R-3TS1-K9GI
func TestBoxExpiredBeforeFirstWriteRefusesEvenWithAValidAnswerReady(t *testing.T) {
	s := newBoxScenario(t)
	forward := make(chan *http.Request, 1)
	release := make(chan struct{})
	defer close(release)
	s.onBox = func(req *http.Request) (*http.Response, error) {
		forward <- req
		<-release
		return boxHTTPReply(http.StatusOK, "{}"), nil
	}
	s.start(t)
	s.prime(t)
	client, timer := s.dial(t, nil)
	socketLine(t, client, boxRequest(protocol.MethodToolsList, "1")+"\n")
	req := waitBox(t, forward)
	timer <- time.Time{}
	waitBox(t, req.Context().Done())
	release <- struct{}{}
	expectBoxAnswer(t, client, boxmcp.Answer{Refusal: daemon.CannotReach})
}

type refusalDeadlineConn struct {
	net.Conn
	deadline chan time.Time
	writing  chan struct{}
	expire   <-chan struct{}
}

func (c *refusalDeadlineConn) SetWriteDeadline(at time.Time) error {
	c.deadline <- at
	return nil
}

func (c *refusalDeadlineConn) Write([]byte) (int, error) {
	close(c.writing)
	<-c.expire
	return 0, os.ErrDeadlineExceeded
}

// R-3TS1-K9GI R-KCBP-KCBI
func TestBoxExpiredRefusalDoesNotStrandSelfUpdateWhenPeerStopsReading(t *testing.T) {
	s := newBoxScenario(t)
	const binary = "\x7fELFupdate"
	digest, err := protocol.Digest(strings.NewReader(binary))
	if err != nil {
		t.Fatal(err)
	}
	s.onBinary = func() *http.Response { return boxHTTPReply(http.StatusOK, binary) }
	s.start(t)
	s.prime(t)

	deadline := make(chan time.Time, 1)
	writing := make(chan struct{})
	expire := make(chan struct{})
	var fire sync.Once
	defer fire.Do(func() { close(expire) })
	client, timer := s.dial(t, func(conn net.Conn) net.Conn {
		return &refusalDeadlineConn{Conn: conn, deadline: deadline, writing: writing, expire: expire}
	})
	socketLine(t, client, "{")
	timer <- time.Time{}
	if when := waitBox(t, deadline); when.IsZero() {
		t.Fatal("expired refusal has no deadline")
	}
	waitBox(t, writing)
	s.state(t, activeBoxState(2, digest))
	waitBox(t, s.listener.closed)
	select {
	case <-s.done:
		t.Fatal("Run returned while the accepted socket was still writing")
	default:
	}
	fire.Do(func() { close(expire) })
	waitBox(t, s.done)
	if s.result != nil {
		t.Errorf("Run = %v, want successful self-update", s.result)
	}
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("accepted socket remained open: %v", err)
	}
}
