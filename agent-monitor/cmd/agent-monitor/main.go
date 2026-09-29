// Package main wires the agent-monitor process to the CLI run seam.
package main

import (
	"os"
	"syscall"
	"unsafe"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

func terminal(fd uintptr) bool {
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&state)))
	return errno == 0
}

func main() {
	os.Exit(int(runMachine()))
}

func runMachine() cli.ExitCode {
	watcher := newMachineWatcher()
	defer watcher.close()
	console := newMachineConsole()
	signals := newMachineSignals(console)
	defer signals.close()
	// A panic propagates after putting the terminal back; normal Run already
	// restored it, so this deferred cleanup then has nothing to do.
	defer console.cleanup()
	code := cli.Run(os.Args[1:], cli.System{
		Home:          os.Getenv("HOME"),
		Root:          os.DirFS("/"),
		NoColor:       os.Getenv("NO_COLOR"),
		Term:          os.Getenv("TERM"),
		Terminal:      terminal(os.Stdout.Fd()),
		StdinTerminal: terminal(os.Stdin.Fd()),
		Watcher:       watcher,
		Interrupt:     signals.interrupt,
		Console:       console,
	}, os.Stdout, os.Stderr)
	signals.waitForTermination()
	return code
}
