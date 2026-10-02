package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
)

func waitReady(t *testing.T, notify *net.UnixConn) {
	t.Helper()
	if err := notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var packet [32]byte
	n, _, err := notify.ReadFromUnix(packet[:])
	if err != nil || string(packet[:n]) != "READY=1" {
		t.Fatalf("ready=%q error=%v", packet[:n], err)
	}
}

type servingRun struct {
	client   *http.Client
	endpoint string
	cancel   context.CancelFunc
	result   chan int
	stdout   bytes.Buffer
	stderr   writeLines
	fds      []uintptr
	trail    *testTrail
}

type writeLines struct {
	bytes.Buffer
	lines []string
}

func (w *writeLines) Write(p []byte) (int, error) {
	w.lines = append(w.lines, string(p))
	return w.Buffer.Write(p)
}

func startRun(t *testing.T, srv *testTrail) *servingRun {
	return startConfiguredRun(t, srv, nil)
}

func startConfiguredRun(t *testing.T, srv *testTrail, configure func(*Process)) *servingRun {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	path, notify := readySocket(t)
	ctx, cancelCause := context.WithCancelCause(context.Background())
	cancel := func() { cancelCause(errors.New("explicit run cancellation")) }
	run := &servingRun{client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: "http://" + ln.Addr().String(), cancel: cancel, result: make(chan int, 1), trail: srv}
	t.Cleanup(cancel)
	p := Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(fd uintptr) (net.Listener, error) { run.fds = append(run.fds, fd); return ln, nil }, Banner: emptyBanner, MCP: srv.server, Telemetry: srv.writer, Rand: testWidgetRand(), Stdout: &run.stdout, Stderr: &run.stderr}
	if configure != nil {
		configure(&p)
	}
	go func() { run.result <- Run(ctx, p) }()

	waitReady(t, notify)
	return run
}

func (run *servingRun) request(t *testing.T, method, path, body, user, requestID string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, run.endpoint+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-Id", user)
	req.Header.Set("X-Request-Id", requestID)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := run.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(data)
}
func (run *servingRun) stop(t *testing.T) {
	t.Helper()
	// A live Run must not have returned after responding to requests.
	select {
	case code := <-run.result:
		t.Fatalf("Run returned before cancellation: %d", code)
	default:
	}
	started := time.Now()
	run.cancel()
	select {
	case code := <-run.result:
		if code != ExitSuccess {
			t.Errorf("exit=%d stderr=%q", code, run.stderr.String())
		}
	case <-time.After(6 * time.Second):
		t.Fatal("idle Run failed to stop before default drain")
	}
	if time.Since(started) >= 5*time.Second {
		t.Error("idle stop exceeded drain")
	}
	if !reflect.DeepEqual(run.fds, []uintptr{3}) {
		t.Errorf("inherited descriptors=%v", run.fds)
	}
	if run.stdout.Len() != 0 {
		t.Errorf("stdout=%q", run.stdout.String())
	}
}

type servedWidget struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Status string `json:"status"`
}

func runTool(t *testing.T, run *servingRun, name string, args json.RawMessage) mcp.Result {
	t.Helper()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: run.endpoint + "/mcp", HTTPClient: run.client})
	result, err := client.CallTool(context.Background(), identity.Caller{UserID: "test-user"}, name, args)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError() {
		t.Fatalf("tool %s failed: %+v", name, result)
	}
	return result
}
func runWidgets(t *testing.T, run *servingRun) []servedWidget {
	t.Helper()
	data, err := runTool(t, run, "list_widgets", nil).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		StructuredContent struct {
			Widgets []servedWidget `json:"widgets"`
		} `json:"structuredContent"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result.StructuredContent.Widgets
}

// R-DPQ2-9T5Q R-E6T1-SZSX R-KEWX-FE96 R-P9B9-6C49 R-PAJ5-K3UY R-PBR1-XVLN R-IXZD-5HBP
func TestRunSharesWidgetsAndRestartsFresh(t *testing.T) {
	for range 2 {
		run := startRun(t, testMCP(t))
		want := []servedWidget{{"alpha", 3, "active"}, {"beta", 0, "paused"}, {"gamma", 12, "retired"}}
		for range 2 {
			if got := runWidgets(t, run); !reflect.DeepEqual(got, want) {
				t.Fatalf("initial widgets=%+v", got)
			}
		}
		code, _ := run.request(t, http.MethodPost, "/widgets", "name=form-widget&count=4&status=paused", "test-user", "")
		if code != http.StatusSeeOther {
			t.Fatalf("form status=%d", code)
		}
		want = append(want, servedWidget{"form-widget", 4, "paused"})
		if got := runWidgets(t, run); !reflect.DeepEqual(got, want) {
			t.Fatalf("after form widgets=%+v", got)
		}
		created := runTool(t, run, "create_widget", json.RawMessage(`{"name":"tool-widget","count":7,"status":"retired"}`))
		data, err := created.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err = json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		if _, present := object["isError"]; present {
			t.Fatal("successful create has isError member")
		}
		code, body := run.request(t, http.MethodGet, "/widgets/table", "", "test-user", "")
		rows := regexp.MustCompile(`(?is)<tr(?:\s[^>]*)?>.*?</tr\s*>`).FindAllString(body, -1)
		if code != http.StatusOK || len(rows) == 0 {
			t.Fatalf("table status=%d body=%q", code, body)
		}
		last := rows[len(rows)-1]
		cells := regexp.MustCompile(`(?is)<td(?:\s[^>]*)?>(.*?)</td\s*>`).FindAllStringSubmatch(last, -1)
		if len(cells) < 3 {
			t.Fatalf("last row lacks cells: %q", last)
		}
		tags := regexp.MustCompile(`<[^>]*>`)
		for i, wantText := range []string{"tool-widget", "7", "retired"} {
			got := strings.Join(strings.Fields(tags.ReplaceAllString(cells[i][1], "")), " ")
			if got != wantText {
				t.Errorf("last row cell %d=%q want %q", i, got, wantText)
			}
		}
		if !strings.Contains(last, `class="num"`) || !strings.Contains(cells[2][1], `class="status"`) || !strings.Contains(cells[2][1], `data-status="retired"`) {
			t.Errorf("last row missing widget hooks: %q", last)
		}

		want = append(want, servedWidget{"tool-widget", 7, "retired"})
		if got := runWidgets(t, run); !reflect.DeepEqual(got, want) {
			t.Fatalf("after tool widgets=%+v", got)
		}
		run.stop(t)
		if run.stderr.Len() != 0 {
			t.Errorf("stderr=%q", run.stderr.String())
		}
	}
}

type retryListener struct {
	net.Listener
	once    sync.Once
	retried chan struct{}
}

func (l *retryListener) Accept() (net.Conn, error) {
	retry := false
	l.once.Do(func() { retry = true; close(l.retried) })
	if retry {
		return nil, temporaryAcceptError{}
	}
	return l.Listener.Accept()
}

type temporaryAcceptError struct{}

func (temporaryAcceptError) Error() string   { return "retry accept" }
func (temporaryAcceptError) Temporary() bool { return true }
func (temporaryAcceptError) Timeout() bool   { return false }

// R-DOI5-W1F1
func TestRunDiscardsRetryAndPanicLogs(t *testing.T) {
	srv := testMCP(t)
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	ln := &retryListener{Listener: base, retried: make(chan struct{})}
	path, notify := readySocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	result := make(chan int, 1)
	go func() {
		result <- Run(ctx, Process{Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: func(page.User) page.Banner { panic("banner failed") }, MCP: srv.server, Telemetry: srv.writer, Rand: testWidgetRand(), Stdout: &stdout, Stderr: &stderr})
	}()
	waitReady(t, notify)
	<-ln.retried
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+base.Addr().String()+"/widgets", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-Id", "test-user")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("panic request unexpectedly answered")
	}
	started := time.Now()
	cancel()
	select {
	case code := <-result:
		if code != ExitSuccess {
			t.Errorf("exit=%d", code)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Run did not stop")
	}
	if time.Since(started) >= 5*time.Second {
		t.Error("idle stop exceeded drain")
	}
	if logs.Len() != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("log=%q stdout=%q stderr=%q", logs.String(), stdout.String(), stderr.String())
	}
}

func assertNoNotification(t *testing.T, notify *net.UnixConn) {
	t.Helper()
	if err := notify.SetReadDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	var packet [32]byte
	_, _, err := notify.ReadFromUnix(packet[:])
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("unexpected notification, read error=%v", err)
	}
}

func testWidgetRand() io.Reader {
	source := make([]byte, 1024)
	for i := range 128 {
		source[i*8] = byte(i)
	}
	return bytes.NewReader(source)
}
