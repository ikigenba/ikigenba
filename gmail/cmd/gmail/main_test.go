package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/version"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/cli"
	"github.com/ikigenba/ikigenba/gmail/internal/pages"
)

type childResult struct {
	code           int
	stdout, stderr string
}

func command(t *testing.T, binary string, env, args []string) childResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh")
	c.Path = binary
	c.Args = append([]string{binary}, args...)
	c.Env = env
	c.Dir = t.TempDir()
	var out, errout bytes.Buffer
	c.Stdout, c.Stderr = &out, &errout
	err := c.Run()
	if ctx.Err() != nil {
		t.Fatal("child command timed out")
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	return childResult{code, out.String(), errout.String()}
}

type trailEvent struct {
	Name  string         `json:"event"`
	Attrs map[string]any `json:"attrs"`
}

// R-VZJO-4L0F R-W0RK-ICR4 R-W1ZG-W4HT R-W37D-9W8I
// R-W4F9-NNZ7 R-W5N6-1FPW R-W6V2-F7GL
func TestBinary(t *testing.T) {
	t.Setenv(version.CommitVariable, "0123456789abcdef0123456789abcdef")
	t.Setenv(version.ReleaseVariable, "chosen-release")
	t.Setenv(services.Variable, "")
	id := version.Read()
	display := id.String()
	env := []string{version.CommitVariable + "=0123456789abcdef0123456789abcdef", version.ReleaseVariable + "=chosen-release"}
	binary := filepath.Join(t.TempDir(), "gmail")
	build := exec.Command("go", "build")
	build.Args = append(build.Args, "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, test := range []struct {
		args []string
		env  []string
		want childResult
	}{
		{[]string{"--version"}, env, childResult{0, display + "\n", ""}},
		{[]string{"--version"}, []string{}, childResult{0, "\n", ""}},
		{[]string{"manifest"}, env, childResult{0, cli.Manifest, ""}},
		{[]string{"bogus"}, env, childResult{2, "", "gmail: unknown command 'bogus'\n\nsee 'gmail --help' for usage\n"}},
		{nil, env, childResult{2, "", "gmail: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"}},
	} {
		if got := command(t, binary, test.env, test.args); got != test.want {
			t.Fatalf("%v = %+v, want %+v", test.args, got, test.want)
		}
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			short, err := os.MkdirTemp("", "gmail-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(short) }()
			notifyPath := filepath.Join(short, "notify")
			notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = notify.Close() }()
			if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			socket := filepath.Join(short, "gmail")
			ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			fd, err := ln.File()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = fd.Close() }()
			childEnv := append([]string{}, env...)
			childEnv = append(childEnv, "NOTIFY_SOCKET="+notifyPath)
			var received []trailEvent
			trail := make(chan trailEvent, 64)
			if sig == syscall.SIGTERM {
				trailPath := filepath.Join(short, "telemetry")
				tl, err := net.Listen("unix", trailPath)
				if err != nil {
					t.Fatal(err)
				}
				srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var event trailEvent
					if r.Method != http.MethodPost || r.URL.Path != "/ingest" {
						t.Errorf("unexpected trail request %s %s", r.Method, r.URL.Path)
					}
					if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
						t.Error(err)
					}
					trail <- event
					w.WriteHeader(http.StatusNoContent)
				})}
				go func() { _ = srv.Serve(tl) }()
				defer func() { _ = srv.Close() }()
				servicesPath := filepath.Join(short, "services.json")
				data, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "telemetry", "url": "https://trail.example.test", "description": "fixture", "socket": trailPath, "enabled": true, "mcp": false}}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(servicesPath, data, 0600); err != nil {
					t.Fatal(err)
				}
				childEnv = append(childEnv, services.Variable+"="+servicesPath)
			}
			c := exec.Command("/bin/sh", "-c", "LISTEN_PID=$$ LISTEN_FDS=1 exec \"$0\"")
			c.Args = append(c.Args, binary)
			c.Env, c.Dir, c.ExtraFiles = childEnv, t.TempDir(), []*os.File{fd}
			var out, errout bytes.Buffer
			c.Stdout, c.Stderr = &out, &errout
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			wait := make(chan error, 1)
			go func() { wait <- c.Wait() }()
			t.Cleanup(func() { _ = c.Process.Kill() })
			b := make([]byte, 128)
			n, _, err := notify.ReadFromUnix(b)
			if err != nil {
				t.Fatal(err)
			}
			if string(b[:n]) != "READY=1" {
				t.Fatalf("notification = %q", b[:n])
			}
			if stat, err := os.Stat(filepath.Join(c.Dir, "state", "gmail.db")); err != nil || !stat.Mode().IsRegular() {
				t.Fatalf("database missing: %v", err)
			}
			httpClient := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			}}}
			defer httpClient.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://gmail/mcp", HTTPClient: httpClient})
			result, err := client.CallTool(ctx, identity.Caller{UserID: "owner"}, "transfers", json.RawMessage(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError() {
				t.Fatal("transfers refused")
			}
			encoded, err := result.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Meta map[string]json.RawMessage `json:"_meta"`
			}
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			var info map[string]string
			if err := json.Unmarshal(decoded.Meta["io.modelcontextprotocol/serverInfo"], &info); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(info, map[string]string{"name": pages.ServiceName, "version": display}) {
				t.Fatalf("server info = %v", info)
			}
			if sig == syscall.SIGINT {
				r, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://gmail/about", nil)
				if err != nil {
					t.Fatal(err)
				}
				r.Host = "gmail.sbx.ikigenba.dev"
				r.Header.Set("X-Forwarded-Proto", "https")
				r.Header.Set("X-User-Id", "owner")
				r.Header.Set("X-User-Email", "mg@example.com")
				answer, err := httpClient.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(answer.Body)
				_ = answer.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				templates, err := page.Templates().ParseFS(app.Assets(), "*.html")
				if err != nil {
					t.Fatal(err)
				}
				banner := page.New(pages.ServiceName, id).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
				banner.Trail = []page.Level{pages.AboutLevel}
				var expected bytes.Buffer
				if err := templates.ExecuteTemplate(&expected, "about", pages.AboutData{Banner: banner, Description: pages.Description}); err != nil {
					t.Fatal(err)
				}
				if answer.StatusCode != http.StatusOK || !bytes.Equal(body, expected.Bytes()) {
					t.Fatalf("about differs: status %d", answer.StatusCode)
				}
			}
			if err := c.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-wait:
				if err != nil {
					t.Fatalf("stop: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("child did not drain")
			}
			if out.Len() != 0 || (sig == syscall.SIGTERM && errout.Len() != 0) {
				t.Fatalf("serving streams = %q, %q", out.String(), errout.String())
			}
			if sig == syscall.SIGINT {
				lines := strings.Split(strings.TrimSuffix(errout.String(), "\n"), "\n")
				last := lines[len(lines)-1]
				start := strings.IndexByte(last, '{')
				var event trailEvent
				if start < 0 {
					t.Fatalf("missing stop event: %q", last)
				}
				if err := json.Unmarshal([]byte(last[start:]), &event); err != nil {
					t.Fatal(err)
				}
				if event.Name != "service.stopping" || !reflect.DeepEqual(event.Attrs, map[string]any{"reason": "SIGINT"}) {
					t.Fatalf("stop event = %+v", event)
				}
			}
			if sig == syscall.SIGTERM {
				for {
					select {
					case event := <-trail:
						received = append(received, event)
					default:
						goto drained
					}
				}
			drained:
				if len(received) < 2 {
					t.Fatalf("trail count = %d", len(received))
				}
				first, last := received[0], received[len(received)-1]
				if first.Name != "service.started" || !reflect.DeepEqual(first.Attrs, map[string]any{"version": display}) {
					t.Fatalf("first = %+v", first)
				}
				if last.Name != "service.stopping" || !reflect.DeepEqual(last.Attrs, map[string]any{"reason": "SIGTERM"}) {
					t.Fatalf("last = %+v", last)
				}
			}
		})
	}
}
