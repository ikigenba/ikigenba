package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home"
	"github.com/ikigenba/ikigenba/home/internal/bookmarks"
	"github.com/ikigenba/ikigenba/home/internal/pages"
)

func fixedNow() time.Time {
	return time.Date(2026, 10, 23, 9, 0, 0, 123456789, time.FixedZone("fixture", 7200))
}

type repeatReader byte

func (r repeatReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(r)
	}
	return len(b), nil
}

type touchReader struct{ touch func() }

func (r touchReader) Read([]byte) (int, error) { r.touch(); return 0, io.EOF }

type touchTransport struct{ touch func() }

func (r touchTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.touch()
	return nil, fmt.Errorf("forbidden")
}

type fixtureTransport struct{}

func (fixtureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
}
func databaseConfig(dir string) db.Config {
	return db.Config{Path: filepath.Join(dir, "state", "home.db"), Migrations: home.Migrations(), Now: fixedNow}
}
func status(t *testing.T, cfg db.Config) (string, error) {
	t.Helper()
	var b bytes.Buffer
	err := db.Status(context.Background(), cfg, &b)
	return b.String(), err
}

// R-KUM6-DPW0 R-KX1Z-59DE R-KY9V-J143 R-KZHR-WSUS R-L0PO-AKLH R-L1XK-OCC6 R-L35H-242V R-KS6D-M6EM
func TestDatabaseStatus(t *testing.T) {
	for _, kind := range []string{"absent", "applied", "newer", "invalid", "working directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			cfg := databaseConfig(dir)
			if kind == "invalid" {
				if err := os.Mkdir(filepath.Join(dir, "state"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.Path, []byte("fixture invalid database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "applied" || kind == "newer" || kind == "working directory" {
				opening := cfg
				if kind == "newer" {
					opening.Migrations = newerMigrations(t)
				}
				d, err := db.Open(context.Background(), opening)
				if err != nil {
					t.Fatal(err)
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
			}
			before, wantErr := status(t, cfg)
			var out bytes.Buffer
			stderr := &writes{}
			if kind == "working directory" {
				t.Chdir(dir)
				dir = ""
			}
			p := Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &out, Stderr: stderr,
				LookupEnv: func(string) (string, bool) { t.Error("environment read"); return "", false }, Unsetenv: func(string) error { t.Error("environment unset"); return nil }, Inherit: func(uintptr) (net.Listener, error) { t.Error("inherit called"); return nil, fmt.Errorf("forbidden") }, Now: func() time.Time { t.Error("clock read"); return time.Time{} }, Rand: touchReader{func() { t.Error("random read") }}, HTTP: &http.Client{Transport: touchTransport{func() { t.Error("HTTP used") }}}, Banner: func(page.User) page.Banner { t.Error("banner called"); return page.Banner{} }, Sink: sinkFunc(func(context.Context, telemetry.Event) error { t.Error("sink called"); return nil })}
			code := Run(context.Background(), p)
			if out.String() != before {
				t.Fatalf("status mismatch got %q want %q", out.String(), before)
			}
			if wantErr == nil {
				if code != ExitSuccess || stderr.count() != 0 {
					t.Fatalf("code=%d stderr=%q", code, stderr.text())
				}
			} else {
				if code != ExitServerFailed || stderr.count() != 1 || stderr.text() != "home: "+strings.ReplaceAll(wantErr.Error(), "\n", " ")+"\n" {
					t.Fatalf("failure code=%d stderr=%q", code, stderr.text())
				}
			}
			after, afterErr := status(t, cfg)
			if after != before || fmt.Sprint(afterErr) != fmt.Sprint(wantErr) {
				t.Fatal("status changed database")
			}
			if kind == "absent" {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatal(entries, err)
				}
			}
		})
	}
}
func newerMigrations(t *testing.T) fs.FS {
	t.Helper()
	migration, err := fs.ReadFile(home.Migrations(), "0001_bookmarks.sql")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"0001_bookmarks.sql": &fstest.MapFile{Data: migration}, "0002_future.sql": &fstest.MapFile{Data: []byte("CREATE TABLE future_fixture (value TEXT);")}}
}

// R-KWJA-PMOM R-2541-8WS5 R-44CL-C43Y R-5CAQ-PAN3 R-KFGP-CUAW
func TestDatabaseLifecycle(t *testing.T) {
	for _, emptyState := range []bool{false, true} {
		dir := t.TempDir()
		if emptyState {
			if err := os.Mkdir(filepath.Join(dir, "state"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		f := startRun(t, func(p *Process) { p.Dir = dir })
		info, err := os.Stat(filepath.Join(dir, "state", "home.db"))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("database not ready %v", err)
		}
		for _, route := range []string{"/", "/about", "/bookmarks", "/missing"} {
			request(t, f.address, route)
		}
		if code := f.stop(t, "done"); code != ExitSuccess {
			t.Fatal(code)
		}
		if f.stdout.count() != 0 || f.stderr.count() != 0 {
			t.Fatal(f.stdout.text(), f.stderr.text())
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(filepath.Join(dir, "state", "home.db"+suffix)); !os.IsNotExist(err) {
				t.Fatal("database handle remains open", err)
			}
		}
		for _, event := range f.capture.Events() {
			if !event.Time.Equal(fixedNow().UTC().Truncate(time.Microsecond)) {
				t.Fatal(event.Time)
			}
		}
		d, err := db.Open(context.Background(), databaseConfig(dir))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := bookmarks.New(d, bookmarks.Config{Now: fixedNow, Rand: repeatReader(0x5a)}).List(context.Background())
		if err != nil || len(rows) != 0 {
			t.Fatal(rows, err)
		}
		if err = d.Close(); err != nil {
			t.Fatal(err)
		}
		expected := t.TempDir()
		d, err = db.Open(context.Background(), databaseConfig(expected))
		if err != nil {
			t.Fatal(err)
		}
		if err = d.Close(); err != nil {
			t.Fatal(err)
		}
		got, e := status(t, databaseConfig(dir))
		want, we := status(t, databaseConfig(expected))
		if e != nil || we != nil || got != want {
			t.Fatalf("migration times %q != %q (%v,%v)", got, want, e, we)
		}
	}
}

// R-SMN8-OVD1
func TestDatabaseOpenFailure(t *testing.T) {
	for _, kind := range []string{"state file", "bad database"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state")
			if kind == "bad database" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "home.db")
			}
			original := []byte("unchanged fixture")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			_, expected := db.Open(context.Background(), databaseConfig(dir))
			if expected == nil {
				t.Fatal("expected failure")
			}
			ln := &failedListener{}
			var out bytes.Buffer
			stderr := &writes{}
			env := map[string]string{"LISTEN_PID": "23", "LISTEN_FDS": "1", "NOTIFY_SOCKET": "/unused"}
			code := Run(context.Background(), Process{Dir: dir, Version: "database failure fixture", Now: fixedNow, Rand: touchReader{func() { t.Error("random used") }}, HTTP: &http.Client{Transport: touchTransport{func() { t.Error("HTTP used") }}}, Pid: 23, LookupEnv: lookup(t, env), Stdout: &out, Stderr: stderr, Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Sink: sinkFunc(func(context.Context, telemetry.Event) error { t.Error("event recorded"); return nil }), Banner: func(page.User) page.Banner { t.Error("banner called"); return page.Banner{} }})
			if code != ExitServerFailed || out.Len() != 0 || stderr.count() != 1 || stderr.text() != "home: cannot open database state/home.db: "+strings.ReplaceAll(expected.Error(), "\n", " ")+"\n" || ln.accepts != 0 {
				t.Fatalf("code=%d stderr=%q", code, stderr.text())
			}
			data, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
			if err != nil || !bytes.Equal(data, original) {
				t.Fatal("input changed", err)
			}
		})
	}
}

type recordingTransport struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req.Clone(req.Context()))
	return fixtureTransport{}.RoundTrip(req)
}
func submit(t *testing.T, address, name string) {
	t.Helper()
	form := url.Values{"name": {name}, "url": {"http://bookmark.fixture.test/path"}, "description": {"fixture description"}}
	req, err := http.NewRequest("POST", "http://"+address+"/bookmarks", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User-Id", "fixture-user")
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 303 {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("POST status %d %s", response.StatusCode, b)
	}
}

// R-LNPI-HNLY R-LOXE-VFCN R-LQ5B-973C R-X6DA-DEI4 R-SGJQ-S0NK
func TestPersistentBookmarksAndFetchWiring(t *testing.T) {
	outer := t.TempDir()
	marker := filepath.Join(outer, "marker")
	if err := os.WriteFile(marker, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(outer, "run")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	transport := &recordingTransport{}
	f := startRun(t, func(p *Process) { p.Dir = dir; p.HTTP = &http.Client{Transport: transport} })
	submit(t, f.address, "Persistent fixture bookmark")
	for _, route := range []string{"/", "/bookmarks"} {
		_, _, body := request(t, f.address, route)
		if !strings.Contains(body, "Persistent fixture bookmark") {
			t.Fatal("created bookmark missing")
		}
	}
	transport.mu.Lock()
	if len(transport.requests) == 0 {
		t.Error("no favicon request")
	}
	for _, req := range transport.requests {
		if req.Header.Get("User-Agent") != pages.ServiceName+" ("+pages.Description+")" {
			t.Error("wrong user agent")
		}
	}
	transport.mu.Unlock()
	if code := f.stop(t, "saved"); code != ExitSuccess {
		t.Fatal(code)
	}
	for _, event := range f.capture.Events() {
		switch event.Name {
		case "service.started", "service.stopping", "request.started", "request.finished", "bookmark.created", "bookmark.deleted":
		default:
			t.Fatal(event)
		}
	}
	d, err := db.Open(context.Background(), databaseConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := bookmarks.New(d, bookmarks.Config{Now: fixedNow, Rand: repeatReader(0x5a)}).List(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ID != "bmk_5a5a5a5a5a5a5a5a" || !rows[0].Created.Equal(fixedNow().UTC().Truncate(time.Second)) {
		t.Fatal(rows, err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	f = startRun(t, func(p *Process) { p.Dir = dir })
	_, _, body := request(t, f.address, "/bookmarks")
	if !strings.Contains(body, "Persistent fixture bookmark") {
		t.Fatal("bookmark not persisted")
	}
	f.stop(t, "read")
	f = startRun(t, nil)
	_, _, body = request(t, f.address, "/bookmarks")
	if strings.Contains(body, "Persistent fixture bookmark") {
		t.Fatal("bookmark leaked between dirs")
	}
	f.stop(t, "different directory")
	entries, err := os.ReadDir(outer)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	content, err := fs.ReadFile(os.DirFS(outer), "marker")
	if err != nil || string(content) != "unchanged" || !reflect.DeepEqual(names, []string{"marker", "run"}) {
		t.Fatal("writes outside Dir", names, err)
	}
}

type serialProbe struct {
	active  atomic.Bool
	overlap atomic.Bool
	next    atomic.Uint64
}

func (r *serialProbe) Read(b []byte) (int, error) {
	if !r.active.CompareAndSwap(false, true) {
		r.overlap.Store(true)
	}
	defer r.active.Store(false)
	for i := range b {
		b[i] = byte(r.next.Add(1) & 0xff)
	}
	return len(b), nil
}

// R-LRD7-MYU1
func TestRandomSourceSerialized(t *testing.T) {
	random := &serialProbe{}
	f := startRun(t, func(p *Process) { p.Rand = random })
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			if i%2 == 0 {
				submit(t, f.address, fmt.Sprintf("bookmark %d", i))
			} else {
				request(t, f.address, "/about")
			}
		})
	}
	wg.Wait()
	f.stop(t, "concurrent end")
	if random.overlap.Load() {
		t.Fatal("overlapping random reads")
	}
}

// R-1OK2-4RC4
func TestNewerDatabaseServesWithWarning(t *testing.T) {
	dir := t.TempDir()
	cfg := databaseConfig(dir)
	cfg.Migrations = newerMigrations(t)
	d, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	store := bookmarks.New(d, bookmarks.Config{Now: fixedNow, Rand: repeatReader(0x5a)})
	_, err = store.Create(context.Background(), bookmarks.Draft{Name: "Newer database bookmark", URL: "http://fixture.test", CreatedBy: "fixture-user"})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := status(t, databaseConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	var warning bytes.Buffer
	cfg = databaseConfig(dir)
	cfg.Service = pages.ServiceName
	cfg.Stderr = &warning
	d, err = db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	if warning.Len() == 0 {
		t.Fatal("fixture has no newer schema warning")
	}
	f := startRun(t, func(p *Process) { p.Dir = dir })
	if f.stderr.text() != warning.String() || f.stderr.count() != 1 {
		t.Fatalf("warning %q", f.stderr.text())
	}
	statusCode, _, body := request(t, f.address, "/bookmarks")
	if statusCode != 200 || !strings.Contains(body, "Newer database bookmark") {
		t.Fatal(statusCode, body)
	}
	f.stop(t, "newer stop")
	after, err := status(t, databaseConfig(dir))
	if err != nil || before != after {
		t.Fatal("newer database status changed", err)
	}
	started := 0
	for _, event := range f.capture.Events() {
		if event.Name == "service.started" {
			started++
		}
		switch event.Name {
		case "service.started", "service.stopping", "request.started", "request.finished":
		default:
			t.Fatal(event)
		}
	}
	if started != 1 {
		t.Fatal(started)
	}
}

// R-KXR7-3EFB
func TestRefusalsPreserveState(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, kind := range []string{"drain", "missing", "several", "inherit"} {
			dir := t.TempDir()
			var original []byte
			path := filepath.Join(dir, "state", "home.db")
			if existing {
				d, err := db.Open(context.Background(), databaseConfig(dir))
				if err != nil {
					t.Fatal(err)
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
				original, err = fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
				if err != nil {
					t.Fatal(err)
				}
			}
			env := map[string]string{"LISTEN_PID": "23", "LISTEN_FDS": "1"}
			switch kind {
			case "drain":
				env["DRAIN_SECONDS"] = "0"
			case "missing":
				delete(env, "LISTEN_PID")
			case "several":
				env["LISTEN_FDS"] = "2"
			}
			var out, stderr bytes.Buffer
			p := Process{Dir: dir, Pid: 23, Version: "fixture", Stdout: &out, Stderr: &stderr, LookupEnv: lookup(t, env), Now: func() time.Time { t.Error("clock touched"); return time.Time{} }, Rand: touchReader{func() { t.Error("random used") }}, HTTP: &http.Client{Transport: touchTransport{func() { t.Error("HTTP used") }}}, Inherit: func(uintptr) (net.Listener, error) {
				if kind != "inherit" {
					t.Error("inherit called")
				}
				return nil, fmt.Errorf("fixture inherit failed")
			}}
			if code := Run(context.Background(), p); code == ExitSuccess {
				t.Fatal("refusal succeeded")
			}
			if existing {
				got, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
				if err != nil || !bytes.Equal(got, original) {
					t.Fatal("database changed", err)
				}
			} else {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatal("refusal created state", entries, err)
				}
			}
		}
	}
}

type callbackWriter struct {
	writes
	callback func()
}

func (w *callbackWriter) Write(b []byte) (int, error) {
	n, err := w.writes.Write(b)
	w.callback()
	return n, err
}

// R-7SBX-KDKV
func TestCancellationBeforeReady(t *testing.T) {
	for _, stage := range []string{"lookup", "unset", "inherit", "clock", "warning", "notify lookup"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			var expected bytes.Buffer
			if stage == "warning" {
				cfg := databaseConfig(dir)
				cfg.Migrations = newerMigrations(t)
				d, err := db.Open(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
				cfg = databaseConfig(dir)
				cfg.Service = pages.ServiceName
				cfg.Stderr = &expected
				d, err = db.Open(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ln := &failedListener{err: fmt.Errorf("must not accept")}
			capture := &telemetry.Capture{}
			var out bytes.Buffer
			stderr := &callbackWriter{callback: func() {
				if stage == "warning" {
					cancel()
				}
			}}
			env := map[string]string{"LISTEN_PID": "23", "LISTEN_FDS": "1", "NOTIFY_SOCKET": "/must/not/notify"}
			p := Process{Dir: dir, Pid: 23, Version: "fixture", Stdout: &out, Stderr: stderr, Sink: capture, Rand: touchReader{func() { t.Error("random read") }}, HTTP: &http.Client{Transport: touchTransport{func() { t.Error("HTTP sent") }}}, LookupEnv: func(key string) (string, bool) {
				if stage == "lookup" || stage == "notify lookup" && key == "NOTIFY_SOCKET" {
					cancel()
				}
				value, ok := env[key]
				return value, ok
			}, Unsetenv: func(string) error {
				if stage == "unset" {
					cancel()
				}
				return nil
			}, Inherit: func(uintptr) (net.Listener, error) {
				if stage == "inherit" {
					cancel()
				}
				return ln, nil
			}, Now: func() time.Time {
				if stage == "clock" {
					cancel()
				}
				return fixedNow()
			}}
			code := Run(ctx, p)
			if code != ExitSuccess || ln.accepts != 0 || len(capture.Events()) != 0 || out.Len() != 0 || stderr.text() != expected.String() {
				t.Fatalf("stage=%s code=%d accepts=%d events=%v out=%q err=%q expected=%q", stage, code, ln.accepts, capture.Events(), out.String(), stderr.text(), expected.String())
			}
			if stage == "warning" && stderr.count() != 1 {
				t.Fatal("warning not atomic")
			}
		})
	}
}
