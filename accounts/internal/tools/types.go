// Package tools exposes accounts through typed MCP tools.
package tools

import (
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Config carries the record, host and event writer.
type Config struct {
	Store     *store.Store
	Host      host.Host
	Telemetry *telemetry.Writer
}

// Tool names in catalog order.
const (
	ToolGroups       = "groups"
	ToolGroup        = "group"
	ToolCreateGroup  = "create_group"
	ToolRemoveGroup  = "remove_group"
	ToolUsers        = "users"
	ToolAddUser      = "add_user"
	ToolAddKey       = "add_key"
	ToolRemoveKey    = "remove_key"
	ToolAddMember    = "add_member"
	ToolRemoveMember = "remove_member"
	ToolLockUser     = "lock_user"
	ToolUnlockUser   = "unlock_user"
	ToolDeleteUser   = "delete_user"
)

// Descriptions are the tool catalog copy.
const (
	DescribeGroups       string = "List the host groups."
	DescribeGroup        string = "Read a group and its members."
	DescribeCreateGroup  string = "Create a business group."
	DescribeRemoveGroup  string = "Remove an empty business group."
	DescribeUsers        string = "List the host users with their access."
	DescribeAddUser      string = "Add a user with a group and key."
	DescribeAddKey       string = "Add a public key to a user."
	DescribeRemoveKey    string = "Remove a public key from a user."
	DescribeAddMember    string = "Add a user to a group."
	DescribeRemoveMember string = "Remove a user from a group."
	DescribeLockUser     string = "Lock a user and keep their access settings."
	DescribeUnlockUser   string = "Restore access to a locked user."
	DescribeDeleteUser   string = "Delete a locked user and their home."
	ArgGroup             string = "Business group slug."
	ArgUser              string = "SSH user name."
	ArgKey               string = "OpenSSH public key, including its comment."
	Failed               string = "the host change or read failed"
	KeysUnread           string = "the key was removed but the remaining keys could not be read"
)

// FirstLine returns the description's first line.
func FirstLine(description string) string { line, _, _ := strings.Cut(description, "\n"); return line }

// GroupsArgs declares the tool input.
type GroupsArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// UsersArgs declares the tool input.
type UsersArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// GroupArgs declares the tool input.
type GroupArgs struct {
	Group string `json:"group" description:"Business group slug." mcp:"required"`
}

// UserArgs declares the tool input.
type UserArgs struct {
	User string `json:"user" description:"SSH user name." mcp:"required"`
}

// AddUserArgs declares the tool input.
type AddUserArgs struct {
	User  string `json:"user" description:"SSH user name." mcp:"required"`
	Group string `json:"group" description:"Business group slug." mcp:"required"`
	Key   string `json:"key" description:"OpenSSH public key, including its comment." mcp:"required"`
}

// KeyArgs declares the tool input.
type KeyArgs struct {
	User string `json:"user" description:"SSH user name." mcp:"required"`
	Key  string `json:"key" description:"OpenSSH public key, including its comment." mcp:"required"`
}

// MemberArgs declares the tool input.
type MemberArgs struct {
	User  string `json:"user" description:"SSH user name." mcp:"required"`
	Group string `json:"group" description:"Business group slug." mcp:"required"`
}

// Group is a structured tool answer.
type Group struct {
	Slug        string `json:"slug" mcp:"required"`
	MemberCount int    `json:"member_count" mcp:"required"`
	Created     string `json:"created" mcp:"required"`
	CreatedBy   string `json:"created_by" mcp:"required"`
}

// GroupList is a structured tool answer.
type GroupList struct {
	Groups []Group `json:"groups" mcp:"required"`
	Next   *string `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// GroupRead is a structured tool answer.
type GroupRead struct {
	Slug      string   `json:"slug" mcp:"required"`
	Members   []string `json:"members" mcp:"required"`
	Created   string   `json:"created" mcp:"required"`
	CreatedBy string   `json:"created_by" mcp:"required"`
}

// User is a structured tool answer.
type User struct {
	Name       string   `json:"name" mcp:"required"`
	Groups     []string `json:"groups" mcp:"required"`
	Keys       []string `json:"keys" mcp:"required"`
	State      string   `json:"state" mcp:"required"`
	LastSignIn string   `json:"last_sign_in" mcp:"required"`
	Created    string   `json:"created" mcp:"required"`
	CreatedBy  string   `json:"created_by" mcp:"required"`
}

// UserList is a structured tool answer.
type UserList struct {
	Users []User  `json:"users" mcp:"required"`
	Next  *string `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// GroupDone is a structured tool answer.
type GroupDone struct {
	Group string `json:"group" mcp:"required"`
}

// UserDone is a structured tool answer.
type UserDone struct {
	User string `json:"user" mcp:"required"`
}

// MemberDone is a structured tool answer.
type MemberDone struct {
	User  string `json:"user" mcp:"required"`
	Group string `json:"group" mcp:"required"`
}

// KeyRemoved is a structured tool answer.
type KeyRemoved struct {
	User      string `json:"user" mcp:"required"`
	NoKeyLeft bool   `json:"no_key_left" mcp:"required"`
}
