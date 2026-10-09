package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// R-S5NJ-XROQ R-GRVN-JE8T R-AF9N-8W16 R-AGHJ-MNRV R-BWX8-2SNQ
func TestStartupSettingsComeFirst(t *testing.T) {
	for _, key := range []string{"DRAIN_SECONDS", "TREE_MAX_BYTES", "OUTPUT_MAX_BYTES", "OPERATION_SECONDS", "SCRIPT_SECONDS", "RUN_KEEP_DAYS", "RUN_KEEP_COUNT"} {
		t.Run(key, func(t *testing.T) {
			h := newHarness(t)
			h.set(key, "0")

			h.p.LookupEnv = func(k string) (string, bool) {
				switch k {
				case "LISTEN_PID", "LISTEN_FDS", "PATH", "NOTIFY_SOCKET", "IKIGENBA_SERVICES":
					t.Fatalf("premature lookup %s", k)
				}
				return h.lookup(k)
			}
			h.p.Unsetenv = func(string) error { t.Fatal("removed environment before startup accepted"); return nil }
			h.p.Sink = forbiddenSink{t}
			h.p.Banner = func(page.User) page.Banner { t.Fatal("banner before startup accepted"); return page.Banner{} }
			h.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Fatal("MCP before startup accepted"); return nil }
			h.p.Inherit = func(uintptr) (net.Listener, error) { t.Fatal("inherited before accepted settings"); return nil, nil }
			_, err := settings.Read(h.lookup)
			code := cli.Run(context.Background(), h.p)
			if code != cli.ExitUsage || h.stderr.String() != "scripts: "+err.Error()+"\n" || h.stdout.String() != "" || len(h.stderr.snapshot()) != 1 {
				t.Fatalf("refusal %d %q", code, h.stderr.String())
			}
			if _, err = os.Stat(filepath.Join(h.p.Dir, "state", "scripts.db")); !os.IsNotExist(err) {
				t.Fatalf("database touched: %v", err)
			}
			if _, err = os.Stat(h.p.Dir); !os.IsNotExist(err) {
				t.Fatalf("working directory touched: %v", err)
			}
		})
	}
}

// R-AHPG-0FIK R-AIXC-E799 R-AK58-RYZY
func TestActivationValidation(t *testing.T) {
	for _, tc := range []struct {
		pid, fds string
		multiple bool
	}{{"123", "", false}, {"other", "1", false}, {"123", "0", false}, {"123", "-1", false}, {"123", "+1", false}, {"123", "1x", false}, {"123", " 1", false}, {"123", "01x", false}, {"123", "2", true}, {"123", "0002", true}, {"123", strings.Repeat("9", 100), true}} {
		t.Run(tc.pid+"_"+tc.fds, func(t *testing.T) {
			h := newHarness(t)
			h.set("LISTEN_PID", tc.pid)
			h.set("LISTEN_FDS", tc.fds)
			seen := map[string]bool{}
			h.p.LookupEnv = func(k string) (string, bool) {
				seen[k] = true
				if k == "PATH" || k == "NOTIFY_SOCKET" {
					t.Fatal("looked up path/notification before activation accepted")
				}
				return h.lookup(k)
			}
			h.p.Unsetenv = func(string) error { t.Fatal("removed environment before startup accepted"); return nil }
			h.p.Sink = forbiddenSink{t}
			h.p.Banner = func(page.User) page.Banner { t.Fatal("banner before startup accepted"); return page.Banner{} }
			h.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Fatal("MCP before startup accepted"); return nil }
			h.p.Inherit = func(uintptr) (net.Listener, error) { t.Fatal("took invalid activation socket"); return nil, nil }
			code := cli.Run(context.Background(), h.p)
			want := "scripts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
			if tc.multiple {
				want = "scripts: " + tc.fds + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"
			}
			if code != cli.ExitUsage || h.stderr.String() != want || h.stdout.String() != "" || len(h.stderr.snapshot()) != 1 {
				t.Fatalf("%d %q", code, h.stderr.String())
			}
			for _, key := range []string{"DRAIN_SECONDS", "TREE_MAX_BYTES", "OUTPUT_MAX_BYTES", "OPERATION_SECONDS", "SCRIPT_SECONDS", "RUN_KEEP_DAYS", "RUN_KEEP_COUNT", "REPOS_DIR"} {
				if !seen[key] {
					t.Errorf("setting never read: %s", key)
				}
			}
			if _, e := os.Stat(h.p.Dir); !os.IsNotExist(e) {
				t.Fatalf("touched Dir: %v", e)
			}
		})
	}
	for _, value := range []string{"1", "01", "0001"} {
		t.Run(value, func(t *testing.T) { h := newHarness(t); h.set("LISTEN_FDS", value); h.start(); h.stop() })
	}
}

// R-S4FN-JZY1 R-AML1-JIHC R-ANSX-XA81
func TestInheritanceAndEnvironmentRemoval(t *testing.T) {
	h := newHarness(t)
	var keys []string
	calls := 0
	h.p.Unsetenv = func(k string) error { keys = append(keys, k); return errors.New("ignored removal error") }
	h.p.Inherit = func(fd uintptr) (net.Listener, error) {
		calls++
		if fd != 3 {
			t.Fatalf("descriptor %d", fd)
		}
		return nil, errors.New("bad descriptor")
	}
	h.p.LookupEnv = func(k string) (string, bool) {
		if k == "PATH" {
			t.Fatal("PATH before inherited")
		}
		return h.lookup(k)
	}
	code := cli.Run(context.Background(), h.p)
	if calls != 1 || !reflect.DeepEqual(keys, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) || code != cli.ExitServerFailed || h.stderr.String() != "scripts: bad descriptor\n" || len(h.stderr.snapshot()) != 1 {
		t.Fatalf("%d %d %v %q", code, calls, keys, h.stderr.String())
	}
	if _, e := os.Stat(h.p.Dir); !os.IsNotExist(e) {
		t.Fatalf("Dir touched: %v", e)
	}
}

// R-S83C-PB64 R-016A-RVJW
func TestPrerequisitesBeforeState(t *testing.T) {
	for _, which := range []string{"git", "python"} {
		t.Run(which, func(t *testing.T) {
			h := newHarness(t)
			empty := filepath.Join(h.root, "bin")
			mustCLI(t, os.Mkdir(empty, 0700))
			if which == "python" {
				mustCLI(t, os.Symlink(h.git, filepath.Join(empty, "git")))
			} else {
				mustCLI(t, os.Symlink(h.python, filepath.Join(empty, runner.Interpreter)))
			}
			h.set("PATH", empty)
			h.p.Banner = func(page.User) page.Banner { t.Fatal("banner before prerequisite"); return page.Banner{} }
			h.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Fatal("MCP before prerequisite"); return nil }
			h.p.Sink = forbiddenSink{t}
			h.p.Environ = func() []string { t.Fatal("started git before prerequisite"); return nil }
			code := cli.Run(context.Background(), h.p)
			name := "git"
			if which == "python" {
				name = runner.Interpreter
			}
			if code != cli.ExitServerFailed || h.stderr.String() != "scripts: "+name+" not found on PATH\n" || h.stdout.String() != "" {
				t.Fatalf("%d %q", code, h.stderr.String())
			}
			if _, e := os.Stat(h.p.Dir); !os.IsNotExist(e) {
				t.Fatalf("Dir touched: %v", e)
			}
		})
	}
}

// R-H1MU-LK6D R-H2UQ-ZBX2
func TestStateFailures(t *testing.T) {
	for _, kind := range []string{"state-file", "invalid-db", "runs-file"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			mustCLI(t, os.MkdirAll(h.p.Dir, 0700))
			target := filepath.Join(h.p.Dir, "state")
			if kind != "state-file" {
				mustCLI(t, os.Mkdir(target, 0700))
				if kind == "invalid-db" {
					target = filepath.Join(target, "scripts.db")
				} else {
					target = filepath.Join(target, "runs")
				}
			}
			mustCLI(t, os.WriteFile(target, []byte("unchanged"), 0600))
			var oracleErr error
			prefix := "scripts: cannot open database state/scripts.db: "
			if kind == "runs-file" {
				oracleErr = os.MkdirAll(filepath.Join(h.p.Dir, "state", "runs"), 0700)
				prefix = "scripts: cannot create directory state/runs: "
			} else {
				_, oracleErr = db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
			}
			if oracleErr == nil {
				t.Fatal("fixture did not refuse startup")
			}
			code := cli.Run(context.Background(), h.p)
			if code != cli.ExitServerFailed || h.stderr.String() != prefix+strings.ReplaceAll(oracleErr.Error(), "\n", " ")+"\n" || len(h.stderr.snapshot()) != 1 || h.stdout.String() != "" || len(h.sink.capture.Events()) != 0 {
				t.Fatalf("%d %q", code, h.stderr.String())
			}
			scoped, e := os.OpenRoot(h.p.Dir)
			mustCLI(t, e)
			rel, e := filepath.Rel(h.p.Dir, target)
			mustCLI(t, e)
			b, e := scoped.ReadFile(rel)
			_ = scoped.Close()
			mustCLI(t, e)
			if string(b) != "unchanged" {
				t.Fatalf("changed file: %q", b)
			}
			if kind == "invalid-db" {
				if _, e = os.Stat(filepath.Join(h.p.Dir, "state", "runs")); !os.IsNotExist(e) {
					t.Fatalf("created runs: %v", e)
				}
			}
		})
	}
}

// R-ZV2S-V0UF R-HJXC-C4AS R-B63F-NUCG R-WNGS-KWCF R-HOSX-V79K R-L2WR-PRUX R-K58H-2BO4 R-H8Y8-W6MJ
func TestReadyInitialStateAndLifecycle(t *testing.T) {
	for _, servicesPath := range []string{"", "absent", "malformed"} {
		t.Run(servicesPath, func(t *testing.T) {
			h := newHarness(t)
			if servicesPath != "" {
				p := filepath.Join(h.root, servicesPath)
				h.set("IKIGENBA_SERVICES", p)
				if servicesPath == "malformed" {
					mustCLI(t, os.WriteFile(p, []byte("not json"), 0600))
				}
			}
			trace := filepath.Join(h.root, "trace")
			h.set("GIT_TRACE2_EVENT", trace)
			var calls atomic.Int32
			orig := h.p.MCP
			h.p.MCP = func(w *telemetry.Writer) *mcp.Server { calls.Add(1); return orig(w) }
			seen := map[string]bool{}
			origLookup := h.p.LookupEnv
			h.p.LookupEnv = func(k string) (string, bool) {
				switch k {
				case "LISTEN_PID", "LISTEN_FDS", "PATH", "NOTIFY_SOCKET", "IKIGENBA_SERVICES":
					for _, key := range []string{"DRAIN_SECONDS", "TREE_MAX_BYTES", "OUTPUT_MAX_BYTES", "OPERATION_SECONDS", "SCRIPT_SECONDS", "RUN_KEEP_DAYS", "RUN_KEEP_COUNT", "REPOS_DIR"} {
						if !seen[key] {
							t.Errorf("%s before setting %s", k, key)
						}
					}
				}
				seen[k] = true
				return origLookup(k)
			}
			h.start()
			if calls.Load() != 1 {
				t.Fatalf("MCP calls %d", calls.Load())
			}
			for _, p := range []string{"state", "state/scripts.db", "state/runs"} {
				if _, e := os.Stat(filepath.Join(h.p.Dir, p)); e != nil {
					t.Fatal(e)
				}
			}
			files, e := os.ReadDir(filepath.Join(h.p.Dir, "state", "runs"))
			mustCLI(t, e)
			if len(files) != 0 {
				t.Fatal("initial runs nonempty")
			}
			if _, e = os.Stat(trace); !os.IsNotExist(e) {
				t.Fatalf("git started before request: %v", e)
			}
			repos, _ := h.lookup("REPOS_DIR")
			if _, e = os.Stat(repos); !os.IsNotExist(e) {
				t.Fatalf("repository root created: %v", e)
			}
			v := h.call("list", nil)
			if a, ok := v["scripts"].([]any); !ok || len(a) != 0 {
				t.Fatalf("list %v", v)
			}
			h.stop()
			if calls.Load() != 1 {
				t.Fatalf("MCP called again: %d", calls.Load())
			}
			events := h.sink.capture.Events()
			if events[0].Name != "service.started" || events[len(events)-1].Name != "service.stopping" {
				t.Fatalf("lifecycle %v", events)
			}
			start := events[0]
			if start.Service != "scripts" || start.RequestID != "" || start.User != "" || !reflect.DeepEqual(start.Attrs, telemetry.Attrs{"version": testVersion}) {
				t.Fatalf("start %v", start)
			}
			allowed := " DRAIN_SECONDS REPOS_DIR TREE_MAX_BYTES OUTPUT_MAX_BYTES OPERATION_SECONDS SCRIPT_SECONDS RUN_MEMORY_MAX_BYTES RUNS_MEMORY_MAX_BYTES RUNS_CPU_PERCENT RUN_PIDS_MAX RUN_MAX_ACTIVE RUN_MAX_QUEUED RUN_KEEP_DAYS RUN_KEEP_COUNT IKIGENBA_SERVICES LISTEN_PID LISTEN_FDS NOTIFY_SOCKET PATH "
			for key := range seen {
				if !strings.Contains(allowed, " "+key+" ") {
					t.Errorf("looked up %s", key)
				}
			}
			if h.stdout.String() != "" || h.stderr.String() != "" {
				t.Fatalf("unexpected streams %q %q", h.stdout.String(), h.stderr.String())
			}
		})
	}
}

// R-JUC5-S888 R-JRWD-0OQU R-JVK2-5ZYX R-JPGK-959G R-KDUJ-WK3C R-JZ7R-BB70 R-JXZU-XJGB R-WM8W-74LQ R-HF1Q-T1C0
func TestRunCompositionUsesInjectedState(t *testing.T) {
	h := newHarness(t)
	h.p.Rand = repeatingRandom(0x5a)
	h.set("REPOS_DIR", "../repos-b/state/repos")
	h.start()
	h.repository("import sys\nprint(\"%d.%d\" % sys.version_info[:2])\n")
	h.set("REPOS_DIR", filepath.Join(h.root, "wrong-repos"))
	h.set("PATH", "/absent")
	sc := h.create("alpha")
	if sc["id"] != "scr_5a5a5a5a5a5a5a5a" || sc["created"] != h.now.UTC().Truncate(time.Second).Format(time.RFC3339) {
		t.Fatalf("script %v", sc)
	}
	r := h.call("run", map[string]any{"name": "alpha"})
	id, _ := r["id"].(string)
	if id != "run_5a5a5a5a5a5a5a5a" {
		t.Fatalf("run %v", r)
	}
	finished := h.event("run.finished")
	if finished.Attrs["status"] != "exited" {
		t.Fatal(finished)
	}
	result := h.call("result", map[string]any{"run": id})
	if result["status"] != "exited" || result["exit_code"] != float64(0) || result["stdout"] != runner.PythonVersion+"\n" || result["started"] != sc["created"] {
		t.Fatalf("result %v", result)
	}
	if _, e := os.Stat(filepath.Join(h.p.Dir, "state", "runs", sc["id"].(string), id)); e != nil {
		t.Fatal(e)
	}
	h.stop()
	for _, event := range h.sink.capture.Events() {
		if event.Service != "scripts" || !event.Time.Equal(h.now.UTC().Truncate(time.Microsecond)) {
			t.Fatalf("injected writer: %v", event)
		}
	}
}

// R-996F-83FE
func TestCatalogPathsSurviveRestart(t *testing.T) {
	h := newHarness(t)
	h.repository("print('hello')\n")
	h.start()
	h.create("alpha")
	h.stop()
	next := newHarness(t)
	next.p.Dir = h.p.Dir
	next.start()
	list := next.call("list", nil)
	if len(list["scripts"].([]any)) != 1 {
		t.Fatalf("restart list %v", list)
	}
	next.stop()
	third := newHarness(t)
	third.start()
	list = third.call("list", nil)
	if len(list["scripts"].([]any)) != 0 {
		t.Fatalf("other Dir %v", list)
	}
	third.stop()
}

// R-H5AJ-QVEG R-B7BC-1M35
func TestCancellationAndNotificationFailure(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := cli.Run(ctx, h.p); code != cli.ExitSuccess || h.stdout.String() != "" || h.stderr.String() != "" || len(h.sink.capture.Events()) != 0 {
		t.Fatalf("canceled start %d %q", code, h.stderr.String())
	}
	broken := newHarness(t)
	path := filepath.Join(broken.root, "absent.sock")
	broken.set("NOTIFY_SOCKET", path)
	code := cli.Run(context.Background(), broken.p)
	writes := broken.stderr.snapshot()
	if code != cli.ExitServerFailed || len(writes) != 1 || !strings.HasPrefix(string(writes[0]), "scripts: ") || !strings.Contains(string(writes[0]), path) || strings.Count(string(writes[0]), "\n") != 1 || len(broken.sink.capture.Events()) != 0 {
		t.Fatalf("notify failure %d %q", code, writes)
	}
}

func seedCatalog(t *testing.T, h *runHarness) (store.Script, []store.Run) {
	t.Helper()
	stDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, e)
	st := store.New(stDB, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	defer func() { _ = stDB.Close() }()
	sc, e := st.Create(context.Background(), store.Draft{Owner: "owner", Name: "alpha", Repo: "rep_0102030405060708", Ref: "main"})
	mustCLI(t, e)
	var records []store.Run
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("run_%016x", i+1)
		r, e := st.AddRun(context.Background(), store.Run{ID: id, Script: sc.ID, Ref: "main", SHA: strings.Repeat("a", 40), User: "owner", RequestID: "recovered", Trigger: "manual", Status: store.StatusRunning, Started: h.now.Add(-time.Duration(3-i) * 24 * time.Hour)})
		mustCLI(t, e)
		if i == 0 {
			r, e = st.FinishRun(context.Background(), id, store.Ending{Status: store.StatusExited, Finished: r.Started.Add(time.Hour)})
			mustCLI(t, e)
		}
		records = append(records, r)
		folder := filepath.Join(h.p.Dir, "state", "runs", sc.ID, id)
		mustCLI(t, os.MkdirAll(folder, 0700))
		mustCLI(t, os.WriteFile(filepath.Join(folder, "marker"), []byte("unchanged"), 0600))
	}
	return sc, records
}

// R-AYS1-D7WA R-LL79-GBZC R-HDTU-F9LB R-HG9N-6T2P
func TestRecoveryAndPruningBeforeReady(t *testing.T) {
	for _, cancelStart := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelStart), func(t *testing.T) {
			h := newHarness(t)
			h.set("RUN_KEEP_DAYS", "1")
			h.set("RUN_KEEP_COUNT", "1")
			sc, rs := seedCatalog(t, h)
			if cancelStart {
				ctx, cancel := context.WithCancel(context.Background())
				h.p.Now = func() time.Time { cancel(); return h.now }
				if code := cli.Run(ctx, h.p); code != cli.ExitSuccess {
					t.Fatalf("canceled recovery %d", code)
				}
			} else {
				h.start()
				v := h.call("result", map[string]any{"run": rs[1].ID})
				if v["status"] != "killed" || v["exit_code"] != nil {
					t.Fatalf("recovery %v", v)
				}
				h.stop()
			}
			stDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, e)
			st := store.New(stDB, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
			defer func() { _ = stDB.Close() }()
			r, e := st.RunByID(context.Background(), rs[1].ID)
			mustCLI(t, e)
			if r.Status != store.StatusKilled {
				t.Fatal(r)
			}
			if _, e = st.RunByID(context.Background(), rs[0].ID); !errors.Is(e, store.ErrNotFound) {
				t.Fatalf("not pruned: %v", e)
			}
			if _, e = os.Stat(filepath.Join(h.p.Dir, "state", "runs", sc.ID, rs[0].ID)); !os.IsNotExist(e) {
				t.Fatalf("old folder kept: %v", e)
			}
			b, e := os.ReadFile(filepath.Join(h.p.Dir, "state", "runs", sc.ID, rs[1].ID, "marker"))
			mustCLI(t, e)
			if string(b) != "unchanged" {
				t.Fatal("recovered folder changed")
			}
			events := h.sink.capture.Events()
			if len(events) == 0 || events[0].Name != "run.finished" || events[0].Attrs["status"] != "killed" {
				t.Fatalf("events %v", events)
			}
			if cancelStart && len(events) != 1 {
				t.Fatalf("startup cancellation events %v", events)
			}
		})
	}
}

type failListener struct {
	net.Listener
	err   error
	once  sync.Once
	retry bool
}

func (l *failListener) Accept() (net.Conn, error) {
	first := false
	l.once.Do(func() { first = true })
	if first {
		return nil, l.err
	}
	if l.retry {
		return l.Listener.Accept()
	}
	return nil, l.err
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "retry accept" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

// R-BN61-0MQ6 R-C1ST-LVMI
func TestAcceptFailuresAndPanicAvoidGlobalLogger(t *testing.T) {
	h := newHarness(t)
	h.listener = &failListener{Listener: h.listener, err: errors.New("accept broke")}
	h.start()
	if code := h.finish(); code != cli.ExitServerFailed || !strings.HasSuffix(h.stderr.String(), "scripts: accept broke\n") {
		t.Fatalf("%d %q", code, h.stderr.String())
	}
	retry := newHarness(t)
	retry.listener = &failListener{Listener: retry.listener, err: temporaryError{}, retry: true}
	retry.p.Banner = func(page.User) page.Banner { panic("banner") }
	var buffer lockedBuffer
	old := log.Writer()
	log.SetOutput(&buffer)
	defer log.SetOutput(old)
	retry.start()
	req, e := http.NewRequest("GET", "http://"+retry.listener.Addr().String()+"/", nil)
	mustCLI(t, e)
	req.Header.Set("X-User-Id", "owner")
	_, e = retry.http.Do(req)
	if e == nil {
		t.Fatal("panic unexpectedly answered")
	}
	retry.stop()
	if buffer.String() != "" {
		t.Fatalf("default logger: %q", buffer.String())
	}
}

// R-HTOJ-EA8C R-HW4C-5TPQ R-H6IG-4N55 R-HXC8-JLGF
func TestQuietServeAndLifecycleOrdering(t *testing.T) {
	h := newHarness(t)
	h.repository("raise SystemExit(3)\n")
	h.start()
	select {
	case code := <-h.done:
		t.Fatalf("returned before cancellation: %d", code)
	default:
	}
	status, notfound := h.request("GET", "/nope")
	if !strings.Contains(notfound, "Not found") {
		t.Fatal("missing notfound page")
	}
	if status != 404 {
		t.Fatalf("status %d", status)
	}
	reqResult, e := h.result("show", map[string]any{"name": "missing"})
	mustCLI(t, e)
	if !reqResult.IsError() {
		t.Fatal("missing script not refused")
	}
	req, e := http.NewRequest("POST", "http://"+h.listener.Addr().String()+"/mcp", nil)
	mustCLI(t, e)
	res, e := h.http.Do(req)
	mustCLI(t, e)
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 500 {
		t.Fatal(res.StatusCode)
	}
	h.create("alpha")
	r := h.call("run", map[string]any{"name": "alpha"})
	h.event("run.finished")
	v := h.call("result", map[string]any{"run": r["id"]})
	if v["status"] != "exited" || v["exit_code"] != float64(3) {
		t.Fatal(v)
	}
	h.stop()
	events := h.sink.capture.Events()
	last := events[len(events)-1]
	if last.Name != "service.stopping" || last.RequestID != "" || last.User != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "test stop"}) {
		t.Fatalf("stop %v", last)
	}
	if h.stdout.String() != "" || h.stderr.String() != "" {
		t.Fatalf("streams %q %q", h.stdout.String(), h.stderr.String())
	}
	for _, e := range events {
		if e.Name != "service.started" && e.Name != "service.stopping" && e.RequestID == "" {
			t.Fatalf("event without request identity: %v", e)
		}
	}
}

type rejectingSink struct{}

func (rejectingSink) Deliver(context.Context, telemetry.Event) error { return errors.New("reject") }

// R-BZD0-UC54
func TestUndeliveredEventsUseWholeDiagnostics(t *testing.T) {
	h := newHarness(t)
	rejected := &capturedRejection{}
	h.p.Sink = rejected
	h.start()
	status, _ := h.request("GET", "/about")
	if status != 200 {
		t.Fatal(status)
	}
	h.stop()
	writes := h.stderr.snapshot()
	if len(writes) != 4 {
		t.Fatalf("writes %d: %q", len(writes), writes)
	}
	names := []string{"service.started", "request.started", "request.finished", "service.stopping"}
	rejectedEvents := rejected.capture.Events()
	if len(rejectedEvents) != len(writes) {
		t.Fatalf("rejected events %d", len(rejectedEvents))
	}
	for i, b := range writes {
		encoded, e := rejectedEvents[i].MarshalJSON()
		mustCLI(t, e)
		if string(b) != "scripts: undelivered event: "+string(encoded)+"\n" {
			t.Fatalf("noncanonical event line %q", b)
		}
		if i == 1 || i == 2 {
			if rejectedEvents[i].RequestID != "http-request" || rejectedEvents[i].User != "owner" {
				t.Fatal(rejectedEvents[i])
			}
		}
		if !strings.HasPrefix(string(b), "scripts: undelivered event: ") || !strings.HasSuffix(string(b), "\n") || !strings.Contains(string(b), `"event":"`+names[i]+`"`) {
			t.Fatalf("line %d %s", i, b)
		}
	}
}

var _ io.Writer = (*lockedBuffer)(nil)

// R-HHHJ-KKTE R-ASOJ-GD6T R-S4FN-JZY1
func TestGitEnvironmentAndAllProductsRemainUnderDir(t *testing.T) {
	h := newHarness(t)
	h.repository("import os\nwith open(os.path.join(os.environ['IKIGENBA_OUT_DIR'], 'answer.txt'),'w') as f: f.write('answer')\n")
	h.set("TMPDIR", filepath.Join(h.root, "temporary"))
	mustCLI(t, os.MkdirAll(filepath.Join(h.root, "temporary"), 0700))
	h.set("LISTEN_FDNAMES", "scripts")
	var envCalls int
	orig := h.p.Environ
	h.p.Environ = func() []string {
		envCalls++
		for _, entry := range orig() {
			if strings.HasPrefix(entry, "LISTEN_") {
				t.Errorf("activation environment leaked: %s", entry)
			}
		}
		return orig()
	}
	h.start()
	before := outsideSnapshot(t, h.root, h.p.Dir, h.p.Cgroup)
	sc := h.create("alpha")
	r := h.call("run", map[string]any{"name": "alpha"})
	h.event("run.finished")
	h.call("result", map[string]any{"run": r["id"]})
	status, _ := h.request("GET", "/alpha/runs/"+r["id"].(string)+"/")
	if status != 200 {
		t.Fatalf("run page %d", status)
	}
	h.stop()
	if envCalls == 0 {
		t.Fatal("git did not consume injected environment")
	}
	if !reflect.DeepEqual(before, outsideSnapshot(t, h.root, h.p.Dir, h.p.Cgroup)) {
		t.Fatal("changed files outside process directory")
	}
	b, e := os.ReadFile(filepath.Join(h.p.Dir, "state", "runs", sc["id"].(string), r["id"].(string), "out", "answer.txt"))
	mustCLI(t, e)
	if string(b) != "answer" {
		t.Fatalf("output %q", b)
	}
}
func outsideSnapshot(t *testing.T, root string, excluded ...string) map[string]string {
	t.Helper()
	scoped, e := os.OpenRoot(root)
	mustCLI(t, e)
	defer func() { _ = scoped.Close() }()
	result := map[string]string{}
	excludedPaths := map[string]bool{}
	for _, path := range excluded {
		rel, err := filepath.Rel(root, path)
		mustCLI(t, err)
		excludedPaths[rel] = true
	}
	mustCLI(t, fs.WalkDir(scoped.FS(), ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if excludedPaths[p] {
			return filepath.SkipDir
		}
		if d.IsDir() {
			result[p] = "directory"
			return nil
		}
		b, e := scoped.ReadFile(p)
		if e != nil {
			return e
		}
		result[p] = string(b)
		return nil
	}))
	return result
}

// R-H42N-D3NR
func TestRecoveryCatalogFailureRefusesStartup(t *testing.T) {
	h := newHarness(t)
	seedCatalog(t, h)
	var once sync.Once
	h.p.Now = func() time.Time {
		if _, err := os.Stat(filepath.Join(h.p.Dir, "state", "runs")); err == nil {
			once.Do(func() {
				handle, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: func() time.Time { return h.now }})
				mustCLI(t, e)
				mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec("DROP TABLE catalog"); return err }))
				mustCLI(t, handle.Close())
			})
		}
		return h.now
	}
	h.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Fatal("MCP after recovery failure"); return nil }
	code := cli.Run(context.Background(), h.p)
	if code != cli.ExitServerFailed || h.stdout.String() != "" || h.stderr.String() != "scripts: "+store.Unreachable+"\n" || len(h.sink.capture.Events()) != 0 {
		t.Fatalf("recovery failure %d %q %v", code, h.stderr.String(), h.sink.capture.Events())
	}
}

const waitingMain = `import os
while not os.path.exists(os.path.join(os.environ['IKIGENBA_OUT_DIR'], 'release')):
    pass
print('finished')
`

// R-HQ0U-8Z09 R-HUWF-S1Z1 R-HTOJ-EA8C
func TestGracefulDrainWaitsForRunThenStopsEarly(t *testing.T) {
	h := newHarness(t)
	h.set("DRAIN_SECONDS", "30")
	h.repository(waitingMain)
	h.start()
	sc := h.create("alpha")
	r := h.call("run", map[string]any{"name": "alpha"})
	h.event("run.started")
	h.cancel(errors.New("graceful"))
	// A refused fresh connection proves the listening socket is closed.
	connectionDeadline(t, h.listener.Addr().String())
	select {
	case code := <-h.done:
		t.Fatalf("returned while script alive: %d", code)
	default:
	}
	mustCLI(t, os.WriteFile(filepath.Join(h.p.Dir, "state", "runs", sc["id"].(string), r["id"].(string), "out", "release"), nil, 0600))
	if code := h.finish(); code != cli.ExitSuccess {
		t.Fatalf("drain %d %s", code, h.stderr.String())
	}
	stDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, e)
	st := store.New(stDB, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	defer func() { _ = stDB.Close() }()
	ended, e := st.RunByID(context.Background(), r["id"].(string))
	mustCLI(t, e)
	if ended.Status != store.StatusExited || ended.ExitCode != 0 {
		t.Fatalf("ended %v", ended)
	}
	running, e := st.Running(context.Background())
	mustCLI(t, e)
	if len(running) != 0 {
		t.Fatalf("running %v", running)
	}
	if h.stderr.String() != "" {
		t.Fatalf("stderr %s", h.stderr.String())
	}
	events := h.sink.capture.Events()
	last := events[len(events)-1]
	if last.Name != "service.stopping" || last.Attrs["reason"] != "graceful" {
		t.Fatal(last)
	}
}
func connectionDeadline(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, e := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if e != nil {
			return
		}
		_ = c.Close()
		if time.Now().After(deadline) {
			t.Fatal("listener stayed open")
		}
	}
}

// R-HSGN-0IHN R-HW4C-5TPQ R-HZS1-B4XT R-HUWF-S1Z1
func TestDrainDeadlineKillsRunBeforeStoppingEvent(t *testing.T) {
	h := newHarness(t)
	h.repository("import os, subprocess, sys\nsubprocess.Popen([sys.executable, '-c', 'while True: pass'])\nwhile True: pass\n")
	h.start()
	h.create("alpha")
	r := h.call("run", map[string]any{"name": "alpha"})
	h.event("run.started")
	start := time.Now()
	h.cancel(errors.New("deadline"))
	if code := h.finish(); code != cli.ExitSuccess {
		t.Fatalf("drain %d %s", code, h.stderr.String())
	}
	elapsed := time.Since(start)
	if elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatalf("drain elapsed %v", elapsed)
	}
	for _, e := range h.sink.capture.Events() {
		if e.Name == "service.stopping" || e.Name == "run.finished" {
			t.Fatalf("event delivered after deadline: %v", e)
		}
	}
	writes := h.stderr.snapshot()
	if len(writes) != 2 || !strings.Contains(string(writes[0]), `"event":"run.finished"`) || !strings.Contains(string(writes[0]), `"status":"killed"`) || !strings.Contains(string(writes[1]), `"event":"service.stopping"`) {
		t.Fatalf("cutoff lines %q", writes)
	}
	stDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, e)
	st := store.New(stDB, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	defer func() { _ = stDB.Close() }()
	ended, e := st.RunByID(context.Background(), r["id"].(string))
	mustCLI(t, e)
	if ended.Status != store.StatusKilled {
		t.Fatal(ended)
	}
	running, e := st.Running(context.Background())
	mustCLI(t, e)
	if len(running) != 0 {
		t.Fatal(running)
	}
	assertNoProcess(t, "IKIGENBA_RUN_ID="+r["id"].(string))
}
func assertNoProcess(t *testing.T, variable string) {
	t.Helper()
	entries, e := os.ReadDir("/proc")
	mustCLI(t, e)
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if e != nil {
			continue
		}
		for _, v := range bytes.Split(b, []byte{0}) {
			if string(v) == variable {
				t.Fatalf("process %s survived: %s", entry.Name(), variable)
			}
		}
	}
}

// R-HYK4-XD74 R-I27U-2OF7
func TestUnfinishedRequestStopsOnlyAfterEventDiagnostic(t *testing.T) {
	h := newHarness(t)
	h.start()
	c, e := net.Dial("tcp", h.listener.Addr().String())
	mustCLI(t, e)
	defer func() { _ = c.Close() }()
	mustCLI(t, c.SetDeadline(time.Now().Add(5*time.Second)))
	_, e = io.WriteString(c, "POST /mcp HTTP/1.1\r\nHost: backend\r\nX-User-Id: owner\r\nX-Request-Id: partial\r\nContent-Type: application/json\r\nContent-Length: 100000\r\n\r\n{")
	mustCLI(t, e)
	h.event("request.started")
	start := time.Now()
	h.cancel(errors.New("unfinished"))
	b := make([]byte, 1)
	_, e = c.Read(b)
	if e == nil {
		t.Fatal("unfinished response written")
	}
	if !strings.Contains(h.stderr.String(), `"event":"service.stopping"`) {
		t.Fatal("connection closed before stopping diagnostic")
	}
	if code := h.finish(); code != cli.ExitServerFailed {
		t.Fatalf("code %d", code)
	}
	if time.Since(start) < time.Second || time.Since(start) >= 2*time.Second {
		t.Fatal("cutoff outside drain window")
	}
	writes := h.stderr.snapshot()
	if string(writes[len(writes)-1]) != "scripts: stopped with 1 request unfinished\n" {
		t.Fatalf("last diagnostic %q", writes)
	}
	for _, e := range h.sink.capture.Events() {
		if e.Name == "service.stopping" || e.Name == "request.finished" {
			t.Fatalf("cutoff event delivered %v", e)
		}
	}
}

// R-I0ZX-OWOI R-I3FQ-GG5W
func TestBlockedGitIsKilledWithoutCutoffMutations(t *testing.T) {
	h := newHarness(t)
	h.repository("print('hello')\n")
	h.start()
	sc := h.create("alpha")
	h.event("request.finished")
	for {
		select {
		case <-h.gitTimers:
		default:
			goto emptied
		}
	}
emptied:
	fifo := filepath.Join(h.root, "git-trace")
	mustCLI(t, syscall.Mkfifo(fifo, 0600))
	h.set("GIT_TRACE", fifo)
	result := make(chan error, 2)
	for _, tc := range []struct {
		tool, id string
		args     string
	}{{"create", "blocked-create", `{"name":"beta","repo":"rep_0102030405060708"}`}, {"run", "blocked-run", `{"name":"alpha"}`}} {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, e := h.client.CallTool(ctx, identity.Caller{UserID: "owner", RequestID: tc.id}, tc.tool, json.RawMessage(tc.args))
			result <- e
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-h.gitTimers:
		case <-time.After(5 * time.Second):
			t.Fatal("git did not begin")
		}
	}
	cancelledAt := time.Now()
	h.cancel(errors.New("git deadline"))
	if code := h.finish(); code != cli.ExitServerFailed {
		t.Fatalf("cutoff %d %s", code, h.stderr.String())
	}
	assertNoProcess(t, "GIT_TRACE="+fifo)
	if time.Since(cancelledAt) >= 2*time.Second {
		t.Fatal("blocked git cutoff return and absence exceeded deadline plus one second")
	}
	for i := 0; i < 2; i++ {
		select {
		case <-result:
		case <-time.After(5 * time.Second):
			t.Fatal("cutoff client did not end")
		}
	}
	assertNoProcess(t, "GIT_TRACE="+fifo)
	for _, event := range h.sink.capture.Events() {
		if (event.RequestID == "blocked-create" || event.RequestID == "blocked-run") && (event.Name == "tool.called" || strings.HasPrefix(event.Name, "script.") || strings.HasPrefix(event.Name, "run.")) {
			t.Fatalf("cutoff event %v", event)
		}
	}
	for _, line := range h.stderr.snapshot() {
		if strings.Contains(string(line), `"request_id":"blocked-`) && (strings.Contains(string(line), `"event":"tool.called"`) || strings.Contains(string(line), `"event":"script.`) || strings.Contains(string(line), `"event":"run.`)) {
			t.Fatalf("cutoff diagnostic %s", line)
		}
	}
	stDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, e)
	st := store.New(stDB, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	defer func() { _ = stDB.Close() }()
	scripts, e := st.List(context.Background(), "owner")
	mustCLI(t, e)
	if len(scripts) != 1 || scripts[0].Name != "alpha" {
		t.Fatalf("cutoff create persisted %v", scripts)
	}
	records, e := st.Runs(context.Background(), sc["id"].(string))
	mustCLI(t, e)
	if len(records) != 0 {
		t.Fatal(records)
	}
	entries, e := os.ReadDir(filepath.Join(h.p.Dir, "state", "runs", sc["id"].(string)))
	if e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("cutoff folders %v", entries)
	}
}

// R-ZP6T-PRMC
func TestRunRequestFinishingBodyDuringDrainIsRefused(t *testing.T) {
	h := newHarness(t)
	h.set("DRAIN_SECONDS", "30")
	h.repository(waitingMain)
	h.start()
	sc := h.create("alpha")
	r := h.call("run", map[string]any{"name": "alpha"})
	h.event("run.started")
	params := map[string]any{"jsonrpc": "2.0", "id": 42, "method": "tools/call", "params": map[string]any{"name": "run", "arguments": map[string]any{"name": "alpha"}, "_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": mcp.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}}
	body, e := json.Marshal(params)
	mustCLI(t, e)
	c, e := net.Dial("tcp", h.listener.Addr().String())
	mustCLI(t, e)
	defer func() { _ = c.Close() }()
	mustCLI(t, c.SetDeadline(time.Now().Add(5*time.Second)))
	headers := fmt.Sprintf("POST /mcp HTTP/1.1\r\nHost: backend\r\nX-User-Id: owner\r\nX-Request-Id: delayed-run\r\nContent-Type: application/json\r\nMCP-Protocol-Version: %s\r\nMcp-Method: tools/call\r\nMcp-Name: run\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", mcp.ProtocolVersion, len(body))
	_, e = io.WriteString(c, headers)
	mustCLI(t, e)
	h.event("request.started")
	h.cancel(errors.New("delayed"))
	connectionDeadline(t, h.listener.Addr().String())
	_, e = c.Write(body)
	mustCLI(t, e)
	res, e := http.ReadResponse(bufio.NewReader(c), nil)
	mustCLI(t, e)
	b, e := io.ReadAll(res.Body)
	mustCLI(t, e)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(b), `"isError":true`) || !strings.Contains(string(b), runs.Stopping) {
		t.Fatalf("drain refusal %d %s", res.StatusCode, b)
	}
	mustCLI(t, os.WriteFile(filepath.Join(h.p.Dir, "state", "runs", sc["id"].(string), r["id"].(string), "out", "release"), nil, 0600))
	if code := h.finish(); code != cli.ExitSuccess {
		t.Fatalf("stop %d %s", code, h.stderr.String())
	}
	entries, e := os.ReadDir(filepath.Join(h.p.Dir, "state", "runs", sc["id"].(string)))
	mustCLI(t, e)
	if len(entries) != 1 || entries[0].Name() != r["id"] {
		t.Fatalf("late run made folder: %v", entries)
	}
}

type overlapReader struct {
	active  atomic.Int32
	overlap atomic.Bool
	n       byte
}

func (r *overlapReader) Read(b []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	runtime.Gosched()
	for i := range b {
		r.n++
		b[i] = r.n
	}
	r.active.Add(-1)
	return len(b), nil
}

type overlapWriter struct {
	active  atomic.Int32
	overlap atomic.Bool
	buffer  lockedBuffer
}

func (w *overlapWriter) Write(b []byte) (int, error) {
	if w.active.Add(1) != 1 {
		w.overlap.Store(true)
	}
	runtime.Gosched()
	n, e := w.buffer.Write(b)
	w.active.Add(-1)
	return n, e
}

// R-C0KX-83VT
func TestConcurrentRequestRandomnessAndDiagnosticsAreSerialized(t *testing.T) {
	h := newHarness(t)
	h.repository("print('done')\n")
	random := &overlapReader{}
	stderr := &overlapWriter{}
	h.p.Rand = random
	h.p.Stderr = stderr
	h.p.Sink = rejectingSink{}
	h.start()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			req, e := http.NewRequest("GET", "http://"+h.listener.Addr().String()+"/about", nil)
			mustCLI(t, e)
			req.Header.Set("X-User-Id", "owner")
			res, e := h.http.Do(req)
			mustCLI(t, e)
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		})
	}
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			name := fmt.Sprintf("parallel-%d", i)
			h.create(name)
			h.call("run", map[string]any{"name": name})
		})
	}
	wg.Wait()
	h.create("alpha")
	h.call("run", map[string]any{"name": "alpha"})
	h.stop()
	if random.overlap.Load() || stderr.overlap.Load() {
		t.Fatalf("overlapping process seams random=%v stderr=%v", random.overlap.Load(), stderr.overlap.Load())
	}
	lines := stderr.buffer.snapshot()
	if len(lines) < 18 {
		t.Fatalf("expected lifecycle and request diagnostics, got %d", len(lines))
	}
	for _, b := range lines {
		if !strings.HasPrefix(string(b), "scripts: undelivered event: ") || strings.Count(string(b), "\n") != 1 {
			t.Fatalf("diagnostic fragmented %q", b)
		}
	}
}

type capturedRejection struct{ capture telemetry.Capture }

func (s *capturedRejection) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(ctx, e)
	return telemetry.ErrRejected
}

// R-B63F-NUCG R-HOSX-V79K
func TestAbstractNotificationAndNoNotificationSocket(t *testing.T) {
	t.Run("abstract", func(t *testing.T) {
		h := newHarness(t)
		name := "@" + filepath.Base(filepath.Dir(h.notify.LocalAddr().String()))
		_ = h.notify.Close()
		var e error
		h.notify, e = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: name, Net: "unixgram"})
		mustCLI(t, e)
		h.set("NOTIFY_SOCKET", name)
		h.start()
		h.stop()
	})
	t.Run("none", func(t *testing.T) {
		h := newHarness(t)
		h.set("NOTIFY_SOCKET", "")
		ctx, cancel := context.WithCancelCause(context.Background())
		sink := &cancelOnReady{cancel: cancel}
		h.p.Sink = sink
		code := cli.Run(ctx, h.p)
		if code != cli.ExitSuccess {
			t.Fatalf("no-notify %d", code)
		}
		events := sink.capture.Events()
		if len(events) != 2 || events[0].Name != "service.started" || events[1].Name != "service.stopping" {
			t.Fatalf("events %v", events)
		}
	})
	t.Run("canceled-by-MCP", func(t *testing.T) {
		h := newHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		orig := h.p.MCP
		h.p.MCP = func(w *telemetry.Writer) *mcp.Server { cancel(); return orig(w) }
		if code := cli.Run(ctx, h.p); code != cli.ExitSuccess || len(h.sink.capture.Events()) != 0 || h.stderr.String() != "" {
			t.Fatalf("canceled before readiness %d %q", code, h.stderr.String())
		}
	})
}

type cancelOnReady struct {
	capture telemetry.Capture
	cancel  context.CancelCauseFunc
}

func (s *cancelOnReady) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(ctx, e)
	if e.Name == "service.started" {
		s.cancel(errors.New("ready"))
	}
	return nil
}

// R-WM8W-74LQ
func TestRequestIDUsesProcessRandomBytes(t *testing.T) {
	h := newHarness(t)
	known := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	random := bytes.NewReader(known)
	h.p.Rand = random
	h.start()
	req, e := http.NewRequest("GET", "http://"+h.listener.Addr().String()+"/about", nil)
	mustCLI(t, e)
	req.Header.Set("X-User-Id", "owner")
	req.Header.Set("X-Forwarded-Proto", "https")
	res, e := h.http.Do(req)
	mustCLI(t, e)
	_, e = io.Copy(io.Discard, res.Body)
	mustCLI(t, e)
	mustCLI(t, res.Body.Close())
	if res.StatusCode != http.StatusOK {
		t.Fatalf("about status %d", res.StatusCode)
	}
	h.stop()
	events := h.sink.capture.Events()
	if len(events) != 4 {
		t.Fatalf("events %v", events)
	}
	expected := hex.EncodeToString(known)
	for i, name := range []string{"request.started", "request.finished"} {
		event := events[i+1]
		if event.Name != name || event.RequestID != expected || event.User != "owner" {
			t.Fatalf("minted request identity %v; want %s", event, expected)
		}
	}
	if random.Len() != 0 {
		t.Fatalf("request ID left %d random bytes unread", random.Len())
	}
}

// R-HXC8-JLGF
func TestStaleCatalogWithoutRequestsEmitsOnlyLifecycle(t *testing.T) {
	h := newHarness(t)
	h.repository("print('unused')\n")
	database := filepath.Join(h.p.Dir, "state", "scripts.db")
	stDB, e := db.Open(context.Background(), db.Config{Path: database, Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, e)
	st := store.New(stDB, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	for i, repo := range []string{"rep_0102030405060708", "rep_1112131415161718"} {
		sc, e := st.Create(context.Background(), store.Draft{Owner: "owner", Name: fmt.Sprintf("stale-%d", i), Repo: repo, Ref: "main"})
		mustCLI(t, e)
		runID := fmt.Sprintf("run_%016x", i+1)
		started := h.now.Add(-time.Hour)
		_, e = st.AddRun(context.Background(), store.Run{ID: runID, Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: "owner", RequestID: "earlier-run", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: started})
		mustCLI(t, e)
		_, e = st.FinishRun(context.Background(), runID, store.Ending{Status: store.StatusExited, Finished: started.Add(time.Minute)})
		mustCLI(t, e)
		if i == 0 {
			folder := filepath.Join(h.p.Dir, "state", "runs", sc.ID, runID)
			mustCLI(t, os.MkdirAll(folder, 0700))
			mustCLI(t, os.WriteFile(filepath.Join(folder, "marker"), []byte("kept"), 0600))
		}
	}
	mustCLI(t, stDB.Close())
	h.start()
	h.stop()
	events := h.sink.capture.Events()
	if len(events) != 2 || events[0].Name != "service.started" || events[1].Name != "service.stopping" {
		t.Fatalf("unsolicited events from stale catalog: %v", events)
	}
	if h.stdout.String() != "" || h.stderr.String() != "" {
		t.Fatalf("unexpected streams %q %q", h.stdout.String(), h.stderr.String())
	}
}

// R-BEMQ-C8JB
func TestDrainDeliversCompleteInProgressCreate(t *testing.T) {
	h := newHarness(t)
	h.set("DRAIN_SECONDS", "30")
	h.repository("pass\n")
	h.start()
	fifo := filepath.Join(h.root, "graceful-git-trace")
	mustCLI(t, syscall.Mkfifo(fifo, 0600))
	h.set("GIT_TRACE", fifo)
	statuses := make(chan int, 1)
	h.http.Transport = responseTransport{RoundTripper: h.http.Transport, statuses: statuses}
	type response struct {
		result mcp.Result
		err    error
	}
	done := make(chan response, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := h.client.CallTool(ctx, identity.Caller{UserID: "owner", RequestID: "graceful-create"}, "create", json.RawMessage(`{"name":"graceful","repo":"rep_0102030405060708"}`))
		done <- response{result, err}
	}()
	select {
	case <-h.gitTimers:
	case <-time.After(10 * time.Second):
		t.Fatal("create git did not begin")
	}
	cancelledAt := time.Now()
	h.cancel(errors.New("graceful create"))
	connectionDeadline(t, h.listener.Addr().String())
	reader, err := os.OpenFile(filepath.Clean(fifo), os.O_RDWR, 0600)
	mustCLI(t, err)
	defer func() { _ = reader.Close() }()
	copied := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, reader); close(copied) }()
	var answer response
	select {
	case answer = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("create did not complete during drain")
	}
	mustCLI(t, answer.err)
	raw, err := answer.result.MarshalJSON()
	mustCLI(t, err)
	var body struct {
		Structured struct{ ID, Name string } `json:"structuredContent"`
	}
	mustCLI(t, json.Unmarshal(raw, &body))
	if answer.result.IsError() || body.Structured.ID == "" || body.Structured.Name != "graceful" {
		t.Fatalf("incomplete create response %s", raw)
	}
	if status := <-statuses; status != http.StatusOK {
		t.Fatalf("create HTTP status %d", status)
	}
	if code := h.finish(); code != cli.ExitSuccess || h.stderr.String() != "" || time.Since(cancelledAt) >= 30*time.Second {
		t.Fatalf("graceful drain %d %q", code, h.stderr.String())
	}
	created, finished := false, false
	for _, event := range h.sink.capture.Events() {
		if event.RequestID == "graceful-create" {
			if event.Name == "script.created" {
				created = event.Attrs["script"] == body.Structured.ID
			}
			if event.Name == "request.finished" {
				finished = event.Attrs["status"] == int64(http.StatusOK)
			}
		}
	}
	if !created || !finished {
		t.Fatalf("create completion events missing: %v", h.sink.capture.Events())
	}
	mustCLI(t, reader.Close())
	select {
	case <-copied:
	case <-time.After(time.Second):
		t.Fatal("trace reader did not close")
	}
}

type responseTransport struct {
	http.RoundTripper
	statuses chan int
}

func (r responseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := r.RoundTripper.RoundTrip(request)
	if response != nil {
		r.statuses <- response.StatusCode
	}
	return response, err
}
