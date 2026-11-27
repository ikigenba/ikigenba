// Package pages draws the service's browser pages.
package pages

import (
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
)

// ServiceName is the service's routing name.
const ServiceName = "accounts"

// Description is the service's one-line description.
const Description string = "Manage SSH hosts, business accounts and their users."

// MinuteLayout formats landing page times in UTC.
const MinuteLayout = "2006-01-02 15:04"

// SecondLayout formats report and host creation times in UTC.
const SecondLayout = "2006-01-02 15:04:05 UTC"

// MaxRestoreRows is the maximum number of restores on a host page.
const MaxRestoreRows = 25

// MaxDatabaseRows is the maximum number of databases shown per account.
const MaxDatabaseRows = 50

// ToolsLevel locates the tools page.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel locates the about screen.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// HostLevel locates a host's detail page.
func HostLevel(name string) page.Level { return page.Level{Name: name, URL: "/hosts/" + name} }

// Config supplies the page dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
	MCP          *mcp.Server
	Configured   bool
	Domain       string
	Now          func() time.Time
}

// LandingData describes the independent windows of the four lists.
type LandingData struct {
	Banner                                   page.Banner
	Configured                               bool
	Domain                                   string
	Hosts                                    []HostRow
	HostsPage, HostsPrev, HostsNext          string
	KeptVolumes                              []KeptVolumeRow
	VolumesPage, VolumesPrev, VolumesNext    string
	Accounts                                 []AccountRow
	AccountsPage, AccountsPrev, AccountsNext string
	Users                                    []UserRow
	UsersPage, UsersPrev, UsersNext          string
}

// HostRow describes a host and its last contact on the landing page.
type HostRow struct {
	Name, DNSName, State, Reason, OnDelete, Health, Type string
	RootGB, DataGB                                       int
	EIP                                                  string
	AccountCount                                         int
	Reported, ReportedText, Closed, ClosedText           string
	BackupFlagged                                        int
}

// KeptVolumeRow describes a data volume retained after a deletion or replacement.
type KeptVolumeRow struct {
	Volume, Host, Cause  string
	SizeGB               int
	Deleted, DeletedText string
}

// AccountRow describes an account on the landing page.
type AccountRow struct {
	Slug, Host, DNSName, State, Reason string
	UserCount, InviteCount             int
	Backup, Snapshot, SnapshotText     string
	Created, CreatedText               string
}

// UserRow describes a user on the landing page.
type UserRow struct {
	Login, Account             string
	Admin                      bool
	State                      string
	Pending                    bool
	KeyCount                   int
	LastSignIn, LastSignInText string
}

// HostData describes the host detail page.
type HostData struct {
	Banner        page.Banner
	Host          HostView
	Report        *ReportView
	Backups       []BackupRow
	DatabaseCount int
	Restores      []RestoreRow
	RestoreTotal  int
}

// HostView describes the host and its resources.
type HostView struct {
	Name, DNSName, State, Reason, OnDelete, Health, Type  string
	RootGB, DataGB                                        int
	EIP                                                   string
	AccountCount                                          int
	Reported, ReportedText, Closed, ClosedText            string
	BackupFlagged                                         int
	Replace, InstanceID, DataVolume, Created, CreatedText string
	Allowlist                                             []string
}

// BackupRow describes the backup status of an account placed on the host.
type BackupRow struct {
	Account, Health                                                                           string
	FilesStale                                                                                bool
	Snapshot, SnapshotText, Checked, CheckedText, Error, Final, FinalAt, FinalAtText, Restore string
	Databases                                                                                 []DatabaseRow
	DatabaseTotal                                                                             int
}

// DatabaseRow describes a database from the host's latest report.
type DatabaseRow struct {
	Path, Health, Written, WrittenText, Error string
}

// RestoreRow describes a restore requested on the host.
type RestoreRow struct {
	Account, Kind, Path, AsOf, AsOfText, Copy, State, Reason, Asked, AskedText string
}

// ReportView describes the latest daemon report.
type ReportView struct {
	Received, ReceivedText, Hostname, InstanceID, Type, AMI, OS, Kernel, DaemonVersion, Booted, BootedText string
	Applied, Wanted                                                                                        int64
	Error                                                                                                  string
	Load1, Load5, Load15, CPUPercent                                                                       float64
	MemoryUsed, MemoryAvailable, SwapUsed, SwapTotal                                                       int64
	Root, Data                                                                                             VolumeView
	Sessions, Updates                                                                                      int
	Reboot                                                                                                 bool
	FailedUnits                                                                                            []string
	Slices                                                                                                 []SliceRow
	Bans                                                                                                   []BanRow
	BanTotal                                                                                               int
	Allowlist                                                                                              []string
}

// VolumeView describes space and inode use on a volume.
type VolumeView struct{ UsedBytes, SizeBytes, InodesUsed, InodesTotal int64 }

// SliceRow describes one account's resource use.
type SliceRow struct {
	Account     string
	CPUPercent  float64
	MemoryBytes int64
}

// BanRow describes a reported SSH ban and its times.
type BanRow struct{ Address, Banned, BannedText, Expires, ExpiresText string }

// ToolsData describes the registered tools.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow describes one registered tool.
type ToolRow struct{ Name, Description string }

// AboutData describes the service and host defaults.
type AboutData struct {
	Banner                                              page.Banner
	Description, DefaultType                            string
	DefaultRootGB, DefaultDataGB                        int
	BackupEveryMinutes                                  int
	BackupHourly, BackupDaily, BackupWeekly             int
	BackupWindowDays                                    int
	BackupFilesStaleMinutes, BackupDatabaseStaleMinutes int
}

// NotFoundData supplies the unknown-page footer.
type NotFoundData struct{ Banner page.Banner }

// JoinData describes the enrollment form or the outcome of a submission.
type JoinData struct {
	Outcome, Account, DNSName string
	Admin                     bool
	User, Key                 string
}

// Enrollment outcomes consumed by the join template.
const (
	JoinForm     = ""
	JoinUnusable = "unusable"
	JoinName     = "name"
	JoinKey      = "key"
	JoinLimited  = "limited"
	JoinFull     = "full"
	JoinDone     = "done"
)
