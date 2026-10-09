// Package runner starts and ends an isolated child session.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Spec describes a child session and its control group.
type Spec struct {
	Dir       string
	Env       []string
	Stdin     []byte
	Stdout    io.Writer
	Stderr    io.Writer
	Cgroup    string
	MemoryMax int64
	PidsMax   int64
}

// Process coordinates concurrent ending and collection.
type Process struct {
	mu     sync.Mutex
	pid    int
	pidfd  int
	ended  bool
	killed bool
	code   int
	done   chan struct{}
}

var spawnMu sync.Mutex

// Start launches this binary in its agent role.
func Start(ctx context.Context, s Spec) (*Process, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cgroup, cgroupFS, made, err := prepareGroup(s)
	if err != nil {
		return nil, err
	}
	if cgroup != nil {
		defer func() { _ = cgroup.Close() }()
	}
	success := false
	defer func() {
		if !success && made {
			removeGroup(s.Cgroup, cgroupFS)
		}
	}()
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer func() { _ = inR.Close() }()
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inW.Close()
		return nil, err
	}
	defer func() { _ = outW.Close() }()
	errR, errW, err := os.Pipe()
	if err != nil {
		_ = inW.Close()
		_ = outR.Close()
		return nil, err
	}
	defer func() { _ = errW.Close() }()
	attr := &syscall.SysProcAttr{Setsid: true}
	if cgroupFS {
		attr.UseCgroupFD = true
		attr.CgroupFD = int(cgroup.Fd())
	}
	pid, err := fork(executable, s, []uintptr{inR.Fd(), outW.Fd(), errW.Fd()}, attr)
	if err != nil {
		_ = inW.Close()
		_ = outR.Close()
		_ = errR.Close()
		return nil, err
	}
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		killSession(pid)
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &status, 0, nil)
		_ = inW.Close()
		_ = outR.Close()
		_ = errR.Close()
		return nil, err
	}
	p := &Process{pid: pid, pidfd: pidfd, done: make(chan struct{})}
	_ = outW.Close()
	_ = errW.Close()
	_ = inR.Close()
	if cgroup != nil && !cgroupFS {
		if err = os.WriteFile(s.Cgroup+"/cgroup.procs", []byte(strconv.Itoa(pid)), 0600); err != nil {
			_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
			killSession(pid)
			var status syscall.WaitStatus
			_, _ = syscall.Wait4(pid, &status, 0, nil)
			_ = unix.Close(pidfd)
			_ = inW.Close()
			_ = outR.Close()
			_ = errR.Close()
			return nil, err
		}
	}
	go func() { _, _ = inW.Write(s.Stdin); _ = inW.Close() }()
	stop := make(chan struct{})
	var streams sync.WaitGroup
	streams.Add(2)
	go copyStream(outR, s.Stdout, stop, &streams)
	go copyStream(errR, s.Stderr, stop, &streams)
	go p.reap(s, cgroupFS, inW, stop, &streams)
	success = true
	return p, nil
}

// Go's exec seam does not close arbitrary non-CLOEXEC descriptors. Mark those
// descriptors across the fork and restore the parent's flags afterwards.
func fork(executable string, s Spec, files []uintptr, attr *syscall.SysProcAttr) (int, error) {
	spawnMu.Lock()
	defer spawnMu.Unlock()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	var restored []int
	defer func() {
		for _, fd := range restored {
			_, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0)
		}
	}()
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil || fd < 3 {
			continue
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			continue
		}
		if flags&unix.FD_CLOEXEC == 0 {
			if _, err = unix.FcntlInt(uintptr(fd), unix.F_SETFD, flags|unix.FD_CLOEXEC); err != nil {
				return 0, err
			}
			restored = append(restored, fd)
		}
	}
	return syscall.ForkExec(executable, []string{executable, "agent"}, &syscall.ProcAttr{Dir: s.Dir, Env: s.Env, Files: files, Sys: attr})
}

func prepareGroup(s Spec) (*os.File, bool, bool, error) {
	if s.Cgroup == "" {
		return nil, false, false, nil
	}
	if s.MemoryMax < 1 || s.PidsMax < 1 {
		return nil, false, false, errors.New("control group limits must be positive")
	}
	made := false
	if err := os.Mkdir(s.Cgroup, 0700); err != nil {
		info, e := os.Stat(s.Cgroup)
		if e != nil || !info.IsDir() {
			return nil, false, false, err
		}
	} else {
		made = true
	}
	cgroupFS := false
	cleanup := func() {
		if made {
			removeGroup(s.Cgroup, cgroupFS)
		}
	}
	for _, item := range []struct{ name, value string }{{"memory.max", strconv.FormatInt(s.MemoryMax, 10)}, {"memory.oom.group", "1"}, {"pids.max", strconv.FormatInt(s.PidsMax, 10)}, {"memory.swap.max", "0"}} {
		if err := os.WriteFile(s.Cgroup+"/"+item.name, []byte(item.value), 0600); err != nil {
			cleanup()
			return nil, false, false, err
		}
	}
	f, err := os.Open(s.Cgroup)
	if err != nil {
		cleanup()
		return nil, false, false, err
	}
	var stat unix.Statfs_t
	if err = unix.Fstatfs(int(f.Fd()), &stat); err != nil {
		_ = f.Close()
		cleanup()
		return nil, false, false, err
	}
	cgroupFS = stat.Type == unix.CGROUP2_SUPER_MAGIC
	return f, cgroupFS, made, nil
}
func removeGroup(path string, cgroupFS bool) {
	var stat unix.Statfs_t
	if unix.Statfs(path, &stat) == nil && stat.Type == unix.CGROUP2_SUPER_MAGIC {
		cgroupFS = true
	}
	if !cgroupFS {
		_ = os.RemoveAll(path)
		return
	}
	// A cgroup directory is removable once its asynchronous kill has completed.
	for {
		err := os.Remove(path)
		if err == nil || !errors.Is(err, syscall.EBUSY) {
			return
		}
		runtime.Gosched()
	}
}
func copyStream(f *os.File, w io.Writer, stop <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { _ = f.Close() }()
	fd := int(f.Fd())
	if fd < 0 || int64(fd) > 2147483647 {
		return
	}
	_ = unix.SetNonblock(fd, true)
	buffer := make([]byte, 32768)
	drainRemaining := -1
	for {
		if drainRemaining < 0 {
			select {
			case <-stop:
				available, err := unix.IoctlGetInt(fd, unix.TIOCINQ)
				if err != nil {
					return
				}
				drainRemaining = available
			default:
			}
		}
		if drainRemaining == 0 {
			return
		}
		target := buffer
		if drainRemaining > 0 && drainRemaining < len(target) {
			target = target[:drainRemaining]
		}
		n, err := unix.Read(fd, target)
		if drainRemaining > 0 {
			drainRemaining -= n
		}
		if n > 0 && w != nil {
			written, e := w.Write(buffer[:n])
			if e != nil || written != n {
				w = nil
			}
		}
		if err == nil && n == 0 {
			return
		}
		if err != nil && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			return
		}
		if n > 0 || errors.Is(err, unix.EINTR) {
			continue
		}
		select {
		case <-stop:
			return
		default:
		}
		_, _ = unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 20)
	}
}
func (p *Process) reap(s Spec, cgroupFS bool, inW *os.File, stop chan struct{}, streams *sync.WaitGroup) {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, p.pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		break
	}
	p.mu.Lock()
	p.ended = true
	p.mu.Unlock()
	killSession(p.pid)
	if s.Cgroup != "" {
		_ = os.WriteFile(s.Cgroup+"/cgroup.kill", []byte("1"), 0600)
		removeGroup(s.Cgroup, cgroupFS)
	}
	var status syscall.WaitStatus
	for {
		_, err := syscall.Wait4(p.pid, &status, 0, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		break
	}
	_ = inW.Close()
	close(stop)
	streams.Wait()
	code := status.ExitStatus()
	if status.Signaled() {
		code = 128 + int(status.Signal())
	}
	p.mu.Lock()
	if p.killed {
		code = 137
	}
	p.code = code
	_ = unix.Close(p.pidfd)
	p.mu.Unlock()
	close(p.done)
}

// Wait returns the child outcome after session cleanup.
func (p *Process) Wait() int { <-p.done; return p.code }

// Kill claims cancellation once and signals the child without waiting.
func (p *Process) Kill() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ended || p.killed {
		return false
	}
	p.killed = true
	_ = unix.PidfdSendSignal(p.pidfd, unix.SIGKILL, nil, 0)
	return true
}
func processSession(pid int) (int, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, false
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 4 {
		return 0, false
	}
	session, err := strconv.Atoi(fields[3])
	return session, err == nil && fields[0] != "Z"
}
func killSession(session int) {
	for {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return
		}
		live := false
		for _, entry := range entries {
			pid, err := strconv.Atoi(entry.Name())
			if err != nil {
				continue
			}
			sid, alive := processSession(pid)
			if !alive || sid != session {
				continue
			}
			live = true
			fd, err := unix.PidfdOpen(pid, 0)
			if err != nil {
				continue
			}
			sid, alive = processSession(pid)
			if alive && sid == session {
				_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
			}
			_ = unix.Close(fd)
		}
		if !live {
			return
		}
		runtime.Gosched()
	}
}
