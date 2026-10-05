package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type credentialTransport struct {
	base      http.RoundTripper
	header    string
	t         *testing.T
	forbidden []string
}

func (c *credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	b := []byte{}
	if r.Body != nil {
		var err error
		b, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(b))
	}
	assertNoCredential(c.t, c.forbidden, []byte(r.URL.String()+fmt.Sprint(r.Header)+string(b)))
	r.Header.Set("Authorization", c.header)
	return c.base.RoundTrip(r)
}

type credentialSink struct {
	capture telemetry.Capture
	reject  bool
}

func (s *credentialSink) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(ctx, e)
	if s.reject {
		return errors.New("sink unavailable")
	}
	return nil
}
func assertNoCredential(t *testing.T, secrets []string, b []byte) {
	t.Helper()
	for _, s := range secrets {
		if bytes.Contains(b, []byte(s)) {
			t.Fatalf("credential substring %q found", s)
		}
	}
}

func TestCredentialSequence(t *testing.T) {
	// R-VZA3-2BOY R-BB9G-J9E5 R-BCHC-X14U R-BDP9-ASVJ R-BEX5-OKM8 R-BG52-2CCX
	user, password := strings.Join([]string{"AZBZCZDZ", "EXFXGXHX", "JXKXLXMX", "NXOX"}, ""), strings.Join([]string{"PZQZRZSZ", "TUVWXZYX", "AXBYCXDY", "EXFY"}, "")
	encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	var secrets []string
	for _, s := range []string{user, password, encoded} {
		if len(s) < 16 {
			t.Fatal("short credential")
		}
		for i := 0; i+8 <= len(s); i++ {
			part := s[i : i+8]
			upper := false
			for _, c := range part {
				if c >= 'A' && c <= 'Z' {
					upper = true
				}
			}
			if !upper {
				t.Fatalf("credential lacks uppercase in %q", part)
			}
			secrets = append(secrets, part)
		}
	}
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			h := newHarness(t)
			h.p.Database = filepath.Join(h.root, "separate", "catalog.db")
			mustCLI(t, os.MkdirAll(filepath.Dir(h.p.Database), 0700))
			sink := &credentialSink{reject: reject}
			h.p.Sink = sink
			script := `import os,json,sys,time
raw=open('/proc/self/environ','rb').read()
sys.stdout.buffer.write(raw);sys.stdout.buffer.flush()
out=os.environ['IKIGENBA_OUT_DIR']
os.mkdir(out+'/d')
open(out+'/d/env','wb').write(raw)
open(out+'/env','wb').write(raw)
if json.load(open(os.environ['IKIGENBA_INPUT'])).get('hold'):
    while True: time.sleep(3600)
`
			h.repository(script)
			assertNoCredential(t, secrets, []byte(script+fmt.Sprint(h.environ())+h.repo+h.sha))
			h.start()
			files, e := os.OpenRoot(h.root)
			mustCLI(t, e)
			defer func() { _ = files.Close() }()
			transport := &credentialTransport{base: h.http.Transport, t: t, forbidden: secrets}
			h.http.Transport = transport
			h.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "credential-sequence"}
			for pass, header := range []string{"Bearer " + password, "Basic " + encoded} {
				transport.header = header
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				listed, e := h.client.ListTools(ctx, caller)
				cancel()
				mustCLI(t, e)
				if len(listed) != 9 {
					t.Fatalf("tools: %d", len(listed))
				}
				name, other := fmt.Sprintf("credential-%d", pass), fmt.Sprintf("deleted-%d", pass)
				created := h.create(name)
				scriptID := created["id"].(string)
				h.call("update", map[string]any{"name": name, "ref": "main"})
				h.call("show", map[string]any{"name": name})
				h.call("list", nil)
				run := func(n string, hold bool) string {
					return h.call("run", map[string]any{"name": n, "input": map[string]any{"hold": hold}})["id"].(string)
				}
				ended := run(name, false)
				waitCredentialRun(t, h, ended, "exited")
				checkEnv := func(sid, id string) {
					env := filepath.Join(h.p.Dir, "state", "runs", sid, id, "out", "env")
					deadline := time.NewTimer(10 * time.Second)
					defer deadline.Stop()
					for {
						relative, e := filepath.Rel(h.root, env)
						mustCLI(t, e)
						b, e := files.ReadFile(relative)
						if e == nil {
							assertNoCredential(t, secrets, b)
							return
						}
						if !errors.Is(e, fs.ErrNotExist) {
							t.Fatal(e)
						}
						select {
						case <-deadline.C:
							t.Fatal("environment file absent")
						default:
						}
						h.call("result", map[string]any{"run": id})
					}
				}
				checkEnv(scriptID, ended)
				held := run(name, true)
				waitCredentialRun(t, h, held, "running")
				checkEnv(scriptID, held)
				h.call("cancel", map[string]any{"run": held})
				failed := h.call("run", map[string]any{"name": name, "ref": "missing-branch", "input": map[string]any{"hold": false}})
				if failed["status"] != "failed" {
					t.Fatalf("missing branch: %v", failed)
				}
				h.call("runs", map[string]any{"name": name})
				refused, e := h.result("cancel", map[string]any{"run": ended})
				mustCLI(t, e)
				if !refused.IsError() {
					t.Fatal("cancel ended accepted")
				}
				prefix := "/" + name + "/runs/" + ended
				paths := []string{"/", "/about", "/_appkit/theme.css", "/" + name + "/", "/" + name, "/missing-script/", prefix + "/", prefix, "/" + name + "/runs/run_ffffffffffffffff/", prefix + "/input.json", prefix + "/stdout", prefix + "/stderr", prefix + "/out/", prefix + "/out/d", prefix + "/out/env", prefix + "/out/d/env", "/" + name + "/runs/" + held + "/", "/" + name + "/runs/" + held + "/stdout"}
				for _, path := range paths {
					req, e := http.NewRequest(http.MethodGet, "http://"+h.listener.Addr().String()+path, nil)
					mustCLI(t, e)
					req.Header.Set("X-User-Id", "owner")
					req.Header.Set("X-User-Email", "owner@example.test")
					req.Header.Set("X-Request-Id", "credential-get")
					res, e := h.http.Do(req)
					mustCLI(t, e)
					b, e := io.ReadAll(res.Body)
					mustCLI(t, e)
					mustCLI(t, res.Body.Close())
					assertNoCredential(t, secrets, b)
					if _, ok := res.Header["Set-Cookie"]; ok {
						t.Fatalf("cookie for %s", path)
					}
				}
				second := h.create(other)
				active := run(other, true)
				waitCredentialRun(t, h, active, "running")
				checkEnv(second["id"].(string), active)
				h.call("delete", map[string]any{"name": other})
			}
			h.stop()
			assertNoCredential(t, secrets, []byte(h.stderr.String()))
			for _, event := range sink.capture.Events() {
				b, e := json.Marshal(event)
				mustCLI(t, e)
				assertNoCredential(t, secrets, b)
			}
			for _, root := range []string{h.p.Dir, filepath.Dir(h.p.Database)} {
				mustCLI(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
					if e != nil {
						return e
					}
					if d.Type().IsRegular() {
						relative, e := filepath.Rel(h.root, path)
						if e != nil {
							return e
						}
						b, e := files.ReadFile(relative)
						if e != nil {
							return e
						}
						assertNoCredential(t, secrets, b)
					}
					return nil
				}))
			}
		})
	}
}
func waitCredentialRun(t *testing.T, h *runHarness, id, status string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		v := h.call("result", map[string]any{"run": id})
		if v["status"] == status {
			return
		}
		if v["status"] != "running" {
			t.Fatalf("run %s: %v", id, v)
		}
		select {
		case <-deadline.C:
			t.Fatal("run did not reach " + status)
		default:
		}
	}
}
