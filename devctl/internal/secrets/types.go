package secrets

import "fmt"

// Entry describes the secret names stored for an app.
type Entry struct {
	App  string
	Keys []string
}

// ObjectError reports a parameter whose value is not a secrets object.
type ObjectError struct {
	Parameter string
	Reason    string
}

// Error returns the parameter name followed by the reason it is invalid.
func (e ObjectError) Error() string {
	return fmt.Sprintf("%s: %s", e.Parameter, e.Reason)
}
