package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home/internal/cli"
)

type credentialSink struct {
	capture telemetry.Capture
	reject  bool
	created chan string
}

func (s *credentialSink) Deliver(ctx context.Context, event telemetry.Event) error {
	if err := s.capture.Deliver(ctx, event); err != nil {
		return err
	}
	if event.Name == "bookmark.created" {
		s.created <- fmt.Sprint(event.Attrs["bookmark"])
	}
	if s.reject {
		return fmt.Errorf("fixture rejection: %w", telemetry.ErrRejected)
	}
	return nil
}

type credentialRequest struct {
	method, path string
	signed       bool
}

var credentialRequests = []credentialRequest{
	{"GET", "/", false}, {"HEAD", "/", false}, {"POST", "/about", false},
	{"GET", "/", true}, {"HEAD", "/", true}, {"GET", "/about", true},
	{"GET", "/_appkit/theme.css", true}, {"GET", "/missing-fixture", true}, {"POST", "/", true},
	{"GET", "/bookmarks", true}, {"POST", "/bookmarks", true}, {"GET", "/bookmarks/bmk_5a5a5a5a5a5a5a5a/icon", true}, {"POST", "/bookmarks/bmk_5a5a5a5a5a5a5a5a/delete", true}, {"POST", "/bookmarks", true},
}

// R-449G-CKT1 R-MCK6-JYLH R-5TU6-1298 R-KT8X-S0TS R-YSFR-NA2O R-SGJQ-S0NK R-MDS2-XQC6
func TestCredentialSequenceLeavesNoSecrets(t *testing.T) {
	const username = "ghijklmnopqrstuvwxyzZYXWVUTSRQ"
	const password = "qrstuvwxyzGHIJKLMNOPQRSTUVWXYZ"
	const email = "wxyzWXYZghijklmnop@qrstuvwxyzGHIJKLM"
	const referenceEmail = "NNNNPPPPQQQQRRRR@SSSSTTTTUUUUVVVV"
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	values := []string{username, password, encoded, email}
	for _, value := range values {
		validateMarker(t, value)
	}
	if len(username) < 16 || len(password) < 16 || len(email) < 17 || strings.Count(email, "@") != 1 || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		t.Fatal("invalid marker fixture")
	}
	// Every longer substring contains an eight-byte substring, so checking all
	// eight-byte windows detects every leak the contract defines.
	fragments := markerWindows(values)
	for _, r := range credentialRequests {
		assertNoMarkers(t, r.method+" "+r.path+" fixture-user fixture-request", fragments)
	}
	assertNoMarkers(t, referenceEmail, markerWindows([]string{email}))
	dir, err := os.MkdirTemp("", "home-secret-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	notifyPath := filepath.Join(dir, "ready.sock")
	env := map[string]string{"LISTEN_PID": "4242", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notifyPath}
	for _, reject := range []bool{false, true} {
		baseline, baselineStderr, baselineHTTP, baselineTree := runCredentialPasses(t, env, referenceEmail, "", "", reject)
		assertNoMarkers(t, eventText(baseline)+baselineStderr+baselineHTTP+baselineTree, fragments)
		events, stderr, requests, tree := runCredentialPasses(t, env, email, "Bearer "+password, "Basic "+encoded, reject)
		if len(events) == 0 {
			t.Fatal("sink received no events")
		}
		assertNoMarkers(t, eventText(events)+requests+tree, fragments)
		for _, event := range events {
			switch event.Name {
			case "service.started", "service.stopping", "request.started", "request.finished", "bookmark.created", "bookmark.deleted":
			default:
				t.Fatal(event)
			}
		}
		if reject {
			assertNoMarkers(t, stderr, fragments)
		}
	}
}
func validateMarker(t *testing.T, value string) {
	t.Helper()
	for _, window := range markerWindows([]string{value}) {
		marked := false
		for _, c := range window {
			if c >= 'g' && c <= 'z' || c >= 'G' && c <= 'Z' {
				marked = true
			}
		}
		if !marked {
			t.Fatalf("fixture window %q could collide with a hexadecimal id", window)
		}
	}
}
func markerWindows(values []string) []string {
	var fragments []string
	for _, value := range values {
		for i := 0; i+8 <= len(value); i++ {
			fragments = append(fragments, strings.ToLower(value[i:i+8]))
		}
	}
	return fragments
}
func assertNoMarkers(t *testing.T, text string, fragments []string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, fragment := range fragments {
		if strings.Contains(lower, fragment) {
			t.Fatalf("record holds marked substring %q", fragment)
		}
	}
}
func eventText(events []telemetry.Event) string {
	var result strings.Builder
	for _, event := range events {
		fmt.Fprintf(&result, "%s\n%s\n%s\n", event.Name, event.RequestID, event.User)
		for _, value := range event.Attrs {
			fmt.Fprintf(&result, "%v\n", value)
		}
	}
	return result.String()
}
func runCredentialPasses(t *testing.T, env map[string]string, email, bearer, basic string, reject bool) ([]telemetry.Event, string, string, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: env["NOTIFY_SOCKET"], Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = notify.Close()
		if err := os.Remove(env["NOTIFY_SOCKET"]); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(errors.New("fixture cancelled"))
	var stdout, stderr bytes.Buffer
	sink := &credentialSink{reject: reject, created: make(chan string, 8)}
	root := t.TempDir()
	runDir := filepath.Join(root, "run")
	if err := os.Mkdir(runDir, 0700); err != nil {
		t.Fatal(err)
	}
	outgoing := &credentialTransport{}
	servicesPath := filepath.Join(root, "services.json")
	if err := os.WriteFile(servicesPath, []byte(`{"services":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	runEnv := map[string]string{}
	for key, value := range env {
		runEnv[key] = value
	}
	runEnv["IKIGENBA_SERVICES"] = servicesPath
	p := cli.Process{Dir: runDir, Now: credentialNow, Rand: &credentialRandom{}, HTTP: &http.Client{Transport: outgoing}, LookupEnv: func(key string) (string, bool) { value, ok := runEnv[key]; return value, ok }, Pid: 4242, Stdout: &stdout, Stderr: &stderr, Version: "fixture-display", Inherit: func(uintptr) (net.Listener, error) { return listener, nil }, Banner: func(user page.User) page.Banner {
		return page.Banner{Email: user.Email, ProfileURL: user.ProfileURL, LogoutURL: user.LogoutURL}
	}, Sink: sink}
	done := make(chan int, 1)
	go func() { done <- cli.Run(ctx, p) }()
	t.Cleanup(func() { cancel(errors.New("cleanup")); _ = listener.Close() })
	if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var buf [128]byte
	n, _, err := notify.ReadFromUnix(buf[:])
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "READY=1" {
		t.Fatalf("readiness: %q", buf[:n])
	}
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	bookmarkID := ""
	for pass, authorization := range []string{bearer, basic} {
		for i, r := range credentialRequests {
			var body io.Reader
			if r.method == "POST" && r.path == "/bookmarks" {
				body = strings.NewReader(url.Values{"name": {fmt.Sprintf("fixture bookmark %d %d", pass, i)}, "url": {"http://fixture-site.test/path"}, "description": {"fixture detail"}}.Encode())
			}
			request, err := http.NewRequest(r.method, "http://"+listener.Addr().String()+strings.ReplaceAll(r.path, "bmk_5a5a5a5a5a5a5a5a", bookmarkID), body)
			if err != nil {
				t.Fatal(err)
			}
			request.Host = "home.fixture.test"
			if body != nil {
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			request.Header.Set("X-Forwarded-Proto", "https")
			request.Header.Set("X-User-Email", email)
			request.Header.Set("X-Request-Id", "fixture-request-"+strconv.Itoa(i))
			if r.signed {
				request.Header.Set("X-User-Id", "fixture-user")
			}
			if authorization != "" {
				request.Header.Set("Authorization", authorization)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			if body != nil {
				if response.StatusCode != 303 {
					data, _ := io.ReadAll(response.Body)
					t.Fatalf("credential POST failed %d %s", response.StatusCode, data)
				}
				select {
				case bookmarkID = <-sink.created:
				case <-time.After(3 * time.Second):
					t.Fatal("missing creation event")
				}
			}
			if strings.HasSuffix(r.path, "/icon") && response.StatusCode != 200 {
				t.Fatalf("icon status %d", response.StatusCode)
			}
			if strings.HasSuffix(r.path, "/delete") && response.StatusCode != 303 {
				t.Fatalf("delete status %d", response.StatusCode)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil {
				t.Fatalf("response: %v %v", readErr, closeErr)
			}
		}
	}
	cancel(errors.New("fixture stop"))
	select {
	case code := <-done:
		if code != cli.ExitSuccess {
			t.Fatalf("Run exit %d stderr %s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not finish")
	}
	if stdout.Len() != 0 {
		t.Fatalf("serve stdout %q", stdout.String())
	}
	var tree strings.Builder
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		tree.WriteString(relative)
		if entry.Type().IsRegular() {
			data, e := fs.ReadFile(os.DirFS(root), relative)
			if e != nil {
				return e
			}
			tree.Write(data)
		} else if entry.Type()&os.ModeSymlink != 0 {
			target, e := os.Readlink(path)
			if e != nil {
				return e
			}
			tree.WriteString(target)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return sink.capture.Events(), stderr.String(), outgoing.text(), tree.String()
}

func credentialNow() time.Time { return time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC) }

type credentialRandom struct{ next byte }

func (r *credentialRandom) Read(b []byte) (int, error) {
	for i := range b {
		r.next++
		b[i] = r.next
	}
	return len(b), nil
}

type credentialTransport struct {
	mu       sync.Mutex
	requests strings.Builder
}

func (r *credentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests.WriteString(req.URL.String())
	for name, values := range req.Header {
		r.requests.WriteString(name)
		for _, value := range values {
			r.requests.WriteString(value)
		}
	}
	if req.Body != nil {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		r.requests.Write(data)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(strings.NewReader("fixture icon"))}, nil
}
func (r *credentialTransport) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests.String()
}
