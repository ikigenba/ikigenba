// Package tools exposes sessions through MCP.
package tools

import (
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
)

// Config provides the record and event writer used by the catalog.
type Config struct {
	Store     *store.Store
	Telemetry *telemetry.Writer
}

// Tool names, tool descriptions and argument descriptions form the catalog copy.
const (
	ToolPushTurns                 = "push_turns"
	DescribePushTurns      string = "Keep content-free session turns."
	ToolGetLatestTurns            = "get_latest_turns"
	DescribeGetLatestTurns string = "Read the latest held turn of each session."
	ToolListSessions              = "list_sessions"
	DescribeListSessions   string = "List root sessions and their figures."
	ToolTotalSessions             = "total_sessions"
	DescribeTotalSessions  string = "Total session figures by calendar period."
	ToolListRules                 = "list_rules"
	DescribeListRules      string = "List work classification rules."
	ToolAddRule                   = "add_rule"
	DescribeAddRule        string = "Add a work classification rule."
	ToolRemoveRule                = "remove_rule"
	DescribeRemoveRule     string = "Remove a work classification rule."
	ToolListPrices                = "list_prices"
	DescribeListPrices     string = "List model token prices."
	ToolSetPrice                  = "set_price"
	DescribeSetPrice       string = "Keep a model token price."
	ToolRemovePrice               = "remove_price"
	DescribeRemovePrice    string = "Remove a model token price."
	ToolGetAwayLimit              = "get_away_limit"
	DescribeGetAwayLimit   string = "Read the away limit."
	ToolSetAwayLimit              = "set_away_limit"
	DescribeSetAwayLimit   string = "Set the away limit."
	ArgTool                string = "Agent tool owning the session."
	ArgID                  string = "Session identifier."
	ArgParent              string = "Parent session when this is a subagent."
	ArgHost                string = "Machine that ran the session."
	ArgDirectory           string = "Working directory of the session."
	ArgHeadless            string = "Whether the session ran unattended."
	ArgCommand             string = "First command of the session."
	ArgCommandName         string = "Command name."
	ArgCommandArgument     string = "Command argument."
	ArgTurns               string = "Content-free turns to keep."
	ArgMoment              string = "RFC 3339 turn instant."
	ArgWhose               string = "Party responsible for the turn."
	ArgModel               string = "Model used for the turn."
	ArgInputTokens         string = "Input."
	ArgCachedTokens        string = "Cached."
	ArgOutputTokens        string = "Output."
	ArgReasoningTokens     string = "Reasoning token count."
	ArgPushSessions        string = "Sessions and turns to keep."
	ArgLatestSessions      string = "Sessions whose latest moments to read."
	ArgPeriod              string = "Calendar period to select."
	ArgOf                  string = "Date within the selected period."
	ArgFrom                string = "Date within the first period."
	ArgTo                  string = "Date within the last period."
	ArgGroup               string = "Dimension to group sessions by."
	ArgFilterPerson        string = "Filter by person email."
	ArgFilterKind          string = "Filter by work kind."
	ArgFilterTool          string = "Filter by agent tool."
	ArgFilterHost          string = "Filter by session host."
	ArgFilterCommand       string = "Filter by command name."
	ArgRuleHost            string = "Host prefix of the rule."
	ArgRuleDirectory       string = "Directory prefix of the rule."
	ArgRuleCommand         string = "Command name of the rule."
	ArgRuleKind            string = "Work kind assigned by the rule."
	ArgPriceModel          string = "Model whose price to keep."
	ArgEffective           string = "Effective date of the price."
	ArgInputRate           string = "USD per million input tokens."
	ArgCachedRate          string = "USD per million cached tokens."
	ArgOutputRate          string = "USD per million output tokens."
	ArgReasoningRate       string = "USD per million reasoning tokens."
	ArgMinutes             string = "Away limit in minutes."
)

// SessionRef identifies one session by its owning tool and opaque id.
type SessionRef struct {
	Tool string `json:"tool" description:"Agent tool owning the session." mcp:"required"`
	ID   string `json:"id" description:"Session identifier." mcp:"required"`
}

// Command carries the first command and its argument.
type Command struct {
	Name     string `json:"name" description:"Command name." mcp:"required"`
	Argument string `json:"argument" description:"Command argument." mcp:"required"`
}

// PushTurn is the content-free wire representation of one turn.
type PushTurn struct {
	Moment    string `json:"moment" description:"RFC 3339 turn instant." mcp:"required"`
	Whose     string `json:"whose" description:"Party responsible for the turn." mcp:"required"`
	Model     string `json:"model" description:"Model used for the turn." mcp:"required"`
	Input     int64  `json:"input" description:"Input." mcp:"required"`
	Cached    int64  `json:"cached" description:"Cached." mcp:"required"`
	Output    int64  `json:"output" description:"Output." mcp:"required"`
	Reasoning int64  `json:"reasoning" description:"Reasoning token count." mcp:"required"`
}

// PushSession carries session metadata and the turns to keep.
type PushSession struct {
	Tool      string      `json:"tool" description:"Agent tool owning the session." mcp:"required"`
	ID        string      `json:"id" description:"Session identifier." mcp:"required"`
	Parent    *SessionRef `json:"parent" description:"Parent session when this is a subagent."`
	Host      string      `json:"host" description:"Machine that ran the session." mcp:"required"`
	Directory string      `json:"directory" description:"Working directory of the session." mcp:"required"`
	Headless  bool        `json:"headless" description:"Whether the session ran unattended." mcp:"required"`
	Command   *Command    `json:"command" description:"First command of the session."`
	Turns     []PushTurn  `json:"turns" description:"Content-free turns to keep." mcp:"required"`
}

// PushTurnsArgs names the batch of sessions to push.
type PushTurnsArgs struct {
	Sessions []PushSession `json:"sessions" description:"Sessions and turns to keep." mcp:"required"`
}

// LatestTurnsArgs names the sessions whose latest turn to read.
type LatestTurnsArgs struct {
	Sessions []SessionRef `json:"sessions" description:"Sessions whose latest moments to read." mcp:"required"`
}

// ListSessionsArgs filters and pages root sessions.
type ListSessionsArgs struct {
	Period  *string `json:"period" description:"Calendar period to select."`
	Of      *string `json:"of" description:"Date within the selected period."`
	Person  *string `json:"person" description:"Filter by person email."`
	Kind    *string `json:"kind" description:"Filter by work kind."`
	Tool    *string `json:"tool" description:"Filter by agent tool."`
	Host    *string `json:"host" description:"Filter by session host."`
	Command *string `json:"command" description:"Filter by command name."`
	Page    *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit   *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// TotalSessionsArgs selects periods, filters and a grouping for totals.
type TotalSessionsArgs struct {
	Period  string  `json:"period" description:"Calendar period to select." mcp:"required"`
	From    string  `json:"from" description:"Date within the first period." mcp:"required"`
	To      *string `json:"to" description:"Date within the last period."`
	Group   string  `json:"group" description:"Dimension to group sessions by." mcp:"required"`
	Person  *string `json:"person" description:"Filter by person email."`
	Kind    *string `json:"kind" description:"Filter by work kind."`
	Tool    *string `json:"tool" description:"Filter by agent tool."`
	Host    *string `json:"host" description:"Filter by session host."`
	Command *string `json:"command" description:"Filter by command name."`
}

// NoArgs is the input to configuration reads.
type NoArgs struct {
}

// AddRuleArgs names a rule key and the kind to assign.
type AddRuleArgs struct {
	Host      string  `json:"host" description:"Host prefix of the rule." mcp:"required"`
	Directory string  `json:"directory" description:"Directory prefix of the rule." mcp:"required"`
	Command   *string `json:"command" description:"Command name of the rule."`
	Kind      string  `json:"kind" description:"Work kind assigned by the rule." mcp:"required"`
}

// RemoveRuleArgs names the rule to remove.
type RemoveRuleArgs struct {
	Host      string  `json:"host" description:"Host prefix of the rule." mcp:"required"`
	Directory string  `json:"directory" description:"Directory prefix of the rule." mcp:"required"`
	Command   *string `json:"command" description:"Command name of the rule."`
}

// SetPriceArgs carries the effective model rates to keep.
type SetPriceArgs struct {
	Model     string `json:"model" description:"Model whose price to keep." mcp:"required"`
	Effective string `json:"effective" description:"Effective date of the price." mcp:"required"`
	Input     string `json:"input" description:"USD per million input tokens." mcp:"required"`
	Cached    string `json:"cached" description:"USD per million cached tokens." mcp:"required"`
	Output    string `json:"output" description:"USD per million output tokens." mcp:"required"`
	Reasoning string `json:"reasoning" description:"USD per million reasoning tokens." mcp:"required"`
}

// RemovePriceArgs names the model price to remove.
type RemovePriceArgs struct {
	Model     string `json:"model" description:"Model whose price to keep." mcp:"required"`
	Effective string `json:"effective" description:"Effective date of the price." mcp:"required"`
}

// SetAwayLimitArgs carries the away limit in minutes.
type SetAwayLimitArgs struct {
	Minutes int `json:"minutes" description:"Away limit in minutes." mcp:"required"`
}

// Latest answers the latest held turn of one session.
type Latest struct {
	Tool   string  `json:"tool" description:"Agent tool owning the session." mcp:"required"`
	ID     string  `json:"id" description:"Session identifier." mcp:"required"`
	Latest *string `json:"latest"`
}

// LatestList answers one latest entry per requested session.
type LatestList struct {
	Sessions []Latest `json:"sessions" mcp:"required"`
}

// Tokens answers each token count and their total.
type Tokens struct {
	Input     int64 `json:"input" mcp:"required"`
	Cached    int64 `json:"cached" mcp:"required"`
	Output    int64 `json:"output" mcp:"required"`
	Reasoning int64 `json:"reasoning" mcp:"required"`
	Total     int64 `json:"total" mcp:"required"`
}

// Session answers a root session and its current figures.
type Session struct {
	Tool             string   `json:"tool" mcp:"required"`
	ID               string   `json:"id" mcp:"required"`
	Host             string   `json:"host" mcp:"required"`
	Person           string   `json:"person" mcp:"required"`
	Directory        string   `json:"directory" mcp:"required"`
	Headless         bool     `json:"headless" mcp:"required"`
	Command          *Command `json:"command"`
	First            string   `json:"first" mcp:"required"`
	Last             string   `json:"last" mcp:"required"`
	Kind             string   `json:"kind" mcp:"required"`
	AttentionSeconds int64    `json:"attention_seconds" mcp:"required"`
	WallSeconds      int64    `json:"wall_seconds" mcp:"required"`
	AgentSeconds     int64    `json:"agent_seconds" mcp:"required"`
	Tokens           Tokens   `json:"tokens" mcp:"required"`
	Unpriced         int64    `json:"unpriced" mcp:"required"`
	Dollars          string   `json:"dollars" mcp:"required"`
	Subagents        int      `json:"subagents" mcp:"required"`
}

// SessionList answers a page of root sessions and its continuation.
type SessionList struct {
	Sessions []Session `json:"sessions" mcp:"required"`
	Next     *string   `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// GroupTotals answers one group of root session figures.
type GroupTotals struct {
	Group            string `json:"group" mcp:"required"`
	AttentionSeconds int64  `json:"attention_seconds" mcp:"required"`
	WallSeconds      int64  `json:"wall_seconds" mcp:"required"`
	AgentSeconds     int64  `json:"agent_seconds" mcp:"required"`
	Tokens           Tokens `json:"tokens" mcp:"required"`
	Unpriced         int64  `json:"unpriced" mcp:"required"`
	Dollars          string `json:"dollars" mcp:"required"`
	Sessions         int    `json:"sessions" mcp:"required"`
}

// PeriodTotals answers the groups of one calendar period.
type PeriodTotals struct {
	Start  string        `json:"start" mcp:"required"`
	Groups []GroupTotals `json:"groups" mcp:"required"`
}

// Totals answers calendar periods in ascending order.
type Totals struct {
	Periods []PeriodTotals `json:"periods" mcp:"required"`
}

// Rule answers a classification rule as held.
type Rule struct {
	Host      string `json:"host" mcp:"required"`
	Directory string `json:"directory" mcp:"required"`
	Command   string `json:"command" mcp:"required"`
	Kind      string `json:"kind" mcp:"required"`
}

// RuleList answers all classification rules.
type RuleList struct {
	Rules []Rule `json:"rules" mcp:"required"`
}

// Price answers one effective model price with canonical rates.
type Price struct {
	Model     string `json:"model" mcp:"required"`
	Effective string `json:"effective" mcp:"required"`
	Input     string `json:"input" mcp:"required"`
	Cached    string `json:"cached" mcp:"required"`
	Output    string `json:"output" mcp:"required"`
	Reasoning string `json:"reasoning" mcp:"required"`
}

// PriceList answers all effective model prices.
type PriceList struct {
	Prices []Price `json:"prices" mcp:"required"`
}

// PriceRef identifies a removed model price.
type PriceRef struct {
	Model     string `json:"model" mcp:"required"`
	Effective string `json:"effective" mcp:"required"`
}

// AwayLimit answers the away limit in minutes.
type AwayLimit struct {
	Minutes int `json:"minutes" mcp:"required"`
}
