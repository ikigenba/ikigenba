package spacecreate

import (
	"fmt"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

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

// NoHostBackupError reports the absence of the required host backup.
type NoHostBackupError struct {
	Bucket string
	Prefix string
}

func (e *NoHostBackupError) Error() string {
	return "restore: no host backup under s3://" + e.Bucket + "/" + e.Prefix
}

// ExitCode returns the general operation failure status.
func (e *NoHostBackupError) ExitCode() int { return 1 }

// KnownHostsError reports a failed removal of a previous host key.
type KnownHostsError struct {
	Domain string
	Status int
	Stderr string
}

func (e *KnownHostsError) Error() string {
	return fmt.Sprintf("known_hosts: ssh-keygen -R %s: exit status %d", e.Domain, e.Status)
}

// Detail quotes the failed process diagnostic.
func (e *KnownHostsError) Detail() string { return seam.QuoteOutput(e.Stderr) }

// ExitCode returns the general operation failure status.
func (e *KnownHostsError) ExitCode() int { return 1 }

// NotInReleaseError reports an apex app absent from the release.
type NotInReleaseError struct {
	App     string
	Release string
}

func (e *NotInReleaseError) Error() string {
	return fmt.Sprintf("'%s' is not an app in %s", e.App, e.Release)
}

// ExitCode returns the general operation failure status.
func (e *NotInReleaseError) ExitCode() int { return 1 }
