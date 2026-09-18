package spacecreate

// RefusedError reports a create request that is valid syntax but cannot proceed.
type RefusedError struct {
	Message string
}

func (e *RefusedError) Error() string {
	return e.Message
}

// ExitCode reports the command-line refusal status.
func (e *RefusedError) ExitCode() int {
	return 2
}
