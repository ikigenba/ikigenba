package runs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

type sequence struct {
	mu sync.Mutex
	n  byte
}

func (s *sequence) Read(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	for i := range b {
		b[i] = s.n
	}
	return len(b), nil
}

type eventSink struct {
	capture telemetry.Capture
	events  chan telemetry.Event
}

func (s *eventSink) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(ctx, e)
	s.events <- e
	return nil
}

type harness struct {
	t         *testing.T
	recordID  int
	core      *runs.Core
	cfg       runs.Config
	st        *store.Store
	sc        store.Script
	sink      *eventSink
	env       []string
	git       string
	repo      string
	sha       string
	clockMu   sync.Mutex
	now       time.Time
	timers    chan chan time.Time
	durations chan time.Duration
	limit     *limits.Limits
}

func fixture(t *testing.T, script string, extra ...map[string]string) *harness {
	t.Helper()
	root := t.TempDir()
	gitPath, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	python, e := exec.LookPath(runner.Interpreter)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Dir(gitPath) + ":" + filepath.Dir(python)
	h := &harness{t: t, git: gitPath, repo: filepath.Join(root, "repos", "rep_0102030405060708.git"), now: time.Date(2025, 1, 2, 3, 4, 5, 789000000, time.FixedZone("east", 3600)), timers: make(chan chan time.Time, 64), durations: make(chan time.Duration, 64), sink: &eventSink{events: make(chan telemetry.Event, 128)}}
	h.env = []string{"PATH=" + path, "HOME=" + root, "XDG_CONFIG_HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2025-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2025-01-01T00:00:00Z", "LANG=C.UTF-8"}
	work := filepath.Join(root, "work")
	must(t, os.MkdirAll(work, 0700))
	h.command(work, "init", "--initial-branch=main")
	must(t, os.WriteFile(filepath.Join(work, "main.py"), []byte(script), 0600))
	must(t, os.MkdirAll(filepath.Join(work, "sub"), 0700))
	must(t, os.WriteFile(filepath.Join(work, "sub", "data.txt"), []byte("data"), 0600))
	for _, files := range extra {
		for name, contents := range files {
			if link, ok := strings.CutPrefix(name, "symlink:"); ok {
				must(t, os.Symlink(contents, filepath.Join(work, link)))
			} else {
				must(t, os.WriteFile(filepath.Join(work, name), []byte(contents), 0600))
			}
		}
	}
	h.command(work, "add", ".")
	h.command(work, "commit", "-m", "fixture")
	must(t, os.MkdirAll(filepath.Dir(h.repo), 0700))
	h.command(root, "clone", "--bare", work, h.repo)
	h.sha = strings.TrimSpace(h.command(h.repo, "rev-parse", "HEAD"))
	st, e := store.Open(context.Background(), store.Config{Source: filepath.Join(root, "state", "catalog.db"), Now: h.readNow, Rand: &sequence{}})
	must(t, e)
	h.st = st
	h.sc, e = st.Create(context.Background(), store.Draft{Owner: "owner", Name: "job", Repo: "rep_0102030405060708", Ref: "main"})
	must(t, e)
	g, e := git.Find(path, func() []string { return append([]string(nil), h.env...) })
	must(t, e)
	s := settings.Defaults()
	h.limit = limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	writer := telemetry.New(telemetry.Config{Service: "scripts", Sink: h.sink, Stderr: io.Discard, Now: h.readNow, Rand: &sequence{}})
	h.cfg = runs.Config{Store: st, Source: source.New(source.Config{Repos: filepath.Dir(h.repo), Git: g, Limits: h.limit}), Writer: writer, Runs: filepath.Join(root, "state", "runs"), Path: path, Services: filepath.Join(root, "services"), ScriptSeconds: 9, OutputMaxBytes: 1024, KeepDays: 1, KeepCount: 1, Now: h.readNow, ScriptAfter: func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		h.timers <- ch
		h.durations <- d
		return ch
	}, Rand: &sequence{}}
	h.core = runs.New(h.cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h.core.Drain(ctx)
		writer.Shutdown(context.Background(), "test")
		_ = st.Close()
		cleanupRoot, e := os.OpenRoot(root)
		if e != nil {
			t.Error(e)
			return
		}
		defer func() { _ = cleanupRoot.Close() }()
		_ = fs.WalkDir(cleanupRoot.FS(), ".", func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			return cleanupRoot.Chmod(p, info.Mode().Perm()|0700)
		})
	})
	return h
}
func (h *harness) readNow() time.Time { h.clockMu.Lock(); defer h.clockMu.Unlock(); return h.now }
func (h *harness) setNow(v time.Time) { h.clockMu.Lock(); h.now = v; h.clockMu.Unlock() }
func (h *harness) command(dir string, args ...string) string {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.git, args...)
	cmd.Dir = dir
	cmd.Env = h.env
	b, e := cmd.CombinedOutput()
	if e != nil {
		h.t.Fatalf("git %v: %s: %v", args, b, e)
	}
	return string(b)
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func until(t *testing.T, fn func() bool) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for !fn() {
		select {
		case <-timer.C:
			t.Fatal("process observation deadline")
		default:
			runtime.Gosched()
		}
	}
}
func read(t *testing.T, path string) []byte {
	t.Helper()
	root, e := os.OpenRoot(filepath.Dir(path))
	must(t, e)
	defer func() { _ = root.Close() }()
	b, e := root.ReadFile(filepath.Base(path))
	must(t, e)
	return b
}
func (h *harness) run(input []byte) store.Run {
	h.t.Helper()
	r, e := h.core.Run(context.Background(), h.sc, runs.Request{Input: input, Caller: identity.Caller{UserID: "owner", RequestID: "request"}})
	must(h.t, e)
	return r
}
func (h *harness) finished(id string) telemetry.Event {
	h.t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-h.sink.events:
			if e.Name == "run.finished" && e.Attrs["run"] == id {
				return e
			}
		case <-timer.C:
			h.t.Fatal("run.finished deadline")
		}
	}
}
func (h *harness) record(id string) store.Run {
	h.t.Helper()
	r, e := h.st.RunByID(context.Background(), id)
	must(h.t, e)
	return r
}

const waitScript = `import os
while not os.path.exists(os.path.join(os.environ['IKIGENBA_RUN_DIR'], 'release')):
    pass
`

func (h *harness) release(r store.Run) {
	h.t.Helper()
	must(h.t, os.WriteFile(filepath.Join(h.core.Folder(r), "release"), nil, 0600))
}
func entries(t *testing.T, dir string) []string {
	t.Helper()
	ds, e := os.ReadDir(dir)
	must(t, e)
	ns := make([]string, len(ds))
	for i, d := range ds {
		ns[i] = d.Name()
	}
	return ns
}

// R-TRAW-TG9L R-TSIT-780A R-TTQP-KZQZ R-TUYL-YRHO R-TW6I-CJ8D R-TXEE-QAZ2
// R-UQ6M-B6LX R-UREI-OYCM R-U4PT-0XF8 R-U75L-SGWM R-U8DI-68NB R-U9LE-K0E0
// R-XBLK-US05 R-UC17-BJVE R-UFOW-GV3H R-V2DM-4W0V R-J746-MBVK R-VG9V-XZCB R-VI8B-3WNW
func TestRunFolderEnvironmentAndLifetime(t *testing.T) {
	script := `import os, json
root=os.environ['IKIGENBA_RUN_DIR']
initial_out=sorted(os.listdir(os.environ['IKIGENBA_OUT_DIR']))
with open(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'probe.json'),'w') as f:
    json.dump({'env':open('/proc/self/environ','rb').read().decode().split('\u0000')[:-1], 'entries':sorted(os.listdir(root)), 'input':open(os.environ['IKIGENBA_INPUT']).read(), 'cwd':os.getcwd(), 'out':initial_out},f)
os.makedirs(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'charts'))
open(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'charts','sales.svg'),'w').write('opaque')
while not os.path.exists(os.path.join(root,'release')): pass
`
	h := fixture(t, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := []byte(" { \"a\": 1 } \n")
	r, e := h.core.Run(ctx, h.sc, runs.Request{Input: raw, Caller: identity.Caller{UserID: "owner", RequestID: "request"}})
	must(t, e)
	if r.Status != store.StatusRunning || !store.ValidRunID(r.ID) || r.Script != h.sc.ID || r.SHA != h.sha || r.Ref != "main" || r.User != "owner" || r.RequestID != "request" || r.Trigger != store.TriggerManual || r.Started != h.readNow().UTC().Truncate(time.Second) {
		t.Fatalf("record: %+v", r)
	}
	if got := h.record(r.ID); !reflect.DeepEqual(got, r) {
		t.Fatalf("catalog: %+v", got)
	}
	folder := h.core.Folder(r)
	if folder != filepath.Join(h.cfg.Runs, r.Script, r.ID) {
		t.Fatal(folder)
	}
	probe := filepath.Join(folder, runs.OutDir, "probe.json")
	until(t, func() bool {
		root, e := os.OpenRoot(filepath.Dir(probe))
		if e != nil {
			return false
		}
		defer func() { _ = root.Close() }()
		b, _ := root.ReadFile(filepath.Base(probe))
		return json.Valid(b)
	})
	var p struct {
		Env, Entries, Out []string
		Input, Cwd        string
	}
	must(t, json.Unmarshal(read(t, probe), &p))
	expected := []string{"input.json", "out", "stderr", "stdout", "tree"}
	if !reflect.DeepEqual(p.Entries, expected) || p.Input != string(raw) || len(p.Out) != 0 || p.Cwd != filepath.Join(folder, runs.TreeDir) {
		t.Fatalf("probe %+v", p)
	}
	env := map[string]string{}
	for _, v := range p.Env {
		k, x, _ := strings.Cut(v, "=")
		if _, ok := env[k]; ok {
			t.Fatal("duplicate env")
		}
		env[k] = x
	}
	want := map[string]string{"PATH": h.cfg.Path, "HOME": folder, "LANG": "C.UTF-8", "IKIGENBA_RUN_ID": r.ID, "IKIGENBA_SCRIPT": h.sc.ID, "IKIGENBA_SHA": h.sha, "IKIGENBA_RUN_DIR": folder, "IKIGENBA_OUT_DIR": filepath.Join(folder, runs.OutDir), "IKIGENBA_INPUT": filepath.Join(folder, runs.InputFile), "IKIGENBA_USER_ID": "owner", "IKIGENBA_REQUEST_ID": "request", "IKIGENBA_SERVICES": h.cfg.Services}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("env %v", env)
	}
	must(t, filepath.WalkDir(filepath.Join(folder, runs.TreeDir), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		s, e := d.Info()
		if e == nil && s.Mode().Perm()&0222 != 0 {
			t.Errorf("writable %s", path)
		}
		return e
	}))
	if string(read(t, filepath.Join(folder, runs.OutDir, "charts", "sales.svg"))) != "opaque" {
		t.Fatal("out")
	}
	cancel()
	if h.record(r.ID).Status != store.StatusRunning {
		t.Fatal("caller ended run")
	}
	t1 := h.readNow().Add(2*time.Second + 1500*time.Microsecond)
	h.setNow(t1)
	h.release(r)
	ev := h.finished(r.ID)
	ended := h.record(r.ID)
	if ended.Status != store.StatusExited || ended.ExitCode != 0 || ended.Finished != t1.UTC().Truncate(time.Second) || ev.User != "owner" || ev.RequestID != "request" || ev.Attrs["duration_us"] != int64(2001500) {
		t.Fatalf("ended %+v event %+v", ended, ev)
	}
	var started, finished int
	for _, ev := range h.sink.capture.Events() {
		if ev.Attrs["run"] == r.ID {
			switch ev.Name {
			case "run.started":
				started++
			case "run.finished":
				finished++
			default:
				t.Fatal(ev)
			}
		}
	}
	if started != 1 || finished != 1 {
		t.Fatal(started, finished)
	}
	if runs.InputFile != "input.json" || runs.TreeDir != "tree" || runs.OutDir != "out" || runs.StdoutFile != "stdout" || runs.StderrFile != "stderr" {
		t.Fatal("names")
	}
	for _, sentinel := range []error{store.ErrNotFound, store.ErrEnded, limits.ErrHalted, context.Canceled} {
		if errors.Is(fmt.Errorf("wrapped: %w", runs.ErrDraining), sentinel) {
			t.Fatal("sentinel overlap")
		}
	}
}

// R-UD93-PBM3 R-UEH0-33CS R-UGWS-UMU6 R-UJCL-M6BK R-XGH6-DUYX
func TestOutputBoundsExitAndTimer(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		out, err     string
		code         int
		cut          bool
	}{
		{"overflow", "import sys\nfor i in range(1000): print('line %04d' % i)\nprint('done',file=sys.stderr)\n", strings.Repeat("", 0), "done\n", 0, true},
		{"exact", "import sys\nsys.stdout.write('x'*1024)\nsys.exit(3)\n", strings.Repeat("x", 1024), "", 3, false},
		{"signal", "import os,signal\nos.kill(os.getpid(),signal.SIGTERM)\n", "", "", 143, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture(t, tc.script)
			r := h.run(nil)
			h.finished(r.ID)
			ended := h.record(r.ID)
			out := read(t, filepath.Join(h.core.Folder(r), runs.StdoutFile))
			errOut := read(t, filepath.Join(h.core.Folder(r), runs.StderrFile))
			if tc.name == "overflow" {
				var b strings.Builder
				for i := range 1000 {
					fmt.Fprintf(&b, "line %04d\n", i)
				}
				tc.out = b.String()[:1024]
			}
			if string(out) != tc.out || string(errOut) != tc.err || ended.StdoutBytes != int64(len(out)) || ended.StderrBytes != int64(len(errOut)) || ended.Truncated != tc.cut || ended.Status != store.StatusExited || ended.ExitCode != tc.code {
				t.Fatalf("output/record %q %q %+v", out, errOut, ended)
			}
			if d := <-h.durations; d != 9*time.Second {
				t.Fatal(d)
			}
			select {
			case <-h.durations:
				t.Fatal("extra timer")
			default:
			}
		})
	}
}

// R-UXZE-7F7W R-XF9A-0388 R-U123-VM75 R-U5XP-EP5X
func TestConcurrentRunsAndLiveSizes(t *testing.T) {
	script := `import os,sys
raw=open(os.environ['IKIGENBA_INPUT'],'rb').read()
sys.stdout.buffer.write(raw);sys.stdout.flush()
open(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'result.txt'),'wb').write(raw)
while not os.path.exists(os.path.join(os.environ['IKIGENBA_RUN_DIR'],'release')): pass
`
	h := fixture(t, script)
	h.cfg.Rand = &sequence{n: 0x59}
	h.core = runs.New(h.cfg)
	r := h.run([]byte("hello"))
	if r.ID != "run_5a5a5a5a5a5a5a5a" {
		t.Fatal(r.ID)
	}
	until(t, func() bool {
		a, b := h.core.Sizes(r)
		return a == 5 && b == 0 && fileExists(filepath.Join(h.core.Folder(r), runs.OutDir, "result.txt"))
	})
	if rec := h.record(r.ID); rec.StdoutBytes != 0 || rec.StderrBytes != 0 {
		t.Fatal(rec)
	}
	secondCore := h.core
	r2, e := secondCore.Run(context.Background(), h.sc, runs.Request{Input: []byte("other"), Caller: identity.Caller{UserID: "owner", RequestID: "other-request"}})
	must(t, e)
	t.Cleanup(func() { ctx, c := context.WithCancel(context.Background()); c(); secondCore.Drain(ctx) })
	if r2.Status != store.StatusRunning || r2.ID == r.ID {
		t.Fatal(r2)
	}
	until(t, func() bool { a, _ := secondCore.Sizes(r2); return a == 5 })
	if string(read(t, filepath.Join(h.core.Folder(r), runs.InputFile))) != "hello" || string(read(t, filepath.Join(h.core.Folder(r), runs.StdoutFile))) != "hello" || string(read(t, filepath.Join(h.core.Folder(r), runs.OutDir, "result.txt"))) != "hello" {
		t.Fatal("cross run write")
	}
	must(t, os.WriteFile(filepath.Join(secondCore.Folder(r2), "release"), nil, 0600))
	h.finished(r2.ID)
	if h.record(r.ID).Status != store.StatusRunning || string(read(t, filepath.Join(h.core.Folder(r), runs.StdoutFile))) != "hello" {
		t.Fatal("other ending changed first")
	}
	h.release(r)
	h.finished(r.ID)
}
func fileExists(path string) bool { s, e := os.Stat(path); return e == nil && s.Mode().IsRegular() }

// R-XCTH-8JQU R-JD7O-J6L1 R-V61B-A78Y R-V797-NYZN R-V8H4-1QQC R-JEFK-WYBQ R-JFNH-AQ2F R-VFSI-CD6I R-J5WA-8K4V
func TestTimeoutCancelAndDrainKill(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel", "drain"} {
		t.Run(mode, func(t *testing.T) {
			h := fixture(t, `import os,subprocess,sys
child=subprocess.Popen([sys.executable,'-c','while True: pass'])
open(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'pids'),'w').write(str(os.getpid())+' '+str(child.pid))
print('starting',flush=True)
while True: pass
`)
			h.cfg.ScriptSeconds = math.MaxInt64
			h.core = runs.New(h.cfg)
			r := h.run(nil)
			pidsFile := filepath.Join(h.core.Folder(r), runs.OutDir, "pids")
			until(t, func() bool { a, _ := h.core.Sizes(r); return a == 9 && fileExists(pidsFile) })
			if d := <-h.durations; d != time.Duration(math.MaxInt64) {
				t.Fatal(d)
			}
			want := store.StatusKilled
			switch mode {
			case "timeout":
				(<-h.timers) <- time.Time{}
				want = store.StatusTimedOut
			case "cancel":
				ended, e := h.core.Cancel(context.Background(), r.ID)
				must(t, e)
				if ended.Status != store.StatusKilled {
					t.Fatal(ended)
				}
			case "drain":
				ctx, c := context.WithCancel(context.Background())
				c()
				h.core.Drain(ctx)
			}
			h.finished(r.ID)
			ended := h.record(r.ID)
			if ended.Status != want || ended.ExitCode != 0 || string(read(t, filepath.Join(h.core.Folder(r), runs.StdoutFile))) != "starting\n" {
				t.Fatal(ended)
			}
			for _, pid := range strings.Fields(string(read(t, pidsFile))) {
				if !processGone(pid) {
					t.Fatalf("process %s alive", pid)
				}
			}
			if ended.StdoutBytes != int64(len(read(t, filepath.Join(h.core.Folder(r), runs.StdoutFile)))) {
				t.Fatal("post-record output")
			}
			_, e := h.core.Cancel(context.Background(), r.ID)
			if !errors.Is(e, store.ErrEnded) {
				t.Fatal(e)
			}
			_, e = h.core.Cancel(context.Background(), "run_ffffffffffffffff")
			if !errors.Is(e, store.ErrNotFound) {
				t.Fatal(e)
			}
		})
	}
}
func processGone(pid string) bool {
	root, e := os.OpenRoot("/proc")
	if e != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	b, e := root.ReadFile(filepath.Join(pid, "stat"))
	if os.IsNotExist(e) {
		return true
	}
	if e != nil {
		return false
	}
	_, tail, ok := strings.Cut(string(b), ") ")
	return ok && strings.HasPrefix(tail, "Z ")
}

// R-FM6M-9NH9 R-FNEI-NF7Y R-UWA4-81BE R-UXI0-LT23 R-XE1D-MBHJ R-UZ7A-L6YL R-UWRH-TNH7
func TestFailedRunsAndFolders(t *testing.T) {
	for _, mode := range []string{"missing-ref", "missing-repo", "large-tree", "start-failed", "git-error"} {
		t.Run(mode, func(t *testing.T) {
			h := fixture(t, waitScript, map[string]string{"a.txt": strings.Repeat("a", 600), "b.txt": strings.Repeat("b", 600)})
			sc := h.sc
			var old store.Run
			if mode == "missing-ref" {
				old = h.addRecord(store.StatusExited, h.readNow().Add(-72*time.Hour))
				must(t, os.MkdirAll(h.core.Folder(old), 0700))
			}
			reason := ""
			sha := h.sha
			want := []string{runs.InputFile}
			switch mode {
			case "missing-ref":
				sc.Ref = "unknown"
				reason = store.ReasonCommitMissing
				sha = ""
			case "missing-repo":
				sc.Repo = "rep_ffffffffffffffff"
				reason = store.ReasonRepositoryMissing
				sha = ""
			case "large-tree":
				s := settings.Defaults()
				s.TreeMaxBytes = 1024
				h.limit = limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
				g, e := git.Find(h.cfg.Path, func() []string { return h.env })
				must(t, e)
				h.cfg.Source = source.New(source.Config{Repos: filepath.Dir(h.repo), Git: g, Limits: h.limit})
				h.core = runs.New(h.cfg)
				reason = store.ReasonTooLarge
				want = append(want, runs.TreeDir)
			case "start-failed":
				h.cfg.Path = t.TempDir()
				h.core = runs.New(h.cfg)
				reason = store.ReasonStartFailed
				want = []string{runs.InputFile, runs.OutDir, runs.TreeDir}
			case "git-error":
				h.command(h.repo, "config", "core.bare", "false")
				must(t, os.WriteFile(filepath.Join(h.repo, "index"), []byte("broken"), 0600))
				sc.Ref = "main"
				h.command(h.repo, "config", "extensions.objectFormat", "bogus")
				reason = store.ReasonGitFailed
				h.cfg.OutputMaxBytes = 3
				h.core = runs.New(h.cfg)
				sha = ""
				want = []string{runs.InputFile, runs.StderrFile}
			}
			r, e := h.core.Run(context.Background(), sc, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "failure"}})
			must(t, e)
			if r.Status != store.StatusFailed || r.Reason != reason || r.SHA != sha || r.Finished.Before(r.Started) {
				t.Fatalf("failed %+v", r)
			}
			if got := entries(t, h.core.Folder(r)); !reflect.DeepEqual(got, want) {
				t.Fatalf("folder %v want %v", got, want)
			}
			if string(read(t, filepath.Join(h.core.Folder(r), runs.InputFile))) != "{}" {
				t.Fatal("default input")
			}
			h.finished(r.ID)
			if mode == "missing-ref" {
				if _, e := h.st.RunByID(context.Background(), old.ID); !errors.Is(e, store.ErrNotFound) || !h.core.Gone(old) {
					t.Fatal("failed ending did not prune", e)
				}
			}
			for _, ev := range h.sink.capture.Events() {
				if ev.Attrs["run"] == r.ID && ev.Name == "run.started" {
					t.Fatal("failed emitted started")
				}
			}
			if mode == "git-error" {
				b := read(t, filepath.Join(h.core.Folder(r), runs.StderrFile))
				g, e := git.Find(h.cfg.Path, func() []string { return h.env })
				must(t, e)
				_, e = g.Output(context.Background(), h.repo, "rev-parse", "--verify", "main")
				var ge *git.Error
				if !errors.As(e, &ge) {
					t.Fatal(e)
				}
				if string(b) != ge.Stderr[:3] || !r.Truncated || r.StderrBytes != int64(len(b)) {
					t.Fatalf("git stderr %q record %+v", b, r)
				}
			} else if r.StderrBytes != 0 || r.Truncated {
				t.Fatal("failed stream fields")
			}
			if mode == "large-tree" {
				tree := filepath.Join(h.core.Folder(r), runs.TreeDir)
				if string(read(t, filepath.Join(tree, "a.txt"))) != strings.Repeat("a", 600) {
					t.Fatal("partial a")
				}
				if _, e := os.Lstat(filepath.Join(tree, "b.txt")); !os.IsNotExist(e) {
					t.Fatal("oversize b", e)
				}
				info, e := os.Stat(tree)
				must(t, e)
				if info.Mode().Perm()&0222 != 0 {
					t.Fatal("partial tree writable")
				}
			}
			select {
			case <-h.durations:
				t.Fatal("failed timer")
			default:
			}
		})
	}
}

// R-FKYP-VVQK R-FPUB-EYPC R-FR27-SQG1 R-V3LI-INRK R-V9P0-FIH1 R-UWRH-TNH7
func TestCutoffAndCatalogFailureCleanup(t *testing.T) {
	for _, mode := range []string{"cancel", "halt", "closed-running", "closed-failed", "delete-store", "delete-core", "folder"} {
		t.Run(mode, func(t *testing.T) {
			h := fixture(t, waitScript)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := h.cfg
			if mode == "folder" {
				must(t, os.WriteFile(filepath.Join(filepath.Dir(cfg.Runs), "blocked"), nil, 0600))
				cfg.Runs = filepath.Join(filepath.Dir(cfg.Runs), "blocked", "runs")
			} else {
				s := settings.Defaults()
				var once sync.Once
				h.limit = limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time {
					once.Do(func() {
						switch mode {
						case "cancel":
							cancel()
						case "halt":
							h.limit.Halt()
						case "closed-running", "closed-failed":
							must(t, h.st.Close())
						case "delete-store":
							must(t, h.st.Delete(context.Background(), h.sc.ID))
						}
					})
					return make(chan time.Time)
				}})
				g, e := git.Find(cfg.Path, func() []string { return h.env })
				must(t, e)
				cfg.Source = source.New(source.Config{Repos: filepath.Dir(h.repo), Git: g, Limits: h.limit})
			}
			h.core = runs.New(cfg)
			if mode == "closed-failed" {
				h.sc.Ref = "absent"
			}
			if mode == "delete-core" {
				entered := make(chan struct{})
				release := make(chan struct{})
				s := settings.Defaults()
				var once sync.Once
				l := limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time {
					once.Do(func() { close(entered); <-release })
					return make(chan time.Time)
				}})
				g, e := git.Find(cfg.Path, func() []string { return h.env })
				must(t, e)
				cfg.Source = source.New(source.Config{Repos: filepath.Dir(h.repo), Git: g, Limits: l})
				h.core = runs.New(cfg)
				result := make(chan error, 1)
				go func() {
					_, e := h.core.Run(ctx, h.sc, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "cutoff"}})
					result <- e
				}()
				<-entered
				deleted := make(chan error, 1)
				go func() { deleted <- h.core.Delete(context.Background(), h.sc.ID) }()
				probeCtx, probeCancel := context.WithCancel(context.Background())
				probeCancel()
				until(t, func() bool {
					_, e := h.core.Run(probeCtx, h.sc, runs.Request{Caller: identity.Caller{UserID: "owner"}})
					return errors.Is(e, store.ErrNotFound)
				})
				close(release)
				if e = <-result; !errors.Is(e, store.ErrNotFound) {
					t.Fatal(e)
				}
				must(t, <-deleted)
				if _, e = os.Lstat(filepath.Join(cfg.Runs, h.sc.ID)); !os.IsNotExist(e) {
					t.Fatal("script folder remains", e)
				}
			} else {
				r, e := h.core.Run(ctx, h.sc, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "cutoff"}})
				if e == nil || r != (store.Run{}) {
					t.Fatalf("cutoff %+v %v", r, e)
				}
				switch mode {
				case "cancel":
					if !errors.Is(e, context.Canceled) {
						t.Fatal(e)
					}
				case "halt":
					if !errors.Is(e, limits.ErrHalted) {
						t.Fatal(e)
					}
				case "delete-store":
					if !errors.Is(e, store.ErrNotFound) {
						t.Fatal(e)
					}
				case "closed-running", "closed-failed":
					if !catalogError(e) {
						t.Fatal(e)
					}
				}
				id := "run_0101010101010101"
				if _, e = os.Lstat(filepath.Join(cfg.Runs, h.sc.ID, id)); !os.IsNotExist(e) && mode != "folder" {
					t.Fatal("folder remains", e)
				}
			}
			for _, ev := range h.sink.capture.Events() {
				if strings.HasPrefix(ev.Name, "run.") {
					t.Fatal(ev)
				}
			}
		})
	}
}

func (h *harness) addRecord(status string, started time.Time) store.Run {
	h.t.Helper()
	h.recordID++
	id := fmt.Sprintf("run_%016x", 100+h.recordID)
	var e error
	r := store.Run{ID: id, Script: h.sc.ID, SHA: h.sha, Ref: "main", User: "historical-user", RequestID: "historical-request", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: started}
	r, e = h.st.AddRun(context.Background(), r)
	must(h.t, e)
	if status != store.StatusRunning {
		r, e = h.st.FinishRun(context.Background(), r.ID, store.Ending{Status: status, Finished: started.Add(time.Second)})
		must(h.t, e)
	}
	return r
}

// R-VAWW-TA7Q R-V2UZ-QI6O R-TZU7-HUGG R-U123-VM75
func TestRecoverAndForeignCancel(t *testing.T) {
	h := fixture(t, waitScript)
	r := h.addRecord(store.StatusRunning, h.readNow().Add(-time.Hour))
	dir := h.core.Folder(r)
	must(t, os.MkdirAll(dir, 0700))
	must(t, os.WriteFile(filepath.Join(dir, runs.StdoutFile), []byte("kept"), 0600))
	must(t, os.Symlink("stdout", filepath.Join(dir, runs.StderrFile)))
	snapshot := entries(t, dir)
	must(t, h.core.Recover(context.Background()))
	h.finished(r.ID)
	ended := h.record(r.ID)
	if ended.Status != store.StatusKilled || ended.StdoutBytes != 4 || ended.StderrBytes != 0 || ended.Truncated || !reflect.DeepEqual(entries(t, dir), snapshot) {
		t.Fatal(ended)
	}
	must(t, os.RemoveAll(dir))
	if !h.core.Gone(ended) || !reflect.DeepEqual(h.record(r.ID), ended) {
		t.Fatal("gone changed record")
	}
	must(t, os.MkdirAll(filepath.Dir(dir), 0700))
	must(t, os.Symlink(filepath.Dir(dir), dir))
	if !h.core.Gone(ended) {
		t.Fatal("symlink counts directory")
	}
	a, b := h.core.Sizes(ended)
	if a != 4 || b != 0 {
		t.Fatal(a, b)
	}
	foreign := h.addRecord(store.StatusRunning, h.readNow().Add(time.Hour))
	h.setNow(h.readNow().Add(-time.Hour))
	canceled, e := h.core.Cancel(context.Background(), foreign.ID)
	must(t, e)
	ev := h.finished(foreign.ID)
	if canceled.Status != store.StatusKilled || canceled.Finished != foreign.Started || ev.Attrs["duration_us"] != int64(0) || ev.User != foreign.User {
		t.Fatal(canceled, ev)
	}
	h.cfg.Runs = filepath.Join(t.TempDir(), "a", "b")
	h.core = runs.New(h.cfg)
	must(t, h.core.Recover(context.Background()))
	s, e := os.Stat(h.cfg.Runs)
	must(t, e)
	if !s.IsDir() {
		t.Fatal("recover root")
	}
	must(t, h.st.Close())
	if e = h.core.Recover(context.Background()); !catalogError(e) {
		t.Fatal(e)
	}
}

// R-VBEA-EWDJ R-VCM6-SO48 R-V8YH-NCW5 R-XHP2-RMPM R-UN0A-RHJN
func TestPruneKeepingAndReadOnlyRemoval(t *testing.T) {
	h := fixture(t, waitScript)
	old := h.addRecord(store.StatusExited, h.readNow().Add(-72*time.Hour))
	newer := h.addRecord(store.StatusExited, h.readNow().Add(-time.Hour))
	running := h.addRecord(store.StatusRunning, h.readNow().Add(-96*time.Hour))
	outside := filepath.Join(t.TempDir(), "outside")
	must(t, os.WriteFile(outside, []byte("safe"), 0600))
	outsideInfo, e := os.Stat(outside)
	must(t, e)
	for _, r := range []store.Run{old, newer, running} {
		tree := filepath.Join(h.core.Folder(r), runs.TreeDir)
		must(t, os.MkdirAll(filepath.Join(tree, "sub"), 0700))
		must(t, os.WriteFile(filepath.Join(tree, "sub", "data"), []byte("payload"), 0400))
		must(t, os.Symlink(outside, filepath.Join(tree, "link")))
		must(t, os.Chmod(filepath.Join(tree, "sub"), readOnlyDirectory()))
		must(t, os.Chmod(tree, readOnlyDirectory()))
	}
	before := len(h.sink.capture.Events())
	must(t, h.core.Prune(context.Background()))
	if _, e = h.st.RunByID(context.Background(), old.ID); !errors.Is(e, store.ErrNotFound) || !h.core.Gone(old) {
		t.Fatal(e)
	}
	if h.core.Gone(newer) || h.core.Gone(running) {
		t.Fatal("pruned kept run")
	}
	s, e := os.Stat(filepath.Join(h.cfg.Runs, h.sc.ID))
	must(t, e)
	if !s.IsDir() {
		t.Fatal("pruned script dir")
	}
	after, e := os.Stat(outside)
	must(t, e)
	if after.Mode() != outsideInfo.Mode() || string(read(t, outside)) != "safe" {
		t.Fatal("followed link")
	}
	if len(h.sink.capture.Events()) != before {
		t.Fatal("prune event")
	}
	// No automatic timer or accessor prunes a run after the clock passes keeping.
	h.setNow(h.readNow().Add(96 * time.Hour))
	r := h.run(nil)
	_, e = h.core.Cancel(context.Background(), newer.ID)
	if !errors.Is(e, store.ErrEnded) {
		t.Fatal(e)
	}
	h.core.Folder(newer)
	h.core.Sizes(newer)
	if h.core.Gone(newer) {
		t.Fatal("early prune")
	}
	h.release(r)
	h.finished(r.ID)
	if _, e = h.st.RunByID(context.Background(), newer.ID); !errors.Is(e, store.ErrNotFound) || !h.core.Gone(newer) {
		t.Fatal("finish did not prune", e)
	}
	// A non-writable script directory prevents removing its run but other scripts still prune.
	blocked := h.addRecord(store.StatusExited, h.readNow().Add(-72*time.Hour))
	_ = h.addRecord(store.StatusExited, h.readNow())
	must(t, os.MkdirAll(h.core.Folder(blocked), 0700))
	parent := filepath.Dir(h.core.Folder(blocked))
	must(t, os.Chmod(parent, readOnlyDirectory()))
	t.Cleanup(func() { _ = restoreDirectory(parent) })
	other, e := h.st.Create(context.Background(), store.Draft{Owner: "owner", Name: "retained", Repo: h.sc.Repo, Ref: "main"})
	must(t, e)
	original := h.sc
	h.sc = other
	removable := h.addRecord(store.StatusExited, h.readNow().Add(-96*time.Hour))
	_ = h.addRecord(store.StatusExited, h.readNow())
	h.sc = original
	must(t, os.MkdirAll(h.core.Folder(removable), 0700))
	must(t, h.core.Prune(context.Background()))
	if _, e = h.st.RunByID(context.Background(), removable.ID); !errors.Is(e, store.ErrNotFound) || !h.core.Gone(removable) {
		t.Fatal("prune stopped after unremovable run", e)
	}
	if h.core.Gone(blocked) {
		t.Fatal("unremovable folder removed")
	}
	if h.record(blocked.ID).ID != blocked.ID {
		t.Fatal("record removed")
	}
	must(t, restoreDirectory(parent))
	must(t, h.st.Close())
	if e = h.core.Prune(context.Background()); !catalogError(e) {
		t.Fatal(e)
	}
}

// R-V42W-49XD R-V6IO-VTER R-V8YH-NCW5
func TestDeleteKillsOnlyItsScript(t *testing.T) {
	h := fixture(t, waitScript)
	r := h.run(nil)
	other, e := h.st.Create(context.Background(), store.Draft{Owner: "owner", Name: "other", Repo: h.sc.Repo, Ref: "main"})
	must(t, e)
	r2, e := h.core.Run(context.Background(), other, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "other"}})
	must(t, e)
	must(t, h.core.Delete(context.Background(), h.sc.ID))
	h.finished(r.ID)
	if _, e = h.st.RunByID(context.Background(), r.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = os.Lstat(filepath.Join(h.cfg.Runs, h.sc.ID)); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if h.record(r2.ID).Status != store.StatusRunning || h.core.Gone(r2) {
		t.Fatal("other script changed")
	}
	if e = h.core.Delete(context.Background(), h.sc.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	h.release(r2)
	h.finished(r2.ID)
}

// R-U2A0-9DXU R-VC4T-71YF R-VF1Z-K7LM
func TestGracefulDrainKeepsAcceptedCalls(t *testing.T) {
	h := fixture(t, waitScript)
	entered := make(chan struct{})
	releaseGit := make(chan struct{})
	var once sync.Once
	g, e := git.Find(h.cfg.Path, func() []string { return h.env })
	must(t, e)
	l := limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time {
		once.Do(func() { close(entered); <-releaseGit })
		return make(chan time.Time)
	}})
	h.cfg.Source = source.New(source.Config{Repos: filepath.Dir(h.repo), Git: g, Limits: l})
	h.core = runs.New(h.cfg)
	result := make(chan store.Run, 1)
	errs := make(chan error, 1)
	go func() {
		r, e := h.core.Run(context.Background(), h.sc, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "accepted"}})
		result <- r
		errs <- e
	}()
	<-entered
	drained := make(chan struct{})
	go func() { h.core.Drain(context.Background()); close(drained) }()
	probeCtx, cancel := context.WithCancel(context.Background())
	cancel()
	until(t, func() bool { _, e := h.core.Run(probeCtx, h.sc, runs.Request{}); return errors.Is(e, runs.ErrDraining) })
	select {
	case <-drained:
		t.Fatal("drain returned during accepted git")
	default:
	}
	close(releaseGit)
	r := <-result
	must(t, <-errs)
	if r.Status != store.StatusRunning {
		t.Fatal(r)
	}
	select {
	case <-drained:
		t.Fatal("drain returned during running script")
	default:
	}
	h.release(r)
	select {
	case <-drained:
	case <-time.After(10 * time.Second):
		t.Fatal("drain deadline")
	}
	h.finished(r.ID)
	before := len(h.sink.capture.Events())
	rs, e := h.st.Runs(context.Background(), h.sc.ID)
	must(t, e)
	refused, e := h.core.Run(context.Background(), h.sc, runs.Request{})
	if !errors.Is(e, runs.ErrDraining) || refused != (store.Run{}) {
		t.Fatal(e)
	}
	after, e := h.st.Runs(context.Background(), h.sc.ID)
	must(t, e)
	if !reflect.DeepEqual(after, rs) || len(h.sink.capture.Events()) != before {
		t.Fatal("refusal changed state")
	}
}

func catalogError(e error) bool {
	return e != nil && !errors.Is(e, store.ErrNotFound) && !errors.Is(e, store.ErrNameTaken) && !errors.Is(e, store.ErrEnded)
}

// R-6I3O-0UCG R-VIPO-PITP
func TestUnboundedOutAndSilentCore(t *testing.T) {
	treeMax := int64(4096)
	n := settings.Defaults().OutputMaxBytes + treeMax + 1025
	script := fmt.Sprintf("import os\nwith open(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'large'),'wb') as f:\n    f.write(b'x' * %d)\nprint('product')\n", n)
	h := fixture(t, script)
	s := settings.Defaults()
	s.TreeMaxBytes = treeMax
	g, e := git.Find(h.cfg.Path, func() []string { return h.env })
	must(t, e)
	l := limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	h.cfg.Source = source.New(source.Config{Repos: filepath.Dir(h.repo), Git: g, Limits: l})
	h.core = runs.New(h.cfg)
	out, e := os.Create(filepath.Join(t.TempDir(), "host-stdout"))
	must(t, e)
	errOut, e := os.Create(filepath.Join(t.TempDir(), "host-stderr"))
	must(t, e)
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout = out
	os.Stderr = errOut
	defer func() { os.Stdout = savedOut; os.Stderr = savedErr; _ = out.Close(); _ = errOut.Close() }()
	r := h.run(nil)
	h.finished(r.ID)
	ended := h.record(r.ID)
	info, e := os.Stat(filepath.Join(h.core.Folder(r), runs.OutDir, "large"))
	must(t, e)
	if info.Size() != n || ended.Truncated || ended.Status != store.StatusExited || ended.ExitCode != 0 {
		t.Fatal(info.Size(), ended)
	}
	must(t, h.core.Prune(context.Background()))
	must(t, h.core.Recover(context.Background()))
	_, e = h.core.Cancel(context.Background(), r.ID)
	if !errors.Is(e, store.ErrEnded) {
		t.Fatal(e)
	}
	h.core.Gone(r)
	h.core.Sizes(r)
	must(t, h.core.Delete(context.Background(), h.sc.ID))
	h.core.Drain(context.Background())
	a, e := out.Stat()
	must(t, e)
	b, e := errOut.Stat()
	must(t, e)
	if a.Size() != 0 || b.Size() != 0 {
		t.Fatal("core wrote host streams")
	}
}

// R-XGH6-DUYX R-V797-NYZN
func TestLeftoverChildAndCancelExitRace(t *testing.T) {
	h := fixture(t, `import os,subprocess,sys
child=subprocess.Popen([sys.executable,'-c',"import os;print('tick',flush=True);open(os.environ['IKIGENBA_OUT_DIR']+'/child-ready','w').write(str(os.getpid()));\nwhile True: pass"])
while not os.path.exists(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'child-ready')): pass
`)
	r := h.run(nil)
	h.finished(r.ID)
	ended := h.record(r.ID)
	pid := string(read(t, filepath.Join(h.core.Folder(r), runs.OutDir, "child-ready")))
	if !processGone(pid) || ended.Status != store.StatusExited || ended.ExitCode != 0 || ended.StdoutBytes != int64(len(read(t, filepath.Join(h.core.Folder(r), runs.StdoutFile)))) {
		t.Fatal(pid, ended)
	}
	// Competing cancellation and a natural exit settle once, with their observed winner.
	h2 := fixture(t, waitScript)
	run := h2.run(nil)
	ready := make(chan struct{})
	result := make(chan error, 1)
	go func() { <-ready; _, e := h2.core.Cancel(context.Background(), run.ID); result <- e }()
	close(ready)
	h2.release(run)
	e := <-result
	ev := h2.finished(run.ID)
	record := h2.record(run.ID)
	if e == nil {
		if record.Status != store.StatusKilled {
			t.Fatal(record)
		}
	} else if !errors.Is(e, store.ErrEnded) || record.Status != store.StatusExited || record.ExitCode != 0 {
		t.Fatal(e, record)
	}
	h2.core.Drain(context.Background())
	h2.cfg.Writer.Shutdown(context.Background(), "race")
	count := 0
	for _, event := range h2.sink.capture.Events() {
		if event.Name == "run.finished" && event.Attrs["run"] == run.ID {
			count++
		}
	}
	if count != 1 || ev.Attrs["status"] != record.Status {
		t.Fatal(count, ev)
	}
}

// R-VI8B-3WNW
func TestEndingCatalogFailureHasNoFinishedEvent(t *testing.T) {
	h := fixture(t, waitScript)
	r := h.run(nil)
	must(t, h.st.Close())
	h.release(r)
	h.core.Drain(context.Background())
	h.cfg.Writer.Shutdown(context.Background(), "failure")
	for _, ev := range h.sink.capture.Events() {
		if ev.Name == "run.finished" && ev.Attrs["run"] == r.ID {
			t.Fatal(ev)
		}
	}
	st, e := store.Open(context.Background(), store.Config{Source: filepath.Join(filepath.Dir(h.cfg.Runs), "catalog.db"), Now: h.readNow, Rand: &sequence{}})
	must(t, e)
	defer func() { _ = st.Close() }()
	rec, e := st.RunByID(context.Background(), r.ID)
	must(t, e)
	if rec.Status != store.StatusRunning {
		t.Fatal(rec)
	}
	h.cfg.Store = st
	core := runs.New(h.cfg)
	must(t, core.Recover(context.Background()))
	recovered, e := st.RunByID(context.Background(), r.ID)
	must(t, e)
	if recovered.Status != store.StatusKilled {
		t.Fatal(recovered)
	}
}

func readOnlyDirectory() os.FileMode { return os.FileMode(0700) &^ os.FileMode(0222) }

// R-FKYP-VVQK R-VFSI-CD6I R-J5WA-8K4V R-UWRH-TNH7
func TestDrainDeadlineCutsOffProcessBeforeCatalogAdmission(t *testing.T) {
	h := fixture(t, waitScript)
	entered := make(chan struct{})
	release := make(chan struct{})
	h.cfg.ScriptAfter = func(time.Duration) <-chan time.Time { close(entered); <-release; return make(chan time.Time) }
	h.core = runs.New(h.cfg)
	result := make(chan error, 1)
	go func() {
		r, e := h.core.Run(context.Background(), h.sc, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "pending"}})
		if r != (store.Run{}) {
			result <- fmt.Errorf("unexpected recorded run: %+v", r)
			return
		}
		result <- e
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	drained := make(chan struct{})
	go func() { h.core.Drain(ctx); close(drained) }()
	probeCtx, probeCancel := context.WithCancel(context.Background())
	probeCancel()
	until(t, func() bool { _, e := h.core.Run(probeCtx, h.sc, runs.Request{}); return errors.Is(e, runs.ErrDraining) })
	cancel()
	select {
	case <-drained:
		t.Fatal("drain returned with pending process")
	default:
	}
	close(release)
	e := <-result
	if !errors.Is(e, limits.ErrHalted) {
		t.Fatal(e)
	}
	select {
	case <-drained:
	case <-time.After(10 * time.Second):
		t.Fatal("drain did not wait for pending call")
	}
	rs, e := h.st.Runs(context.Background(), h.sc.ID)
	must(t, e)
	if len(rs) != 0 {
		t.Fatal(rs)
	}
	ds := entries(t, filepath.Join(h.cfg.Runs, h.sc.ID))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	h.cfg.Writer.Shutdown(context.Background(), "cutoff")
	for _, e := range h.sink.capture.Events() {
		if strings.HasPrefix(e.Name, "run.") {
			t.Fatal(e)
		}
	}
}

func restoreDirectory(path string) error {
	root, e := os.OpenRoot(path)
	if e != nil {
		return e
	}
	defer func() { _ = root.Close() }()
	return root.Chmod(".", 0700)
}

type checkedRandom struct {
	active, calls, bytes atomic.Int64
	concurrent           atomic.Bool
}

func (r *checkedRandom) Read(b []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.concurrent.Store(true)
	}
	defer r.active.Add(-1)
	n := r.calls.Add(1)
	r.bytes.Add(int64(len(b)))
	for i := range b {
		b[i] = byte(n & 255)
		runtime.Gosched()
	}
	return len(b), nil
}

// R-U5XP-EP5X R-UXZE-7F7W
func TestCoreSerializesRandomAcrossConcurrentCalls(t *testing.T) {
	h := fixture(t, waitScript)
	random := &checkedRandom{}
	h.cfg.Rand = random
	h.core = runs.New(h.cfg)
	result := make(chan store.Run, 8)
	errs := make(chan error, 8)
	start := make(chan struct{})
	for range 8 {
		go func() {
			<-start
			r, e := h.core.Run(context.Background(), h.sc, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "concurrent"}})
			result <- r
			errs <- e
		}()
	}
	close(start)
	records := make([]store.Run, 0, 8)
	for range 8 {
		r := <-result
		must(t, <-errs)
		if r.Status != store.StatusRunning {
			t.Fatal(r)
		}
		records = append(records, r)
	}
	if random.concurrent.Load() || random.calls.Load() != 8 || random.bytes.Load() != 64 {
		t.Fatal("random calls overlapped or read wrong byte count")
	}
	for _, r := range records {
		h.release(r)
	}
	for _, r := range records {
		until(t, func() bool { return h.record(r.ID).Status == store.StatusExited })
	}
	h.core.Drain(context.Background())
}

// R-U8DI-68NB R-V8YH-NCW5
func TestTreeWriteRefusedAndSymlinkTargetUntouched(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside")
	must(t, os.WriteFile(outside, []byte("outside"), 0600))
	before, e := os.Stat(outside)
	must(t, e)
	h := fixture(t, "open('notes.txt','w').write('unexpected')\n", map[string]string{"symlink:outside": outside})
	r := h.run(nil)
	h.finished(r.ID)
	ended := h.record(r.ID)
	if ended.Status != store.StatusExited || ended.ExitCode != 1 {
		t.Fatal(ended)
	}
	if _, e := os.Lstat(filepath.Join(h.core.Folder(r), runs.TreeDir, "notes.txt")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	after, e := os.Stat(outside)
	must(t, e)
	if after.Mode() != before.Mode() || string(read(t, outside)) != "outside" {
		t.Fatal("tree followed symlink")
	}
	must(t, h.core.Delete(context.Background(), h.sc.ID))
	after, e = os.Stat(outside)
	must(t, e)
	if after.Mode() != before.Mode() || string(read(t, outside)) != "outside" {
		t.Fatal("deletion followed symlink")
	}
}

// R-XF9A-0388
func TestRepeatedRunIDPreservesExistingRun(t *testing.T) {
	for _, state := range []string{"running", "ended"} {
		t.Run(state, func(t *testing.T) {
			h := fixture(t, `import os,sys
raw=open(os.environ['IKIGENBA_INPUT'],'rb').read()
sys.stdout.buffer.write(raw);sys.stdout.flush()
open(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'result.txt'),'wb').write(raw)
while not os.path.exists(os.path.join(os.environ['IKIGENBA_RUN_DIR'],'release')): pass
`)
			h.cfg.Rand = strings.NewReader(strings.Repeat("Z", 16))
			h.core = runs.New(h.cfg)
			first := h.run([]byte("first-input"))
			until(t, func() bool {
				out, _ := h.core.Sizes(first)
				return out == 11 && fileExists(filepath.Join(h.core.Folder(first), runs.OutDir, "result.txt"))
			})
			if state == "ended" {
				h.release(first)
				h.finished(first.ID)
			}
			beforeRecord := h.record(first.ID)
			beforeFiles := runFolderSnapshot(t, h.core.Folder(first))
			second, e := h.core.Run(context.Background(), h.sc, runs.Request{Input: []byte("second-input"), Caller: identity.Caller{UserID: "owner", RequestID: "second"}})
			if e == nil || second != (store.Run{}) {
				t.Fatalf("repeated id accepted: %+v %v", second, e)
			}
			if h.core.Gone(first) || !reflect.DeepEqual(h.record(first.ID), beforeRecord) || !reflect.DeepEqual(runFolderSnapshot(t, h.core.Folder(first)), beforeFiles) {
				t.Fatal("repeated id changed existing run")
			}
			if state == "running" {
				h.release(first)
				h.finished(first.ID)
			}
			h.core.Drain(context.Background())
			h.cfg.Writer.Shutdown(context.Background(), "collision")
			starts, ends := 0, 0
			for _, ev := range h.sink.capture.Events() {
				if ev.Attrs["run"] == first.ID {
					switch ev.Name {
					case "run.started":
						starts++
					case "run.finished":
						ends++
					}
				}
			}
			if starts != 1 || ends != 1 {
				t.Fatal("collision emitted events", starts, ends)
			}
			if len(h.durations) != 1 {
				t.Fatal("collision started a process")
			}
		})
	}
}

type folderEntry struct {
	Mode  fs.FileMode
	Bytes string
}

func runFolderSnapshot(t *testing.T, path string) map[string]folderEntry {
	t.Helper()
	root, e := os.OpenRoot(path)
	must(t, e)
	defer func() { _ = root.Close() }()
	entries := make(map[string]folderEntry)
	must(t, fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		entry := folderEntry{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			b, e := root.ReadFile(name)
			if e != nil {
				return e
			}
			entry.Bytes = string(b)
		}
		entries[name] = entry
		return nil
	}))
	return entries
}
