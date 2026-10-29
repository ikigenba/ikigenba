// Package pages draws the service's read-only browser pages.
package pages

import (
	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
)

// ServiceName is the service's routing name.
const ServiceName = "accounts"

// Description is the service's one-line description.
const Description string = "Manage business groups and the people who sign in over SSH."

// MinuteLayout formats visible times in UTC.
const MinuteLayout = "2006-01-02 15:04"

// ToolsLevel locates the tools page.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel locates the about screen.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// Config supplies the page dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
	Host         host.Host
	MCP          *mcp.Server
}

// LandingData describes the host's two lists and their independent windows.
type LandingData struct {
	Banner                             page.Banner
	Groups                             []GroupRow
	GroupsPage, GroupsPrev, GroupsNext string
	Users                              []UserRow
	UsersPage, UsersPrev, UsersNext    string
}

// GroupRow describes one host group and its recorded creation.
type GroupRow struct {
	Slug                 string
	MemberCount          int
	Members              []string
	Created, CreatedText string
}

// UserRow describes one host user.
type UserRow struct {
	Login, State               string
	Groups                     []string
	KeyCount                   int
	LastSignIn, LastSignInText string
}

// ToolsData describes the registered tools.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow describes one registered tool.
type ToolRow struct{ Name, Description string }

// AboutData describes the service.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NotFoundData supplies the unknown-page footer.
type NotFoundData struct{ Banner page.Banner }
