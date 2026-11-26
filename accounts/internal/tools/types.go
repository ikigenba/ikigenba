package tools

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Config connects the tools to the store, cloud, operations engine and trail.
type Config struct {
	Store       *store.Store
	Cloud       cloud.Cloud
	Hosts       settings.Hosts
	Configured  bool
	Wake        func()
	Telemetry   *telemetry.Writer
	AccountsURL func() (string, error)
}

// DNSCheckTimeout bounds one account's DNS reservation check.
const DNSCheckTimeout = 5 * time.Second

// FirstLine returns the first line of a tool description.
func FirstLine(description string) string {
	line, _, _ := strings.Cut(description, "\n")
	return line
}

// Tool names in catalog order.
const (
	ToolHosts      = "hosts"
	ToolHost       = "host"
	ToolCreateHost = "create_host"
	ToolStopHost   = "stop_host"
	ToolStartHost  = "start_host"
	ToolResizeHost = "resize_host"
	ToolRetryHost  = "retry_host"
	ToolDeleteHost = "delete_host"

	ToolKeptVolumes      = "kept_volumes"
	ToolDeleteKeptVolume = "delete_kept_volume"

	ToolAccounts      = "accounts"
	ToolAccount       = "account"
	ToolCreateAccount = "create_account"
	ToolRemoveAccount = "remove_account"
	ToolUsers         = "users"
	ToolAddUser       = "add_user"
	ToolAddKey        = "add_key"
	ToolRemoveKey     = "remove_key"
	ToolLockUser      = "lock_user"
	ToolUnlockUser    = "unlock_user"
	ToolDeleteUser    = "delete_user"
	ToolSetAdmin      = "set_admin"
	ToolCreateInvite  = "create_invite"
	ToolListInvites   = "list_invites"
	ToolRevokeInvite  = "revoke_invite"
	ToolUnbanIP       = "unban_ip"
	ToolAllowIP       = "allow_ip"
	ToolDisallowIP    = "disallow_ip"
)

// Tool and argument descriptions.
const (
	DescribeHosts      string = "List live SSH hosts in creation order."
	DescribeHost       string = "Read a host, its accounts, current operation and latest report."
	DescribeCreateHost string = "Provision a new SSH host."
	DescribeStopHost   string = "Stop a running host."
	DescribeStartHost  string = "Start a stopped host."
	DescribeResizeHost string = "Change a host instance type or grow its volumes."
	DescribeRetryHost  string = "Resume a failed host operation."
	DescribeDeleteHost string = "Delete a host, keeping its data volume unless keep_data is false."

	DescribeKeptVolumes      string = "List data volumes kept from deleted hosts, most recently deleted first."
	DescribeDeleteKeptVolume string = "Permanently delete a kept data volume.\nThis cannot be undone."

	DescribeAccounts      string = "List SSH accounts by slug."
	DescribeAccount       string = "Read an SSH account, its users and its current operation."
	DescribeCreateAccount string = "Place an SSH account on a host."
	DescribeRemoveAccount string = "Remove an account that has no users."
	DescribeUsers         string = "List SSH users across accounts or in one account."
	DescribeAddUser       string = "Add a user to one account with one public key."
	DescribeAddKey        string = "Add a public key to a user."
	DescribeRemoveKey     string = "Remove a public key from a user."
	DescribeLockUser      string = "Lock a user and keep their access settings."
	DescribeUnlockUser    string = "Restore access to a locked user."
	DescribeDeleteUser    string = "Delete a locked user and their home."
	DescribeSetAdmin      string = "Set or clear an SSH user’s account admin flag."
	DescribeCreateInvite  string = "Make a single-use invite link for an SSH account."
	DescribeListInvites   string = "List unexpired, unused invites for an SSH account."
	DescribeRevokeInvite  string = "Revoke an unexpired, unused SSH account invite."
	DescribeUnbanIP       string = "Lift an SSH ban on one running host or all running hosts."
	DescribeAllowIP       string = "Allow an address or network on one SSH host and lift any bans it covers."
	DescribeDisallowIP    string = "Remove an address or network from one SSH host's allowlist."
	ArgHost               string = "SSH host name."
	ArgIP                 string = "IP address or, for allowlist changes, a CIDR prefix."
	ArgAccount            string = "SSH account slug."
	ArgUser               string = "SSH user name."
	ArgKey                string = "OpenSSH public key, including its comment."
	ArgAdmin              string = "Whether the user has account admin rights."
	ArgInvite             string = "SSH account invite ID."
	ArgType               string = "EC2 instance type."
	ArgRootGB             string = "Root volume size in GB."
	ArgDataGB             string = "Data volume size in GB."
	ArgKeepData           string = "Keep the data volume when deleting the host. Defaults to true."
	ArgVolume             string = "Kept data volume ID."
)

// Refusal text returned to tool callers.
const (
	NotConfigured   string = "this space is not configured to operate SSH hosts"
	InvalidName     string = "the name is invalid"
	ReservedName    string = "the name is reserved"
	NameTaken       string = "an account or user already holds that name"
	NoHost          string = "no live host has that name"
	NoAccount       string = "no account has that slug"
	NoUser          string = "no user has that name"
	NotInAccount    string = "that user belongs to a different account"
	InvalidKey      string = "the public key is invalid"
	KeyNotHeld      string = "the user does not hold that public key"
	TypeNotAllowed  string = "that instance type is not allowed"
	SizeOutOfRange  string = "a volume size is outside the allowed range"
	SizeShrinks     string = "volumes can only grow"
	NothingToChange string = "there is no change to make"
	HostBusy        string = "a host operation is already running"
	HostFailed      string = "the host has failed"
	HostNotFailed   string = "the host has not failed"
	HostNotRunning  string = "the host is not running"
	HostNotStopped  string = "the host is not stopped"
	HostHasAccounts string = "the host still holds accounts that are not being removed"
	HostFull        string = "the host already holds its maximum number of accounts"
	HostGone        string = "the host is being deleted"
	AccountHasUsers string = "the account still holds users"
	AccountRemoving string = "the account is being removed"
	AccountFull     string = "the host already holds its maximum number of users"
	TooManyKeys     string = "the user already holds the maximum number of keys"
	GrowLimit       string = "a volume has reached its growth limit for the current window"
	UserNotLocked   string = "the user must be locked before deletion"
	UserRemoving    string = "the user is being removed"
	NameHasRecord   string = "a DNS record already holds that name"
	CloudFailed     string = "the DNS check failed"
	InvalidAddress  string = "the IP address or allowlist entry is invalid"
	PrefixTooWide   string = "the allowlist prefix must be at least /24 for IPv4 or /48 for IPv6"
	TooManyUnbans   string = "a host already holds its maximum number of pending unbans"
	TooManyInvites  string = "the account already holds its maximum number of unexpired invites"
	NoInvite        string = "no unexpired, unused invite has that id"
	NoLink          string = "this space has no accounts URL for invite links"
	AllowlistFull   string = "the host already holds its maximum number of allowlist entries"
	NoKeptVolume    string = "no kept data volume has that id"
	VolumeDeleting  string = "the kept data volume is already being deleted"
)

// HostsArgs selects a window of live hosts.
type HostsArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// HostArgs names a host.
type HostArgs struct {
	Host string `json:"host" description:"SSH host name." mcp:"required"`
}

// DeleteHostArgs names a host and selects whether to keep its data volume.
type DeleteHostArgs struct {
	Host     string `json:"host" description:"SSH host name." mcp:"required"`
	KeepData *bool  `json:"keep_data" description:"Keep the data volume when deleting the host. Defaults to true."`
}

// KeptVolumesArgs selects a window of kept data volumes.
type KeptVolumesArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// KeptVolumeArgs identifies a kept data volume.
type KeptVolumeArgs struct {
	Volume string `json:"volume" description:"Kept data volume ID." mcp:"required"`
}

// CreateHostArgs specifies the resources for a new host.
type CreateHostArgs struct {
	Type   *string `json:"type" description:"EC2 instance type."`
	RootGB *int    `json:"root_gb" description:"Root volume size in GB."`
	DataGB *int    `json:"data_gb" description:"Data volume size in GB."`
}

// ResizeHostArgs names a host and the resources to change.
type ResizeHostArgs struct {
	Host   string  `json:"host" description:"SSH host name." mcp:"required"`
	Type   *string `json:"type" description:"EC2 instance type."`
	RootGB *int    `json:"root_gb" description:"Root volume size in GB."`
	DataGB *int    `json:"data_gb" description:"Data volume size in GB."`
}

// AccountsArgs selects a window of accounts.
type AccountsArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// AccountArgs names an account by slug.
type AccountArgs struct {
	Account string `json:"account" description:"SSH account slug." mcp:"required"`
}

// CreateAccountArgs names an account and the host that will hold it.
type CreateAccountArgs struct {
	Account string `json:"account" description:"SSH account slug." mcp:"required"`
	Host    string `json:"host" description:"SSH host name." mcp:"required"`
}

// UsersArgs selects a window of users, optionally in one account.
type UsersArgs struct {
	Account *string `json:"account" description:"SSH account slug."`
	Page    *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit   *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// UserArgs identifies an account and one of its users.
type UserArgs struct {
	Account string `json:"account" description:"SSH account slug." mcp:"required"`
	User    string `json:"user" description:"SSH user name." mcp:"required"`
}

// UserKeyArgs identifies an account, user and public key.
type UserKeyArgs struct {
	Account string `json:"account" description:"SSH account slug." mcp:"required"`
	User    string `json:"user" description:"SSH user name." mcp:"required"`
	Key     string `json:"key" description:"OpenSSH public key, including its comment." mcp:"required"`
}

// SetAdminArgs sets the admin flag of a named account user.
type SetAdminArgs struct {
	Account string `json:"account" description:"SSH account slug." mcp:"required"`
	User    string `json:"user" description:"SSH user name." mcp:"required"`
	Admin   bool   `json:"admin" description:"Whether the user has account admin rights." mcp:"required"`
}

// CreateInviteArgs creates an invite for one account.
type CreateInviteArgs struct {
	Account string `json:"account" description:"SSH account slug." mcp:"required"`
	Admin   *bool  `json:"admin" description:"Whether the user has account admin rights."`
}

// InviteArgs identifies an invite.
type InviteArgs struct {
	Invite string `json:"invite" description:"SSH account invite ID." mcp:"required"`
}

// UnbanArgs selects an address and, optionally, one running host.
type UnbanArgs struct {
	IP   string  `json:"ip" description:"IP address or, for allowlist changes, a CIDR prefix." mcp:"required"`
	Host *string `json:"host" description:"SSH host name."`
}

// HostIPArgs identifies a host and an allowlist entry.
type HostIPArgs struct {
	Host string `json:"host" description:"SSH host name." mcp:"required"`
	IP   string `json:"ip" description:"IP address or, for allowlist changes, a CIDR prefix." mcp:"required"`
}

// Host summarizes a live host.
type Host struct {
	Host     string `json:"host" mcp:"required"`
	DNSName  string `json:"dns_name" mcp:"required"`
	State    string `json:"state" mcp:"required"`
	Reason   string `json:"reason" mcp:"required"`
	OnDelete string `json:"on_delete" mcp:"required"`
	Health   string `json:"health" mcp:"required"`
	Type     string `json:"type" mcp:"required"`
	RootGB   int    `json:"root_gb" mcp:"required"`
	DataGB   int    `json:"data_gb" mcp:"required"`
	EIP      string `json:"eip" mcp:"required"`
	Accounts int    `json:"accounts" mcp:"required"`
	Reported string `json:"reported" mcp:"required"`
}

// HostList holds one window of hosts.
type HostList struct {
	Hosts []Host  `json:"hosts" mcp:"required"`
	Next  *string `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// HostRead includes a host's record, accounts, operation and latest report.
type HostRead struct {
	Host         string          `json:"host" mcp:"required"`
	DNSName      string          `json:"dns_name" mcp:"required"`
	State        string          `json:"state" mcp:"required"`
	Reason       string          `json:"reason" mcp:"required"`
	OnDelete     string          `json:"on_delete" mcp:"required"`
	Health       string          `json:"health" mcp:"required"`
	Type         string          `json:"type" mcp:"required"`
	RootGB       int             `json:"root_gb" mcp:"required"`
	DataGB       int             `json:"data_gb" mcp:"required"`
	EIP          string          `json:"eip" mcp:"required"`
	Accounts     int             `json:"accounts" mcp:"required"`
	Reported     string          `json:"reported" mcp:"required"`
	InstanceID   string          `json:"instance_id" mcp:"required"`
	DataVolume   string          `json:"data_volume" mcp:"required"`
	Version      int64           `json:"version" mcp:"required"`
	Applied      int64           `json:"applied" mcp:"required"`
	Created      string          `json:"created" mcp:"required"`
	CreatedBy    string          `json:"created_by" mcp:"required"`
	AccountNames []string        `json:"account_names" mcp:"required"`
	Allowlist    []string        `json:"allowlist" mcp:"required"`
	Operation    *Operation      `json:"operation"`
	Report       json.RawMessage `json:"report"`
}

// Unbanned describes the pending unban recorded for the selected hosts.
type Unbanned struct {
	IP    string `json:"ip" mcp:"required"`
	Host  string `json:"host" mcp:"required"`
	Hosts int    `json:"hosts" mcp:"required"`
}

// AllowlistChange shows the host's allowlist after a tool call.
type AllowlistChange struct {
	Host      string   `json:"host" mcp:"required"`
	IP        string   `json:"ip" mcp:"required"`
	Allowlist []string `json:"allowlist" mcp:"required"`
	Pending   bool     `json:"pending" mcp:"required"`
}

// Operation summarizes a recorded operation.
type Operation struct {
	Operation string `json:"operation"`
	Kind      string `json:"kind" mcp:"required"`
	Step      string `json:"step" mcp:"required"`
	Status    string `json:"status" mcp:"required"`
	Error     string `json:"error" mcp:"required"`
	Started   string `json:"started" mcp:"required"`
}

// HostChange identifies the operation started for a host.
type HostChange struct {
	Host      string `json:"host" mcp:"required"`
	State     string `json:"state" mcp:"required"`
	OnDelete  string `json:"on_delete" mcp:"required"`
	Operation string `json:"operation"`
}

// HostResize identifies a resize operation and whether it restarts the host.
type HostResize struct {
	Host      string `json:"host" mcp:"required"`
	State     string `json:"state" mcp:"required"`
	Operation string `json:"operation"`
	Restarts  bool   `json:"restarts" mcp:"required"`
}

// KeptVolume summarizes a data volume from a deleted host.
type KeptVolume struct {
	Volume  string `json:"volume" mcp:"required"`
	Host    string `json:"host" mcp:"required"`
	SizeGB  int    `json:"size_gb" mcp:"required"`
	Deleted string `json:"deleted" mcp:"required"`
	State   string `json:"state" mcp:"required"`
	Reason  string `json:"reason" mcp:"required"`
}

// KeptVolumeList holds one window of kept data volumes.
type KeptVolumeList struct {
	KeptVolumes []KeptVolume `json:"kept_volumes" mcp:"required"`
	Next        *string      `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// Account summarizes an account and its placement.
type Account struct {
	Account   string `json:"account" mcp:"required"`
	Host      string `json:"host" mcp:"required"`
	DNSName   string `json:"dns_name" mcp:"required"`
	State     string `json:"state" mcp:"required"`
	Reason    string `json:"reason" mcp:"required"`
	Users     int    `json:"users" mcp:"required"`
	Invites   int    `json:"invites" mcp:"required"`
	Created   string `json:"created" mcp:"required"`
	CreatedBy string `json:"created_by" mcp:"required"`
}

// AccountList holds one window of accounts.
type AccountList struct {
	Accounts []Account `json:"accounts" mcp:"required"`
	Next     *string   `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// AccountRead includes the account's users and current operation.
type AccountRead struct {
	Account   string     `json:"account" mcp:"required"`
	Host      string     `json:"host" mcp:"required"`
	DNSName   string     `json:"dns_name" mcp:"required"`
	State     string     `json:"state" mcp:"required"`
	Reason    string     `json:"reason" mcp:"required"`
	Users     int        `json:"users" mcp:"required"`
	Invites   int        `json:"invites" mcp:"required"`
	Created   string     `json:"created" mcp:"required"`
	CreatedBy string     `json:"created_by" mcp:"required"`
	UserNames []string   `json:"user_names" mcp:"required"`
	Operation *Operation `json:"operation"`
}

// User summarizes a user and their SSH access.
type User struct {
	User       string   `json:"user" mcp:"required"`
	Account    string   `json:"account" mcp:"required"`
	Admin      bool     `json:"admin" mcp:"required"`
	State      string   `json:"state" mcp:"required"`
	Pending    bool     `json:"pending" mcp:"required"`
	Keys       []string `json:"keys" mcp:"required"`
	LastSignIn string   `json:"last_sign_in" mcp:"required"`
	Created    string   `json:"created" mcp:"required"`
	CreatedBy  string   `json:"created_by" mcp:"required"`
}

// UserList holds one window of users.
type UserList struct {
	Users []User  `json:"users" mcp:"required"`
	Next  *string `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// UserChange describes the user's recorded state after a change.
type UserChange struct {
	User    string `json:"user" mcp:"required"`
	Account string `json:"account" mcp:"required"`
	Admin   bool   `json:"admin" mcp:"required"`
	State   string `json:"state" mcp:"required"`
	Pending bool   `json:"pending" mcp:"required"`
}

// KeyRemoved reports the user's state and remaining key count.
type KeyRemoved struct {
	User     string `json:"user" mcp:"required"`
	Account  string `json:"account" mcp:"required"`
	Admin    bool   `json:"admin" mcp:"required"`
	State    string `json:"state" mcp:"required"`
	Pending  bool   `json:"pending" mcp:"required"`
	KeysLeft int    `json:"keys_left" mcp:"required"`
}

// Invite describes an unexpired invite without its token.
type Invite struct {
	Invite    string `json:"invite" mcp:"required"`
	Account   string `json:"account" mcp:"required"`
	Admin     bool   `json:"admin" mcp:"required"`
	Created   string `json:"created" mcp:"required"`
	Expires   string `json:"expires" mcp:"required"`
	CreatedBy string `json:"created_by" mcp:"required"`
}

// InviteMade includes the link returned when an invite is created.
type InviteMade struct {
	Invite    string `json:"invite" mcp:"required"`
	Account   string `json:"account" mcp:"required"`
	Admin     bool   `json:"admin" mcp:"required"`
	Created   string `json:"created" mcp:"required"`
	Expires   string `json:"expires" mcp:"required"`
	CreatedBy string `json:"created_by" mcp:"required"`
	Link      string `json:"link" mcp:"required"`
}

// InviteList contains one account's unexpired, unused invites.
type InviteList struct {
	Invites []Invite `json:"invites" mcp:"required"`
}
