package tools

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/sessions/internal/figures"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/trail"
)

// FirstLine returns the description's first line.
func FirstLine(description string) string { s, _, _ := strings.Cut(description, "\n"); return s }

// Register adds the complete sessions tool catalog in display order.
func Register(srv *mcp.Server, cfg Config) {
	h := handlers{cfg}
	mcp.AddTool(srv, mcp.Tool[PushTurnsArgs, LatestList]{Name: ToolPushTurns, Description: DescribePushTurns, Effect: mcp.Additive, Handler: h.push})
	mcp.AddTool(srv, mcp.Tool[LatestTurnsArgs, LatestList]{Name: ToolGetLatestTurns, Description: DescribeGetLatestTurns, Effect: mcp.Read, Handler: h.latest})
	mcp.AddTool(srv, mcp.Tool[ListSessionsArgs, SessionList]{Name: ToolListSessions, Description: DescribeListSessions, Effect: mcp.Read, Handler: h.sessions})
	mcp.AddTool(srv, mcp.Tool[TotalSessionsArgs, Totals]{Name: ToolTotalSessions, Description: DescribeTotalSessions, Effect: mcp.Read, Handler: h.totals})
	mcp.AddTool(srv, mcp.Tool[NoArgs, RuleList]{Name: ToolListRules, Description: DescribeListRules, Effect: mcp.Read, Handler: h.rules})
	mcp.AddTool(srv, mcp.Tool[AddRuleArgs, Rule]{Name: ToolAddRule, Description: DescribeAddRule, Effect: mcp.Additive, Handler: h.addRule})
	mcp.AddTool(srv, mcp.Tool[RemoveRuleArgs, Rule]{Name: ToolRemoveRule, Description: DescribeRemoveRule, Effect: mcp.Destructive, Handler: h.removeRule})
	mcp.AddTool(srv, mcp.Tool[NoArgs, PriceList]{Name: ToolListPrices, Description: DescribeListPrices, Effect: mcp.Read, Handler: h.prices})
	mcp.AddTool(srv, mcp.Tool[SetPriceArgs, Price]{Name: ToolSetPrice, Description: DescribeSetPrice, Effect: mcp.Additive, Handler: h.setPrice})
	mcp.AddTool(srv, mcp.Tool[RemovePriceArgs, PriceRef]{Name: ToolRemovePrice, Description: DescribeRemovePrice, Effect: mcp.Destructive, Handler: h.removePrice})
	mcp.AddTool(srv, mcp.Tool[NoArgs, AwayLimit]{Name: ToolGetAwayLimit, Description: DescribeGetAwayLimit, Effect: mcp.Read, Handler: h.away})
	mcp.AddTool(srv, mcp.Tool[SetAwayLimitArgs, AwayLimit]{Name: ToolSetAwayLimit, Description: DescribeSetAwayLimit, Effect: mcp.Additive, Handler: h.setAway})
}

type handlers struct{ cfg Config }

func text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type toolError string

func (e toolError) Error() string { return string(e) }

func answerError(err error) error {
	if err == nil {
		return nil
	}
	var r *store.RefusedError
	if errors.As(err, &r) {
		return toolError(r.Reason)
	}
	return toolError(store.Unreachable)
}
func moment(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z07:00") }
func latestAnswer(ls []store.LatestTurn) LatestList {
	out := LatestList{Sessions: make([]Latest, 0, len(ls))}
	for _, l := range ls {
		v := Latest{Tool: l.Tool, ID: l.ID}
		if !l.Latest.IsZero() {
			s := moment(l.Latest)
			v.Latest = &s
		}
		out.Sessions = append(out.Sessions, v)
	}
	return out
}
func (h handlers) push(ctx context.Context, c identity.Caller, a PushTurnsArgs) (LatestList, error) {
	if len(a.Sessions) == 0 {
		trail.PushTurns(ctx, h.cfg.Telemetry, 0, 0, 0, trail.ResultRefused)
		return LatestList{}, toolError(store.NoSessions)
	}
	entries := make([]store.PushSession, 0, len(a.Sessions))
	for _, x := range a.Sessions {
		s := store.PushSession{Tool: x.Tool, ID: x.ID, Host: x.Host, Directory: x.Directory, Headless: x.Headless}
		if x.Parent != nil {
			s.Parent = &store.SessionRef{Tool: x.Parent.Tool, ID: x.Parent.ID}
		}
		if x.Command != nil {
			s.Command = store.Command{Name: x.Command.Name, Argument: x.Command.Argument}
		}
		for _, t := range x.Turns {
			s.Turns = append(s.Turns, store.PushTurn{Moment: t.Moment, Whose: t.Whose, Model: t.Model, Input: t.Input, Cached: t.Cached, Output: t.Output, Reasoning: t.Reasoning})
		}
		entries = append(entries, s)
	}
	p, err := h.cfg.Store.Push(ctx, c.UserID, c.Email, entries)
	if err != nil {
		p = store.Pushed{}
	}
	trail.PushTurns(ctx, h.cfg.Telemetry, len(p.Sessions), p.New, p.Changed, trail.Result(err))
	return latestAnswer(p.Sessions), answerError(err)
}
func (h handlers) latest(ctx context.Context, _ identity.Caller, a LatestTurnsArgs) (LatestList, error) {
	refs := make([]store.SessionRef, 0, len(a.Sessions))
	for _, r := range a.Sessions {
		refs = append(refs, store.SessionRef{Tool: r.Tool, ID: r.ID})
	}
	ls, err := h.cfg.Store.Latest(ctx, refs)
	return latestAnswer(ls), answerError(err)
}
func tokens(k figures.Tokens) Tokens {
	return Tokens{Input: k.Input, Cached: k.Cached, Output: k.Output, Reasoning: k.Reasoning, Total: k.Total()}
}
func row(s store.SessionRow) Session {
	f := s.Summary.Figures
	r := Session{Tool: string(s.Tool), ID: s.ID, Host: s.Host, Person: s.Person, Directory: s.Directory, Headless: s.Headless, First: moment(s.First), Last: moment(s.Last), Kind: string(s.Summary.Kind), AttentionSeconds: figures.Seconds(f.Attention), WallSeconds: figures.Seconds(f.Wall), AgentSeconds: figures.Seconds(f.Agent), Tokens: tokens(f.Tokens), Unpriced: f.Unpriced, Dollars: f.Dollars.Dollars(), Subagents: s.Summary.Subagents}
	if s.Command.Name != "" {
		r.Command = &Command{Name: s.Command.Name, Argument: s.Command.Argument}
	}
	return r
}
func (h handlers) sessions(ctx context.Context, _ identity.Caller, a ListSessionsArgs) (SessionList, error) {
	out := SessionList{Sessions: []Session{}}
	z, err := paging.Window(a.Limit)
	if err != nil {
		return out, err
	}
	token := paging.Token{Tool: ToolListSessions}
	pos := store.Position{}
	if p := text(a.Page); p != "" {
		var f int64
		var t, i string
		if err := token.Decode(p, &f, &t, &i); err != nil {
			return out, errors.New(paging.BadPage)
		}
		pos = store.Position{First: time.UnixMilli(f).UTC(), Tool: t, ID: i}
	}
	f := store.Filter{Period: text(a.Period), Of: text(a.Of), Person: text(a.Person), Kind: text(a.Kind), Tool: text(a.Tool), Host: text(a.Host), Command: text(a.Command)}
	rows, err := h.cfg.Store.Sessions(ctx, f, pos, z+1)
	if err != nil {
		return out, answerError(err)
	}
	if len(rows) > z {
		s := rows[z-1]
		next := token.Encode(s.First.UnixMilli(), string(s.Tool), s.ID)
		out.Next = &next
		rows = rows[:z]
	}
	for _, s := range rows {
		out.Sessions = append(out.Sessions, row(s))
	}
	return out, nil
}
func (h handlers) totals(ctx context.Context, _ identity.Caller, a TotalSessionsArgs) (Totals, error) {
	q := store.TotalsQuery{Period: a.Period, From: a.From, To: text(a.To), Group: a.Group, Filter: store.Filter{Person: text(a.Person), Kind: text(a.Kind), Tool: text(a.Tool), Host: text(a.Host), Command: text(a.Command)}}
	ps, err := h.cfg.Store.Totals(ctx, q)
	out := Totals{Periods: make([]PeriodTotals, 0, len(ps))}
	for _, p := range ps {
		v := PeriodTotals{Start: p.Start.UTC().Format("2006-01-02"), Groups: make([]GroupTotals, 0, len(p.Groups))}
		for _, g := range p.Groups {
			f := g.Figures
			v.Groups = append(v.Groups, GroupTotals{Group: g.Group, AttentionSeconds: figures.Seconds(f.Attention), WallSeconds: figures.Seconds(f.Wall), AgentSeconds: figures.Seconds(f.Agent), Tokens: tokens(f.Tokens), Unpriced: f.Unpriced, Dollars: f.Dollars.Dollars(), Sessions: g.Sessions})
		}
		out.Periods = append(out.Periods, v)
	}
	return out, answerError(err)
}
func rule(r figures.Rule) Rule {
	return Rule{Host: r.Host, Directory: r.Directory, Command: r.Command, Kind: string(r.Kind)}
}
func price(p figures.Price) Price {
	return Price{Model: p.Model, Effective: p.Effective.UTC().Format("2006-01-02"), Input: p.Input.String(), Cached: p.Cached.String(), Output: p.Output.String(), Reasoning: p.Reasoning.String()}
}
func (h handlers) rules(ctx context.Context, _ identity.Caller, _ NoArgs) (RuleList, error) {
	rs, err := h.cfg.Store.Rules(ctx)
	out := RuleList{Rules: make([]Rule, 0, len(rs))}
	for _, r := range rs {
		out.Rules = append(out.Rules, rule(r))
	}
	return out, answerError(err)
}
func (h handlers) addRule(ctx context.Context, _ identity.Caller, a AddRuleArgs) (Rule, error) {
	r, n, err := h.cfg.Store.AddRule(ctx, a.Host, a.Directory, text(a.Command), a.Kind)
	if err != nil {
		n = 0
	}
	trail.AddRule(ctx, h.cfg.Telemetry, a.Host, a.Directory, text(a.Command), a.Kind, n, trail.Result(err))
	return rule(r), answerError(err)
}
func (h handlers) removeRule(ctx context.Context, _ identity.Caller, a RemoveRuleArgs) (Rule, error) {
	r, n, err := h.cfg.Store.RemoveRule(ctx, a.Host, a.Directory, text(a.Command))
	if err != nil {
		n = 0
		r = figures.Rule{}
	}
	trail.RemoveRule(ctx, h.cfg.Telemetry, a.Host, a.Directory, text(a.Command), string(r.Kind), n, trail.Result(err))
	return rule(r), answerError(err)
}
func (h handlers) prices(ctx context.Context, _ identity.Caller, _ NoArgs) (PriceList, error) {
	ps, err := h.cfg.Store.Prices(ctx)
	out := PriceList{Prices: make([]Price, 0, len(ps))}
	for _, p := range ps {
		out.Prices = append(out.Prices, price(p))
	}
	return out, answerError(err)
}
func (h handlers) setPrice(ctx context.Context, _ identity.Caller, a SetPriceArgs) (Price, error) {
	p, n, err := h.cfg.Store.SetPrice(ctx, store.PriceInput{Model: a.Model, Effective: a.Effective, Input: a.Input, Cached: a.Cached, Output: a.Output, Reasoning: a.Reasoning})
	if err != nil {
		n = 0
	}
	trail.SetPrice(ctx, h.cfg.Telemetry, a.Model, a.Effective, n, trail.Result(err))
	return price(p), answerError(err)
}
func (h handlers) removePrice(ctx context.Context, _ identity.Caller, a RemovePriceArgs) (PriceRef, error) {
	n, err := h.cfg.Store.RemovePrice(ctx, a.Model, a.Effective)
	if err != nil {
		n = 0
	}
	trail.RemovePrice(ctx, h.cfg.Telemetry, a.Model, a.Effective, n, trail.Result(err))
	return PriceRef(a), answerError(err)
}
func (h handlers) away(ctx context.Context, _ identity.Caller, _ NoArgs) (AwayLimit, error) {
	d, err := h.cfg.Store.AwayLimit(ctx)
	return AwayLimit{Minutes: int(d / time.Minute)}, answerError(err)
}
func (h handlers) setAway(ctx context.Context, _ identity.Caller, a SetAwayLimitArgs) (AwayLimit, error) {
	n, err := h.cfg.Store.SetAwayLimit(ctx, a.Minutes)
	if err != nil {
		n = 0
	}
	trail.SetAwayLimit(ctx, h.cfg.Telemetry, a.Minutes, n, trail.Result(err))
	return AwayLimit(a), answerError(err)
}
