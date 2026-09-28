package main

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// machineWatcher coalesces filesystem events and periodic process rechecks.
type machineWatcher struct {
	mu      sync.Mutex
	fd      int
	watches []int
	changes chan struct{}
	done    chan struct{}
}

func newMachineWatcher() *machineWatcher {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		fd = -1
	}
	w := &machineWatcher{fd: fd, changes: make(chan struct{}, 1), done: make(chan struct{})}
	go w.poll()
	return w
}

func (w *machineWatcher) Watch(names []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fd < 0 {
		return
	}
	for _, wd := range w.watches {
		if wd < 0 || wd > 1<<31-1 {
			continue
		}
		_, _ = syscall.InotifyRmWatch(w.fd, uint32(wd))
	}
	w.watches = nil
	for _, name := range names {
		wd, err := syscall.InotifyAddWatch(w.fd, "/"+name, syscall.IN_CREATE|syscall.IN_DELETE|syscall.IN_MOVED_FROM|syscall.IN_MOVED_TO|syscall.IN_MODIFY|syscall.IN_CLOSE_WRITE)
		if err == nil {
			w.watches = append(w.watches, wd)
		}
	}
}

func (w *machineWatcher) Changes() <-chan struct{} { return w.changes }
func (w *machineWatcher) notify() {
	select {
	case w.changes <- struct{}{}:
	default:
	}
}
func (w *machineWatcher) poll() {
	events := time.NewTicker(50 * time.Millisecond)
	periodic := time.NewTicker(2 * time.Second)
	defer events.Stop()
	defer periodic.Stop()
	var buffer [65536]byte
	for {
		select {
		case <-w.done:
			return
		case <-periodic.C:
			w.notify()
		case <-events.C:
			w.mu.Lock()
			n := 0
			if w.fd >= 0 {
				n, _ = syscall.Read(w.fd, buffer[:])
			}
			w.mu.Unlock()
			if n > 0 {
				w.notify()
			}
		}
	}
}
func (w *machineWatcher) close() {
	close(w.done)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fd >= 0 {
		_ = syscall.Close(w.fd)
		w.fd = -1
	}
}

func machineInterrupt() <-chan struct{} {
	interrupted := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT)
	go func() {
		<-signals
		close(interrupted)
		signal.Stop(signals)
		signal.Reset(syscall.SIGINT)
	}()
	return interrupted
}
