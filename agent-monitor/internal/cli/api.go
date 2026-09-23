// Package cli contains the agent-monitor command and its run seam.
package cli

// ExitCode is the result of running the command.
type ExitCode int

const (
	// ExitSuccess reports a successful command.
	ExitSuccess ExitCode = 0
	// ExitWriteFailed reports an output write failure.
	ExitWriteFailed ExitCode = 1
	// ExitUsage reports an invalid command line.
	ExitUsage ExitCode = 2
)
