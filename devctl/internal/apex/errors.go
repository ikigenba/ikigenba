// Package apex manages the root domain's application and space.
package apex

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

// HolderStoppedError reports that the space currently holding the apex is not running.
type HolderStoppedError struct {
	Domain string
	Label  string
	State  cloud.InstanceState
}

func (e *HolderStoppedError) Error() string {
	return fmt.Sprintf("'%s' holds the apex and is %s", e.Domain, e.State)
}

// ExitCode returns the general command failure status.
func (e *HolderStoppedError) ExitCode() int { return 1 }

// Detail tells the operator how to make the holder usable.
func (e *HolderStoppedError) Detail() string {
	return fmt.Sprintf("run 'devctl space start %s' first", e.Label)
}

// NotSpaceAddressError reports an apex record that names no space address.
type NotSpaceAddressError struct {
	Root   string
	Values []string
}

func (e *NotSpaceAddressError) Error() string {
	return fmt.Sprintf("%s points at %s, which is not a space's address", e.Root, strings.Join(e.Values, ", "))
}

// NoApexAppError reports a holder with no application configured for the apex.
type NoApexAppError struct {
	Root   string
	Domain string
}

func (e *NoApexAppError) Error() string {
	return fmt.Sprintf("%s points at %s, whose host.apex is not set", e.Root, e.Domain)
}

// StepError associates a host reachability failure with its command step.
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return e.Step + ": " + e.Err.Error() }

// Unwrap returns the underlying failure.
func (e *StepError) Unwrap() error { return e.Err }

// ExitCode returns the general command failure status.
func (e *StepError) ExitCode() int { return 1 }

// Detail forwards diagnostic detail supplied by the underlying failure.
func (e *StepError) Detail() string {
	var detailed interface{ Detail() string }
	if errors.As(e.Err, &detailed) {
		return detailed.Detail()
	}
	return ""
}
