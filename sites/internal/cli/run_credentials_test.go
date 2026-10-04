package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
	"github.com/ikigenba/ikigenba/sites/internal/visitor"
)

type credentialRandom struct {
	mu sync.Mutex
	n  byte
}

func (r *credentialRandom) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	for i := range p {
		p[i] = 0x90 + r.n + byte(i)
	}
	return len(p), nil
}

type credentialSink struct {
	capture telemetry.Capture
	fail    bool
	viewed  chan telemetry.Event
}

func (s *credentialSink) Deliver(ctx context.Context, e telemetry.Event) error {
	if err := s.capture.Deliver(ctx, e); err != nil {
		return err
	}
	if e.Name == "site.viewed" && s.viewed != nil {
		s.viewed <- e
	}
	if s.fail {
		return errors.New("delivery refused")
	}
	return nil
}

// Eight-character windows suffice: every longer secret contains one.
func credentialWindows(t *testing.T, values ...string) []string {
	t.Helper()
	var out []string
	for _, value := range values {
		if len(value) < 16 {
			t.Fatal("credential fixture is too short")
		}
		for _, c := range value {
			alphanumeric := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
			if !alphanumeric {
				t.Fatal("credential fixture is not ASCII alphanumeric")
			}
		}
		for i := 0; i+8 <= len(value); i++ {
			window := value[i : i+8]
			if !strings.ContainsAny(window, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
				t.Fatal("credential window lacks an uppercase letter")
			}
			out = append(out, window)
		}
	}
	return out
}

func credentialAbsent(t *testing.T, where string, data []byte, secrets []string) {
	t.Helper()
	for _, secret := range secrets {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("credential secret in %s", where)
		}
	}
}

type credentialTransport struct {
	t       *testing.T
	base    http.RoundTripper
	header  string
	secrets []string
}

func (c credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	credentialAbsent(c.t, "request URL", []byte(r.URL.String()+r.Host), c.secrets)
	for k, values := range r.Header {
		credentialAbsent(c.t, "request header", []byte(k+strings.Join(values, "\n")), c.secrets)
	}
	r = r.Clone(r.Context())
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		if err = r.Body.Close(); err != nil {
			return nil, err
		}
		credentialAbsent(c.t, "request body/tool arguments", b, c.secrets)
		r.Body = io.NopCloser(bytes.NewReader(b))
	}
	r.Header.Set("Authorization", c.header)
	return c.base.RoundTrip(r)
}

func credentialFiles(t *testing.T, roots ...string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			b, err := startReadFile(path)
			if err != nil {
				return err
			}
			out[path] = b
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func credentialDisk(t *testing.T, f *startFixture, repos string) map[string][]byte {
	t.Helper()
	roots := []string{f.p.Dir, repos}
	if f.p.Database != "" {
		for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
			roots = append(roots, f.p.Database+suffix)
		}
	}
	return credentialFiles(t, roots...)
}

func credentialGET(t *testing.T, f *startFixture, path, host string, want int) {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, "http://sites"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Host = host
	r.Header.Set("X-User-Id", "owner")
	r.Header.Set("X-Request-Id", "credential-view")
	r.Header.Set("X-Forwarded-Proto", "http")
	res, err := f.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, res.Body)
	closeErr := res.Body.Close()
	if readErr != nil || closeErr != nil || res.StatusCode != want {
		t.Fatalf("GET %s host %s: status %d, read %v, close %v", path, host, res.StatusCode, readErr, closeErr)
	}
}

// R-DN5E-UIA0 R-DODB-8A0P R-796D-SRTD R-DPL7-M1RE R-FQ1J-ABP3 R-FR9F-O3FS
func TestRunCredentialRouteAndToolMatrix(t *testing.T) {
	user, password := "ABCD1234EFGH5678", "IJKL9012MNOP3456"
	encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	secrets := credentialWindows(t, user, password, encoded)
	for _, basic := range []bool{false, true} {
		for _, failing := range []bool{false, true} {
			for _, external := range []bool{false, true} {
				t.Run(fmt.Sprintf("basic=%v/failing=%v/external=%v", basic, failing, external), func(t *testing.T) {
					f := newStartFixture(t)
					f.p.Rand = &credentialRandom{}
					f.p.Sleep = func(context.Context, time.Duration) {}
					if external {
						f.p.Database = filepath.Join(t.TempDir(), "catalog.db")
					}
					root := t.TempDir()
					sha := startRepo(t, f, root)
					f.set("REPOS_DIR", root)
					for _, value := range f.p.Environ() {
						credentialAbsent(t, "environment", []byte(value), secrets)
					}
					for path, data := range credentialDisk(t, f, root) {
						credentialAbsent(t, "fixture path", []byte(path), secrets)
						credentialAbsent(t, "repository fixture", data, secrets)
					}
					sink := &credentialSink{fail: failing}
					f.p.Sink = sink
					header := "Bearer " + password
					if basic {
						header = "Basic " + encoded
					}
					f.http.Transport = credentialTransport{t: t, base: f.http.Transport, header: header, secrets: secrets}
					f.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
					f.start(t)
					public := f.call(t, "create", `{"name":"blog","repo":"rep_0123456789abcdef","visibility":"public"}`)
					private := f.call(t, "create", `{"name":"private","repo":"rep_0123456789abcdef","visibility":"private"}`)
					f.call(t, "publish", `{"name":"blog"}`)
					f.call(t, "publish", `{"name":"private"}`)
					f.call(t, "list", `{}`)
					f.call(t, "show", `{"name":"blog"}`)
					f.call(t, "update", `{"name":"private","listed":false}`)
					f.call(t, "apex", `{"name":"blog"}`)
					for _, path := range []string{"/", "/about", "/_appkit/theme.css", "/blog/", "/private/"} {
						credentialGET(t, f, path, "sites.example.test", http.StatusOK)
					}
					if err := os.RemoveAll(filepath.Join(f.p.Dir, "cache/sites", private["id"].(string), sha)); err != nil {
						t.Fatal(err)
					}
					credentialGET(t, f, "/private/", "sites.example.test", http.StatusOK)
					credentialGET(t, f, "/no-such-site/", "sites.example.test", http.StatusNotFound)
					credentialGET(t, f, "/", "example.test", http.StatusFound)
					// A failed lazy rebuild exercises the final allowed site event.
					if err := os.RemoveAll(filepath.Join(f.p.Dir, "cache/sites", public["id"].(string), sha)); err != nil {
						t.Fatal(err)
					}
					if err := os.RemoveAll(filepath.Join(root, "rep_0123456789abcdef.git")); err != nil {
						t.Fatal(err)
					}
					credentialGET(t, f, "/blog/", "sites.example.test", http.StatusServiceUnavailable)
					f.call(t, "delete", `{"name":"blog"}`)
					if code := f.stop(t); code != cli.ExitSuccess {
						t.Fatal("exit", code)
					}
					credentialAbsent(t, "stderr", []byte(f.err.text()), secrets)
					for path, data := range credentialDisk(t, f, root) {
						credentialAbsent(t, "persistent file "+path, data, secrets)
					}
					allowed := map[string]bool{"service.started": true, "service.stopping": true, "request.started": true, "request.finished": true, "tool.called": true, "site.created": true, "site.published": true, "site.updated": true, "site.deleted": true, "site.apex": true, "site.viewed": true, "site.unavailable": true}
					seen := map[string]bool{}
					for _, event := range sink.capture.Events() {
						seen[event.Name] = true
						if !allowed[event.Name] {
							t.Fatal("unexpected event", event.Name)
						}
						data, err := event.MarshalJSON()
						if err != nil {
							t.Fatal(err)
						}
						credentialAbsent(t, "event", data, secrets)
						if strings.HasPrefix(event.Name, "site.") && (event.Service != "sites" || !event.Time.Equal(f.p.Now().UTC().Truncate(time.Microsecond))) {
							t.Fatalf("site envelope %#v", event)
						}
					}
					for name := range allowed {
						if !seen[name] {
							t.Fatal("event matrix did not reach", name)
						}
					}
				})
			}
		}
	}
}

// R-UJPS-M0T3
func TestRunVisitorRemainsOnlyInCookieAndTrail(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprintf("external=%v", external), func(t *testing.T) {
			f := newStartFixture(t)
			f.p.Rand = &credentialRandom{}
			if external {
				f.p.Database = filepath.Join(t.TempDir(), "catalog.db")
			}
			root := t.TempDir()
			startRepo(t, f, root)
			f.set("REPOS_DIR", root)
			sink := &credentialSink{viewed: make(chan telemetry.Event, 8)}
			f.p.Sink = sink
			f.start(t)
			f.call(t, "create", `{"name":"blog","repo":"rep_0123456789abcdef"}`)
			f.call(t, "publish", `{"name":"blog"}`)
			before := credentialDisk(t, f, root)
			res := f.get(t, "/blog/")
			_, err := io.Copy(io.Discard, res.Body)
			closeErr := res.Body.Close()
			if err != nil || closeErr != nil || res.StatusCode != http.StatusOK {
				t.Fatal("view", res.StatusCode, err, closeErr)
			}
			id := ""
			for _, cookie := range res.Cookies() {
				if cookie.Name == visitor.CookieName {
					id = cookie.Value
				}
			}
			if !visitor.ValidID(id) {
				t.Fatal("visitor cookie missing or invalid")
			}
			select {
			case event := <-sink.viewed:
				if event.Attrs["visitor"] != id {
					t.Fatal("cookie not in trail", event)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("view event not delivered")
			}
			hexID := strings.TrimPrefix(id, visitor.IDPrefix)
			raw, err := hex.DecodeString(hexID)
			if err != nil {
				t.Fatal(err)
			}
			for path, data := range credentialDisk(t, f, root) {
				for _, value := range [][]byte{[]byte(id), []byte(hexID), raw} {
					if !bytes.Contains(before[path], value) && bytes.Contains(data, value) {
						t.Fatal("visitor persisted", path)
					}
				}
			}
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code)
			}
		})
	}
}
