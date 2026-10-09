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

	"github.com/ikigenba/ikigenba/prompts/internal/runner"
)

// This package's permitted runner probe runs as the test binary's agent role.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "agent" {
		os.Exit(probe())
	}
	os.Exit(m.Run())
}
func probe() int {
	switch os.Getenv("PROBE") {
	case "inspect":
		env, e := os.ReadFile("/proc/self/environ")
		if e != nil {
			return 9
		}
		stat, e := os.ReadFile("/proc/self/stat")
		if e != nil {
			return 9
		}
		parent, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", os.Getppid()))
		if e != nil {
			return 9
		}
		entries, e := os.ReadDir(".")
		if e != nil {
			return 9
		}
		var names []string
		for _, v := range entries {
			names = append(names, v.Name())
		}
		var fds []string
		for fd := 3; fd < 256; fd++ {
			if v, e := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd)); e == nil {
				fds = append(fds, v)
			}
		}
		pipe, e := os.Readlink("/proc/self/fd/0")
		if e != nil {
			return 9
		}
		in, e := io.ReadAll(os.Stdin)
		if e != nil {
			return 9
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args, "env": string(env), "stat": string(stat), "parent": string(parent), "names": names, "fds": fds, "pipe": pipe, "input": string(in), "uid": os.Getuid(), "euid": os.Geteuid(), "gid": os.Getgid(), "egid": os.Getegid()})
		return 0
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		return 0
	case "close":
		b := make([]byte, 10)
		_, _ = io.ReadFull(os.Stdin, b)
		_ = os.Stdin.Close()
		return 0
	case "no-read":
		_, _ = os.Stdout.WriteString("ready\n")
		waitFile("release")
		return 3
	case "hold", "hold-pid":
		if os.Getenv("PROBE") == "hold-pid" {
			_, _ = fmt.Fprintf(os.Stdout, "pid:%d\n", os.Getpid())
		}
		_, _ = os.Stdout.WriteString("ready\n")
		waitFile("release")
		_, _ = os.Stdout.WriteString("released\n")
		return 0
	case "flood":
		b := bytes.Repeat([]byte("x"), 1048576)
		_, _ = os.Stdout.Write(b)
		_, _ = os.Stderr.Write(b)
		return 0
	case "term":
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {}
	case "kill":
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	case "exit3":
		return 3
	case "descendants", "descendants-hold", "escaped":
		groups := []bool{false, true}
		if os.Getenv("PROBE") == "escaped" {
			groups = []bool{false}
		}
		var pids []int
		for i, group := range groups {
			file := fmt.Sprintf("pid-%d", i)
			command := "printf 'tick\\n'; printf '%s' \"$$\" > " + file + "; while :; do :; done"
			bash := os.Getenv("BASH")
			if !filepath.IsAbs(bash) {
				return 9
			}
			cmd := &exec.Cmd{Path: bash, Args: []string{bash, "-c", command}}
			cmd.Env = []string{"HOME=" + os.Getenv("HOME")}
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: group}
			if os.Getenv("PROBE") == "escaped" {
				cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			}
			if e := cmd.Start(); e != nil {
				return 9
			}
			waitFile(file)
			pids = append(pids, cmd.Process.Pid)
		}
		b, _ := json.Marshal(pids)
		_, _ = fmt.Fprintf(os.Stdout, "pids:%s\n", b)
		if os.Getenv("PROBE") == "descendants-hold" {
			waitFile("release")
		}
		_, _ = os.Stdout.WriteString("main done\n")
		return 0
	}
	return 0
}
func waitFile(path string) {
	for {
		if _, e := os.Stat(path); e == nil {
			return
		}
		runtime.Gosched()
	}
}
func statFields(stat string) []string {
	return strings.Fields(stat[strings.LastIndexByte(stat, ')')+1:])
}
func gone(pid int) bool {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	return os.IsNotExist(e) || (e == nil && statFields(string(b))[0] == "Z")
}
func wait(t *testing.T, p *runner.Process) int {
	t.Helper()
	done := make(chan int, 1)
	go func() { done <- p.Wait() }()
	select {
	case n := <-done:
		return n
	case <-time.After(5 * time.Second):
		p.Kill()
		t.Fatal("runner wait deadline")
		return -1
	}
}
func start(t *testing.T, mode string, s runner.Spec) *runner.Process {
	t.Helper()
	if s.Dir == "" {
		s.Dir = t.TempDir()
	}
	s.Env = append(s.Env, "PROBE="+mode, "HOME="+s.Dir)
	p, e := runner.Start(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Kill(); p.Wait() })
	return p
}

type lines struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	ready   chan string
	pending string
}

func newLines() *lines { return &lines{ready: make(chan string, 20)} }
func (l *lines) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buffer.Write(b)
	l.pending += string(b)
	for {
		at := strings.IndexByte(l.pending, '\n')
		if at < 0 {
			break
		}
		l.ready <- l.pending[:at]
		l.pending = l.pending[at+1:]
	}
	return len(b), nil
}
func (l *lines) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }
func (l *lines) next(t *testing.T) string {
	t.Helper()
	select {
	case line := <-l.ready:
		return line
	case <-time.After(5 * time.Second):
		t.Fatal("probe readiness deadline")
		return ""
	}
}
func release(t *testing.T, dir string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); e != nil {
		t.Fatal(e)
	}
}

// R-X8M3-2YWS R-X9TZ-GQNH R-5827-RHPW R-XEPK-ZTM9 R-12YA-5PC4 R-0ZAL-0E41 R-11QD-RXLF R-XM0Z-AG2F R-5E5P-OCFD
func TestProcessSeam(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "caller-file")
	if e := os.WriteFile(file, []byte("unchanged"), 0600); e != nil {
		t.Fatal(e)
	}
	fd, e := syscall.Open(file, syscall.O_RDONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = syscall.Close(fd) }()
	var out bytes.Buffer
	env := []string{"PROBE=inspect", "HOME=" + dir, "CUSTOM=value"}
	p, e := runner.Start(context.Background(), runner.Spec{Dir: dir, Env: env, Stdout: &out, Stdin: []byte("fixture-stdin-secret-marker"), MemoryMax: 0, PidsMax: 0})
	if e != nil {
		t.Fatal(e)
	}
	if wait(t, p) != 0 {
		t.Fatal(out.String())
	}
	var result struct {
		Args                           []string
		Env, Stat, Parent, Pipe, Input string
		Names, Fds                     []string
		UID, EUID, GID, EGID           int
	}
	if e = json.Unmarshal(out.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if len(result.Args) != 2 || result.Args[0] != exe || result.Args[1] != "agent" || len(result.Names) != 1 || result.Names[0] != "caller-file" {
		t.Fatal(result)
	}
	if result.Env != strings.Join(env, "\x00")+"\x00" || result.Input != "fixture-stdin-secret-marker" || !strings.HasPrefix(result.Pipe, "pipe:") {
		t.Fatal(result)
	}
	fields := statFields(result.Stat)
	pid := strings.Fields(result.Stat)[0]
	if fields[2] != pid || fields[3] != pid || statFields(result.Parent)[3] == pid {
		t.Fatal(fields)
	}
	for _, target := range result.Fds {
		if target == file {
			t.Fatal("inherited descriptor")
		}
	}
	if result.UID != os.Getuid() || result.EUID != os.Geteuid() || result.GID != os.Getgid() || result.EGID != os.Getegid() {
		t.Fatal(result)
	}
	b, e := readFixture(file)
	if e != nil || string(b) != "unchanged" {
		t.Fatal(string(b), e)
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 1 {
		t.Fatal(entries, e)
	}
	var groupOut bytes.Buffer
	group := filepath.Join(t.TempDir(), "group")
	groupChild := start(t, "inspect", runner.Spec{Dir: dir, Cgroup: group, MemoryMax: 1, PidsMax: 1, Stdout: &groupOut})
	if wait(t, groupChild) != 0 {
		t.Fatal("group inspect")
	}
	if e = json.Unmarshal(groupOut.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	for _, target := range result.Fds {
		if target == file || target == group {
			t.Fatal("inherited caller/group descriptor", target)
		}
	}

}

// R-XH5D-RD3N R-5BPW-WSXZ R-XPOO-FRAI R-XOGS-1ZJT
func TestStreams(t *testing.T) {
	for _, in := range [][]byte{nil, {}, bytes.Repeat([]byte{0, 255, 11, 98}, 262144)} {
		var out bytes.Buffer
		p := start(t, "echo", runner.Spec{Stdin: in, Stdout: &out})
		if wait(t, p) != 0 || !bytes.Equal(out.Bytes(), in) {
			t.Fatal("stdin mismatch")
		}
		var inspect bytes.Buffer
		p = start(t, "inspect", runner.Spec{Stdin: in, Stdout: &inspect})
		if wait(t, p) != 0 {
			t.Fatal("inspect")
		}
		var r struct{ Pipe string }
		if e := json.Unmarshal(inspect.Bytes(), &r); e != nil || !strings.HasPrefix(r.Pipe, "pipe:") {
			t.Fatal(r, e)
		}
	}
	var capturedOut, capturedErr bytes.Buffer
	full := start(t, "flood", runner.Spec{Stdout: &capturedOut, Stderr: &capturedErr})
	if wait(t, full) != 0 || !bytes.Equal(capturedOut.Bytes(), bytes.Repeat([]byte("x"), 1048576)) || !bytes.Equal(capturedErr.Bytes(), bytes.Repeat([]byte("x"), 1048576)) {
		t.Fatal("streams lost bytes")
	}
	p := start(t, "close", runner.Spec{Stdin: bytes.Repeat([]byte("x"), 1048576)})
	if wait(t, p) != 0 {
		t.Fatal("closed input")
	}
	dir := t.TempDir()
	out := newLines()
	p = start(t, "no-read", runner.Spec{Dir: dir, Stdout: out, Stdin: bytes.Repeat([]byte("x"), 1048576)})
	if out.next(t) != "ready" {
		t.Fatal("not ready")
	}
	release(t, dir)
	if wait(t, p) != 3 {
		t.Fatal("unread input")
	}
	p = start(t, "flood", runner.Spec{})
	if wait(t, p) != 0 {
		t.Fatal("nil streams")
	}
	bad := &failingWriter{}
	p = start(t, "flood", runner.Spec{Stdout: bad, Stderr: bad})
	if wait(t, p) != 0 {
		t.Fatal("failed writer")
	}
	if bad.calls < 2 {
		t.Fatal(bad.calls)
	}
}

type failingWriter struct {
	mu    sync.Mutex
	calls int
}

func (w *failingWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.calls > 1 {
		return 0, errors.New("writer failed")
	}
	return len(b), nil
}

// R-XQWK-TJ17 R-XS4H-7ARW R-XTCD-L2IL R-YLE2-DSKK R-Y1VO-9GPG
func TestEndings(t *testing.T) {
	for mode, want := range map[string]int{"exit3": 3, "term": 143, "kill": 137, "inspect": 0} {
		p := start(t, mode, runner.Spec{})
		if got := wait(t, p); got != want {
			t.Fatal(mode, got)
		}
		if p.Kill() || p.Wait() != want {
			t.Fatal("changed completed status")
		}
	}
	group := filepath.Join(t.TempDir(), "group")
	groupChild := start(t, "kill", runner.Spec{Cgroup: group, MemoryMax: 1, PidsMax: 1})
	if wait(t, groupChild) != 137 {
		t.Fatal("external signal in group")
	}

	dir := t.TempDir()
	out := newLines()
	p := start(t, "hold", runner.Spec{Dir: dir, Stdout: out})
	if out.next(t) != "ready" {
		t.Fatal("not ready")
	}
	if !p.Kill() {
		t.Fatal("not running")
	}
	if wait(t, p) != 137 {
		t.Fatal("kill code")
	}
}

// R-XB1V-UIE6 R-H1U6-AR4J R-15E2-X8TI R-H322-OIV8 R-17TV-OSAW R-5AI0-J17A R-Y33K-N8G5
func TestSessionAndConcurrentEnding(t *testing.T) {
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Fatal(e)
	}
	for _, kill := range []bool{false, true} {
		dir := t.TempDir()
		out := newLines()
		mode := "descendants"
		if kill {
			mode = "descendants-hold"
		}
		p := start(t, mode, runner.Spec{Dir: dir, Env: []string{"BASH=" + bash}, Stdout: out})
		var pids []int
		for i := 0; i < 3; i++ {
			line := out.next(t)
			if strings.HasPrefix(line, "pids:") {
				if e = json.Unmarshal([]byte(strings.TrimPrefix(line, "pids:")), &pids); e != nil {
					t.Fatal(e)
				}
			} else if line != "tick" {
				t.Fatal(line)
			}
		}
		if len(pids) != 2 {
			t.Fatal(pids)
		}
		const callers = 8
		codes := make(chan int, callers)
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Go(func() { codes <- p.Wait() })
		}
		want := 0
		if kill {
			want = 137
			answers := make(chan bool, callers)
			for i := 0; i < callers; i++ {
				wg.Go(func() { answers <- p.Kill() })
			}
			wins := 0
			for i := 0; i < callers; i++ {
				if <-answers {
					wins++
				}
			}
			if wins != 1 {
				t.Fatal(wins)
			}
		}
		wg.Wait()
		for i := 0; i < callers; i++ {
			if code := <-codes; code != want {
				t.Fatal(code, want)
			}
		}
		for _, pid := range pids {
			if !gone(pid) {
				t.Fatal("session member survived", pid)
			}
		}
		if !kill && out.String() != "tick\ntick\npids:"+string(mustJSON(t, pids))+"\nmain done\n" {
			t.Fatal(out.String())
		}
		if p.Kill() {
			t.Fatal("late kill")
		}
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

// R-59A4-59GL
func TestEscapedSessionDoesNotHoldWait(t *testing.T) {
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	out := newLines()
	p := start(t, "escaped", runner.Spec{Dir: dir, Env: []string{"BASH=" + bash}, Stdout: out})
	if out.next(t) != "tick" {
		t.Fatal("tick")
	}
	var pids []int
	if e = json.Unmarshal([]byte(strings.TrimPrefix(out.next(t), "pids:")), &pids); e != nil || len(pids) != 1 {
		t.Fatal(pids, e)
	}
	defer func() { _ = syscall.Kill(pids[0], syscall.SIGKILL) }()
	if wait(t, p) != 0 {
		t.Fatal("escaped writer held Wait")
	}
}

// R-Y4BH-106U R-191S-2K1L R-Y6R9-SJO8 R-YHQD-8HCH R-YK66-00TV
func TestStartRefusalsAndContext(t *testing.T) {
	cause := errors.New("cancelled fixture")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	group := filepath.Join(t.TempDir(), "group")
	p, e := runner.Start(ctx, runner.Spec{Dir: t.TempDir(), Cgroup: group, MemoryMax: 1, PidsMax: 1})
	if p != nil || !errors.Is(e, cause) {
		t.Fatal(p, e)
	}
	if _, e = os.Stat(group); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if e = os.WriteFile(file, nil, 0600); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{file, filepath.Join(dir, "absent")} {
		group = filepath.Join(t.TempDir(), "group")
		p, e = runner.Start(context.Background(), runner.Spec{Dir: path, Cgroup: group, MemoryMax: 1, PidsMax: 1})
		if p != nil || e == nil {
			t.Fatal(p, e)
		}
		if _, e = os.Stat(group); !os.IsNotExist(e) {
			t.Fatal(e)
		}
	}
	for _, limits := range [][2]int64{{0, 1}, {1, 0}, {-1, 2}} {
		group = filepath.Join(t.TempDir(), "group")
		p, e = runner.Start(context.Background(), runner.Spec{Dir: dir, Cgroup: group, MemoryMax: limits[0], PidsMax: limits[1]})
		if p != nil || e == nil {
			t.Fatal(p, e)
		}
		if _, e = os.Stat(group); !os.IsNotExist(e) {
			t.Fatal(e)
		}
	}
	ctx, cancel = context.WithCancelCause(context.Background())
	out := newLines()
	p, e = runner.Start(ctx, runner.Spec{Dir: dir, Env: []string{"PROBE=hold", "HOME=" + dir}, Stdout: out})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Kill(); p.Wait() })
	if out.next(t) != "ready" {
		t.Fatal("ready")
	}
	cancel(cause)
	release(t, dir)
	if out.next(t) != "released" || wait(t, p) != 0 {
		t.Fatal("context killed started run")
	}
}

// R-Y972-K35M R-YAEY-XUWB R-YE2O-364E
func TestControlGroupLifecycle(t *testing.T) {
	for _, kill := range []bool{false, true} {
		parent := t.TempDir()
		group := filepath.Join(parent, "group")
		if e := os.Mkdir(group, 0700); e != nil {
			t.Fatal(e)
		}
		if e := syscall.Mkfifo(filepath.Join(group, "cgroup.kill"), 0600); e != nil {
			t.Fatal(e)
		}
		killed := make(chan string, 1)
		go func() {
			f, e := openFixture(filepath.Join(group, "cgroup.kill"))
			if e != nil {
				killed <- e.Error()
				return
			}
			b, e := io.ReadAll(f)
			_ = f.Close()
			if e != nil {
				killed <- e.Error()
				return
			}
			killed <- string(b)
		}()
		dir := t.TempDir()
		out := newLines()
		p := start(t, "hold-pid", runner.Spec{Dir: dir, Stdout: out, Cgroup: group, MemoryMax: 134217728, PidsMax: 64})
		reportedPID, err := strconv.Atoi(strings.TrimPrefix(out.next(t), "pid:"))
		if err != nil {
			t.Fatal(err)
		}
		if out.next(t) != "ready" {
			t.Fatal("ready")
		}
		for name, want := range map[string]string{"memory.max": "134217728", "memory.oom.group": "1", "pids.max": "64", "memory.swap.max": "0"} {
			b, e := readFixture(filepath.Join(group, name))
			if e != nil || string(b) != want {
				t.Fatal(name, string(b), e)
			}
		}
		pidBytes, e := readFixture(filepath.Join(group, "cgroup.procs"))
		if e != nil {
			t.Fatal(e)
		}
		pid, e := strconv.Atoi(string(pidBytes))
		if e != nil || gone(pid) || pid != reportedPID {
			t.Fatal(string(pidBytes), e)
		}
		want := 0
		if kill {
			if !p.Kill() {
				t.Fatal("kill")
			}
			want = 137
		} else {
			release(t, dir)
		}
		if wait(t, p) != want {
			t.Fatal("status")
		}
		select {
		case got := <-killed:
			if got != "1" {
				t.Fatal(got)
			}
		case <-time.After(time.Second):
			t.Fatal("missing cgroup kill")
		}
		if _, e = os.Stat(group); !os.IsNotExist(e) {
			t.Fatal(e)
		}
	}
	// A missing leaf group is made, with no implicit parent creation.
	parent := t.TempDir()
	group := filepath.Join(parent, "new")
	p := start(t, "exit3", runner.Spec{Cgroup: group, MemoryMax: 1, PidsMax: 1})
	if wait(t, p) != 3 {
		t.Fatal("new group")
	}
	if _, e := os.Stat(group); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}

// R-YBMV-BMN0 R-YCUR-PEDP R-5CXT-AKOO
func TestControlGroupFailures(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"memory.max", "memory.oom.group", "pids.max", "memory.swap.max", "cgroup.procs"} {
		group := filepath.Join(t.TempDir(), "group")
		if e := os.Mkdir(group, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.Mkdir(filepath.Join(group, name), 0700); e != nil {
			t.Fatal(e)
		}
		p, e := runner.Start(context.Background(), runner.Spec{Dir: dir, Cgroup: group, MemoryMax: 1, PidsMax: 1, Env: []string{"PROBE=hold"}})
		assertNoLiveAgents(t)
		if p != nil || e == nil {
			t.Fatal(name, p, e)
		}
		if _, e = os.Stat(group); e != nil {
			t.Fatal("existing group removed", e)
		}
	}
	for _, group := range []string{filepath.Join(t.TempDir(), "absent", "group"), filepath.Join(t.TempDir(), "file")} {
		if filepath.Base(group) == "file" {
			if e := os.WriteFile(group, nil, 0600); e != nil {
				t.Fatal(e)
			}
		}
		p, e := runner.Start(context.Background(), runner.Spec{Dir: dir, Cgroup: group, MemoryMax: 1, PidsMax: 1})
		assertNoLiveAgents(t)
		if p != nil || e == nil {
			t.Fatal(p, e)
		}
	}
	group := filepath.Join(t.TempDir(), "unreadable")
	if e := os.Mkdir(group, os.FileMode(syscall.S_IWUSR|syscall.S_IXUSR|syscall.S_IWGRP|syscall.S_IXGRP|syscall.S_IWOTH|syscall.S_IXOTH)); e != nil {
		t.Fatal(e)
	}
	unreadableGroup := group
	t.Cleanup(func() { _ = os.Chmod(unreadableGroup, ownerDirectoryMode) })
	p, e := runner.Start(context.Background(), runner.Spec{Dir: dir, Cgroup: group, MemoryMax: 1, PidsMax: 1})
	assertNoLiveAgents(t)
	if p != nil || e == nil {
		t.Fatal("unreadable group", p, e)
	}
	if _, e = os.Stat(group); e != nil {
		t.Fatal("removed existing group", e)
	}
	parent := t.TempDir()
	group = filepath.Join(parent, "group")
	out := newLines()
	p = start(t, "hold", runner.Spec{Dir: dir, Stdout: out, Cgroup: group, MemoryMax: 1, PidsMax: 1})
	if out.next(t) != "ready" {
		t.Fatal("ready")
	}
	if e = os.Chmod(parent, ownerReadOnlyMode); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, ownerDirectoryMode) })
	release(t, dir)
	if wait(t, p) != 0 {
		t.Fatal("cleanup changed status")
	}
}

// R-Y7Z6-6BEX
func TestCallerStreamsAreUntouched(t *testing.T) {
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	errR, errW, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr; _ = outR.Close(); _ = errR.Close() }()
	p := start(t, "flood", runner.Spec{})
	code := wait(t, p)
	_ = outW.Close()
	_ = errW.Close()
	b, e := io.ReadAll(outR)
	if e != nil {
		t.Fatal(e)
	}
	d, e := io.ReadAll(errR)
	if e != nil {
		t.Fatal(e)
	}
	if code != 0 || len(b) != 0 || len(d) != 0 {
		t.Fatal(code, len(b), len(d))
	}
}

const ownerDirectoryMode = os.FileMode(syscall.S_IRUSR | syscall.S_IWUSR | syscall.S_IXUSR)
const ownerReadOnlyMode = os.FileMode(syscall.S_IRUSR | syscall.S_IXUSR)

func readFixture(path string) ([]byte, error) {
	f, e := openFixture(path)
	if e != nil {
		return nil, e
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
func openFixture(path string) (*os.File, error) {
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer func() { _ = root.Close() }()
	return root.Open(filepath.Base(path))
}

// assertNoLiveAgents observes the child seam after failed Start, including
// failures occurring after fork. A rogue process is ended before reporting it.
func assertNoLiveAgents(t *testing.T) {
	t.Helper()
	entries, e := os.ReadDir("/proc")
	if e != nil {
		t.Fatal(e)
	}
	var live []int
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		data, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if e != nil {
			continue
		}
		fields := statFields(string(data))
		if len(fields) < 2 || fields[0] == "Z" || fields[1] != strconv.Itoa(os.Getpid()) {
			continue
		}
		data, e = os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if e != nil {
			continue
		}
		args := bytes.Split(bytes.TrimSuffix(data, []byte{0}), []byte{0})
		if len(args) > 0 && string(args[len(args)-1]) == "agent" {
			live = append(live, pid)
		}
	}
	for _, pid := range live {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &status, 0, nil)
	}
	if len(live) > 0 {
		t.Fatalf("failed Start left live agent children: %v", live)
	}
}
