// Package runner launches and collects Python script process groups.
package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// PythonVersion is the interpreter's minor version.
const PythonVersion = "3.12"

// Interpreter is the host command used for every script.
const Interpreter = "python" + PythonVersion

// ErrNotFound reports that the caller's path contains no interpreter.
var ErrNotFound = errors.New("interpreter not found")

// Spec describes a script's complete process environment.
type Spec struct {
	Dir                string
	Env                []string
	Stdout             io.Writer
	Stderr             io.Writer
	Cgroup             string
	MemoryMax, PidsMax int64
}

// Process owns a script and the process group it leads.
type Process struct {
	mu                sync.Mutex
	pid               int
	cgroup            string
	killed, collected bool
	done              chan struct{}
	code              int
}

// Find searches absolute directories without running the interpreter.
func Find(path string) (string, error) {
	for _, d := range strings.Split(path, ":") {
		if !filepath.IsAbs(d) {
			continue
		}
		p := d + string(os.PathSeparator) + Interpreter
		st, e := os.Stat(p)
		if e == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// Start returns as soon as the script is launched, independently of later context cancellation.
func Start(ctx context.Context, s Spec) (*Process, error) {
	preserveGroup := false
	if s.Cgroup != "" {
		st, err := os.Stat(s.Cgroup)
		preserveGroup = err == nil && st.IsDir()
	}
	success := false
	defer func() {
		if s.Cgroup != "" && !preserveGroup && !success {
			_ = os.RemoveAll(s.Cgroup)
		}
	}()
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	path := ""
	for _, v := range s.Env {
		if strings.HasPrefix(v, "PATH=") {
			path = strings.TrimPrefix(v, "PATH=")
		}
	}
	exe, e := Find(path)
	if e != nil {
		return nil, e
	}
	if s.Cgroup != "" {
		if s.MemoryMax < 1 || s.PidsMax < 1 {
			return nil, errors.New("invalid cgroup limit")
		}
		if st, err := os.Stat(s.Cgroup); err == nil {
			if !st.IsDir() {
				return nil, errors.New("cgroup is not a directory")
			}
		} else {
			if err = os.Mkdir(s.Cgroup, 0700); err != nil {
				return nil, err
			}
		}
	}
	if s.Cgroup != "" {
		for _, v := range []struct{ name, value string }{
			{"memory.max", strconv.FormatInt(s.MemoryMax, 10)}, {"memory.oom.group", "1"},
			{"pids.max", strconv.FormatInt(s.PidsMax, 10)}, {"memory.swap.max", "0"},
		} {
			if err := os.WriteFile(filepath.Join(s.Cgroup, v.name), []byte(v.value), 0600); err != nil {
				return nil, err
			}
		}
	}
	in, e := os.Open(os.DevNull)
	if e != nil {
		return nil, e
	}
	defer func() { _ = in.Close() }()
	out, e := newStream(s.Stdout)
	if e != nil {
		return nil, e
	}
	defer out.dispose()
	errout, e := newStream(s.Stderr)
	if e != nil {
		return nil, e
	}
	defer errout.dispose()
	// Explicit nil entries close even descriptors opened without close-on-exec.
	entries, e := os.ReadDir("/proc/self/fd")
	if e != nil {
		return nil, e
	}
	maxfd := 2
	for _, v := range entries {
		n, err := strconv.Atoi(v.Name())
		if err == nil && n > maxfd {
			maxfd = n
		}
	}
	files := make([]uintptr, maxfd+1)
	for i := range files {
		files[i] = ^uintptr(0)
	}
	files[0] = in.Fd()
	files[1] = uintptr(out.write)
	files[2] = uintptr(errout.write)
	pid, e := syscall.ForkExec(exe, []string{exe, "main.py"}, &syscall.ProcAttr{Dir: s.Dir, Env: s.Env, Files: files, Sys: &syscall.SysProcAttr{Setpgid: true}})
	if e != nil {
		return nil, e
	}
	if s.Cgroup != "" {
		if err := os.WriteFile(filepath.Join(s.Cgroup, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0600); err != nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			var status syscall.WaitStatus
			for {
				if _, err := syscall.Wait4(pid, &status, 0, nil); err != syscall.EINTR {
					break
				}
			}
			for groupAlive(pid) {
				runtime.Gosched()
			}
			return nil, err
		}
	}
	success = true
	p := &Process{pid: pid, cgroup: s.Cgroup, done: make(chan struct{})}
	out.start()
	errout.start()
	go p.collect(out, errout)
	return p, nil
}

// Wait waits for the entire group and returns the script's status.
func (p *Process) Wait() int { <-p.done; return p.code }

// Kill sends SIGKILL without waiting and reports whether this call ended the script.
func (p *Process) Kill() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.collected || p.killed {
		return false
	}
	si, e := waitInfo(p.pid, true)
	if e != nil || si.Signo != 0 {
		return false
	}
	if syscall.Kill(-p.pid, syscall.SIGKILL) != nil {
		return false
	}
	si, e = waitInfo(p.pid, true)
	if e == nil && si.Signo != 0 && !killedStatus(p.pid) {
		return false
	}
	p.killed = true
	return true
}

func waitInfo(pid int, nohang bool) (unix.Siginfo, error) {
	var si unix.Siginfo
	flags := unix.WEXITED | unix.WNOWAIT
	if nohang {
		flags |= unix.WNOHANG
	}
	for {
		e := unix.Waitid(unix.P_PID, pid, &si, flags, nil)
		if errors.Is(e, syscall.EINTR) {
			continue
		}
		return si, e
	}
}
func killedStatus(pid int) bool {
	b, e := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if e != nil {
		return false
	}
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return false
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 50 {
		return false
	}
	status, e := strconv.Atoi(f[49])
	return e == nil && status == int(syscall.SIGKILL)
}
func (p *Process) collect(out, errout *stream) {
	_, _ = waitInfo(p.pid, false)
	p.mu.Lock()
	p.collected = true
	p.mu.Unlock()
	_ = syscall.Kill(-p.pid, syscall.SIGKILL)
	for groupAlive(p.pid) {
		runtime.Gosched()
	}
	var status syscall.WaitStatus
	for {
		_, e := syscall.Wait4(p.pid, &status, 0, nil)
		if e != syscall.EINTR {
			break
		}
	}
	if status.Exited() {
		p.code = status.ExitStatus()
	} else {
		p.code = 128 + int(status.Signal())
	}
	out.finish()
	errout.finish()
	if p.cgroup != "" {
		_ = os.WriteFile(filepath.Join(p.cgroup, "cgroup.kill"), []byte("1"), 0600)
		if err := os.Remove(p.cgroup); err != nil {
			_ = os.RemoveAll(p.cgroup)
		}
	}
	close(p.done)
}
func groupAlive(group int) bool {
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return false
	}
	for _, v := range entries {
		if _, e := strconv.Atoi(v.Name()); e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", v.Name(), "stat"))
		if e != nil {
			continue
		}
		i := strings.LastIndexByte(string(b), ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(string(b[i+1:]))
		if len(f) < 3 {
			continue
		}
		g, e := strconv.Atoi(f[2])
		if e == nil && g == group && f[0] != "Z" {
			return true
		}
	}
	return false
}

type stream struct {
	read, write, wakeRead, wakeWrite, ep int
	writer                               io.Writer
	stop                                 chan struct{}
	done                                 chan struct{}
	started                              bool
}

func newStream(w io.Writer) (*stream, error) {
	s := &stream{read: -1, write: -1, wakeRead: -1, wakeWrite: -1, ep: -1, writer: w, stop: make(chan struct{}), done: make(chan struct{})}
	fail := func(e error) (*stream, error) { s.dispose(); return nil, e }
	var f [2]int
	if e := syscall.Pipe2(f[:], syscall.O_CLOEXEC); e != nil {
		return fail(e)
	}
	s.read, s.write = f[0], f[1]
	if e := syscall.SetNonblock(s.read, true); e != nil {
		return fail(e)
	}
	if e := syscall.Pipe2(f[:], syscall.O_CLOEXEC|syscall.O_NONBLOCK); e != nil {
		return fail(e)
	}
	s.wakeRead, s.wakeWrite = f[0], f[1]
	ep, e := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if e != nil {
		return fail(e)
	}
	s.ep = ep
	for i, fd := range []int{s.read, s.wakeRead} {
		if e := syscall.EpollCtl(ep, syscall.EPOLL_CTL_ADD, fd, &syscall.EpollEvent{Events: syscall.EPOLLIN | syscall.EPOLLHUP, Fd: int32(i)}); e != nil {
			return fail(e)
		}
	}
	return s, nil
}
func (s *stream) start() { s.started = true; _ = syscall.Close(s.write); s.write = -1; go s.copy() }
func (s *stream) dispose() {
	if s.started {
		return
	}
	s.close()
}
func (s *stream) close() {
	for _, fd := range []int{s.read, s.write, s.wakeRead, s.wakeWrite, s.ep} {
		if fd >= 0 {
			_ = syscall.Close(fd)
		}
	}
}
func (s *stream) finish() {
	close(s.stop)
	_, _ = syscall.Write(s.wakeWrite, []byte{1})
	<-s.done
	s.close()
}
func (s *stream) copy() {
	defer close(s.done)
	buf := make([]byte, 32768)
	events := make([]syscall.EpollEvent, 2)
	for {
		for {
			n, e := syscall.Read(s.read, buf)
			if n > 0 && s.writer != nil {
				if _, err := s.writer.Write(buf[:n]); err != nil {
					s.writer = nil
				}
			}
			if e == syscall.EINTR {
				continue
			}
			if e == syscall.EAGAIN {
				break
			}
			if e != nil || n == 0 {
				return
			}
		}
		select {
		case <-s.stop:
			return
		default:
		}
		_, e := syscall.EpollWait(s.ep, events, -1)
		if e != nil && !errors.Is(e, syscall.EINTR) {
			return
		}
	}
}
