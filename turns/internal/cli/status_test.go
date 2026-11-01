package cli

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/config"
	"github.com/ikigenba/ikigenba/turns/internal/local"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/ikigenba/ikigenba/turns/internal/space"
)

func statusToken(t testing.TB) string {
	return fmt.Sprintf("StatusToken-%x", sha256.Sum256([]byte(t.Name())))
}

type statusRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		Name      string `json:"name"`
		Arguments struct {
			Service string `json:"service"`
			Tool    string `json:"tool"`
			Args    struct {
				Sessions []struct {
					Tool session.Tool `json:"tool"`
					ID   string       `json:"id"`
				} `json:"sessions"`
			} `json:"args"`
		} `json:"arguments"`
	} `json:"params"`
}
type statusFake struct {
	t        *testing.T
	requests [][]session.Ref
	latest   map[session.Ref]time.Time
	fail     func(int) (*http.Response, error)
}

func (f *statusFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Helper()
	var req statusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Fatal(err)
	}
	if r.URL.String() != space.Endpoint("test.example") || r.Header.Get("Authorization") != "Bearer "+statusToken(f.t) || req.Method != "tools/call" || req.Params.Name != "call" || req.Params.Arguments.Service != "sessions" || req.Params.Arguments.Tool != "get_latest_turns" {
		f.t.Fatalf("unexpected status request: %s %#v", r.URL, req)
	}
	refs := make([]session.Ref, len(req.Params.Arguments.Args.Sessions))
	answers := make([]map[string]any, len(refs))
	for i, s := range req.Params.Arguments.Args.Sessions {
		refs[i] = session.Ref{Tool: s.Tool, ID: s.ID}
		answers[i] = map[string]any{"tool": s.Tool, "id": s.ID}
		if latest := f.latest[refs[i]]; !latest.IsZero() {
			answers[i]["latest"] = latest.UTC().Format(time.RFC3339Nano)
		}
	}
	f.requests = append(f.requests, refs)
	if f.fail != nil {
		if response, err := f.fail(len(f.requests)); response != nil || err != nil {
			return response, err
		}
	}
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"structuredContent": map[string]any{"sessions": answers}}})
	if err != nil {
		f.t.Fatal(err)
	}
	return statusResponse(200, string(raw)), nil
}
func statusResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func statusPaths(t testing.TB, deps seam.Deps) paths.Paths {
	t.Helper()
	p, err := paths.Resolve(deps.Getenv, deps.Etc)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func statusSetup(t *testing.T) (seam.Deps, paths.Paths, *statusFake, string) {
	t.Helper()
	deps, root := syntheticDeps(t)
	p := statusPaths(t, deps)
	writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=test.example\n", 0600)
	writeFixture(t, filepath.Join(p.Config, paths.TokenFile), statusToken(t), 0600)
	fake := &statusFake{t: t, latest: map[session.Ref]time.Time{}}
	deps.HTTP = &http.Client{Transport: fake}
	return deps, p, fake, root
}
func statusSession(t testing.TB, p paths.Paths, tool session.Tool, id string, n int) {
	t.Helper()
	var b strings.Builder
	switch tool {
	case session.ToolClaude:
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "{\"type\":\"user\",\"timestamp\":%q,\"cwd\":\"/work\",\"message\":{\"content\":\"private prompt\"}}\n", time.Unix(100+int64(i), 0).UTC().Format(time.RFC3339))
		}
		writeFixture(t, filepath.Join(p.Claude, "projects", "work", id+".jsonl"), b.String(), 0600)
	case session.ToolCodex:
		fmt.Fprintf(&b, "{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"cwd\":\"/work\"}}\n", id)
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "{\"type\":\"token_usage_record\",\"timestamp\":%q,\"payload\":{}}\n", time.Unix(100+int64(i), 0).UTC().Format(time.RFC3339))
		}
		writeFixture(t, filepath.Join(p.Codex, "sessions", "a", "b", "c", "rollout-"+id+".jsonl"), b.String(), 0600)
	case session.ToolGrok:
		dir := filepath.Join(p.Grok, "sessions", "%2Fwork", id)
		writeFixture(t, filepath.Join(dir, "summary.json"), fmt.Sprintf("{\"info\":{\"id\":%q,\"cwd\":\"/work\"}}", id), 0600)
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "{\"timestamp\":%d,\"params\":{\"update\":{\"sessionUpdate\":\"user_message_chunk\"}}}\n", 100+i)
		}
		writeFixture(t, filepath.Join(dir, "updates.jsonl"), b.String(), 0600)
	}
}

// R-17H0-GD3Q: every status case checks the stored token stays secret.
func statusRun(t testing.TB, deps seam.Deps) (int, string, string) {
	t.Helper()
	code, out, errText := runCLI(t, deps, []string{"status"}, nil)
	if strings.Contains(out, statusToken(t)) || strings.Contains(errText, statusToken(t)) {
		t.Fatal("status disclosed stored token")
	}
	return code, out, errText
}
func statusZero(spaceName, token, moment, suffix string) string {
	return fmt.Sprintf("space: %s\ntoken: %s\nlast push: %s\nclaude: 0 sessions, 0 turns here%s\ncodex: 0 sessions, 0 turns here%s\ngrok: 0 sessions, 0 turns here%s\n", spaceName, token, moment, suffix, suffix, suffix)
}

// R-ZSBO-0289
func TestStatusHelp(t *testing.T) {
	const want = `Usage: turns status

Print the space and token turns uses, when it last pushed, and per tool how
many sessions and turns are on this machine and how many the space holds.
Pushes nothing.

Options:
  -h, --help   print this help
`
	for _, flag := range []string{"--help", "-h"} {
		deps, root := syntheticDeps(t)
		deps.Getenv = func(string) string { return "" }
		before := snapshotTree(t, root)
		input := &recordingReader{}
		code, out, errText := runCLI(t, deps, []string{"status", flag}, input)
		if code != ExitOK || out != want || errText != "" || input.Reads != 0 {
			t.Fatalf("%d %q %q reads=%d", code, out, errText, input.Reads)
		}
		assertTreeEqual(t, before, snapshotTree(t, root))
	}
}

// R-Z3AT-GWJQ R-Z4IP-UOAF
func TestStatusConfigurationErrors(t *testing.T) {
	for _, kind := range []string{"home", "relative-home", "config", "control-config"} {
		t.Run(kind, func(t *testing.T) {
			deps, root := syntheticDeps(t)
			p := statusPaths(t, deps)
			statusSession(t, p, session.ToolClaude, "readable", 1)
			want := "turns: HOME is not set\n"
			if kind == "home" || kind == "relative-home" {
				getenv := deps.Getenv
				deps.Getenv = func(key string) string {
					if key == "HOME" {
						if kind == "relative-home" {
							return "relative\nvalue"
						}
						return ""
					}
					if key == "CLAUDE_CONFIG_DIR" {
						return p.Claude
					}
					return getenv(key)
				}
				if kind == "relative-home" {
					want = "turns: HOME 'relative\\nvalue' is not an absolute path\n"
				}
			} else {
				configPath := filepath.Join(p.Config, paths.ConfigFile)
				if kind == "control-config" {
					deps.Etc = filepath.Join(root, "host\nroot")
					p = statusPaths(t, deps)
					configPath = p.HostConfig
				}
				if err := os.MkdirAll(configPath, 0700); err != nil {
					t.Fatal(err)
				}
				_, err := config.Load(p)
				want = "turns: could not read the configuration\n\n" + err.Error() + "\n"
			}
			before := snapshotTree(t, root)
			code, out, errText := statusRun(t, deps)
			if code != ExitFailed || out != "" || errText != want {
				t.Fatalf("%d %q %q; want %q", code, out, errText, want)
			}
			assertTreeEqual(t, before, snapshotTree(t, root))
		})
	}
}

// R-00UY-OGF4 R-022V-285T R-03AR-FZWI R-0E9U-VXKR R-0LL9-6K0X
func TestStatusConfigurationAndNoSessions(t *testing.T) {
	for _, kind := range []string{"none", "user", "host", "override", "token-only", "configured"} {
		t.Run(kind, func(t *testing.T) {
			deps, root := syntheticDeps(t)
			p := statusPaths(t, deps)
			name := "none"
			token := "none"
			suffix := ""
			switch kind {
			case "host":
				writeFixture(t, p.HostConfig, "space=host.example\n", 0644)
				name = "host.example"
			case "user":
				writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=user.example\n", 0600)
				name = "user.example"
			case "override":
				writeFixture(t, p.HostConfig, "space=host.example\n", 0644)
				writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=user.example\n", 0600)
				name = "user.example"
			case "token-only":
				writeFixture(t, filepath.Join(p.Config, paths.TokenFile), statusToken(t), 0600)
				token = "set"
			case "configured":
				writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=test.example\n", 0600)
				writeFixture(t, filepath.Join(p.Config, paths.TokenFile), statusToken(t), 0600)
				name = "test.example"
				token = "set"
				suffix = "; 0 sessions, 0 turns on the space"
			}
			before := snapshotTree(t, root)
			code, out, errText := statusRun(t, deps)
			if code != ExitOK || errText != "" || out != statusZero(name, token, "never", suffix) {
				t.Fatalf("%d %q %q", code, out, errText)
			}
			assertTreeEqual(t, before, snapshotTree(t, root))
		})
	}
}

// R-1GH5-LM8H R-1HP1-ZDZ6
func TestStatusLastPush(t *testing.T) {
	for _, text := range []string{"absent", "directory", "", "2026-11-01T14:05:00Z\n", "2026-11-01T14:05:00Z", "2026-11-01T14:05:00Z\n\n", "2026-11-01T14:05:00Z\r\n", "2026-11-01T14:05:00Z \n", " 2026-11-01T14:05:00Z", "2026-11-01 14:05:00Z", "2026-11-01T14:05:00.000Z", "2026-11-01T14:05:00+00:00", "2026-13-01T00:00:00Z", "2026-02-30T00:00:00Z"} {
		t.Run(strconvStatusName(text), func(t *testing.T) {
			deps, _ := syntheticDeps(t)
			p := statusPaths(t, deps)
			path := filepath.Join(p.State, paths.LastPushFile)
			if text == "directory" {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if text != "absent" {
				writeFixture(t, path, text, 0600)
			}
			moment := "never"
			if text == "2026-11-01T14:05:00Z\n" || text == "2026-11-01T14:05:00Z" {
				moment = "2026-11-01T14:05:00Z"
			}
			code, out, errText := statusRun(t, deps)
			if code != ExitOK || errText != "" || out != statusZero("none", "none", moment, "") {
				t.Fatalf("%d %q %q", code, out, errText)
			}
		})
	}
}
func strconvStatusName(s string) string { return fmt.Sprintf("%q", s) }

// R-1IWY-D5PV R-0FHR-9PBG R-0GPN-NH25 R-0HXK-18SU R-00UY-OGF4
func TestStatusCountsAndHeldBoundary(t *testing.T) {
	deps, p, f, _ := statusSetup(t)
	statusSession(t, p, session.ToolClaude, "held", 3)
	statusSession(t, p, session.ToolClaude, "unheld", 2)
	statusSession(t, p, session.ToolCodex, "held-before-local", 1)
	statusSession(t, p, session.ToolGrok, "held", 1)
	writeFixture(t, filepath.Join(p.Claude, "projects", "work", "broken.jsonl"), "null\n", 0600)
	f.latest[session.Ref{Tool: session.ToolClaude, ID: "held"}] = time.Unix(101, 0)
	f.latest[session.Ref{Tool: session.ToolCodex, ID: "held-before-local"}] = time.Unix(99, 0)
	f.latest[session.Ref{Tool: session.ToolGrok, ID: "held"}] = time.Unix(100, 0)
	code, out, errText := statusRun(t, deps)
	want := "space: test.example\ntoken: set\nlast push: never\nclaude: 2 sessions, 5 turns here; 1 session, 2 turns on the space\ncodex: 1 session, 1 turn here; 1 session, 0 turns on the space\ngrok: 1 session, 1 turn here; 1 session, 1 turn on the space\n"
	if code != ExitOK || out != want || errText != "" || len(f.requests) != 1 {
		t.Fatalf("%d %q %q requests=%d", code, out, errText, len(f.requests))
	}
}

// R-0AM5-QMCO
func TestStatusCountInflections(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			deps, p, f, _ := statusSetup(t)
			for _, tool := range session.Tools() {
				for i := 0; i < n; i++ {
					id := fmt.Sprintf("s%d", i)
					statusSession(t, p, tool, id, 1)
					f.latest[session.Ref{Tool: tool, ID: id}] = time.Unix(100, 0)
				}
			}
			code, out, errText := statusRun(t, deps)
			wordS, wordT := "sessions", "turns"
			if n == 1 {
				wordS = "session"
				wordT = "turn"
			}
			want := "space: test.example\ntoken: set\nlast push: never\n"
			for _, tool := range session.Tools() {
				want += fmt.Sprintf("%s: %d %s, %d %s here; %d %s, %d %s on the space\n", tool, n, wordS, n, wordT, n, wordS, n, wordT)
			}
			if code != ExitOK || out != want || errText != "" {
				t.Fatalf("%d %q %q", code, out, errText)
			}
		})
	}
}

// R-IVG9-X4YL R-0KDC-SSA8
func TestStatusLatestPagingAndWire(t *testing.T) {
	for _, sizes := range [][2]int{{space.MaxLatest + 1, 0}, {300, 300}} {
		t.Run(fmt.Sprint(sizes), func(t *testing.T) {
			deps, p, f, _ := statusSetup(t)
			for i := 0; i < sizes[0]; i++ {
				statusSession(t, p, session.ToolClaude, fmt.Sprintf("c%04d", i), 1)
			}
			for i := 0; i < sizes[1]; i++ {
				statusSession(t, p, session.ToolCodex, fmt.Sprintf("x%04d", i), 1)
			}
			source := local.Read(p, time.Time{}).Sessions
			want := make([]session.Ref, len(source))
			for i, s := range source {
				want[i] = s.Ref
			}
			code, _, errText := statusRun(t, deps)
			if code != ExitOK || errText != "" {
				t.Fatalf("%d %q", code, errText)
			}
			var refs []session.Ref
			for _, batch := range f.requests {
				if len(batch) != min(space.MaxLatest, len(want)-len(refs)) {
					t.Fatalf("wrong batch length %d", len(batch))
				}
				refs = append(refs, batch...)
			}
			if !reflect.DeepEqual(refs, want) {
				t.Fatalf("latest refs do not match local.Read order")
			}
			if len(f.requests) != 2 {
				t.Fatalf("%d requests", len(f.requests))
			}
			if sizes[1] > 0 {
				if f.requests[0][299].Tool != session.ToolClaude || f.requests[0][300].Tool != session.ToolCodex || len(f.requests[1]) != 100 {
					t.Fatal("cross-tool page wrong")
				}
			}
		})
	}
}

type statusBrokenBody struct{ message string }

func (b statusBrokenBody) Read([]byte) (int, error) { return 0, errors.New(b.message) }
func (statusBrokenBody) Close() error               { return nil }

// R-Z6YI-M7RT R-18OW-U4UF R-0MT5-KBRM
func TestStatusSpaceFailures(t *testing.T) {
	for _, kind := range []string{"network", "network-lines", "network-echo", "read", "empty-read", "401-lines", "401-raw", "401-echo", "401-empty", "401-newlines"} {
		t.Run(kind, func(t *testing.T) {
			deps, p, f, _ := statusSetup(t)
			statusSession(t, p, session.ToolClaude, "one", 1)
			reason := "cannot reach " + space.Endpoint("test.example")
			detail := ""
			network := false
			f.fail = func(int) (*http.Response, error) {
				switch kind {
				case "network":
					return nil, errors.New("offline-marker")
				case "network-lines":
					return nil, errors.New("net\ndown")
				case "network-echo":
					return nil, errors.New("echo: " + statusToken(t))
				case "read", "empty-read":
					r := statusResponse(200, "")
					message := "read-marker"
					if kind == "empty-read" {
						message = ""
					}
					r.Body = statusBrokenBody{message: message}
					return r, nil
				case "401-lines":
					r := statusResponse(401, "first refusal\nsecond refusal\n")
					r.Header.Set("Content-Type", "text/plain")
					return r, nil
				case "401-raw":
					return statusResponse(401, "raw\t\x1b\x7f\x80\r\nsecond\n"), nil
				case "401-echo":
					return statusResponse(401, "echo: "+statusToken(t)+"\nrefused\n"), nil
				case "401-empty":
					return statusResponse(401, ""), nil
				default:
					return statusResponse(401, "\n\n"), nil
				}
			}
			switch kind {
			case "network", "network-lines", "network-echo":
				network = true
			case "read":
				detail = "read-marker"
			case "401-lines":
				reason = "the space refused the token"
				detail = "> first refusal\n> second refusal"
			case "401-raw":
				reason = "the space refused the token"
				detail = "> raw\t\x1b\x7f\x80\r\n> second"
			case "401-echo":
				reason = "the space refused the token"
				detail = "> echo: <token>\n> refused"
			case "401-empty", "401-newlines":
				reason = "the space refused the token"
			}
			code, out, errText := statusRun(t, deps)
			wantOut := "space: test.example\ntoken: set\nlast push: never\nclaude: 1 session, 1 turn here; unknown on the space\ncodex: 0 sessions, 0 turns here; unknown on the space\ngrok: 0 sessions, 0 turns here; unknown on the space\n"
			wantErr := "turns: could not read the space: " + reason + "\n"
			if detail != "" {
				wantErr += "\n" + detail + "\n"
			}
			if network {
				message := "offline-marker"
				if kind == "network-lines" {
					message = "net\\ndown"
				}
				if kind == "network-echo" {
					message = "echo: <token>"
				}
				wantErr = "turns: could not read the space: " + reason + "\n\nPost \"" + space.Endpoint("test.example") + "\": " + message + "\n"
			}
			expectedCalls := 1
			if code != ExitFailed || out != wantOut || errText != wantErr || len(f.requests) != expectedCalls {
				t.Fatalf("%d %q %q requests=%d; want %q", code, out, errText, len(f.requests), wantErr)
			}
		})
	}
}

// R-0MT5-KBRM R-QBTQ-P5KR
func TestStatusSecondFailureStopsPaging(t *testing.T) {
	deps, p, f, _ := statusSetup(t)
	for i := 0; i < 2*space.MaxLatest+1; i++ {
		id := fmt.Sprintf("c%04d", i)
		statusSession(t, p, session.ToolClaude, id, 1)
		f.latest[session.Ref{Tool: session.ToolClaude, ID: id}] = time.Unix(100, 0)
	}
	writeFixture(t, filepath.Join(p.State, paths.LastPushFile), "2026-11-01T14:05:00Z\n", 0600)
	f.fail = func(n int) (*http.Response, error) {
		if n == 2 {
			return statusResponse(401, "second failure"), nil
		}
		return nil, nil
	}
	code, out, errText := statusRun(t, deps)
	want := fmt.Sprintf("space: test.example\ntoken: set\nlast push: 2026-11-01T14:05:00Z\nclaude: %d sessions, %d turns here; unknown on the space\ncodex: 0 sessions, 0 turns here; unknown on the space\ngrok: 0 sessions, 0 turns here; unknown on the space\n", 2*space.MaxLatest+1, 2*space.MaxLatest+1)
	if code != ExitFailed || out != want || errText != "turns: could not read the space: the space refused the token\n\n> second failure\n" || len(f.requests) != 2 {
		t.Fatalf("%d %q %q calls=%d", code, out, errText, len(f.requests))
	}
}

// R-QALU-BDU2
func TestStatusDoesNotWaitForPushLock(t *testing.T) {
	deps, p, _, _ := statusSetup(t)
	writeFixture(t, filepath.Join(p.State, paths.LockFile), "", 0600)
	lock, err := os.OpenFile(filepath.Join(p.State, paths.LockFile), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		code, out, errText := statusRun(t, deps)
		if code != ExitOK || out != statusZero("test.example", "set", "never", "; 0 sessions, 0 turns on the space") || errText != "" {
			t.Errorf("%d %q %q", code, out, errText)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("status waited for push lock")
	}
}

// R-0SWN-H6H3
func TestStatusLeavesEntireTreeUnchanged(t *testing.T) {
	for _, kind := range []string{"configured", "unconfigured", "failed"} {
		t.Run(kind, func(t *testing.T) {
			deps, p, f, root := statusSetup(t)
			for _, tool := range session.Tools() {
				statusSession(t, p, tool, "one", 2)
			}
			writeFixture(t, filepath.Join(p.Jobs, "job", "session-id"), "one\n", 0600)
			writeFixture(t, filepath.Join(p.Claude, "projects", "work", "broken.jsonl"), "[]\n", 0600)
			writeFixture(t, filepath.Join(p.State, paths.LockFile), "unchanged lock", 0640)
			if kind == "unconfigured" {
				if err := os.Remove(filepath.Join(p.Config, paths.ConfigFile)); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "failed" {
				f.fail = func(int) (*http.Response, error) { return nil, errors.New("offline-marker") }
			}
			before := snapshotTree(t, root)
			code, _, _ := statusRun(t, deps)
			expected := ExitOK
			if kind == "failed" {
				expected = ExitFailed
			}
			if code != expected {
				t.Fatalf("exit %d", code)
			}
			assertTreeEqual(t, before, snapshotTree(t, root))
			if _, err := os.Stat(filepath.Join(p.State, paths.LastPushFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("created last-push")
			}
		})
	}
}

// R-0BU2-4E3D
func TestStatusSkippedSessionOneReadable(t *testing.T) {
	deps, p, _, _ := statusSetup(t)
	statusSession(t, p, session.ToolClaude, "readable", 2)
	writeFixture(t, filepath.Join(p.Claude, "projects", "work", "skipped.jsonl"), "null\n", 0600)
	code, out, errText := statusRun(t, deps)
	want := "space: test.example\ntoken: set\nlast push: never\nclaude: 1 session, 2 turns here; 0 sessions, 0 turns on the space\ncodex: 0 sessions, 0 turns here; 0 sessions, 0 turns on the space\ngrok: 0 sessions, 0 turns here; 0 sessions, 0 turns on the space\n"
	if code != ExitOK || out != want || errText != "" {
		t.Fatalf("%d %q %q; want %q", code, out, errText, want)
	}
}
