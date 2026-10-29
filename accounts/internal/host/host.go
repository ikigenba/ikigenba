// Package host provides command-backed and in-memory SSH account hosts.
package host

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Host is the account management interface supplied by opsctl.
type Host interface {
	GroupAdd(context.Context, string) error
	GroupRemove(context.Context, string) error
	GroupList(context.Context) ([]Group, error)
	GroupMembers(context.Context, string) (Members, error)
	UserAdd(context.Context, string, string, string) error
	UserList(context.Context) ([]User, error)
	UserKeyAdd(context.Context, string, string) error
	UserKeyRemove(context.Context, string, string) error
	UserJoin(context.Context, string, string) error
	UserLeave(context.Context, string, string) error
	UserLock(context.Context, string) error
	UserUnlock(context.Context, string) error
	UserDelete(context.Context, string) error
}

// Group describes a host group and its member count.
type Group struct {
	Slug    string
	Members int
}

// Members describes the users belonging to a host group.
type Members struct {
	Group   string
	Members []string
}

// User describes a host user.
type User struct {
	Name         string
	Groups, Keys []string
	State        string
	LastSignIn   time.Time
}

// Host protocol constants.
const (
	StateActive      = "active"
	StateLocked      = "locked"
	DefaultCommand   = "sudo -n /usr/local/bin/opsctl"
	DiagnosticPrefix = "opsctl: "
)

// Refusal diagnostics for the in-memory host.
const (
	RefuseInvalidName   string = "invalid account name"
	RefuseInvalidKey    string = "invalid ed25519 public key"
	RefuseNoGroup       string = "no such group"
	RefuseNoUser        string = "no such user"
	RefuseNameTaken     string = "name already taken"
	RefuseGroupNotEmpty string = "group has members"
	RefuseUserNotLocked string = "user is not locked"
	HostFailed          string = "host command failed"
)

// RefusedError carries a host refusal without changing its diagnostic.
type RefusedError struct {
	Status     int
	Diagnostic string
}

func (e *RefusedError) Error() string { return e.Diagnostic }

// FailedError carries the exit status of a failed host command.
type FailedError struct{ Status int }

func (e *FailedError) Error() string { return HostFailed + " (" + strconv.Itoa(e.Status) + ")" }

// Fields splits the configured command without shell quoting.
func Fields(value string) []string {
	f := strings.Fields(value)
	if len(f) == 0 {
		return strings.Fields(DefaultCommand)
	}
	return f
}
