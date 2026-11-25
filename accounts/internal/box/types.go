// Package box exposes SSH users' key, account user, and invite tools.
package box

import (
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Actor and tool names.
const (
	ActorPrefix = "ssh:"
	ToolKeys    = "keys"
)

// Box tool and argument descriptions.
const (
	DescribeKeys         string = "List your own SSH user's public keys."
	DescribeAddKey       string = "Add a public key to your own SSH user."
	DescribeRemoveKey    string = "Remove a public key from your own SSH user; your last key stays."
	DescribeUsers        string = "List the users of your account."
	DescribeCreateInvite string = "Make an invite link that adds one ordinary user to your account."
	DescribeListInvites  string = "List your account's unused invites."
	DescribeRevokeInvite string = "Revoke an unused invite of your account."
	DescribeLockUser     string = "Lock a user of your account and end their sessions."
	DescribeUnlockUser   string = "Restore access to a locked user of your account."
	DescribeDeleteUser   string = "Delete a locked user of your account and their home."
	DescribeSetAdmin     string = "Make a user of your account an admin or an ordinary user."
	ArgUser              string = "SSH user name in your account."
)

// Box-specific refusals.
const (
	LastKey   string = "you cannot remove your last key from the host"
	NotSelf   string = "you cannot lock, delete or demote yourself"
	OnlyAdmin string = "only an admin of your account can do that"
)

// Config supplies the record, event writer, and invite link base.
type Config struct {
	Store       *store.Store
	Telemetry   *telemetry.Writer
	AccountsURL func() (string, error)
}

// NoArgs marks a tool with no input members.
type NoArgs struct{}

// KeyArgs identifies a public key belonging to the caller.
type KeyArgs struct {
	Key string `json:"key" description:"OpenSSH public key, including its comment." mcp:"required"`
}

// UserArgs identifies a user in the caller's account.
type UserArgs struct {
	User string `json:"user" description:"SSH user name in your account." mcp:"required"`
}

// SetAdminArgs sets the admin flag of a user in the caller's account.
type SetAdminArgs struct {
	User  string `json:"user" description:"SSH user name in your account." mcp:"required"`
	Admin bool   `json:"admin" description:"Whether the user has account admin rights." mcp:"required"`
}

// KeyList contains the caller's own public keys.
type KeyList struct {
	User string   `json:"user" mcp:"required"`
	Keys []string `json:"keys" mcp:"required"`
}

// User describes one user of the caller's account.
type User struct {
	User       string `json:"user" mcp:"required"`
	Admin      bool   `json:"admin" mcp:"required"`
	State      string `json:"state" mcp:"required"`
	Pending    bool   `json:"pending" mcp:"required"`
	LastSignIn string `json:"last_sign_in" mcp:"required"`
	Created    string `json:"created" mcp:"required"`
	CreatedBy  string `json:"created_by" mcp:"required"`
}

// UserList contains the users of the caller's account.
type UserList struct {
	Users []User `json:"users" mcp:"required"`
}
