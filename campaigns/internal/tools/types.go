// Package tools exposes the campaigns MCP catalog.
package tools

// Entry names a catalog tool and its description.
type Entry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CampaignsArgs supplies arguments for the corresponding MCP tool.
type CampaignsArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// CampaignArgs supplies arguments for the corresponding MCP tool.
type CampaignArgs struct {
	Campaign string  `json:"campaign" mcp:"required"`
	Page     *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit    *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// MergeArgs supplies arguments for the corresponding MCP tool.
type MergeArgs struct {
	Campaign string  `json:"campaign" mcp:"required"`
	Channel  string  `json:"channel" mcp:"required"`
	All      bool    `json:"all"`
	Page     *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit    *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// EngagementArgs supplies arguments for the corresponding MCP tool.
type EngagementArgs struct {
	Piece     string  `json:"piece"`
	Campaign  string  `json:"campaign"`
	Candidate string  `json:"candidate"`
	Page      *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit     *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// CreateArgs supplies arguments for the corresponding MCP tool.
type CreateArgs struct {
	Name        string   `json:"name" mcp:"required"`
	Description string   `json:"description"`
	Channels    []string `json:"channels" mcp:"required"`
	Destination string   `json:"destination" mcp:"required"`
}

// EditArgs supplies arguments for the corresponding MCP tool.
type EditArgs struct {
	Campaign    string  `json:"campaign" mcp:"required"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Destination *string `json:"destination"`
}

// DeleteArgs supplies arguments for the corresponding MCP tool.
type DeleteArgs struct {
	Campaign string `json:"campaign" mcp:"required"`
}

// ProspectListArgs supplies arguments for the corresponding MCP tool.
type ProspectListArgs struct {
	Campaign     string `json:"campaign" mcp:"required"`
	ProspectList string `json:"prospect_list" mcp:"required"`
}

// MemberArgs supplies arguments for the corresponding MCP tool.
type MemberArgs struct {
	Campaign  string `json:"campaign" mcp:"required"`
	Candidate string `json:"candidate" mcp:"required"`
}

// MembersArgs selects businesses to add in caller order.
type MembersArgs struct {
	Campaign   string   `json:"campaign" mcp:"required"`
	Candidates []string `json:"candidates" mcp:"required"`
}

// RejectArgs supplies arguments for the corresponding MCP tool.
type RejectArgs struct {
	Campaign  string `json:"campaign" mcp:"required"`
	Candidate string `json:"candidate" mcp:"required"`
	Why       string `json:"why" mcp:"required"`
}

// PieceArgs supplies arguments for the corresponding MCP tool.
type PieceArgs struct {
	Piece string `json:"piece" mcp:"required"`
}

// ChannelArgs supplies arguments for the corresponding MCP tool.
type ChannelArgs struct {
	Campaign string `json:"campaign" mcp:"required"`
	Channel  string `json:"channel" mcp:"required"`
}

// MaterialArgs supplies arguments for the corresponding MCP tool.
type MaterialArgs struct {
	Campaign string `json:"campaign" mcp:"required"`
	Path     string `json:"path" mcp:"required"`
	Label    string `json:"label" mcp:"required"`
}

// RemoveArgs supplies arguments for the corresponding MCP tool.
type RemoveArgs struct {
	Campaign string `json:"campaign" mcp:"required"`
	Path     string `json:"path" mcp:"required"`
}

// Campaign is the complete campaign definition.
type Campaign struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Channels     []string `json:"channels"`
	Destination  string   `json:"destination"`
	Created      string   `json:"created"`
	CreatedBy    string   `json:"created_by"`
	CreatedEmail string   `json:"created_email"`
	Deleted      bool     `json:"deleted"`
}

// Counts summarizes campaign membership and engagement.
type Counts struct {
	Members   int `json:"members"`
	Delivered int `json:"delivered"`
	Opened    int `json:"opened"`
}

// CampaignSummary is one campaign list entry.
type CampaignSummary struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Channels  []string `json:"channels"`
	Created   string   `json:"created"`
	Members   int      `json:"members"`
	Delivered int      `json:"delivered"`
	Opened    int      `json:"opened"`
}

// Piece is a piece record with engagement totals.
type Piece struct {
	ID             string `json:"id"`
	Campaign       string `json:"campaign"`
	Candidate      string `json:"candidate"`
	Channel        string `json:"channel"`
	State          string `json:"state"`
	Created        string `json:"created"`
	Delivered      string `json:"delivered"`
	DeliveredBy    string `json:"delivered_by"`
	DeliveredEmail string `json:"delivered_email"`
	How            string `json:"how"`
	Dropped        string `json:"dropped"`
	DroppedBy      string `json:"dropped_by"`
	DroppedEmail   string `json:"dropped_email"`
	Opens          int    `json:"opens"`
	First          string `json:"first"`
	Last           string `json:"last"`
}

// Member is a member with its pieces and engagement.
type Member struct {
	Campaign      string  `json:"campaign"`
	Candidate     string  `json:"candidate"`
	ProspectList  string  `json:"prospect_list"`
	Added         string  `json:"added"`
	AddedBy       string  `json:"added_by"`
	AddedEmail    string  `json:"added_email"`
	Rejected      bool    `json:"rejected"`
	RejectedBy    string  `json:"rejected_by"`
	RejectedEmail string  `json:"rejected_email"`
	RejectedAt    string  `json:"rejected_at"`
	Why           string  `json:"why"`
	Opened        bool    `json:"opened"`
	Opens         int     `json:"opens"`
	First         string  `json:"first"`
	Last          string  `json:"last"`
	Pieces        []Piece `json:"pieces"`
}

// Material is a labelled artifact reference.
type Material struct {
	Path       string `json:"path"`
	Label      string `json:"label"`
	Artifact   string `json:"artifact"`
	Added      string `json:"added"`
	AddedBy    string `json:"added_by"`
	AddedEmail string `json:"added_email"`
}

// CampaignList is a window of live campaigns.
type CampaignList struct {
	Campaigns []CampaignSummary `json:"campaigns"`
	Next      *string           `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// CampaignRead combines a campaign with material and one member window.
type CampaignRead struct {
	Campaign Campaign   `json:"campaign"`
	Counts   Counts     `json:"counts"`
	Material []Material `json:"material"`
	Members  []Member   `json:"members"`
	Next     *string    `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// CampaignChange reports a campaign mutation.
type CampaignChange struct {
	Campaign Campaign `json:"campaign"`
	Changed  bool     `json:"changed"`
}

// Skip explains a channel for which no piece was made.
type Skip struct {
	Channel string `json:"channel"`
	Reason  string `json:"reason"`
}

// AddedMember reports a member addition and skipped channels.
type AddedMember struct {
	Member  Member `json:"member"`
	Added   bool   `json:"added"`
	Skipped []Skip `json:"skipped"`
}

// MemberResult reports the outcome for one distinct candidate.
type MemberResult struct {
	Candidate string `json:"candidate"`
	Result    string `json:"result"`
	Skipped   []Skip `json:"skipped"`
}

// MembersAdd reports outcomes in the caller's distinct candidate order.
type MembersAdd struct {
	Results []MemberResult `json:"results"`
}

// ProspectListAdd reports additions and exclusions from a prospect list.
type ProspectListAdd struct {
	Added    []AddedMember `json:"added"`
	Present  int           `json:"present"`
	Excluded int           `json:"excluded"`
}

// MemberChange reports a member mutation.
type MemberChange struct {
	Member  Member `json:"member"`
	Changed bool   `json:"changed"`
}

// PieceRecord is a piece mutation result without engagement.
type PieceRecord struct {
	ID             string `json:"id"`
	Campaign       string `json:"campaign"`
	Candidate      string `json:"candidate"`
	Channel        string `json:"channel"`
	State          string `json:"state"`
	Created        string `json:"created"`
	Delivered      string `json:"delivered"`
	DeliveredBy    string `json:"delivered_by"`
	DeliveredEmail string `json:"delivered_email"`
	How            string `json:"how"`
	Dropped        string `json:"dropped"`
	DroppedBy      string `json:"dropped_by"`
	DroppedEmail   string `json:"dropped_email"`
}

// PieceChange reports a piece mutation.
type PieceChange struct {
	Piece   PieceRecord `json:"piece"`
	Changed bool        `json:"changed"`
}

// Delivered holds the pieces delivered in one batch.
type Delivered struct {
	Pieces []PieceRecord `json:"pieces"`
}

// MaterialChange reports a material mutation.
type MaterialChange struct {
	Material Material `json:"material"`
	Changed  bool     `json:"changed"`
}

// Removed reports removal of one material path.
type Removed struct {
	Path    string `json:"path"`
	Removed bool   `json:"removed"`
}

// Person is a business contact.
type Person struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// Business is the current business returned by candidates.
type Business struct {
	Name   string   `json:"name"`
	Names  []string `json:"names"`
	Street string   `json:"street"`
	City   string   `json:"city"`
	State  string   `json:"state"`
	Zip    string   `json:"zip"`
	County string   `json:"county"`
	Phone  string   `json:"phone"`
	Email  string   `json:"email"`
	People []Person `json:"people"`
}

// MergeRow is one piece with its current business and tracking link.
type MergeRow struct {
	Piece     string   `json:"piece"`
	Candidate string   `json:"candidate"`
	Channel   string   `json:"channel"`
	State     string   `json:"state"`
	Rejected  bool     `json:"rejected"`
	Link      string   `json:"link"`
	Business  Business `json:"business"`
}

// MergeList is a window of merge rows.
type MergeList struct {
	Rows []MergeRow `json:"rows"`
	Next *string    `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// Visit is one recorded visit.
type Visit struct {
	At        string `json:"at"`
	UserAgent string `json:"user_agent"`
	Referrer  string `json:"referrer"`
	Address   string `json:"address"`
}

// Engagement reports opens for a piece or member.
type Engagement struct {
	Campaign  string   `json:"campaign"`
	Candidate string   `json:"candidate"`
	Piece     string   `json:"piece"`
	Opened    bool     `json:"opened"`
	Opens     int      `json:"opens"`
	First     string   `json:"first"`
	Last      string   `json:"last"`
	Visits    *[]Visit `json:"visits"`
	Next      *string  `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}
