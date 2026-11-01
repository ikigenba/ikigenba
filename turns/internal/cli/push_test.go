package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/config"
	"github.com/ikigenba/ikigenba/turns/internal/local"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/push"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/ikigenba/ikigenba/turns/internal/space"
)

func pushToken(t testing.TB) string {
	return fmt.Sprintf("PushToken-%x", sha256.Sum256([]byte(t.Name())))
}

type pushRequest struct {
	ID     int `json:"id"`
	Params struct {
		Arguments struct {
			Tool string `json:"tool"`
			Args struct {
				Sessions []json.RawMessage `json:"sessions"`
			} `json:"args"`
		} `json:"arguments"`
	} `json:"params"`
}
type pushWireSession struct {
	Tool      session.Tool     `json:"tool"`
	ID        string           `json:"id"`
	Host      string           `json:"host"`
	Parent    *session.Ref     `json:"parent"`
	Directory string           `json:"directory"`
	Headless  bool             `json:"headless"`
	Command   *session.Command `json:"command"`
	Turns     []struct {
		Moment    time.Time     `json:"moment"`
		Whose     session.Whose `json:"whose"`
		Model     string        `json:"model"`
		Input     int64         `json:"input"`
		Cached    int64         `json:"cached"`
		Output    int64         `json:"output"`
		Reasoning int64         `json:"reasoning"`
	} `json:"turns"`
}
type pushFake struct {
	t        *testing.T
	requests []pushRequest
	bodies   [][]byte
	sessions [][]pushWireSession
	latest   map[session.Ref]time.Time
	accepted map[session.Ref]map[string]bool
	refuse   func(int, pushRequest) (*http.Response, error)
	hook     func(*http.Request)
}

func (f *pushFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Helper()
	if f.hook != nil {
		f.hook(r)
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	var req pushRequest
	if err = json.Unmarshal(b, &req); err != nil {
		f.t.Fatal(err)
	}
	f.requests = append(f.requests, req)
	f.bodies = append(f.bodies, b)
	if r.URL.String() != space.Endpoint("test.example") || r.Header.Get("Authorization") != "Bearer "+pushToken(f.t) {
		f.t.Fatalf("wrong endpoint/token: %s", r.URL)
	}
	entries := make([]pushWireSession, len(req.Params.Arguments.Args.Sessions))
	for i, raw := range req.Params.Arguments.Args.Sessions {
		if err = json.Unmarshal(raw, &entries[i]); err != nil {
			f.t.Fatal(err)
		}
	}
	f.sessions = append(f.sessions, entries)
	if f.refuse != nil {
		if response, fail := f.refuse(len(f.requests), req); response != nil || fail != nil {
			return response, fail
		}
	}
	answers := []map[string]any{}
	for _, s := range entries {
		ref := session.Ref{Tool: s.Tool, ID: s.ID}
		l := f.latest[ref]
		if req.Params.Arguments.Tool == space.PushTool {
			for _, turn := range s.Turns {
				if f.accepted != nil {
					if f.accepted[ref] == nil {
						f.accepted[ref] = map[string]bool{}
					}
					f.accepted[ref][turn.Moment.Format(time.RFC3339Nano)+string(turn.Whose)] = true
				}
				if turn.Moment.After(l) {
					l = turn.Moment
				}
			}
			if f.latest != nil {
				f.latest[ref] = l
			}
		}
		answer := map[string]any{"tool": s.Tool, "id": s.ID}
		if !l.IsZero() {
			answer["latest"] = l.Format(time.RFC3339Nano)
		}
		answers = append(answers, answer)
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"structuredContent": map[string]any{"sessions": answers}}})
	return pushResponse(200, string(body)), nil
}
func pushResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func pushPaths(t *testing.T, deps seam.Deps) paths.Paths {
	t.Helper()
	p, err := paths.Resolve(deps.Getenv, deps.Etc)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func pushWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile(filepath.Base(path), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(text); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}
func pushSetup(t *testing.T) (seam.Deps, paths.Paths, *pushFake) {
	t.Helper()
	deps, _ := syntheticDeps(t)
	p := pushPaths(t, deps)
	pushWrite(t, filepath.Join(p.Config, paths.ConfigFile), "space=test.example\n")
	pushWrite(t, filepath.Join(p.Config, paths.TokenFile), pushToken(t))
	f := &pushFake{t: t}
	deps.HTTP = &http.Client{Transport: f}
	return deps, p, f
}
func pushClaude(t *testing.T, p paths.Paths, id string, n int) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		line := map[string]any{"type": "user", "timestamp": time.Unix(100+int64(i), 0).UTC().Format(time.RFC3339), "cwd": "/work/" + id, "entrypoint": "sdk-cli", "message": map[string]any{"content": "prompt-private"}}
		raw, _ := json.Marshal(line)
		b.Write(raw)
		b.WriteByte('\n')
	}
	pushWrite(t, filepath.Join(p.Claude, "projects", "work", id+".jsonl"), b.String())
}
func pushCodex(t *testing.T, p paths.Paths, id string, n int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"cwd\":\"/work\"}}\n", id)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "{\"type\":\"token_usage_record\",\"timestamp\":%q,\"payload\":{}}\n", time.Unix(100+int64(i), 0).UTC().Format(time.RFC3339))
	}
	pushWrite(t, filepath.Join(p.Codex, "sessions", "a", "b", "c", "rollout-"+id+".jsonl"), b.String())
}
func pushGrok(t *testing.T, p paths.Paths, id string, n int) {
	t.Helper()
	root := filepath.Join(p.Grok, "sessions", "%2Fwork", id)
	pushWrite(t, filepath.Join(root, "summary.json"), fmt.Sprintf("{\"info\":{\"id\":%q,\"cwd\":\"/work\"}}", id))
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "{\"timestamp\":%d,\"params\":{\"update\":{\"sessionUpdate\":\"user_message_chunk\"}}}\n", 100+i)
	}
	pushWrite(t, filepath.Join(root, "updates.jsonl"), b.String())
}

// R-IF75-DZ0P R-Q2AA-J94S
func pushRun(t *testing.T, deps seam.Deps, args ...string) (int, string, string) {
	t.Helper()
	p, resolveErr := paths.Resolve(deps.Getenv, deps.Etc)
	var before []byte
	var beforeErr error
	if resolveErr == nil {
		before, beforeErr = os.ReadFile(filepath.Join(p.State, paths.LastPushFile))
	}
	code, out, errText := runCLI(t, deps, append([]string{"push"}, args...), strings.NewReader("unused"))
	if strings.Contains(out, pushToken(t)) || strings.Contains(errText, pushToken(t)) {
		t.Fatal("token disclosed")
	}
	if code != ExitOK && resolveErr == nil {
		after, afterErr := os.ReadFile(filepath.Join(p.State, paths.LastPushFile))
		if !bytes.Equal(before, after) || (beforeErr == nil) != (afterErr == nil) || errors.Is(beforeErr, os.ErrNotExist) != errors.Is(afterErr, os.ErrNotExist) {
			t.Fatal("failed run changed last-push")
		}
	}
	return code, out, errText
}
func pushZero(dry bool) string {
	suffix := "pushed"
	if dry {
		suffix = "to push"
	}
	return fmt.Sprintf("claude: 0 sessions, 0 turns %s\ncodex: 0 sessions, 0 turns %s\ngrok: 0 sessions, 0 turns %s\n", suffix, suffix, suffix)
}

// R-ODBB-EQ22 R-OEJ7-SHSR R-OGZ0-K1A5
func TestPushGrammar(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		deps, _ := syntheticDeps(t)
		code, out, errText := pushRun(t, deps, flag)
		want := `Usage: turns push [--dry-run]

Read this home's Claude Code, Codex CLI and Grok CLI session files, ask the
space which turns it already holds, and push the turns it lacks. Session
files are never changed, and no text leaves the machine except a first
command's name and argument.

Options:
  --dry-run    report what would be pushed and push nothing
  -h, --help   print this help
`
		if code != ExitOK || out != want || errText != "" {
			t.Fatalf("%d %q %q", code, out, errText)
		}
	}
	deps, _ := syntheticDeps(t)
	code, out, errText := pushRun(t, deps, "--dry-run=yes")
	if code != ExitUsage || out != "" || errText != "turns: unknown option '--dry-run=yes'\n\nsee 'turns push --help' for usage\n" {
		t.Fatalf("%d %q %q", code, out, errText)
	}
}

// R-35IW-Z5ME R-36QT-CXD3 R-3E27-NJT9 R-PBGI-4ATI R-PCOE-I2K7
func TestPushConfigurationRefusals(t *testing.T) {
	for _, dry := range []bool{false, true} {
		for _, kind := range []string{"home", "relative-home", "config", "control-config", "none", "token", "space", "host"} {
			t.Run(fmt.Sprint(dry)+kind, func(t *testing.T) {
				deps, _ := syntheticDeps(t)
				p := pushPaths(t, deps)
				args := []string{}
				if dry {
					args = append(args, "--dry-run")
				}
				wantCode := ExitNotConfigured
				want := "turns: not configured\n\nrun 'turns configure <space>'\n"
				switch kind {
				case "home":
					deps.Getenv = func(string) string { return "" }
					wantCode = ExitFailed
					want = "turns: HOME is not set\n"
				case "relative-home":
					getenv := deps.Getenv
					deps.Getenv = func(key string) string {
						if key == "HOME" {
							return "relative\nvalue"
						}
						return getenv(key)
					}
					wantCode = ExitFailed
					want = "turns: HOME 'relative\\nvalue' is not an absolute path\n"
				case "config", "control-config":
					configPath := filepath.Join(p.Config, paths.ConfigFile)
					if kind == "control-config" {
						deps.Etc = filepath.Join(deps.Etc, "host\nroot")
						p = pushPaths(t, deps)
						configPath = p.HostConfig
					}
					if err := os.MkdirAll(configPath, 0700); err != nil {
						t.Fatal(err)
					}
					_, err := config.Load(p)
					wantCode = ExitFailed
					want = "turns: could not read the configuration\n\n" + err.Error() + "\n"
				case "token":
					pushWrite(t, filepath.Join(p.Config, paths.TokenFile), pushToken(t))
				case "space":
					pushWrite(t, filepath.Join(p.Config, paths.ConfigFile), "space=test.example\n")
				case "host":
					pushWrite(t, p.HostConfig, "space=test.example\n")
					want = "turns: not configured\n\nrun 'turns configure'\n"
				}
				deps.Hostname = func() (string, error) { t.Fatal("hostname called before configured"); return "", nil }
				before := pushTree(t, p.Home)
				beforeState := pushTree(t, deps.Getenv("XDG_STATE_HOME"))
				code, out, errText := pushRun(t, deps, args...)
				if code != wantCode || out != "" || errText != want {
					t.Fatalf("%d %q %q want %d %q", code, out, errText, wantCode, want)
				}
				if !reflect.DeepEqual(before, pushTree(t, p.Home)) || !reflect.DeepEqual(beforeState, pushTree(t, deps.Getenv("XDG_STATE_HOME"))) {
					t.Fatal("refusal created state")
				}
			})
		}
	}
}
func pushTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	if root == "" {
		return result
	}
	treeRoot, err := os.OpenRoot(root)
	if errors.Is(err, os.ErrNotExist) {
		return result
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = treeRoot.Close() }()
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			result[path] = "directory"
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		b, readErr := treeRoot.ReadFile(relative)
		result[path] = string(b)
		return readErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// R-3FA4-1BJY R-E5Y3-RZLL R-37YP-QP3S R-396M-4GUH R-PNNH-Y08G R-3AEI-I8L6
func TestPushLocalOutcomes(t *testing.T) {
	for _, kind := range []string{"empty", "empty-replace", "empty-dry", "last-directory", "state-file", "hostname-error", "hostname-control", "hostname-empty", "hostname-dot"} {
		t.Run(kind, func(t *testing.T) {
			deps, p, f := pushSetup(t)
			deps.Now = func() time.Time { return time.Date(2026, 2, 3, 4, 5, 6, 789, time.FixedZone("east", 7200)) }
			args := []string{}
			wantCode := ExitOK
			wantOut := pushZero(false)
			wantErr := ""
			switch kind {
			case "empty-replace":
				pushWrite(t, filepath.Join(p.State, paths.LastPushFile), "older contents\nreplace everything\n")
			case "empty-dry":
				args = []string{"--dry-run"}
				wantOut = pushZero(true)
			case "last-directory":
				if err := os.MkdirAll(filepath.Join(p.State, paths.LastPushFile), 0700); err != nil {
					t.Fatal(err)
				}
				wantCode = ExitFailed
			case "state-file":
				pushWrite(t, p.State, "blocked")
				wantCode = ExitFailed
				wantOut = ""
			case "hostname-error":
				deps.Hostname = func() (string, error) { return "", errors.New("hostname-broke") }
				wantCode = ExitFailed
				wantOut = ""
				wantErr = "turns: could not read the host name\n\nhostname-broke\n"
			case "hostname-control":
				deps.Hostname = func() (string, error) { return "", errors.New("host\nname\tfailed") }
				wantCode = ExitFailed
				wantOut = ""
				wantErr = "turns: could not read the host name\n\nhost\\nname\\tfailed\n"
			case "hostname-empty", "hostname-dot":
				host := ""
				if kind == "hostname-dot" {
					host = ".domain"
				}
				deps.Hostname = func() (string, error) { return host, nil }
				wantCode = ExitFailed
				wantOut = ""
				wantErr = "turns: could not read the host name\n"
			}
			code, out, errText := pushRun(t, deps, args...)
			if kind == "state-file" {
				err := os.MkdirAll(p.State, 0700)
				wantErr = fmt.Sprintf("turns: could not lock '%s'\n\n%s\n", filepath.Join(p.State, paths.LockFile), err)
			}
			if kind == "last-directory" {
				err := os.WriteFile(filepath.Join(p.State, paths.LastPushFile), []byte("unused"), 0600)
				wantErr = fmt.Sprintf("turns: could not write '%s'\n\n%s\n", filepath.Join(p.State, paths.LastPushFile), err)
			}
			if code != wantCode || out != wantOut || errText != wantErr || len(f.requests) != 0 {
				t.Fatalf("%d %q %q requests=%d, want %d %q %q", code, out, errText, len(f.requests), wantCode, wantOut, wantErr)
			}
			if kind == "empty" || kind == "empty-replace" {
				b, err := os.ReadFile(filepath.Join(p.State, paths.LastPushFile))
				if err != nil || string(b) != "2026-02-03T02:05:06Z\n" {
					t.Fatalf("last push %q %v", b, err)
				}
			}
			if kind == "empty-dry" {
				if _, err := os.Stat(p.State); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("dry run wrote state")
				}
			}
		})
	}
}

// R-37YP-QP3S R-3AEI-I8L6
func TestPushStateFailurePathsRenderControls(t *testing.T) {
	for _, kind := range []string{"lock", "last-push"} {
		t.Run(kind, func(t *testing.T) {
			deps, _, fake := pushSetup(t)
			getenv := deps.Getenv
			deps.Getenv = func(key string) string {
				if key == "XDG_STATE_HOME" {
					return filepath.Join(getenv(key), "state\nroot")
				}
				return getenv(key)
			}
			p := pushPaths(t, deps)
			path := filepath.Join(p.State, paths.LockFile)
			var failure error
			wantOut := ""
			verb := "lock"
			if kind == "lock" {
				pushWrite(t, p.State, "blocked")
				failure = os.MkdirAll(p.State, 0700)
			} else {
				path = filepath.Join(p.State, paths.LastPushFile)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				failure = os.WriteFile(path, []byte("unused"), 0600)
				verb = "write"
				wantOut = pushZero(false)
			}
			if failure == nil {
				t.Fatal("fixture did not produce the expected filesystem error")
			}
			wantErr := "turns: could not " + verb + " '" + strings.ReplaceAll(path, "\n", "\\n") + "'\n\n" + strings.ReplaceAll(failure.Error(), "\n", "\\n") + "\n"
			code, out, errText := pushRun(t, deps)
			if code != ExitFailed || out != wantOut || errText != wantErr || len(fake.requests) != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q requests=%d; want stdout=%q stderr=%q", code, out, errText, len(fake.requests), wantOut, wantErr)
			}
		})
	}
}

// R-IF75-DZ0P
func TestPushRedactsEchoedTokenAtEachRequestStage(t *testing.T) {
	for _, stage := range []string{"latest", "latest-dry", "push"} {
		for _, kind := range []string{"transport", "401", "503"} {
			t.Run(stage+kind, func(t *testing.T) {
				deps, p, fake := pushSetup(t)
				pushClaude(t, p, "one", 1)
				wantCalls := 1
				if stage == "push" {
					wantCalls = 2
				}
				fake.refuse = func(n int, _ pushRequest) (*http.Response, error) {
					if n != wantCalls {
						return nil, nil
					}
					message := "echo: " + pushToken(t) + "\nrefused\n"
					switch kind {
					case "transport":
						return nil, errors.New(message)
					case "401":
						return pushResponse(401, message), nil
					default:
						return pushResponse(503, message), nil
					}
				}
				var args []string
				wantOut := pushZero(false)
				if stage == "latest-dry" {
					args = []string{"--dry-run"}
					wantOut = ""
				}
				code, out, errText := pushRun(t, deps, args...)
				reason := "the space refused the token"
				detail := "> echo: <token>\n> refused"
				if kind == "503" {
					reason = "the space answered HTTP 503"
				}
				if kind == "transport" {
					reason = "cannot reach " + space.Endpoint("test.example")
					detail = "Post \"" + space.Endpoint("test.example") + "\": echo: <token>\\nrefused\\n"
				}
				wantErr := "turns: push failed: " + reason + "\n\n" + detail + "\n"
				if code != ExitFailed || out != wantOut || errText != wantErr || len(fake.requests) != wantCalls {
					t.Fatalf("code=%d stdout=%q stderr=%q requests=%d; want stderr=%q requests=%d", code, out, errText, len(fake.requests), wantErr, wantCalls)
				}
			})
		}
	}
}

// R-9J5Y-XD2O R-POVE-BRZ5
func TestPushLatestPaging(t *testing.T) {
	for _, sizes := range [][]int{{1, 0}, {500, 0}, {501, 0}, {1001, 0}, {300, 300}} {
		t.Run(fmt.Sprint(sizes), func(t *testing.T) {
			deps, p, f := pushSetup(t)
			for i := 0; i < sizes[0]; i++ {
				pushClaude(t, p, fmt.Sprintf("c%04d", i), 1)
			}
			for i := 0; i < sizes[1]; i++ {
				pushCodex(t, p, fmt.Sprintf("x%04d", i), 1)
			}
			code, _, _ := pushRun(t, deps)
			if code != ExitOK {
				t.Fatal(code)
			}
			expected := local.Read(p).Sessions
			var refs []session.Ref
			sawPush := false
			for i, req := range f.requests {
				if req.Params.Arguments.Tool == space.PushTool {
					sawPush = true
					continue
				}
				if sawPush {
					t.Fatal("latest after push")
				}
				n := len(f.sessions[i])
				if n != min(space.MaxLatest, len(expected)-len(refs)) {
					t.Fatalf("page length %d", n)
				}
				for _, s := range f.sessions[i] {
					refs = append(refs, session.Ref{Tool: s.Tool, ID: s.ID})
				}
			}
			want := make([]session.Ref, len(expected))
			for i, s := range expected {
				want[i] = s.Ref
			}
			if !reflect.DeepEqual(refs, want) {
				t.Fatal("latest refs/order differ")
			}
		})
	}
}

// R-9KDV-B4TD R-PJZS-SP0D R-E4Q7-E7UW R-OFR4-69JG R-M2SD-11Y6
func TestPushPipeline(t *testing.T) {
	for _, host := range []string{"pat-laptop.example.com", "pat-laptop"} {
		for _, mode := range []string{"new", "held", "dry", "dry-repeat"} {
			t.Run(host+mode, func(t *testing.T) {
				deps, p, f := pushSetup(t)
				deps.Hostname = func() (string, error) { return host, nil }
				pushClaude(t, p, "a", 600)
				pushClaude(t, p, "b", 600)
				pushClaude(t, p, "c", 1)
				aPath := filepath.Join(p.Claude, "projects", "work", "a.jsonl")
				aText, readErr := pushRead(aPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				pushWrite(t, filepath.Join(p.Claude, "projects", "work", "a", "subagents", "agent-child.jsonl"), strings.Replace(string(aText), "prompt-private", "<command-name>/task</command-name><command-args>build</command-args>", 1))
				if removeErr := os.Remove(aPath); removeErr != nil {
					t.Fatal(removeErr)
				}
				source := local.Read(p).Sessions
				f.latest = map[session.Ref]time.Time{}
				held := []space.Held{}
				for _, s := range source {
					l := time.Time{}
					if mode == "held" {
						l = s.Turns[len(s.Turns)-1].Moment
						f.latest[s.Ref] = l
					}
					held = append(held, space.Held{Ref: s.Ref, Latest: l})
				}
				batches := push.Batches(push.Select(source, held))
				args := []string{}
				if strings.HasPrefix(mode, "dry") {
					args = []string{"--dry-run"}
					if mode == "dry-repeat" {
						args = append(args, "--dry-run")
					}
				}
				pushWrite(t, filepath.Join(p.State, paths.LastPushFile), "old\n")
				code, out, errText := pushRun(t, deps, args...)
				if code != ExitOK || errText != "" {
					t.Fatalf("%d %q", code, errText)
				}
				wantReport := "claude: 3 sessions, 1201 turns pushed\ncodex: 0 sessions, 0 turns pushed\ngrok: 0 sessions, 0 turns pushed\n"
				if mode == "held" {
					wantReport = pushZero(false)
				}
				if len(args) > 0 {
					wantReport = strings.ReplaceAll(wantReport, "pushed", "to push")
				}
				if out != wantReport {
					t.Fatalf("report %q want %q", out, wantReport)
				}
				if len(args) > 0 {
					if len(f.requests) != 1 {
						t.Fatal("dry push sent mutation")
					}
					b, _ := os.ReadFile(filepath.Join(p.State, paths.LastPushFile))
					if string(b) != "old\n" {
						t.Fatal("dry run changed last-push")
					}
					return
				}
				if len(f.requests) != len(batches)+1 {
					t.Fatal("wrong push count")
				}
				for bi, batch := range batches {
					entries := f.sessions[bi+1]
					if len(entries) != len(batch) {
						t.Fatal("wrong entry count")
					}
					for j, part := range batch {
						s := entries[j]
						want := part.Session
						if s.Host != "pat-laptop" || s.Tool != want.Ref.Tool || s.ID != want.Ref.ID || s.Directory != want.Directory || s.Headless != want.Headless || !reflect.DeepEqual(s.Parent, want.Parent) || !reflect.DeepEqual(s.Command, want.Command) || len(s.Turns) != len(want.Turns) {
							t.Fatal("session metadata/turn count differs")
						}
						for k, turn := range want.Turns {
							got := s.Turns[k]
							if !got.Moment.Equal(turn.Moment) || got.Whose != turn.Whose || got.Model != turn.Model || got.Input != turn.Tokens.Input || got.Cached != turn.Tokens.Cached || got.Output != turn.Tokens.Output || got.Reasoning != turn.Tokens.Reasoning {
								t.Fatal("turn changed")
							}
						}
					}
				}
			})
		}
	}
}

// R-LVGY-QFI0 R-M409-ETOV R-PTQZ-UUXX R-9LLR-OWK2
func TestPushReportPrivacyAndUntouchedHome(t *testing.T) {
	deps, p, f := pushSetup(t)
	pushClaude(t, p, "a", 9)
	pushCodex(t, p, "b", 2)
	pushCodex(t, p, "c", 3)
	pushGrok(t, p, "d", 1)
	pushWrite(t, filepath.Join(p.Claude, "projects", "work", "broken.jsonl"), "not-json\n")
	for _, name := range []string{"bad-a", "bad-b"} {
		pushWrite(t, filepath.Join(p.Codex, "sessions", "a", "b", "c", "rollout-"+name+".jsonl"), "null\n")
	}
	content := `{"type":"user","timestamp":"1970-01-01T00:01:40Z","cwd":"/work/a","message":{"content":"prompt-private"}}
{"type":"assistant","timestamp":"1970-01-01T00:01:41Z","message":{"id":"message","model":"model","content":[{"type":"text","text":"answer-private"},{"type":"thinking","thinking":"thought-private"},{"type":"tool_use","input":{"x":"call-private"}}]}}
{"type":"user","timestamp":"1970-01-01T00:01:42Z","message":{"content":[{"type":"tool_result","content":"result-private"}]}}
`
	// A separate private fixture keeps the report example's nine-turn total intact.
	path := filepath.Join(p.Claude, "projects", "work", "a.jsonl")
	old, readErr := pushRead(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	lines := strings.Split(string(old), "\n")
	pushWrite(t, path, content+strings.Join(lines[3:], "\n"))
	before := pushTree(t, p.Home)
	code, out, errText := pushRun(t, deps)
	want := "claude: 1 session, 9 turns pushed\nclaude: 1 session skipped\ncodex: 2 sessions, 5 turns pushed\ncodex: 2 sessions skipped\ngrok: 1 session, 1 turn pushed\n"
	if code != ExitOK || out != want || errText != "" {
		t.Fatalf("%d %q %q", code, out, errText)
	}
	if !reflect.DeepEqual(before, pushTree(t, p.Home)) {
		t.Fatal("home/session files changed")
	}
	for _, body := range f.bodies {
		for _, marker := range []string{"prompt-private", "answer-private", "thought-private", "call-private", "result-private"} {
			if bytes.Contains(body, []byte(marker)) {
				t.Fatalf("request leaked %s", marker)
			}
		}
	}
	// An absent tool override remains absent even on a failed or dry run.
	deps2, p2, f2 := pushSetup(t)
	grokMissing := filepath.Join(p2.Home, "missing-grok")
	getenv := deps2.Getenv
	deps2.Getenv = func(key string) string {
		if key == "GROK_HOME" {
			return grokMissing
		}
		return getenv(key)
	}
	for _, args := range [][]string{nil, {"--dry-run"}} {
		for _, fail := range []bool{false, true} {
			pushClaude(t, p2, "a", 1)
			f2.refuse = nil
			if fail {
				f2.refuse = func(int, pushRequest) (*http.Response, error) { return nil, errors.New("offline") }
			}
			before := pushTree(t, p2.Home)
			pushRun(t, deps2, args...)
			if !reflect.DeepEqual(before, pushTree(t, p2.Home)) {
				t.Fatal("absent tool or home changed")
			}
			for _, missing := range []string{p2.Codex, grokMissing, p2.Jobs} {
				if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("created absent tool root")
				}
			}
		}
	}
}

type pushBrokenBody struct{}

func (pushBrokenBody) Read([]byte) (int, error) { return 0, errors.New("") }
func (pushBrokenBody) Close() error             { return nil }

// R-3BME-W0BV R-3IXT-6MS1
func TestPushLatestFailures(t *testing.T) {
	cases := []struct {
		name, reason, detail string
		quoted               bool
		answer               func(pushRequest) (*http.Response, error)
	}{
		{"network", "cannot reach " + space.Endpoint("test.example"), "", false, func(pushRequest) (*http.Response, error) { return nil, errors.New("offline") }},
		{"network-lines", "cannot reach " + space.Endpoint("test.example"), "", false, func(pushRequest) (*http.Response, error) { return nil, errors.New("net\ndown") }},
		{"empty-read", "cannot reach " + space.Endpoint("test.example"), "", false, func(pushRequest) (*http.Response, error) {
			r := pushResponse(200, "")
			r.Body = pushBrokenBody{}
			return r, nil
		}},
		{"unauthorized", "the space refused the token", "refused\n\n", true, func(pushRequest) (*http.Response, error) { return pushResponse(401, "refused\n\n"), nil }},
		{"raw-output", "the space refused the token", "raw\t\x1b\x7f\x80\r\nsecond\n", true, func(pushRequest) (*http.Response, error) {
			return pushResponse(401, "raw\t\x1b\x7f\x80\r\nsecond\n"), nil
		}},
		{"newline-body", "the space refused the token", "\n\n", true, func(pushRequest) (*http.Response, error) { return pushResponse(401, "\n\n"), nil }},
		{"unreadable", "the space's answer could not be read", "garbage", true, func(pushRequest) (*http.Response, error) { return pushResponse(200, "garbage"), nil }},
		{"rpc", "the gateway refused the request", "gateway message", true, func(r pushRequest) (*http.Response, error) {
			return pushResponse(200, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"message":"gateway message"}}`, r.ID)), nil
		}},
		{"tool", "sessions refused the query", "tool message", true, func(r pushRequest) (*http.Response, error) {
			return pushResponse(200, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"isError":true,"content":[{"type":"text","text":"tool message"}]}}`, r.ID)), nil
		}},
	}
	for _, tt := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(tt.name+fmt.Sprint(dry), func(t *testing.T) {
				deps, p, f := pushSetup(t)
				pushClaude(t, p, "a", 1)
				pushWrite(t, filepath.Join(p.State, paths.LastPushFile), "unchanged")
				f.refuse = func(_ int, r pushRequest) (*http.Response, error) { return tt.answer(r) }
				args := []string{}
				if dry {
					args = []string{"--dry-run"}
				}
				code, out, errText := pushRun(t, deps, args...)
				wantOut := pushZero(false)
				if dry {
					wantOut = ""
				}
				detail := tt.detail
				if tt.quoted {
					detail = seam.QuoteOutput(detail)
				}
				if strings.HasPrefix(tt.name, "network") {
					message := "offline"
					if tt.name == "network-lines" {
						message = "net\\ndown"
					}
					want := "turns: push failed: " + tt.reason + "\n\n" + (&url.Error{Op: "Post", URL: space.Endpoint("test.example"), Err: errors.New(message)}).Error() + "\n"
					if errText != want {
						t.Fatalf("%q want %q", errText, want)
					}
				} else {
					want := "turns: push failed: " + tt.reason + "\n"
					if detail != "" {
						want += "\n" + detail + "\n"
					}
					if errText != want {
						t.Fatalf("%q want %q", errText, want)
					}
				}
				if code != ExitFailed || out != wantOut || len(f.requests) != 1 {
					t.Fatalf("%d %q %d", code, out, len(f.requests))
				}
			})
		}
	}
	t.Run("second-query", func(t *testing.T) {
		deps, p, f := pushSetup(t)
		for i := 0; i < 501; i++ {
			pushClaude(t, p, fmt.Sprintf("s%04d", i), 1)
		}
		f.refuse = func(n int, _ pushRequest) (*http.Response, error) {
			if n == 2 {
				return pushResponse(401, ""), nil
			}
			return nil, nil
		}
		code, out, errText := pushRun(t, deps)
		if code != ExitFailed || out != pushZero(false) || errText != "turns: push failed: the space refused the token\n" || len(f.requests) != 2 {
			t.Fatalf("%d %q %q %d", code, out, errText, len(f.requests))
		}
	})
}

// R-3GI0-F3AN R-Y7TK-KUOP
func TestPushPartialRetryAndStateIndependence(t *testing.T) {
	deps, p, f := pushSetup(t)
	pushClaude(t, p, "a", 2500)
	f.latest = map[session.Ref]time.Time{}
	f.accepted = map[session.Ref]map[string]bool{}
	f.refuse = func(_ int, r pushRequest) (*http.Response, error) {
		if r.Params.Arguments.Tool == space.PushTool && len(f.requests) == 3 {
			return pushResponse(401, "batch refused"), nil
		}
		return nil, nil
	}
	code, out, errText := pushRun(t, deps)
	if code != ExitFailed || out != "claude: 1 session, 1000 turns pushed\ncodex: 0 sessions, 0 turns pushed\ngrok: 0 sessions, 0 turns pushed\n" || errText != "turns: push failed: the space refused the token\n\n> batch refused\n" || len(f.requests) != 3 {
		t.Fatalf("%d %q %q %d", code, out, errText, len(f.requests))
	}
	f.refuse = nil
	f.requests = nil
	f.bodies = nil
	f.sessions = nil
	// A prior state file has no bearing on which turns the space asks for.
	pushWrite(t, filepath.Join(p.State, paths.LastPushFile), "prior-run\n")
	held := make(map[session.Ref]time.Time)
	for ref, l := range f.latest {
		held[ref] = l
	}
	code, out, _ = pushRun(t, deps)
	if code != ExitOK || out != "claude: 1 session, 1500 turns pushed\ncodex: 0 sessions, 0 turns pushed\ngrok: 0 sessions, 0 turns pushed\n" {
		t.Fatalf("retry %d %q", code, out)
	}
	if len(f.accepted[session.Ref{Tool: session.ToolClaude, ID: "a"}]) != 2500 {
		t.Fatal("retry did not leave every turn exactly once in the space")
	}
	retryBodies := f.bodies
	// Restore identical server answers and compare against a run without State.
	absent := filepath.Join(filepath.Dir(p.State), "fresh-state")
	getenv := deps.Getenv
	deps.Getenv = func(key string) string {
		if key == "XDG_STATE_HOME" {
			return absent
		}
		return getenv(key)
	}
	f.requests = nil
	f.bodies = nil
	f.sessions = nil
	f.latest = held
	code, _, _ = pushRun(t, deps)
	if code != ExitOK || !reflect.DeepEqual(f.bodies, retryBodies) {
		t.Fatal("requests depended on state")
	}
	if got := f.latest[session.Ref{Tool: session.ToolClaude, ID: "a"}]; !got.Equal(time.Unix(2599, 0)) {
		t.Fatalf("space missing final turn: %v", got)
	}
}

// R-OFR4-69JG
func TestPushRepeatedDryRunIsIdentical(t *testing.T) {
	deps, p, f := pushSetup(t)
	pushClaude(t, p, "a", 3)
	code, out, errText := pushRun(t, deps, "--dry-run")
	first := append([][]byte(nil), f.bodies...)
	f.requests = nil
	f.bodies = nil
	f.sessions = nil
	code2, out2, errText2 := pushRun(t, deps, "--dry-run", "--dry-run")
	if code != code2 || out != out2 || errText != errText2 || !reflect.DeepEqual(first, f.bodies) {
		t.Fatal("repeated dry-run changed behavior")
	}
}

// R-396M-4GUH R-PNNH-Y08G
func TestPushDryRunHostnameFailures(t *testing.T) {
	for _, kind := range []string{"error", "control-error", "empty", "dot"} {
		for _, previous := range []bool{false, true} {
			t.Run(fmt.Sprint(kind, previous), func(t *testing.T) {
				deps, p, f := pushSetup(t)
				if previous {
					pushWrite(t, filepath.Join(p.State, paths.LastPushFile), "earlier\n")
				}
				want := "turns: could not read the host name\n"
				deps.Hostname = func() (string, error) {
					switch kind {
					case "error":
						return "", errors.New("hostname-broke")
					case "control-error":
						return "", errors.New("host\nname\tfailed")
					case "dot":
						return ".domain", nil
					}
					return "", nil
				}
				if kind == "error" {
					want += "\nhostname-broke\n"
				}
				if kind == "control-error" {
					want += "\nhost\\nname\\tfailed\n"
				}
				code, out, errText := pushRun(t, deps, "--dry-run")
				if code != ExitFailed || out != "" || errText != want || len(f.requests) != 0 {
					t.Fatalf("%d %q %q", code, out, errText)
				}
			})
		}
	}
}

// R-M409-ETOV
func TestPushSkippedLinesInFailureReports(t *testing.T) {
	for _, stage := range []string{"query", "batch"} {
		t.Run(stage, func(t *testing.T) {
			deps, p, f := pushSetup(t)
			pushClaude(t, p, "a", 1001)
			pushWrite(t, filepath.Join(p.Claude, "projects", "work", "bad.jsonl"), "null\n")
			for _, name := range []string{"bad-a", "bad-b"} {
				pushWrite(t, filepath.Join(p.Codex, "sessions", "a", "b", "c", "rollout-"+name+".jsonl"), "null\n")
			}
			f.refuse = func(n int, _ pushRequest) (*http.Response, error) {
				if (stage == "query" && n == 1) || (stage == "batch" && n == 3) {
					return pushResponse(401, "refused"), nil
				}
				return nil, nil
			}
			code, out, errText := pushRun(t, deps)
			count := "0 sessions, 0 turns"
			if stage == "batch" {
				count = "1 session, 1000 turns"
			}
			want := "claude: " + count + " pushed\nclaude: 1 session skipped\ncodex: 0 sessions, 0 turns pushed\ncodex: 2 sessions skipped\ngrok: 0 sessions, 0 turns pushed\n"
			if code != ExitFailed || out != want || errText != "turns: push failed: the space refused the token\n\n> refused\n" {
				t.Fatalf("%d %q %q", code, out, errText)
			}
		})
	}
}

func pushRead(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.Base(path))
}
