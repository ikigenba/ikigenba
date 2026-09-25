// Package cli contains the agent-monitor command and its run seam.
package cli

import "io/fs"

// System contains the machine state visible to Run.
type System struct {
	Home     string
	Root     fs.FS
	NoColor  string
	Term     string
	Terminal bool
}

// ExitCode is the result of running the command.
type ExitCode int

// Exit codes for command outcomes.
const (
	ExitSuccess         ExitCode = 0
	ExitWriteFailed     ExitCode = 1
	ExitUsage           ExitCode = 2
	ExitDataUnreadable  ExitCode = 3
	ExitSessionNotFound ExitCode = 4
)
