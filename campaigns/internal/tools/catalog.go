package tools

import (
	"strings"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
)

// Config supplies the tool dependencies.
type Config struct {
	Store      *store.Store
	Telemetry  *telemetry.Writer
	Events     *events.Emitter
	Candidates *candidates.Client
	Artifacts  *artifacts.Client
	LinkPrefix string
}

// Refusal categories are stable machine words.
const (
	RefusalNotFound    = "not_found"
	RefusalInvalid     = "invalid"
	RefusalUnavailable = "unavailable"
)

// Refusal sentences never contain supplied values.
const (
	RefuseNotFound       string = "The campaign, member or piece was not found."
	RefuseNoCandidate    string = "The candidate was not found."
	RefuseNoProspectList string = "The prospect list was not found."
	RefuseNoArtifact     string = "The artifact was not found."
	RefuseInvalid        string = "A supplied value is invalid."
	RefuseState          string = "The record does not permit this change."
	RefuseCandidates     string = "Candidates is unavailable."
	RefuseArtifacts      string = "Artifacts is unavailable."
)

// Skip reasons explain why a channel has no piece.
const (
	SkipNoAddress = "no_address"
	SkipNoEmail   = "no_email"
)

// Tool descriptions are centralized copy.
const (
	DescribeCampaigns       string = "Lists campaigns."
	DescribeCampaign        string = "Reads a campaign."
	DescribeCreateCampaign  string = "Creates a campaign."
	DescribeEditCampaign    string = "Edits a campaign."
	DescribeDeleteCampaign  string = "Deletes a campaign."
	DescribeAddProspectList string = "Adds a prospect list to a campaign."
	DescribeAddMember       string = "Adds one member to a campaign."
	DescribeReject          string = "Rejects a member."
	DescribeDrop            string = "Drops a planned piece."
	DescribeDeliver         string = "Marks a piece delivered by hand."
	DescribeDeliverChannel  string = "Marks a channel delivered by hand."
	DescribeAddMaterial     string = "Adds a material reference."
	DescribeRemoveMaterial  string = "Removes a material reference."
	DescribeMergeList       string = "Reads a channel merge list."
	DescribeEngagement      string = "Reads recorded engagement."
)

// Catalog returns an independent ordered catalog.
func Catalog() []Entry {
	return []Entry{
		{"campaigns", DescribeCampaigns},
		{"campaign", DescribeCampaign},
		{"create_campaign", DescribeCreateCampaign},
		{"edit_campaign", DescribeEditCampaign},
		{"delete_campaign", DescribeDeleteCampaign},
		{"add_prospect_list", DescribeAddProspectList},
		{"add_member", DescribeAddMember},
		{"reject", DescribeReject},
		{"drop", DescribeDrop},
		{"deliver", DescribeDeliver},
		{"deliver_channel", DescribeDeliverChannel},
		{"add_material", DescribeAddMaterial},
		{"remove_material", DescribeRemoveMaterial},
		{"merge_list", DescribeMergeList},
		{"engagement", DescribeEngagement},
	}
}

// FirstLine returns the summary of a description.
func FirstLine(description string) string { line, _, _ := strings.Cut(description, "\n"); return line }

// Register installs the tools in catalog order.
func Register(srv *mcp.Server, cfg Config) {
	h := handlers{cfg}
	mcp.AddTool(srv, mcp.Tool[CampaignsArgs, CampaignList]{Name: "campaigns", Description: DescribeCampaigns, Effect: mcp.Read, Handler: h.campaigns})
	mcp.AddTool(srv, mcp.Tool[CampaignArgs, CampaignRead]{Name: "campaign", Description: DescribeCampaign, Effect: mcp.Read, Handler: h.campaign})
	mcp.AddTool(srv, mcp.Tool[CreateArgs, Campaign]{Name: "create_campaign", Description: DescribeCreateCampaign, Effect: mcp.Additive, Handler: h.createCampaign})
	mcp.AddTool(srv, mcp.Tool[EditArgs, CampaignChange]{Name: "edit_campaign", Description: DescribeEditCampaign, Effect: mcp.Additive, Handler: h.editCampaign})
	mcp.AddTool(srv, mcp.Tool[DeleteArgs, CampaignChange]{Name: "delete_campaign", Description: DescribeDeleteCampaign, Effect: mcp.Destructive, Handler: h.deleteCampaign})
	mcp.AddTool(srv, mcp.Tool[ProspectListArgs, ProspectListAdd]{Name: "add_prospect_list", Description: DescribeAddProspectList, Effect: mcp.Additive, Handler: h.addProspectList})
	mcp.AddTool(srv, mcp.Tool[MemberArgs, AddedMember]{Name: "add_member", Description: DescribeAddMember, Effect: mcp.Additive, Handler: h.addMember})
	mcp.AddTool(srv, mcp.Tool[RejectArgs, MemberChange]{Name: "reject", Description: DescribeReject, Effect: mcp.Additive, Handler: h.reject})
	mcp.AddTool(srv, mcp.Tool[PieceArgs, PieceChange]{Name: "drop", Description: DescribeDrop, Effect: mcp.Additive, Handler: h.drop})
	mcp.AddTool(srv, mcp.Tool[PieceArgs, PieceChange]{Name: "deliver", Description: DescribeDeliver, Effect: mcp.Additive, Handler: h.deliver})
	mcp.AddTool(srv, mcp.Tool[ChannelArgs, Delivered]{Name: "deliver_channel", Description: DescribeDeliverChannel, Effect: mcp.Additive, Handler: h.deliverChannel})
	mcp.AddTool(srv, mcp.Tool[MaterialArgs, MaterialChange]{Name: "add_material", Description: DescribeAddMaterial, Effect: mcp.Additive, Handler: h.addMaterial})
	mcp.AddTool(srv, mcp.Tool[RemoveArgs, Removed]{Name: "remove_material", Description: DescribeRemoveMaterial, Effect: mcp.Destructive, Handler: h.removeMaterial})
	mcp.AddTool(srv, mcp.Tool[MergeArgs, MergeList]{Name: "merge_list", Description: DescribeMergeList, Effect: mcp.Read, Handler: h.mergeList})
	mcp.AddTool(srv, mcp.Tool[EngagementArgs, Engagement]{Name: "engagement", Description: DescribeEngagement, Effect: mcp.Read, Handler: h.engagement})
}

type handlers struct{ Config }
