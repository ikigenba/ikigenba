// Package pages draws the service's read-only browser pages.
package pages

import (
	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
)

// ServiceName is the service's routing name.
const ServiceName = "campaigns"

// Description is the service's one-line description.
const Description string = "Keep outreach campaigns, prepare their pieces and follow engagement."

// MinuteLayout and SecondLayout render UTC record times.
const MinuteLayout = "2006-01-02 15:04"

// SecondLayout renders UTC times to the second.
const SecondLayout = "2006-01-02 15:04:05 UTC"

// ToolsLevel and AboutLevel locate the two service reference pages.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel locates the service description.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// Config supplies the page dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
	Candidates   *candidates.Client
	MCP          *mcp.Server
}

// LandingData describes one window of campaigns.
type LandingData struct {
	Banner           page.Banner
	Campaigns        []CampaignRow
	Page, Prev, Next string
}

// CampaignRow describes a campaign in the landing window.
type CampaignRow struct {
	ID, Name                   string
	Channels                   []string
	Created, CreatedText       string
	Members, Delivered, Opened int
}

// CampaignData describes a campaign with one window of members.
type CampaignData struct {
	Banner           page.Banner
	Campaign         CampaignView
	Material         []MaterialRow
	Members          []MemberRow
	Page, Prev, Next string
}

// CampaignView describes a campaign's definition and totals.
type CampaignView struct {
	ID, Name, Description                                     string
	Channels                                                  []string
	Destination, CreatedBy, CreatedUser, Created, CreatedText string
	Members, Delivered, Opened                                int
}

// MaterialRow describes an artifact reference.
type MaterialRow struct{ Label, Path, ArtifactPage string }

// MemberRow describes the public view of a member.
type MemberRow struct {
	Candidate, CandidatePage, Name, ProspectList, ProspectListPage, Added, AddedText string
	Opened, Rejected                                                                 bool
	RejectedBy, RejectedUser, RejectedAt, RejectedAtText, Why                        string
	Pieces                                                                           []PieceRow
}

// PieceRow describes a channel's delivery and engagement.
type PieceRow struct {
	Channel, State, Delivered, DeliveredText, DeliveredBy, DeliveredUser, How string
	Opens                                                                     int
	First, FirstText, Last, LastText                                          string
}

// ToolsData describes the registered tool list.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow describes a registered tool.
type ToolRow struct{ Name, Description string }

// AboutData describes the service.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NotFoundData describes an unknown page.
type NotFoundData struct{ Banner page.Banner }
