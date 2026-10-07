package runner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/runner"
)

func interpreter(t *testing.T) string {
	t.Helper()
	p, e := exec.LookPath(runner.Interpreter)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func fixture(t *testing.T, code string) (string, []string) {
	t.Helper()
	d := t.TempDir()
	if e := os.WriteFile(filepath.Join(d, "main.py"), []byte(code), 0600); e != nil {
		t.Fatal(e)
	}
	return d, []string{"HOME=" + d, "PATH=" + filepath.Dir(interpreter(t)), "LANG=C.UTF-8"}
}
func start(ctx context.Context, t *testing.T, d string, env []string, out, errout io.Writer) *runner.Process {
	t.Helper()
	p, e := runner.Start(ctx, runner.Spec{Dir: d, Env: env, Stdout: out, Stderr: errout})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Kill(); wait(t, p) })
	return p
}
func wait(t *testing.T, p *runner.Process) int {
	t.Helper()
	c := make(chan int, 1)
	go func() { c <- p.Wait() }()
	select {
	case n := <-c:
		return n
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not finish")
		return -1
	}
}
func gone(pid int) bool {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(e, os.ErrNotExist) {
		return true
	}
	if e != nil {
		return false
	}
	i := strings.LastIndexByte(string(b), ')')
	return i >= 0 && strings.Fields(string(b[i+1:]))[0] == "Z"
}

type observed struct {
	mu    sync.Mutex
	b     bytes.Buffer
	lines chan string
}

func newObserved() *observed { return &observed{lines: make(chan string, 20)} }
func (o *observed) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n, e := o.b.Write(p)
	for _, v := range strings.Split(strings.TrimSuffix(string(p), "\n"), "\n") {
		o.lines <- v
	}
	return n, e
}
func (o *observed) next(t *testing.T) string {
	t.Helper()
	select {
	case v := <-o.lines:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("no output")
		return ""
	}
}
func (o *observed) text() string { o.mu.Lock(); defer o.mu.Unlock(); return o.b.String() }

func TestProcessEnvironment(t *testing.T) {
	// R-HGKZ-W41S R-9LE2-6YPB R-8VHE-9O35 R-X4A6-K5JZ R-IZ4U-JD1B R-J0CQ-X4S0 R-J2SJ-OO9E R-XADO-H09G
	d, env := fixture(t, `import os,sys,json
print(json.dumps([sys.argv,sorted(os.listdir()),"%d.%d" % sys.version_info[:2],os.getpgrp()==os.getpid(),os.getpgrp()==os.getpgid(os.getppid()),len(sys.stdin.read()),os.getuid(),os.geteuid(),os.getgid(),os.getegid()]))
sys.stderr.buffer.write(open('/proc/self/environ','rb').read())
`)
	var out, errout bytes.Buffer
	p := start(context.Background(), t, d, env, &out, &errout)
	if n := wait(t, p); n != 0 {
		t.Fatal(n, errout.String())
	}
	if runner.Interpreter != "python"+runner.PythonVersion {
		t.Fatal("interpreter name")
	}
	expected := []any{[]string{"main.py"}, []string{"main.py"}, runner.PythonVersion, true, false, 0, os.Getuid(), os.Geteuid(), os.Getgid(), os.Getegid()}
	want, e := json.Marshal(expected)
	if e != nil {
		t.Fatal(e)
	}
	if strings.TrimSpace(out.String()) != string(want) {
		var got any
		if e := json.Unmarshal(out.Bytes(), &got); e != nil {
			t.Fatal(e)
		}
		compact, e := json.Marshal(got)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(compact, want) {
			t.Fatalf("%s != %s", compact, want)
		}
	}
	if errout.String() != strings.Join(env, "\x00")+"\x00" {
		t.Fatalf("environment: %q", errout.String())
	}
}

func TestFind(t *testing.T) {
	// R-IVH5-E1T8 R-IWP1-RTJX
	if runner.ErrNotFound == nil {
		t.Fatal("nil sentinel")
	}
	d := t.TempDir()
	a := filepath.Join(d, "a")
	b := filepath.Join(d, "b")
	for _, p := range []string{a, b} {
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	pa, pb := filepath.Join(a, runner.Interpreter), filepath.Join(b, runner.Interpreter)
	if e := os.WriteFile(pa, []byte("not executable"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(interpreter(t), pb); e != nil {
		t.Fatal(e)
	}
	p, e := runner.Find(":" + "relative:" + a + ":" + b)
	if e != nil || p != pb {
		t.Fatal(p, e)
	}
	if e := syscall.Chmod(pa, 0700); e != nil {
		t.Fatal(e)
	}
	p, e = runner.Find(a + ":" + b)
	if e != nil || p != pa {
		t.Fatal(p, e)
	}
	for _, path := range []string{"", ".", "relative", d} {
		p, e = runner.Find(path)
		if p != "" || !errors.Is(e, runner.ErrNotFound) {
			t.Fatal(p, e)
		}
	}
	// A directory and a dangling link are not interpreters.
	if e := os.Remove(pa); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(pa, 0700); e != nil {
		t.Fatal(e)
	}
	p, e = runner.Find(a)
	if p != "" || !errors.Is(e, runner.ErrNotFound) {
		t.Fatal(p, e)
	}
}

func TestFindPreservesPathEntries(t *testing.T) {
	// R-IWP1-RTJX
	d := t.TempDir()
	target := filepath.Join(d, "target")
	nested := filepath.Join(target, "nested")
	if e := os.MkdirAll(nested, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(interpreter(t), filepath.Join(target, runner.Interpreter)); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(d, "link")
	if e := os.Symlink(nested, link); e != nil {
		t.Fatal(e)
	}
	for _, entry := range []string{target + "/.", target + "/", link + "/.."} {
		got, e := runner.Find(entry)
		want := entry + "/" + runner.Interpreter
		if e != nil || got != want {
			t.Fatalf("Find(%q) = %q, %v; want %q", entry, got, e, want)
		}
	}
}

func TestDescriptors(t *testing.T) {
	// R-J40G-2G03
	d, env := fixture(t, "import os\nprint(sorted(n for n in os.listdir('/proc/self/fd') if os.path.exists('/proc/self/fd/'+n)))\n")
	fd, e := syscall.Open(filepath.Join(d, "held"), syscall.O_CREAT|syscall.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = syscall.Close(fd) }()
	var out bytes.Buffer
	p := start(context.Background(), t, d, env, &out, nil)
	if n := wait(t, p); n != 0 || out.String() != "['0', '1', '2']\n" {
		t.Fatal(n, out.String())
	}
}
func TestExitStatuses(t *testing.T) {
	// R-9RHK-3TES R-JA3X-ZAPK R-A00U-S7LN
	for _, v := range []struct {
		code string
		n    int
	}{{"", 0}, {"import sys;sys.exit(3)", 3}, {"import os,signal;os.kill(os.getpid(),signal.SIGTERM)", 128 + int(syscall.SIGTERM)}} {
		t.Run(strconv.Itoa(v.n), func(t *testing.T) {
			d, env := fixture(t, v.code)
			p := start(context.Background(), t, d, env, nil, nil)
			if n := wait(t, p); n != v.n {
				t.Fatal(n)
			}
			if p.Kill() {
				t.Fatal("kill after wait")
			}
			if n := wait(t, p); n != v.n {
				t.Fatal(n)
			}
		})
	}
}
func TestAbsentScript(t *testing.T) {
	// R-X7XV-PGS2
	d, env := fixture(t, "")
	if e := os.Remove(filepath.Join(d, "main.py")); e != nil {
		t.Fatal(e)
	}
	p := start(context.Background(), t, d, env, nil, nil)
	if n := wait(t, p); n != 2 {
		t.Fatal(n)
	}
}
func TestStartErrors(t *testing.T) {
	// R-JG7F-W5F1 R-JHFC-9X5Q R-A2GN-JR31
	d, env := fixture(t, "")
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("cancelled before start")
	cancel(cause)
	p, e := runner.Start(ctx, runner.Spec{Dir: d, Env: env})
	if p != nil || !errors.Is(e, cause) {
		t.Fatal(p, e)
	}
	for _, v := range [][]string{nil, {"PATH=" + d}, {"PATH=relative"}} {
		p, e = runner.Start(context.Background(), runner.Spec{Dir: d, Env: v})
		if p != nil || !errors.Is(e, runner.ErrNotFound) {
			t.Fatal(p, e)
		}
	}
	for _, v := range []string{filepath.Join(d, "missing"), filepath.Join(d, "main.py")} {
		p, e = runner.Start(context.Background(), runner.Spec{Dir: v, Env: env})
		if p != nil || e == nil {
			t.Fatal(p, e)
		}
	}
	bin := filepath.Join(d, "bin")
	if e := os.Mkdir(bin, 0700); e != nil {
		t.Fatal(e)
	}
	exe := filepath.Join(bin, runner.Interpreter)
	if e := os.Symlink(interpreter(t), exe); e != nil {
		t.Fatal(e)
	}
	env[1] = "PATH=" + bin
	p = start(context.Background(), t, d, env, nil, nil)
	wait(t, p)
	if e := os.Remove(exe); e != nil {
		t.Fatal(e)
	}
	p, e = runner.Start(context.Background(), runner.Spec{Dir: d, Env: env})
	if p != nil || !errors.Is(e, runner.ErrNotFound) {
		t.Fatal(p, e)
	}
}

type failingWriter struct{ calls int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > 1 {
		return 0, errors.New("full")
	}
	return len(p), nil
}
func TestOutputDiscard(t *testing.T) {
	// R-9NTU-YI6P R-9P1R-C9XE
	for _, fail := range []bool{false, true} {
		d, env := fixture(t, "import sys\nsys.stdout.write('x'*1048576)\nsys.stderr.write('e'*1048576)\n")
		var out, errout io.Writer
		if fail {
			out = &failingWriter{}
			errout = &failingWriter{}
		}
		p := start(context.Background(), t, d, env, out, errout)
		if n := wait(t, p); n != 0 {
			t.Fatal(n)
		}
	}
}
func TestKillConcurrency(t *testing.T) {
	// R-9Q9N-Q1O3 R-9XL2-0O49 R-9YSY-EFUY R-A18R-5ZCC
	d, env := fixture(t, "import signal\nprint('ready',flush=True)\nwhile True: signal.pause()\n")
	o := newObserved()
	p := start(context.Background(), t, d, env, o, nil)
	if o.next(t) != "ready" {
		t.Fatal("ready")
	}
	const count = 12
	answers := make(chan int, count)
	for range count {
		go func() { answers <- p.Wait() }()
	}
	kills := make(chan bool, count)
	for range count {
		go func() { kills <- p.Kill() }()
	}
	hits := 0
	for range count {
		select {
		case v := <-kills:
			if v {
				hits++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("kill blocked")
		}
	}
	if hits != 1 {
		t.Fatal(hits)
	}
	for range count {
		select {
		case n := <-answers:
			if n != 137 {
				t.Fatal(n)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("wait blocked")
		}
	}
	if p.Kill() {
		t.Fatal("repeat kill")
	}
}
func TestContextDetached(t *testing.T) {
	// R-JJV5-1GN4
	d, env := fixture(t, "import os\nprint('ready',flush=True)\nwhile not os.path.exists('go'): pass\nprint('after cancel',flush=True)\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := newObserved()
	p := start(ctx, t, d, env, o, nil)
	o.next(t)
	cancel()
	if e := os.WriteFile(filepath.Join(d, "go"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if n := wait(t, p); n != 0 || o.text() != "ready\nafter cancel\n" {
		t.Fatal(n, o.text())
	}
}
func TestGroupCleanup(t *testing.T) {
	// R-9K65-T6YM R-X6PZ-BP1D R-X95S-38IR R-9WD5-MWDK
	for _, kill := range []bool{false, true} {
		t.Run(strconv.FormatBool(kill), func(t *testing.T) {
			tail := "print('main done',flush=True);print('main err',file=sys.stderr,flush=True)"
			if kill {
				tail = "print('ready',flush=True)\nwhile True: signal.pause()"
			}
			code := `import os,sys,subprocess,signal
child=subprocess.Popen([sys.executable,'-c',"import os,sys,signal;print('tick',flush=True);print('tick err',file=sys.stderr,flush=True);open('child','w').write(str(os.getpid()));signal.pause()"])
while not os.path.exists('child') or not open('child').read(): pass
` + tail + "\n"
			d, env := fixture(t, code)
			o := newObserved()
			var errout bytes.Buffer
			p := start(context.Background(), t, d, env, o, &errout)
			want := 0
			if kill {
				for o.next(t) != "ready" {
					runtime.Gosched()
				}
				if !p.Kill() {
					t.Fatal("kill missed")
				}
				want = 137
			}
			if n := wait(t, p); n != want {
				t.Fatal(n)
			}
			b, e := readChild(d)
			if e != nil {
				t.Fatal(e)
			}
			pid, e := strconv.Atoi(string(b))
			if e != nil {
				t.Fatal(e)
			}
			if !gone(pid) {
				t.Fatal("child still alive", pid)
			}
			if !kill && (o.text() != "tick\nmain done\n" || errout.String() != "tick err\nmain err\n") {
				t.Fatal(o.text())
			}
		})
	}
}
func TestEscapedChildCannotHoldWait(t *testing.T) {
	// R-JCJQ-QU6Y
	d, env := fixture(t, `import os,sys,subprocess
child=subprocess.Popen([sys.executable,'-c',"import os,sys;os.setsid();open('child','w').write(str(os.getpid()));exec(\"while not os.path.exists('release'): pass\\ntry: print('late',flush=True)\\nexcept BrokenPipeError: pass\\nopen('finished','w').write('done')\")"])
while not os.path.exists('child') or not open('child').read(): pass
print('done',flush=True)
`)
	var out bytes.Buffer
	p := start(context.Background(), t, d, env, &out, nil)
	if n := wait(t, p); n != 0 || out.String() != "done\n" {
		t.Fatal(n, out.String())
	}
	b, e := readChild(d)
	if e != nil {
		t.Fatal(e)
	}
	pid, e := strconv.Atoi(string(b))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if e := os.WriteFile(filepath.Join(d, "release"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.After(5 * time.Second)
	for !gone(pid) {
		select {
		case <-deadline:
			t.Fatal("escaped child did not finish")
		default:
			runtime.Gosched()
		}
	}
	if out.String() != "done\n" {
		t.Fatal(out.String())
	}
}
func TestRunnerDoesNotWriteParentStreams(t *testing.T) {
	// R-JMAX-T04I
	out, e := os.CreateTemp(t.TempDir(), "stdout")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = out.Close() }()
	errout, e := os.CreateTemp(t.TempDir(), "stderr")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = errout.Close() }()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errout
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	d, env := fixture(t, "import sys;print('out');print('err',file=sys.stderr)")
	p := start(context.Background(), t, d, env, nil, nil)
	wait(t, p)
	_, _ = runner.Find("")
	p.Kill()
	for _, f := range []*os.File{out, errout} {
		st, e := f.Stat()
		if e != nil {
			t.Fatal(e)
		}
		if st.Size() != 0 {
			t.Fatal("parent stream written")
		}
	}
}

func readChild(dir string) ([]byte, error) {
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	defer func() { _ = root.Close() }()
	f, e := root.Open("child")
	if e != nil {
		return nil, e
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
