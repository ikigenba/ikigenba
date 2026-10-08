package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth"
	"github.com/ikigenba/ikigenba/auth/internal/store"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

func changeDatabase(t *testing.T, p Process, statement string) {
	t.Helper()
	h, err := db.Open(t.Context(), db.Config{Path: filepath.Join(p.Dir, "state", "auth.db"), Migrations: auth.Migrations(), Now: p.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	if statement != "" {
		if err := h.Write(t.Context(), func(tx *sql.Tx) error { _, err := tx.ExecContext(t.Context(), statement); return err }); err != nil {
			t.Fatal(err)
		}
	}
}

func statusOf(t *testing.T, p Process) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := db.Status(t.Context(), db.Config{Path: filepath.Join(p.Dir, "state", "auth.db"), Migrations: auth.Migrations()}, &out)
	return out.String(), err
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory changed: %v", entries)
	}
}

func TestDatabaseStatus(t *testing.T) {
	// R-7P8S-3GN9 R-TAU4-N17Z R-TC21-0SYO R-7SWH-8RVC R-7GPH-F2GE R-7LL2-Y5F6 R-7MSZ-BX5V R-7D1S-9R8B
	for _, kind := range []string{"absent", "applied", "unknown", "invalid"} {
		for _, relative := range []bool{false, true} {
			t.Run(kind+"/relative="+map[bool]string{false: "false", true: "true"}[relative], func(t *testing.T) {
				p := baseProcess(goodEnv(), t.TempDir(), nil)
				if relative {
					t.Chdir(p.Dir)
					p.Dir = ""
				}
				switch kind {
				case "applied":
					changeDatabase(t, p, "")
				case "unknown":
					changeDatabase(t, p, fmt.Sprintf("INSERT INTO schema_migrations(version,applied_at) VALUES (%d, '2026-01-02T03:04:05.000000Z')", unknownMigrationVersion(t)))
				case "invalid":
					if err := os.Mkdir(filepath.Join(p.Dir, "state"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(p.Dir, "state", "auth.db"), []byte("not a database"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := statusOf(t, p)
				if kind == "unknown" && (err != nil || !strings.Contains(want, fmt.Sprintf("%04d", unknownMigrationVersion(t)))) {
					t.Fatalf("ahead status=%q error=%v", want, err)
				}
				if kind == "invalid" && (err == nil || want != "") {
					t.Fatalf("invalid status=%q error=%v", want, err)
				}
				p.Args = []string{"db", "status"}
				p.LookupEnv = func(string) (string, bool) { t.Fatal("status read environment"); return "", false }
				p.Unsetenv = func(string) error { t.Fatal("status removed environment"); return nil }
				p.Inherit = func(uintptr) (net.Listener, error) {
					t.Fatal("status inherited listener")
					return nil, errors.New("unexpected")
				}
				p.Banner = func(page.User) page.Banner { t.Fatal("status called banner"); return page.Banner{} }
				code := Run(t.Context(), p)
				wantCode := 0
				wantDiag := ""
				if err != nil {
					wantCode = 1
					wantDiag = "auth: " + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"
				}
				if code != wantCode || p.Stdout.(*bytes.Buffer).String() != want || p.Stderr.(*countWriter).String() != wantDiag {
					t.Fatalf("code=%d stdout=%q stderr=%q; wanted %d %q %q", code, p.Stdout, p.Stderr, wantCode, want, wantDiag)
				}
				if len(p.Sink.(*telemetry.Capture).Events()) != 0 {
					t.Fatal("status emitted events")
				}
				if err != nil && p.Stderr.(*countWriter).calls != 1 {
					t.Fatal("diagnostic was split")
				}
				after, afterErr := statusOf(t, p)
				if after != want || !sameError(err, afterErr) {
					t.Fatal("status changed database")
				}
				if kind == "absent" {
					dir := p.Dir
					if dir == "" {
						dir = "."
					}
					assertEmptyDir(t, dir)
				}
			})
		}
	}
}
func sameError(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Error() == b.Error()
}

func TestDatabaseCommandGrammar(t *testing.T) {
	// R-7HXD-SU73 R-7VCA-0BCQ R-7U4D-MJM1 R-7LL2-Y5F6
	for _, args := range [][]string{{"db"}, {"db", "bogus"}, {"db", "--bogus"}, {"db", "--help"}, {"db", "manifest"}, {"db", "status", "--version"}, {"db", "status", "extra"}, {"db", "status", "--extra"}, {"--help", "extra"}, {"--help", "db", "status"}, {"--version", "--extra"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			p := baseProcess(goodEnv(), t.TempDir(), nil)
			p.Args = args
			p.LookupEnv = func(string) (string, bool) { t.Fatal("command read environment"); return "", false }
			p.Unsetenv = func(string) error { t.Fatal("command removed environment"); return nil }
			p.Inherit = func(uintptr) (net.Listener, error) {
				t.Fatal("command inherited listener")
				return nil, errors.New("unexpected")
			}
			arg := args[0]
			if len(args) > 1 {
				arg = args[1]
			}
			if len(args) > 2 && args[0] == "db" && args[1] == "status" {
				arg = args[2]
			}
			kind := "command"
			if strings.HasPrefix(arg, "--") {
				kind = "option"
			}
			want := "auth: unknown " + kind + " '" + arg + "'\n\nsee 'auth --help' for usage\n"
			if code := Run(t.Context(), p); code != 2 || p.Stdout.(*bytes.Buffer).Len() != 0 || p.Stderr.(*countWriter).String() != want {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, p.Stdout, p.Stderr)
			}
			assertEmptyDir(t, p.Dir)
		})
	}
}

func TestDatabaseOpenFailures(t *testing.T) {
	// R-SYN4-TBT1 R-7D1S-9R8B
	for _, kind := range []string{"state file", "invalid"} {
		for _, relative := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/relative=%v", kind, relative), func(t *testing.T) {
				p := baseProcess(goodEnv(), t.TempDir(), nil)
				if relative {
					t.Chdir(p.Dir)
					p.Dir = ""
				}
				switch kind {
				case "state file":
					if err := os.WriteFile(filepath.Join(p.Dir, "state"), []byte("blocked"), 0600); err != nil {
						t.Fatal(err)
					}
				case "invalid":
					if err := os.Mkdir(filepath.Join(p.Dir, "state"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(p.Dir, "state", "auth.db"), []byte("not sqlite"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				_, openErr := db.Open(t.Context(), db.Config{Path: filepath.Join(p.Dir, "state", "auth.db"), Migrations: auth.Migrations(), Now: p.Now})
				if openErr == nil {
					t.Fatal("fixture unexpectedly opens")
				}
				ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				tracked := &unacceptedListener{trackedListener: trackedListener{Listener: ln}, t: t}
				p.Inherit = func(uintptr) (net.Listener, error) { return tracked, nil }
				p.Banner = func(page.User) page.Banner { t.Fatal("banner before serve"); return page.Banner{} }
				notifications, addr := databaseReady(t)
				env := goodEnv()
				env["NOTIFY_SOCKET"] = addr
				p.LookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
				want := "auth: cannot open database state/auth.db: " + strings.ReplaceAll(openErr.Error(), "\n", " ") + "\n"
				if code := Run(t.Context(), p); code != 1 || p.Stderr.(*countWriter).String() != want || p.Stdout.(*bytes.Buffer).Len() != 0 || !tracked.closed.Load() {
					t.Fatalf("code=%d out=%q diag=%q closed=%v", code, p.Stdout, p.Stderr, tracked.closed.Load())
				}
				if p.Stderr.(*countWriter).calls != 1 {
					t.Fatal("split diagnostic")
				}
				assertNoNotification(t, notifications)
			})
		}
	}
}

type unacceptedListener struct {
	trackedListener
	t *testing.T
}

func (ln *unacceptedListener) Accept() (net.Conn, error) {
	ln.t.Error("database failure accepted a connection")
	return nil, errors.New("unexpected accept")
}

func unknownMigrationVersion(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(auth.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	greatest := 0
	for _, entry := range entries {
		prefix, _, _ := strings.Cut(entry.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatal(err)
		}
		if version > greatest {
			greatest = version
		}
	}
	return greatest + 1
}

func databaseReady(t *testing.T) (*net.UnixConn, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "auth-db-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	addr := filepath.Join(dir, "ready.sock")
	ln, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln, addr
}

func TestDatabaseReadyMigrationClock(t *testing.T) {
	// R-GHCN-OPOF R-7GPH-F2GE R-7BTV-VZHM
	for _, kind := range []string{"absent", "empty state", "legacy", "applied"} {
		t.Run(kind, func(t *testing.T) {
			p := baseProcess(goodEnv(), t.TempDir(), nil)
			if kind == "empty state" {
				if err := os.Mkdir(filepath.Join(p.Dir, "state"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "legacy" {
				changeDatabase(t, p, "DROP TABLE schema_migrations; DROP TABLE clients; DROP TABLE auth_codes; ALTER TABLE tokens DROP COLUMN kind; ALTER TABLE tokens DROP COLUMN host")
			}
			if kind == "applied" {
				changeDatabase(t, p, "")
			}
			comparison := baseProcess(goodEnv(), t.TempDir(), nil)
			changeDatabase(t, comparison, "")
			want, _ := statusOf(t, comparison)
			if kind == "applied" {
				want, _ = statusOf(t, p)
			}
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
			ready, addr := databaseReady(t)
			env := goodEnv()
			env["NOTIFY_SOCKET"] = addr
			p.LookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan int, 1)
			go func() { done <- Run(ctx, p) }()
			if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 32)
			n, _, err := ready.ReadFromUnix(buf)
			if err != nil {
				t.Fatal(err)
			}
			if string(buf[:n]) != "READY=1" {
				t.Fatal("readiness")
			}
			info, statErr := os.Stat(filepath.Join(p.Dir, "state", "auth.db"))
			if statErr != nil || !info.Mode().IsRegular() {
				t.Fatalf("database is not regular: %v", statErr)
			}
			got, err := statusOf(t, p)
			if err != nil || got != want {
				t.Fatalf("status=%q want=%q err=%v", got, want, err)
			}
			cancel()
			if code := <-done; code != 0 {
				t.Fatalf("code=%d diag=%s", code, p.Stderr)
			}
		})
	}
}

// readyOrderSink ends the request-free run once its first event is delivered.
type readyOrderSink struct {
	capture telemetry.Capture
	cancel  context.CancelFunc
}

func (s *readyOrderSink) Deliver(ctx context.Context, e telemetry.Event) error {
	err := s.capture.Deliver(ctx, e)
	if e.Name == "service.started" {
		s.cancel()
	}
	return err
}

func TestStartedIsRecordedAfterReadiness(t *testing.T) {
	// R-T4QM-Q6II: the injected clock observes readiness synchronously when
	// the first event is formed, rather than after asynchronous delivery.
	p := baseProcess(goodEnv(), t.TempDir(), nil)
	changeDatabase(t, p, "") // An up-to-date database does not read the migration clock.
	ready, addr := databaseReady(t)
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
	env := goodEnv()
	env["NOTIFY_SOCKET"] = addr
	p.LookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink := &readyOrderSink{cancel: cancel}
	p.Sink = sink
	fixed := p.Now()
	clockCalls := 0
	p.Now = func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			raw, err := ready.SyscallConn()
			if err != nil {
				t.Error(err)
				return fixed
			}
			var n int
			var receiveErr error
			buf := make([]byte, 32)
			if err := raw.Read(func(fd uintptr) bool {
				n, _, receiveErr = syscall.Recvfrom(int(fd), buf, syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
				return true
			}); err != nil {
				t.Error(err)
			}
			if receiveErr != nil {
				t.Errorf("first event recorded before readiness: %v", receiveErr)
			} else if string(buf[:n]) != "READY=1" {
				t.Errorf("first event readiness datagram=%q", buf[:n])
			}
		}
		return fixed
	}
	if code := Run(ctx, p); code != 0 {
		t.Fatalf("code=%d diagnostic=%s", code, p.Stderr)
	}
	events := sink.capture.Events()
	if len(events) != 2 || events[0].Name != "service.started" || events[0].RequestID != "" || events[0].User != "" || len(events[0].Attrs) != 1 || events[0].Attrs["version"] != version.Version || events[0].Time != fixed {
		t.Fatalf("events=%v", events)
	}
}

// warningOrderWriter observes the notification queue at the database warning.
type warningOrderWriter struct {
	countWriter
	ready   *net.UnixConn
	written chan struct{}
	t       *testing.T
}

func (w *warningOrderWriter) Write(b []byte) (int, error) {
	raw, err := w.ready.SyscallConn()
	if err != nil {
		w.t.Error(err)
	} else {
		var receiveErr error
		if err := raw.Control(func(fd uintptr) {
			_, _, receiveErr = syscall.Recvfrom(int(fd), make([]byte, 32), syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
		}); err != nil {
			w.t.Error(err)
		}
		if !errors.Is(receiveErr, syscall.EAGAIN) {
			w.t.Errorf("readiness preceded database warning: %v", receiveErr)
		}
	}
	n, err := w.countWriter.Write(b)
	select {
	case w.written <- struct{}{}:
	default:
	}
	return n, err
}

func TestRunServesAheadDatabase(t *testing.T) {
	// R-LO9G-FT7B R-5RXL-FWKQ: the published database warning precedes
	// readiness, the existing session works, and recorded versions stay intact.
	for _, relative := range []bool{false, true} {
		t.Run(fmt.Sprintf("relative=%v", relative), func(t *testing.T) {
			p := baseProcess(goodEnv(), t.TempDir(), nil)
			if relative {
				t.Chdir(p.Dir)
				p.Dir = ""
			}
			path := filepath.Join(p.Dir, "state", "auth.db")
			h, err := db.Open(t.Context(), db.Config{Path: path, Migrations: auth.Migrations(), Now: p.Now})
			if err != nil {
				t.Fatal(err)
			}
			st := store.New(h, p.Rand)
			u, _, err := st.UpsertUserOnLogin("issuer", "subject", "user@example.test", p.Now())
			if err != nil {
				t.Fatal(err)
			}
			session, err := st.CreateSession(u.ID, p.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Write(t.Context(), func(tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), "INSERT INTO schema_migrations(version,applied_at) VALUES (?, ?)", unknownMigrationVersion(t), "2026-01-02T03:04:05.000000Z")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			wantStatus, err := statusOf(t, p)
			if err != nil {
				t.Fatal(err)
			}
			var warning countWriter
			h, err = db.Open(t.Context(), db.Config{Path: path, Migrations: auth.Migrations(), Now: p.Now, Service: "auth", Stderr: &warning})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			if warning.calls != 1 || warning.Len() == 0 {
				t.Fatalf("reference warning=%q calls=%d", warning.String(), warning.calls)
			}
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ln.Close() })
			p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
			ready, addr := databaseReady(t)
			env := goodEnv()
			env["NOTIFY_SOCKET"] = addr
			p.LookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
			diagnostic := &warningOrderWriter{ready: ready, written: make(chan struct{}, 1), t: t}
			p.Stderr = diagnostic
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan int, 1)
			go func() { done <- Run(ctx, p) }()
			select {
			case <-diagnostic.written:
			case <-time.After(5 * time.Second):
				t.Fatal("no database warning")
			}
			if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 32)
			n, _, err := ready.ReadFromUnix(buf)
			if err != nil || string(buf[:n]) != "READY=1" {
				t.Fatalf("readiness=%q error=%v", buf[:n], err)
			}
			after, err := statusOf(t, p)
			if err != nil || after != wantStatus {
				t.Fatalf("status after readiness=%q want=%q error=%v", after, wantStatus, err)
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+"/check", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: "ikigenba_session", Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("existing session check=%d", response.StatusCode)
			}
			cancel()
			if code := <-done; code != 0 {
				t.Fatalf("Run=%d diagnostic=%q", code, diagnostic.String())
			}
			if diagnostic.calls != 1 || diagnostic.String() != warning.String() || p.Stdout.(*bytes.Buffer).Len() != 0 {
				t.Fatalf("diagnostic=%q calls=%d stdout=%q; want=%q", diagnostic.String(), diagnostic.calls, p.Stdout, warning.String())
			}
			assertNoNotification(t, ready)
		})
	}
}
