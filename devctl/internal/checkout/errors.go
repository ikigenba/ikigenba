package checkout

import (
	"fmt"
	"strings"
)

// NotInCheckoutError reports that checkout discovery failed for a directory.
type NotInCheckoutError struct {
	Dir string
}

func (err *NotInCheckoutError) Error() string {
	return fmt.Sprintf("'%s' is not inside a git checkout", err.Dir)
}

// NoAppError reports that a checkout does not contain a named application.
type NoAppError struct {
	Name string
}

func (err *NoAppError) Error() string {
	return fmt.Sprintf("no app '%s' in the checkout", err.Name)
}

// NoRootFileError reports that a checkout has no platform root file.
type NoRootFileError struct {
	Checkout string
}

func (err *NoRootFileError) Error() string {
	return "no " + RootFilePath + " in the checkout"
}

// RootFileError reports malformed platform root-file contents.
type RootFileError struct {
	Detail string
}

func (err *RootFileError) Error() string {
	return RootFilePath + ": " + err.Detail
}

// ManifestError reports an application manifest failure.
type ManifestError struct {
	App    string
	Detail string
	Err    error
}

func (err *ManifestError) Error() string {
	return fmt.Sprintf("%s: %s: %s", err.App, ManifestFile, err.Detail)
}

// Unwrap returns the underlying manifest failure.
func (err *ManifestError) Unwrap() error {
	return err.Err
}

// GitError reports a git process that completed unsuccessfully.
type GitError struct {
	Args     []string
	ExitCode int
	Stderr   string
}

func (err *GitError) Error() string {
	return fmt.Sprintf("git %s: exit status %d", strings.Join(err.Args, " "), err.ExitCode)
}
