package build

import (
	"fmt"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// UsageError reports invalid build command syntax or state.
type UsageError struct {
	Message string
	Help    string
}

// Error returns the primary diagnostic.
func (e *UsageError) Error() string { return e.Message }

// Detail returns the usage hint, when one is available.
func (e *UsageError) Detail() string {
	if e.Help == "" {
		return ""
	}
	return fmt.Sprintf("see '%s' for usage", e.Help)
}

// ExitCode returns the command-line usage exit status.
func (e *UsageError) ExitCode() int { return 2 }

// StaleManifestError reports a mismatch between committed and emitted manifests.
type StaleManifestError struct {
	App string
}

// Error returns the manifest repair instruction.
func (e *StaleManifestError) Error() string {
	return fmt.Sprintf("%s: etc/manifest.toml does not match what the binary emits; run '%s manifest > %s/etc/manifest.toml' and commit", e.App, e.App, e.App)
}

// ExitCode returns the command-line usage exit status.
func (e *StaleManifestError) ExitCode() int { return 2 }

// ProcessError reports a command that ran and returned a nonzero status.
type ProcessError struct {
	Label  string
	Status int
	Stderr string
}

// Error returns the command label and exit status.
func (e *ProcessError) Error() string {
	return fmt.Sprintf("%s: exit status %d", e.Label, e.Status)
}

// Detail returns the command's standard error as quoted diagnostic detail.
func (e *ProcessError) Detail() string { return seam.QuoteOutput(e.Stderr) }

// ExitCode returns the ordinary command failure status.
func (e *ProcessError) ExitCode() int { return 1 }
