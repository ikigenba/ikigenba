// Package pages renders sessions' read-only browser pages.
package pages

import (
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
)

// ServiceName identifies the service.
const ServiceName = "sessions"

// Description describes sessions on the space and on its about screen.
const Description = "The business's time in agent sessions, by kind of work."

// ToolsLevel and AboutLevel identify the pages below the landing page.
var (
	ToolsLevel = page.Level{Name: "tools", URL: "/tools"}
	AboutLevel = page.Level{Name: "about", URL: "/about"}
)

// Config supplies the page handler's collaborators.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
	MCP          *mcp.Server
}

// LandingData supplies the landing template.
type LandingData struct {
	Banner    page.Banner
	AwayLimit Duration
	Weeks     []Week
}

// Week supplies one week's figures.
type Week struct {
	Start         string
	Kinds         []FiguresRow
	Total         FiguresRow
	UntaggedShare int
}

// FiguresRow supplies one kind or the total row.
type FiguresRow struct {
	Kind                   string
	Attention, Wall, Agent Duration
	Tokens, Dollars        string
}

// Duration supplies whole seconds and the hours and minutes displayed.
type Duration struct{ Seconds, Hours, Minutes int }

// ToolsData supplies the tools template.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow supplies one registered tool.
type ToolRow struct{ Name, Description string }

// AboutData supplies the about template.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NotFoundData supplies the notfound template.
type NotFoundData struct{ Banner page.Banner }
