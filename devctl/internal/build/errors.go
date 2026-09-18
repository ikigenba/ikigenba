package build

import "fmt"

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
