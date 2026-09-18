package secrets

import "fmt"

// Entry describes the secret names stored for an app.
type Entry struct {
	App  string
	Keys []string
}

// UsageError reports invalid secrets command syntax.
type UsageError struct {
	Message string
	Help    string
}

// Error returns the syntax error message.
func (e *UsageError) Error() string { return e.Message }

// Detail returns the command-specific usage hint.
func (e *UsageError) Detail() string { return fmt.Sprintf("see '%s' for usage", e.Help) }

// ExitCode returns the command-line usage error status.
func (e *UsageError) ExitCode() int { return 2 }

// ObjectError reports a parameter whose value is not a secrets object.
type ObjectError struct {
	Parameter string
	Reason    string
}

// Error returns the parameter name followed by the reason it is invalid.
func (e ObjectError) Error() string {
	return fmt.Sprintf("%s: %s", e.Parameter, e.Reason)
}
