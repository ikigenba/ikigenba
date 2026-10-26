// Package pages draws the ledger's read-only browser pages.
package pages

import (
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
)

// ServiceName is the service's routing name.
const ServiceName = "ledger"

// Description is the service's one-line description.
const Description = "Keep the business's books: accounts, transactions and statements."

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
	MCP          *mcp.Server
}

// LandingData describes the book as it stands.
type LandingData struct {
	Banner                              page.Banner
	Currency, LockDate, FiscalYearStart string
	OpenAccounts, Transactions          int
	LatestDate                          string
	FailingAssertions                   int
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
