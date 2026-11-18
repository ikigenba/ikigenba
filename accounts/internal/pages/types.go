// Package pages draws the service's read-only browser pages.
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

// LandingData describes the independent windows of the three lists.
type LandingData struct {
	Banner                                   page.Banner
	Configured                               bool
	Domain                                   string
	Hosts                                    []HostRow
	HostsPage, HostsPrev, HostsNext          string
	Accounts                                 []AccountRow
	AccountsPage, AccountsPrev, AccountsNext string
	Users                                    []UserRow
	UsersPage, UsersPrev, UsersNext          string
}

// HostRow describes a host and its last contact on the landing page.
type HostRow struct {
	Name, State, Reason, Health, Type string
	RootGB, DataGB                    int
	EIP                               string
	AccountCount                      int
	Reported, ReportedText            string
}

// AccountRow describes an account on the landing page.
type AccountRow struct {
	Slug, Host, DNSName, State, Reason string
	UserCount                          int
	Created, CreatedText               string
}

// UserRow describes a user on the landing page.
type UserRow struct {
	Login, Account, State      string
	Pending                    bool
	KeyCount                   int
	LastSignIn, LastSignInText string
}

// HostData describes the host detail page.
type HostData struct {
	Banner page.Banner
	Host   HostView
	Report *ReportView
}

// HostView describes the host and its resources.
type HostView struct {
	Name, State, Reason, Health, Type                                    string
	RootGB, DataGB                                                       int
	EIP                                                                  string
	AccountCount                                                         int
	Reported, ReportedText, InstanceID, DataVolume, Created, CreatedText string
}

// ReportView describes the latest daemon report.
type ReportView struct {
	Received, ReceivedText, InstanceID, Type, AMI, OS, Kernel, DaemonVersion, Booted, BootedText string
	Applied, Wanted                                                                              int64
	Error                                                                                        string
	Load1, Load5, Load15, CPUPercent                                                             float64
	MemoryUsed, MemoryAvailable, SwapUsed, SwapTotal                                             int64
	Root, Data                                                                                   VolumeView
	Sessions, Updates                                                                            int
	Reboot                                                                                       bool
	FailedUnits                                                                                  []string
	Slices                                                                                       []SliceRow
}

// VolumeView describes space and inode use on a volume.
type VolumeView struct{ UsedBytes, SizeBytes, InodesUsed, InodesTotal int64 }

// SliceRow describes one account's resource use.
type SliceRow struct {
	Account     string
	CPUPercent  float64
	MemoryBytes int64
}

// ToolsData describes the registered tools.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow describes one registered tool.
type ToolRow struct{ Name, Description string }

// AboutData describes the service and host defaults.
type AboutData struct {
	Banner                       page.Banner
	Description, DefaultType     string
	DefaultRootGB, DefaultDataGB int
}

// NotFoundData supplies the unknown-page footer.
type NotFoundData struct{ Banner page.Banner }
