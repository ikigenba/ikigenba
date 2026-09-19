package deploy

import (
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// UsageError reports invalid deploy command syntax.
type UsageError struct {
	Message string
	Help    string
}

// Error returns the syntax error message.
func (e *UsageError) Error() string { return e.Message }

// Detail returns the command-specific usage hint.
func (e *UsageError) Detail() string {
	if e.Help == "" {
		return ""
	}
	return fmt.Sprintf("see '%s' for usage", e.Help)
}

// ExitCode returns the command-line usage status.
func (e *UsageError) ExitCode() int { return 2 }

// NoFileError reports a path that does not name a regular file.
type NoFileError struct {
	Path string
}

// Error returns the missing-file diagnostic.
func (e *NoFileError) Error() string { return fmt.Sprintf("no such file '%s'", e.Path) }

// ExitCode returns the command-line usage status.
func (e *NoFileError) ExitCode() int { return 2 }

// MissingSecretsError reports manifest secrets absent from the target space.
type MissingSecretsError struct {
	App   string
	Space string
	Names []string
}

// Error returns the missing-secret names.
func (e *MissingSecretsError) Error() string {
	return fmt.Sprintf("%s: secrets missing %s", e.App, strings.Join(e.Names, ","))
}

// Detail returns the command that can push the missing secrets.
func (e *MissingSecretsError) Detail() string {
	return fmt.Sprintf("run 'devctl secrets push %s %s'", e.Space, e.App)
}

// ExitCode returns the command-line usage status.
func (e *MissingSecretsError) ExitCode() int { return 2 }

// ProcessError reports a command that exited unsuccessfully.
type ProcessError struct {
	Label  string
	Status int
	Stderr string
}

// Error returns the command label and exit status.
func (e *ProcessError) Error() string {
	return fmt.Sprintf("%s: exit status %d", e.Label, e.Status)
}

// Detail returns the command's standard error as quoted output.
func (e *ProcessError) Detail() string { return seam.QuoteOutput(e.Stderr) }

// ExitCode returns the ordinary failure status.
func (e *ProcessError) ExitCode() int { return 1 }

// FileError reports why an artifact is not a deployable build output.
type FileError struct {
	Path   string
	Reason string
}

// Error returns the artifact validation diagnostic.
func (e *FileError) Error() string {
	return fmt.Sprintf("'%s' is not a file build wrote: %s", e.Path, e.Reason)
}

// ExitCode returns the command-line usage status.
func (e *FileError) ExitCode() int { return 2 }
