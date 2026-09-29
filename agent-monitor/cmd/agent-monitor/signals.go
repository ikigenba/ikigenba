package main

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type machineSignals struct {
	mu        sync.Mutex
	raw       bool
	first     bool
	ending    bool
	intActive bool
	terminate []os.Signal
	values    chan os.Signal
	interrupt chan struct{}
	done      chan struct{}
	console   *machineConsole
}

func newMachineSignals(console *machineConsole) *machineSignals {
	s := &machineSignals{values: make(chan os.Signal, 8), interrupt: make(chan struct{}), done: make(chan struct{}), console: console}
	for _, sig := range []os.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGABRT} {
		if !signal.Ignored(sig) {
			s.terminate = append(s.terminate, sig)
		}
	}
	s.intActive = !signal.Ignored(syscall.SIGINT)
	if s.intActive {
		signal.Notify(s.values, syscall.SIGINT)
	}
	console.signals = s
	go s.listen()
	return s
}

func (s *machineSignals) startRaw() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = true
	// Notify with no signals would subscribe to every signal.
	if len(s.terminate) > 0 {
		signal.Notify(s.values, s.terminate...)
	}
	if s.intActive {
		signal.Notify(s.values, syscall.SIGINT)
	}
}

func (s *machineSignals) stopRaw() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = false
	for _, sig := range s.terminate {
		signal.Reset(sig)
	}
	if s.first && s.intActive {
		signal.Reset(syscall.SIGINT)
	}
}

func (s *machineSignals) listen() {
	for {
		select {
		case <-s.done:
			return
		case sig := <-s.values:
			s.mu.Lock()
			if sig == syscall.SIGINT && !s.first {
				s.first = true
				close(s.interrupt)
				if !s.raw {
					signal.Reset(syscall.SIGINT)
				}
				s.mu.Unlock()
				continue
			}
			s.ending = true
			s.mu.Unlock()
			// Restoring before re-raising preserves the signal's own exit
			// status and, for quit and abort requests, its stack dump.
			s.console.cleanup()
			signal.Reset(sig)
			if unixSignal, ok := sig.(syscall.Signal); ok {
				_ = syscall.Kill(os.Getpid(), unixSignal)
			}
		}
	}
}

func (s *machineSignals) close() {
	signal.Stop(s.values)
	close(s.done)
}

// waitForTermination keeps a concurrently returning Run from replacing the
// intercepted signal's exit status with its own exit code.
func (s *machineSignals) waitForTermination() {
	s.mu.Lock()
	ending := s.ending
	s.mu.Unlock()
	if ending {
		select {}
	}
}
