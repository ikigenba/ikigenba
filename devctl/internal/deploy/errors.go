package deploy

import (
	"fmt"
	"strings"
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
