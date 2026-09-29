// Package cli contains the agent-monitor command and its run seam.
package cli

import "io/fs"

// System contains the machine state visible to Run.
type System struct {
	Home          string
	Root          fs.FS
	NoColor       string
	Term          string
	Terminal      bool
	StdinTerminal bool
	Watcher       Watcher
	Interrupt     <-chan struct{}
	Console       Console
}

// ExitCode is the result of running the command.
type ExitCode int

// Exit codes for command outcomes.
const (
	ExitSuccess        ExitCode = 0
	ExitWriteFailed    ExitCode = 1
	ExitUsage          ExitCode = 2
	ExitDataUnreadable ExitCode = 3
	ExitNotFound       ExitCode = 4
)

// Watcher announces possible changes in the directories a command reads.
type Watcher interface {
	Watch(names []string)
	Changes() <-chan struct{}
}

// Console supplies raw input and terminal dimensions for browsing.
type Console interface {
	Raw() (restore func())
	Keys() <-chan []byte
	Size() (cols, rows int)
	Resized() <-chan struct{}
}
