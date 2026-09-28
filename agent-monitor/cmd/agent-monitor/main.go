// Package main wires the agent-monitor process to the CLI run seam.
package main

import (
	"os"
	"syscall"
	"unsafe"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

func main() {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios)))
	watcher := newMachineWatcher()
	interrupt := machineInterrupt()
	code := cli.Run(os.Args[1:], cli.System{
		Home:      os.Getenv("HOME"),
		Root:      os.DirFS("/"),
		NoColor:   os.Getenv("NO_COLOR"),
		Term:      os.Getenv("TERM"),
		Terminal:  errno == 0,
		Watcher:   watcher,
		Interrupt: interrupt,
	}, os.Stdout, os.Stderr)
	watcher.close()
	os.Exit(int(code))
}
