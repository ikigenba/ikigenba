package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	sessions "github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/figures"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
)

var ctx = context.Background()

func moment(s string) time.Time {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		panic(e)
	}
	return t.UTC()
}
func openAt(t *testing.T, path string, migrations fs.FS, clock func() time.Time) (*store.Store, *db.DB) {
	t.Helper()
	d, e := db.Open(ctx, db.Config{Path: path, Migrations: migrations, Now: clock})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	return store.New(d, store.Config{clock}), d
}
func fresh(t *testing.T) (*store.Store, *db.DB) {
	t.Helper()
	return openAt(t, filepath.Join(t.TempDir(), "state", "sessions.db"), sessions.Migrations(), func() time.Time { return moment("2026-11-01T12:00:00Z") })
}
func entry(id string, turns ...store.PushTurn) store.PushSession {
	return store.PushSession{Tool: "claude", ID: id, Host: "ld01", Directory: "/mnt/projects/other", Turns: turns}
}
func turn(at, whose string, n int64) store.PushTurn {
	return store.PushTurn{Moment: at, Whose: whose, Model: "claude-opus-5-5", Input: n}
}
func push(t *testing.T, s *store.Store, es ...store.PushSession) store.Pushed {
	t.Helper()
	p, e := s.Push(ctx, "owner", "owner@example", es)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func rows(t *testing.T, s *store.Store) []store.SessionRow {
	t.Helper()
	r, e := s.Sessions(ctx, store.Filter{}, store.Position{}, 10000)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func reason(t *testing.T, e error, want string) {
	t.Helper()
	var r *store.RefusedError
	if !errors.As(e, &r) || r.Reason != want || errors.Is(e, store.ErrUnreachable) {
		t.Fatalf("refusal = %v, want %q", e, want)
	}
}
func check(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// R-1FH5-M5QU R-1GP1-ZXHJ R-1HWY-DP88 R-1J4U-RGYX R-1KCR-58PM R-2NDJ-DWFU
func TestDeclarationsAndCopy(t *testing.T) {
	check(t, []int{store.MaxPushTurns, store.MaxPushSessions, store.MaxLatestSessions, store.MaxPeriods, store.MinAwayMinutes, store.MaxAwayMinutes, store.BatchSize}, []int{1000, 500, 500, 60, 1, 1440, 500})
	check(t, int64(store.MaxTurnCount), int64(1000000000000))
	if store.ErrUnreachable == nil {
		t.Fatal("nil error")
	}
	r := &store.RefusedError{"arbitrary reason"}
	check(t, r.Error(), r.Reason)
	copies := []string{store.Unreachable, store.TooManySessions, store.TooManyTurns, store.NoSessions, store.UnknownTool, store.MissingID, store.BadParent, store.OwnParent, store.MissingHost, store.BadDirectory, store.BadMoment, store.UnknownWhose, store.BadCount, store.OtherOwner, store.UnknownPeriod, store.UnpairedPeriod, store.BadDate, store.UnknownKind, store.UnknownGroup, store.ToBeforeFrom, store.TooManyPeriods, store.UntaggedRule, store.RuleHeld, store.RuleAbsent, store.MissingModel, store.BadRate, store.PriceAbsent, store.AwayOutOfRange}
	seen := map[string]bool{}
	for _, c := range copies {
		if c == "" || strings.ContainsAny(c, "\r\n") || seen[c] {
			t.Fatalf("invalid copy %q", c)
		}
		seen[c] = true
	}
	var _ int16 = store.MaxPushTurns
	var _ int64 = store.MaxTurnCount
	s, d := fresh(t)
	if s == nil || d == nil {
		t.Fatal("constructor returned nil")
	}
}

// R-1LKN-J0GB R-1O0G-AJXP R-1P8C-OBOE R-1QG9-23F3 R-1RO5-FV5S R-1SW1-TMWH R-1U3Y-7EN6
// R-1VBU-L6DV R-1WJQ-YY4K R-1XRN-CPV9 R-1YZJ-QHLY R-207G-49CN R-21FC-I13C R-22N8-VSU1 R-23V5-9KKQ
// R-2531-NCBF R-27IU-EVST R-28QQ-SNJI R-29YN-6FA7 R-1BTX-WHP2 R-2CEF-XYRL R-2DMC-BQIA R-2EU8-PI8Z R-2G25-39ZO
func TestPublicShapes(t *testing.T) {
	c := store.Command{"/work", "arg"}
	pt := store.PushTurn{"2026-10-13T10:00:00Z", "agent", "unknown", 1, 2, 3, 4}
	ref := store.SessionRef{"claude", "shape"}
	e := store.PushSession{"claude", "shape", nil, "host", "/dir", true, c, []store.PushTurn{pt}}
	s, _ := fresh(t)
	p, err := s.Push(ctx, "user", "email", []store.PushSession{e})
	if err != nil {
		t.Fatal(err)
	}
	check(t, p, store.Pushed{[]store.LatestTurn{{"claude", "shape", moment(pt.Moment)}}, 1, 0, 1})
	ls, err := s.Latest(ctx, []store.SessionRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	check(t, ls, p.Sessions)
	f := store.Filter{"week", "2026-10-13", "email", "untagged", "claude", "host", "/work"}
	rs, err := s.Sessions(ctx, f, store.Position{time.Time{}, "", ""}, 1)
	if err != nil {
		t.Fatal(err)
	}
	sum := figures.Summary{Kind: figures.KindUntagged, Figures: figures.Figures{Tokens: figures.Tokens{Input: 1, Cached: 2, Output: 3, Reasoning: 4}, Unpriced: 10}}
	check(t, rs, []store.SessionRow{{figures.ToolClaude, "shape", "host", "email", "/dir", true, c, moment(pt.Moment), moment(pt.Moment), sum}})
	q := store.TotalsQuery{"week", "2026-10-13", "", "tool", f}
	tot, err := s.Totals(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	check(t, tot, []store.PeriodTotals{{moment("2026-10-12T00:00:00Z"), []store.GroupTotals{{"claude", sum.Figures, 1}}}})
	l, err := s.Landing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Landing{true, time.Minute, []store.Week{{time.Time{}, []store.KindTotals{{figures.KindBuild, figures.Figures{}}}, figures.Figures{}}}}
	if !l.Held {
		t.Fatal("not held")
	}
	rule, n, err := s.AddRule(ctx, "host", "/dir", "", "build")
	if err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
	rules, err := s.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 4 {
		t.Fatal(rules)
	}
	removed, n, err := s.RemoveRule(ctx, "host", "/dir", "")
	if err != nil || n != 1 || removed != rule {
		t.Fatalf("remove %v", err)
	}
	pr, n, err := s.SetPrice(ctx, store.PriceInput{"unknown", "2026-01-01", "1", "2", "3", "4"})
	if err != nil || n != 1 || pr.Model != "unknown" {
		t.Fatal(err)
	}
	ps, err := s.Prices(ctx)
	if err != nil || len(ps) != 13 {
		t.Fatal(err)
	}
	if n, err = s.RemovePrice(ctx, "unknown", "2026-01-01"); err != nil || n != 1 {
		t.Fatal(err)
	}
	if n, err = s.SetAwayLimit(ctx, 30); err != nil || n != 1 {
		t.Fatal(err)
	}
	a, err := s.AwayLimit(ctx)
	if err != nil || a != 30*time.Minute {
		t.Fatal(a, err)
	}
	if n, err = s.FinishRefresh(ctx); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

// R-2PTC-5FX8 R-2TH1-AR5B R-2UOX-OIW0 R-4514-7T2E R-49WP-QW16 R-4JNW-T1YQ
func TestFreshSeed(t *testing.T) {
	s, _ := fresh(t)
	check(t, rows(t, s), []store.SessionRow{})
	ls, e := s.Latest(ctx, []store.SessionRef{{"codex", "absent"}})
	if e != nil {
		t.Fatal(e)
	}
	check(t, ls, []store.LatestTurn{{Tool: "codex", ID: "absent"}})
	tot, e := s.Totals(ctx, store.TotalsQuery{Period: "week", From: "2026-10-12", To: "2026-10-26", Group: "kind"})
	if e != nil || len(tot) != 3 {
		t.Fatal(e, tot)
	}
	for _, p := range tot {
		check(t, p.Groups, []store.GroupTotals{})
	}
	l, e := s.Landing(ctx)
	if e != nil {
		t.Fatal(e)
	}
	check(t, l, store.Landing{AwayLimit: 15 * time.Minute})
	n, e := s.FinishRefresh(ctx)
	check(t, n, 0)
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = s.RemoveRule(ctx, "ld01", "/mnt/projects/other", "")
	reason(t, e, store.RuleAbsent)
	_, e = s.RemovePrice(ctx, "claude-opus-5-5", "2026-02-01")
	reason(t, e, store.PriceAbsent)
	rr, e := s.Rules(ctx)
	if e != nil {
		t.Fatal(e)
	}
	check(t, rr, []figures.Rule{{Host: "ld01", Directory: "/mnt/projects/biz", Kind: figures.KindPlan}, {Host: "ld01", Directory: "/mnt/projects/ikigenba", Kind: figures.KindBuild}, {Host: "ld01", Directory: "/mnt/projects/sites-root", Kind: figures.KindBuild}})
	a, e := s.AwayLimit(ctx)
	if e != nil || a != 15*time.Minute {
		t.Fatal(a, e)
	}
	seeds := []store.PriceInput{{"claude-fable-5-1", "2026-01-01", "10", "0.25", "50", "50"}, {"claude-haiku-4-5-20251001", "2026-01-01", "1", "0.10", "5", "5"}, {"claude-haiku-5-5", "2026-01-01", "0.10", "0.01", "0.50", "0.50"}, {"claude-opus-5-5", "2026-01-01", "4", "0.20", "20", "20"}, {"claude-sonnet-5-5", "2026-01-01", "2", "0.10", "10", "10"}, {"gpt-5.4-mini", "2026-01-01", "0.75", "0.075", "4.50", "4.50"}, {"gpt-5.6-luna", "2026-01-01", "0.20", "0.02", "1.20", "1.20"}, {"gpt-6-astra", "2026-01-01", "10", "1", "50", "50"}, {"gpt-6.1-sol", "2026-01-01", "2", "0.10", "10", "10"}, {"grok-4.20-0309-non-reasoning", "2026-01-01", "1.25", "0.20", "2.50", "2.50"}, {"grok-4.6", "2026-01-01", "2", "0.50", "6", "6"}, {"grok-4.7", "2026-01-01", "2", "0.50", "6", "6"}}
	prices, e := s.Prices(ctx)
	if e != nil {
		t.Fatal(e)
	}
	want := make([]figures.Price, 0)
	for _, p := range seeds {
		a, _ := figures.ParseRate(p.Input)
		b, _ := figures.ParseRate(p.Cached)
		c, _ := figures.ParseRate(p.Output)
		d, _ := figures.ParseRate(p.Reasoning)
		want = append(want, figures.Price{Model: p.Model, Effective: moment("2026-01-01T00:00:00Z"), Input: a, Cached: b, Output: c, Reasoning: d})
	}
	check(t, prices, want)
	for i, tc := range []struct {
		host, dir string
		kind      figures.Kind
	}{{"ld01", "/mnt/projects/ikigenba/wip-sessions/sessions", figures.KindBuild}, {"ld01", "/mnt/projects/biz", figures.KindPlan}, {"ld01", "/mnt/projects/bizarre", figures.KindUntagged}, {"ld02", "/mnt/projects/biz", figures.KindUntagged}} {
		e := entry(fmt.Sprint(i), turn("2026-10-13T10:00:00Z", "agent", 1))
		e.Host = tc.host
		e.Directory = tc.dir
		push(t, s, e)
		r, err := s.Sessions(ctx, store.Filter{Host: tc.host}, store.Position{}, 100)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range r {
			if row.ID == e.ID {
				check(t, row.Summary.Kind, tc.kind)
				found = true
			}
		}
		if !found {
			t.Fatal("missing seeded-kind row")
		}
	}
}

// R-2KXQ-MCYG R-2M5N-04P5 R-2X4Q-G2DE
func TestFailurePrecedesArguments(t *testing.T) {
	s, d := fresh(t)
	push(t, s, entry("held", turn("2026-10-13T10:00:00Z", "agent", 1)))
	calls := []func(context.Context) ([]any, error){
		func(c context.Context) ([]any, error) {
			v, e := s.Push(c, "", "", []store.PushSession{{Tool: "bad"}})
			return []any{v}, e
		},
		func(c context.Context) ([]any, error) { v, e := s.Latest(c, nil); return []any{v}, e },
		func(c context.Context) ([]any, error) {
			v, e := s.Sessions(c, store.Filter{Kind: "bad"}, store.Position{}, 1)
			return []any{v}, e
		},
		func(c context.Context) ([]any, error) { v, e := s.Totals(c, store.TotalsQuery{}); return []any{v}, e },
		func(c context.Context) ([]any, error) { v, e := s.Landing(c); return []any{v}, e },
		func(c context.Context) ([]any, error) { v, e := s.Rules(c); return []any{v}, e },
		func(c context.Context) ([]any, error) {
			v, n, e := s.AddRule(c, "ld01", "/mnt/projects/biz", "", "plan")
			return []any{v, n}, e
		},
		func(c context.Context) ([]any, error) { v, n, e := s.RemoveRule(c, "", "", ""); return []any{v, n}, e },
		func(c context.Context) ([]any, error) { v, e := s.Prices(c); return []any{v}, e },
		func(c context.Context) ([]any, error) {
			v, n, e := s.SetPrice(c, store.PriceInput{})
			return []any{v, n}, e
		},
		func(c context.Context) ([]any, error) { v, e := s.RemovePrice(c, "", ""); return []any{v}, e },
		func(c context.Context) ([]any, error) { v, e := s.AwayLimit(c); return []any{v}, e },
		func(c context.Context) ([]any, error) { v, e := s.SetAwayLimit(c, 0); return []any{v}, e },
		func(c context.Context) ([]any, error) { v, e := s.FinishRefresh(c); return []any{v}, e },
	}
	verify := func(c context.Context) {
		t.Helper()
		for i, call := range calls {
			vs, e := call(c)
			if !errors.Is(e, store.ErrUnreachable) {
				t.Fatalf("call %d = %v", i, e)
			}
			if c.Err() != nil && !errors.Is(e, c.Err()) {
				t.Fatal(e)
			}
			for _, v := range vs {
				if !reflect.ValueOf(v).IsZero() {
					t.Fatalf("call %d nonzero result %#v", i, v)
				}
			}
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	verify(canceled)
	d.SetFailing(true)
	verify(ctx)
	d.SetFailing(false)
	check(t, len(rows(t, s)), 1)
	p, e := s.Push(ctx, "", "", nil)
	if !errors.Is(e, store.ErrUnreachable) {
		t.Fatal(e)
	}
	check(t, p, store.Pushed{})
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	verify(ctx)
}

// R-2YCM-TU43 R-2ZKJ-7LUS R-C6AP-5HS7 R-320B-Z5C6 R-CDM3-G48D R-34G4-QOTK R-3MQM-H8XZ
func TestPushValidationAndLimits(t *testing.T) {
	s, _ := fresh(t)
	p, e := s.Push(ctx, "u", "e", nil)
	if e != nil {
		t.Fatal(e)
	}
	check(t, p, store.Pushed{Sessions: []store.LatestTurn{}})
	good := entry("valid", turn("2026-10-13T10:00:00Z", "agent", 1))
	cases := []struct {
		name, want string
		change     func(*store.PushSession)
	}{
		{"tool", store.UnknownTool, func(e *store.PushSession) { e.Tool = "Claude" }}, {"id", store.MissingID, func(e *store.PushSession) { e.ID = "" }},
		{"parentTool", store.BadParent, func(e *store.PushSession) { e.Parent = &store.SessionRef{"bad", "x"} }}, {"parentID", store.BadParent, func(e *store.PushSession) { e.Parent = &store.SessionRef{"claude", ""} }}, {"ownParent", store.OwnParent, func(e *store.PushSession) { e.Parent = &store.SessionRef{e.Tool, e.ID} }},
		{"host", store.MissingHost, func(e *store.PushSession) { e.Host = "" }}, {"emptyDirectory", store.BadDirectory, func(e *store.PushSession) { e.Directory = "" }},
		{"relative", store.BadDirectory, func(e *store.PushSession) { e.Directory = "relative" }}, {"slash", store.BadDirectory, func(e *store.PushSession) { e.Directory = "/trailing/" }},
		{"moment", store.BadMoment, func(e *store.PushSession) { e.Turns[0].Moment = "2026-10-13 14:02:11Z" }}, {"epoch", store.BadMoment, func(e *store.PushSession) { e.Turns[0].Moment = "1969-12-31T23:59:59.999Z" }},
		{"whose", store.UnknownWhose, func(e *store.PushSession) { e.Turns[0].Whose = "user" }}, {"negative", store.BadCount, func(e *store.PushSession) { e.Turns[0].Input = -1 }}, {"large", store.BadCount, func(e *store.PushSession) { e.Turns[0].Reasoning = store.MaxTurnCount + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := good
			bad.Turns = append([]store.PushTurn(nil), good.Turns...)
			tc.change(&bad)
			p, e := s.Push(ctx, "u", "e", []store.PushSession{entry("prior", good.Turns...), bad})
			reason(t, e, tc.want)
			check(t, p, store.Pushed{})
			check(t, rows(t, s), []store.SessionRow{})
		})
	}
	for _, at := range []string{"", "2026-10-13T14:02:11", "0001-01-01T00:00:00Z"} {
		bad := good
		bad.Turns = []store.PushTurn{turn(at, "agent", 0)}
		_, e = s.Push(ctx, "u", "e", []store.PushSession{bad})
		reason(t, e, store.BadMoment)
	}
	for _, field := range []int{0, 1, 2, 3} {
		for _, n := range []int64{-1, store.MaxTurnCount + 1} {
			bad := good
			bad.Turns = []store.PushTurn{turn("2026-10-13T10:00:00Z", "agent", 0)}
			v := &bad.Turns[0]
			switch field {
			case 0:
				v.Input = n
			case 1:
				v.Cached = n
			case 2:
				v.Output = n
			case 3:
				v.Reasoning = n
			}
			_, e = s.Push(ctx, "u", "e", []store.PushSession{bad})
			reason(t, e, store.BadCount)
		}
	}
	tooMany := make([]store.PushSession, store.MaxPushSessions+1)
	_, e = s.Push(ctx, "u", "e", tooMany)
	reason(t, e, store.TooManySessions)
	tooTurns := entry("huge")
	tooTurns.Turns = make([]store.PushTurn, store.MaxPushTurns+1)
	_, e = s.Push(ctx, "u", "e", []store.PushSession{tooTurns})
	reason(t, e, store.TooManyTurns)
	nohost := good
	nohost.Host = ""
	badtool := good
	badtool.Tool = "bad"
	_, e = s.Push(ctx, "u", "e", []store.PushSession{nohost, badtool})
	reason(t, e, store.MissingHost)
	accepted := make([]store.PushSession, store.MaxPushSessions)
	for i := range accepted {
		accepted[i] = entry("limit", turn("2026-10-13T10:00:00Z", "agent", store.MaxTurnCount), turn("1970-01-01T00:00:00Z", "person", 0))
		accepted[i].Directory = "/"
		accepted[i].Parent = &store.SessionRef{"codex", "limit"}
		accepted[i].Turns[1].Model = ""
	}
	p, e = s.Push(ctx, "u", "e", accepted)
	if e != nil || p.New != 2 || len(p.Sessions) != 500 {
		t.Fatal(p, e)
	}
	_, e = s.Push(ctx, "other", "other", []store.PushSession{entry("limit")})
	reason(t, e, store.OtherOwner)
	bad := entry("bad", turn("bad", "agent", 0))
	_, e = s.Push(ctx, "other", "other", []store.PushSession{entry("limit"), bad})
	reason(t, e, store.BadMoment)
	_, e = s.Push(ctx, "u", "changed", accepted[:1])
	if e != nil {
		t.Fatal(e)
	}
	codex := entry("limit", turn("2026-10-13T10:00:00Z", "agent", 1))
	codex.Tool = "codex"
	_, e = s.Push(ctx, "other", "other", []store.PushSession{codex})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Latest(ctx, nil)
	reason(t, e, store.NoSessions)
	_, e = s.Latest(ctx, make([]store.SessionRef, 501))
	reason(t, e, store.TooManySessions)
	_, e = s.Latest(ctx, []store.SessionRef{{"bad", ""}})
	reason(t, e, store.UnknownTool)
	_, e = s.Latest(ctx, []store.SessionRef{{"claude", ""}})
	reason(t, e, store.MissingID)
}

// R-2HA1-H1QD R-35O1-4GK9 R-36VX-I8AY R-39BQ-9RSC R-3E7B-SUR4 R-3FF8-6MHT R-3KAT-PPGL R-3LIQ-3H7A
func TestTurnCorrectionsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "sessions.db")
	clock := func() time.Time { return moment("2026-11-01T12:00:00Z") }
	s, d := openAt(t, path, sessions.Migrations(), clock)
	query := entry("unknown")
	p := push(t, s, query)
	check(t, p, store.Pushed{Sessions: []store.LatestTurn{{Tool: "claude", ID: "unknown"}}})
	check(t, len(rows(t, s)), 0)
	e := entry("one", turn("2026-10-13T14:02:11.2509Z", "agent", 1000))
	p = push(t, s, e)
	check(t, p.New, 1)
	check(t, p.Changed, 0)
	e.Turns[0] = turn("2026-10-13T16:02:11.250+02:00", "agent", 3000)
	e.Turns[0].Output = 500
	p = push(t, s, e)
	check(t, p.New, 0)
	check(t, p.Changed, 1)
	r := rows(t, s)[0]
	check(t, r.Summary.Figures.Tokens, figures.Tokens{Input: 3000, Output: 500})
	check(t, r.Summary.Figures.Dollars.String(), "0.022")
	check(t, r.First, moment("2026-10-13T14:02:11.250Z"))
	if r.First.Location() != time.UTC {
		t.Fatal(r.First.Location())
	}
	p = push(t, s, e)
	check(t, p.New, 0)
	check(t, p.Changed, 0)
	e.Turns = []store.PushTurn{turn("2026-10-13T14:02:11.251Z", "agent", 10), turn("2026-10-13T14:02:11.251Z", "agent", 20)}
	p = push(t, s, e, e)
	check(t, p.New, 1)
	check(t, p.Changed, 0)
	check(t, len(p.Sessions), 2)
	check(t, p.Sessions[0], p.Sessions[1])
	check(t, p.Sessions[0].Latest, moment("2026-10-13T14:02:11.251Z"))
	check(t, rows(t, s)[0].Summary.Figures.Tokens.Input, int64(3020))
	// Counts compare the final value with the value held before the whole call.
	changed := e
	changed.Turns = []store.PushTurn{turn("2026-10-13T14:02:11.251Z", "agent", 99), turn("2026-10-13T14:02:11.251Z", "agent", 20)}
	p = push(t, s, changed)
	check(t, p.Changed, 0)
	check(t, p.New, 0)
	changed.Turns = changed.Turns[1:]
	changed.Turns[0].Model = "unpriced-model"
	p = push(t, s, changed)
	check(t, p.Changed, 1)
	check(t, p.New, 0)
	check(t, rows(t, s)[0].Summary.Figures.Unpriced, int64(20))
	p = push(t, s, e)
	check(t, p.Changed, 1)

	e.Turns = []store.PushTurn{turn("2026-10-12T00:00:00Z", "tool", 0)}
	p = push(t, s, e)
	check(t, p.Sessions[0].Latest, moment("2026-10-13T14:02:11.251Z"))
	_, err := s.Push(ctx, "second", "second@example", []store.PushSession{entry("two", turn("2026-10-14T10:00:00Z", "agent", 2))})
	if err != nil {
		t.Fatal(err)
	}
	refs := make([]store.SessionRef, 500)
	for i := range refs {
		refs[i] = store.SessionRef{"claude", fmt.Sprintf("missing-%d", i)}
	}
	refs[3] = store.SessionRef{"claude", "one"}
	refs[400] = store.SessionRef{"claude", "two"}
	refs[450] = refs[3]
	refs[451] = refs[400]
	latest, err := s.Latest(ctx, refs)
	if err != nil {
		t.Fatal(err)
	}

	wantLatest := make([]store.LatestTurn, len(refs))
	for i, ref := range refs {
		wantLatest[i] = store.LatestTurn{Tool: ref.Tool, ID: ref.ID}
		switch i {
		case 3, 450:
			wantLatest[i].Latest = moment("2026-10-13T14:02:11.251Z")
		case 400, 451:
			wantLatest[i].Latest = moment("2026-10-14T10:00:00Z")
		}
	}
	check(t, latest, wantLatest)
	before := rows(t, s)
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _ := openAt(t, path, sessions.Migrations(), clock)
	check(t, rows(t, reopened), before)
	invalid := entry("invalid", turn("2026-10-14T10:00:00Z", "user", 0))
	_, err = reopened.Push(ctx, "owner", "owner@example", []store.PushSession{entry("new", turn("2026-10-14T10:00:00Z", "agent", 1)), invalid})
	reason(t, err, store.UnknownWhose)
	check(t, rows(t, reopened), before)
}

// R-2VWU-2AMP
func TestConcurrentPush(t *testing.T) {
	s, _ := fresh(t)
	var wg sync.WaitGroup
	results := make(chan store.Pushed, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			p, e := s.Push(ctx, "owner", "email", []store.PushSession{entry("same", turn("2026-10-13T10:00:00Z", "agent", 1))})
			results <- p
			errs <- e
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	n := 0
	for p := range results {
		n += p.New
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	check(t, n, 1)
	check(t, len(rows(t, s)), 1)
}

// R-C9YE-AT0A R-C1F3-METF R-C2N0-06K4 R-C8QH-X19L R-3GN4-KE8I R-CB6A-OKQZ R-3P6F-8SFD
func TestSessionMetadataAndTrees(t *testing.T) {
	s, _ := fresh(t)
	child := entry("child", turn("2026-10-13T10:05:00Z", "person", 0), turn("2026-10-13T10:45:00Z", "agent", 1250))
	child.Tool = "codex"
	child.Parent = &store.SessionRef{"claude", "parent"}
	child.Command = store.Command{"/different", "childarg"}
	p := push(t, s, child)
	check(t, p.Refreshed, 1)
	check(t, rows(t, s)[0].ID, "child")
	check(t, rows(t, s)[0].Summary.Subagents, 0)
	root := entry("parent", turn("2026-10-13T10:00:00Z", "person", 0), turn("2026-10-13T10:30:00Z", "agent", 1250))
	root.Directory = "/mnt/projects/biz"
	root.Command = store.Command{"/deliver", "rootarg"}
	p = push(t, s, root)
	check(t, p.Refreshed, 1)
	rr := rows(t, s)
	check(t, len(rr), 1)
	r := rr[0]
	check(t, r.ID, "parent")
	check(t, r.Tool, figures.ToolClaude)
	check(t, r.Person, "owner@example")
	check(t, r.Host, root.Host)
	check(t, r.Directory, root.Directory)
	check(t, r.Command, root.Command)
	check(t, r.Last, moment("2026-10-13T10:30:00Z"))
	rules, e := s.Rules(ctx)
	if e != nil {
		t.Fatal(e)
	}
	prices, e := s.Prices(ctx)
	if e != nil {
		t.Fatal(e)
	}
	expectedRoot := figures.Session{Key: figures.SessionKey{Tool: figures.ToolClaude, ID: root.ID}, Host: root.Host, Directory: root.Directory, Command: root.Command.Name, Turns: []figures.Turn{{Moment: moment(root.Turns[0].Moment), Whose: figures.WhosePerson, Model: root.Turns[0].Model}, {Moment: moment(root.Turns[1].Moment), Whose: figures.WhoseAgent, Model: root.Turns[1].Model, Tokens: figures.Tokens{Input: 1250}}}}
	expectedChild := figures.Session{Key: figures.SessionKey{Tool: figures.ToolCodex, ID: child.ID}, Parent: expectedRoot.Key, Host: child.Host, Directory: child.Directory, Command: child.Command.Name, Turns: []figures.Turn{{Moment: moment(child.Turns[0].Moment), Whose: figures.WhosePerson, Model: child.Turns[0].Model}, {Moment: moment(child.Turns[1].Moment), Whose: figures.WhoseAgent, Model: child.Turns[1].Model, Tokens: figures.Tokens{Input: 1250}}}}
	check(t, r.Summary, figures.Summarize(expectedRoot, []figures.Session{expectedChild}, rules, prices, 15*time.Minute))
	// Changing the push metadata cannot replace a held value.
	changed := root
	changed.Host = "other"
	changed.Directory = "/else"
	changed.Headless = true
	changed.Command = store.Command{"/plan", "new"}
	_, e = s.Push(ctx, "owner", "new-email", []store.PushSession{changed})
	if e != nil {
		t.Fatal(e)
	}
	check(t, rows(t, s)[0], r)
	// An empty command's argument is discarded, and empty entries do not fill it.
	blank := entry("blank", turn("2026-10-13T11:00:00Z", "agent", 0))
	blank.Command.Argument = "discard"
	push(t, s, blank)
	blank.Turns = nil
	blank.Command = store.Command{"/query", "query"}
	push(t, s, blank)
	br, _ := s.Sessions(ctx, store.Filter{Command: "/query"}, store.Position{}, 100)
	check(t, len(br), 0)
	blank.Turns = []store.PushTurn{turn("2026-10-13T11:01:00Z", "agent", 0)}
	blank.Command = store.Command{"/first", "kept"}
	second := blank
	second.Command = store.Command{"/second", "ignored"}
	push(t, s, blank, second)
	br, _ = s.Sessions(ctx, store.Filter{Command: "/first"}, store.Position{}, 100)
	check(t, len(br), 1)
	check(t, br[0].Command, blank.Command)
	// A parent fills once, including within one call, and the first nonempty link wins.
	a := entry("a", turn("2026-10-13T12:00:00Z", "agent", 1))
	b := entry("b", turn("2026-10-13T12:01:00Z", "agent", 2))
	b.Parent = &store.SessionRef{"claude", "a"}
	push(t, s, a, b)
	a.Parent = &store.SessionRef{"claude", "b"}
	push(t, s, a)
	ar, _ := s.Sessions(ctx, store.Filter{}, store.Position{}, 100)
	found := false
	for _, v := range ar {
		if v.ID == "a" {
			found = true
			check(t, v.Summary.Subagents, 1)
		}
	}
	if !found {
		t.Fatal("cycle removed root")
	}
	x := entry("x", turn("2026-10-13T13:00:00Z", "agent", 1))
	x.Parent = &store.SessionRef{"claude", "y"}
	y := entry("y", turn("2026-10-13T13:01:00Z", "agent", 2))
	y.Parent = &store.SessionRef{"claude", "x"}
	p = push(t, s, x, y)
	check(t, p.Refreshed, 1)
	ar = rows(t, s)
	for _, v := range ar {
		if v.ID == "x" {
			t.Fatal("cycle did not select arriving root")
		}
		if v.ID == "y" {
			check(t, v.Summary.Subagents, 1)
		}
	}
	filled := entry("filled", turn("2026-10-13T14:00:00Z", "agent", 1))
	next := filled
	next.Parent = &store.SessionRef{"claude", "parent"}
	third := next
	third.Parent = &store.SessionRef{"claude", "a"}
	p = push(t, s, filled, next, third)
	check(t, p.Refreshed, 1)
	ar = rows(t, s)
	for _, v := range ar {
		if v.ID == "filled" {
			t.Fatal("parent not filled")
		}
		if v.ID == "parent" {
			check(t, v.Summary.Subagents, 2)
		}
	}
	// First entry carrying a turn defines parent and headless; a query entry cannot create metadata.
	q := entry("created")
	q.Host = "ignored"
	q.Command = store.Command{"/ignored", ""}
	created := entry("created", turn("2026-10-13T15:00:00Z", "agent", 1))
	created.Parent = &store.SessionRef{"claude", "parent"}
	created.Headless = true
	later := created
	later.Parent = &store.SessionRef{"claude", "a"}
	push(t, s, q, created, later)
	ar = rows(t, s)
	for _, v := range ar {
		if v.ID == "parent" {
			check(t, v.Summary.Subagents, 3)
		}
	}
}

// R-3J2X-BXPW R-4G07-NQQN R-4KVT-6TPF
func TestFigureCorrections(t *testing.T) {
	s, _ := fresh(t)
	e := entry("correction", turn("2026-10-13T10:00:00Z", "person", 0), turn("2026-10-13T10:00:30Z", "agent", 1000000), turn("2026-10-13T10:02:30Z", "person", 0))
	e.Turns[1].Cached = 500000
	e.Turns[1].Output = 100000
	push(t, s, e)
	e.Turns = []store.PushTurn{e.Turns[1], turn("2026-10-13T10:03:00Z", "agent", 0)}
	e.Turns[0].Input = 2000000
	push(t, s, e)
	f := rows(t, s)[0].Summary.Figures
	check(t, f.Attention, 120*time.Second)
	check(t, f.Wall, 3*time.Minute)
	check(t, f.Agent, 60*time.Second)
	check(t, f.Tokens, figures.Tokens{Input: 2000000, Cached: 500000, Output: 100000})
	check(t, f.Dollars.Dollars(), "10.10")
	for _, n := range []int{0, 1441} {
		count, err := s.SetAwayLimit(ctx, n)
		reason(t, err, store.AwayOutOfRange)
		check(t, count, 0)
	}
	for _, n := range []int{1, 1440, 15} {
		count, err := s.SetAwayLimit(ctx, n)
		if err != nil || count != 1 {
			t.Fatal(count, err)
		}
		a, err := s.AwayLimit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		check(t, a, time.Duration(n)*time.Minute)
		want := 120 * time.Second
		if n == 1 {
			want = time.Minute
		}
		check(t, rows(t, s)[0].Summary.Figures.Attention, want)
	}
	split := entry("split", turn("2026-10-14T23:59:59.999Z", "agent", 1000000), turn("2026-10-15T00:00:00Z", "agent", 1000000))
	push(t, s, split)
	_, n, err := s.SetPrice(ctx, store.PriceInput{"claude-opus-5-5", "2026-10-15", "8", "0.40", "40", "40"})
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	check(t, rows(t, s)[0].Summary.Figures.Dollars.Dollars(), "12.00")
	n, err = s.RemovePrice(ctx, "claude-opus-5-5", "2026-10-15")
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	check(t, rows(t, s)[0].Summary.Figures.Dollars.Dollars(), "8.00")
}

// R-C077-8N2Q R-BWJI-3BUN R-3RM8-0BWR
func TestListOrderFiltersAndRefusals(t *testing.T) {
	s, _ := fresh(t)
	for _, x := range []struct{ tool, id, at string }{{"claude", "early", "2026-10-13T09:00:00Z"}, {"claude", "b", "2026-10-13T10:00:00Z"}, {"codex", "a", "2026-10-13T10:00:00Z"}, {"claude", "c", "2026-10-13T10:00:00Z"}, {"grok", "last", "2026-10-13T11:00:00Z"}} {
		e := entry(x.id, turn(x.at, "agent", 1))
		e.Tool = x.tool
		e.Directory = "/mnt/projects/biz"
		e.Command = store.Command{"/work", "argument"}
		push(t, s, e)
	}
	want := []string{"last", "a", "c", "b", "early"}
	for _, n := range []int{1, 2, 3} {
		var pos store.Position
		got := []string{}
		for {
			r, e := s.Sessions(ctx, store.Filter{}, pos, n)
			if e != nil {
				t.Fatal(e)
			}

			check(t, len(r), min(n, len(want)-len(got)))
			if len(r) == 0 {
				break
			}
			for _, v := range r {
				got = append(got, v.ID)
			}
			last := r[len(r)-1]
			pos = store.Position{last.First, string(last.Tool), last.ID}
		}
		check(t, got, want)
	}
	absent := store.Position{moment("2026-10-13T10:00:00Z"), "claude", "bb"}
	r, e := s.Sessions(ctx, store.Filter{}, absent, 20)
	if e != nil {
		t.Fatal(e)
	}
	check(t, []string{r[0].ID, r[1].ID}, []string{"b", "early"})
	for _, n := range []int{0, -1} {
		r, e = s.Sessions(ctx, store.Filter{}, store.Position{}, n)
		if e != nil {
			t.Fatal(e)
		}
		check(t, r, []store.SessionRow{})
	}
	for _, tc := range []struct {
		f     store.Filter
		count int
	}{{store.Filter{Period: "week", Of: "2026-10-14"}, 5}, {store.Filter{Period: "month", Of: "2026-10-31"}, 5}, {store.Filter{Person: "owner@example"}, 5}, {store.Filter{Person: "owner"}, 0}, {store.Filter{Tool: "codex"}, 1}, {store.Filter{Kind: "plan"}, 5}, {store.Filter{Kind: "untagged"}, 0}, {store.Filter{Host: "ld01"}, 5}, {store.Filter{Host: "LD01"}, 0}, {store.Filter{Command: "/work"}, 5}, {store.Filter{Command: "/WORK"}, 0}, {store.Filter{Person: "owner@example", Tool: "grok", Kind: "plan", Host: "ld01", Command: "/work"}, 1}} {
		r, e = s.Sessions(ctx, tc.f, store.Position{}, 100)
		if e != nil {
			t.Fatal(e)
		}
		check(t, len(r), tc.count)
	}
	for _, tc := range []struct {
		f    store.Filter
		want string
	}{{store.Filter{Period: "day", Of: "bad", Kind: "bad"}, store.UnknownPeriod}, {store.Filter{Of: "2026-10-14", Kind: "bad"}, store.UnpairedPeriod}, {store.Filter{Period: "week"}, store.UnpairedPeriod}, {store.Filter{Period: "week", Of: "bad", Kind: "bad"}, store.BadDate}, {store.Filter{Kind: "bad", Tool: "bad"}, store.UnknownKind}, {store.Filter{Tool: "bad"}, store.UnknownTool}} {
		r, e = s.Sessions(ctx, tc.f, store.Position{}, 1)
		reason(t, e, tc.want)
		if r != nil {
			t.Fatal(r)
		}
	}
	boundary := entry("boundary", turn("2026-10-25T23:50:00Z", "person", 0), turn("2026-10-26T00:10:00Z", "agent", 1))
	push(t, s, boundary)
	for _, tc := range []struct {
		of string
		n  int
	}{{"2026-10-19", 1}, {"2026-10-26", 0}} {
		r, e = s.Sessions(ctx, store.Filter{Period: "week", Of: tc.of}, store.Position{}, 100)
		if e != nil {
			t.Fatal(e)
		}
		check(t, len(r), tc.n)
	}
}

// R-BXRE-H3LC R-3WHT-JEVJ R-3XPP-X6M8 R-3YXM-AYCX R-CCE7-2CHO
func TestTotals(t *testing.T) {
	s, _ := fresh(t)
	for i, dir := range []string{"/mnt/projects/other", "/mnt/projects/biz", "/mnt/projects/ikigenba"} {
		e := entry(fmt.Sprint(i), turn("2026-10-25T23:50:00Z", "agent", 50000), turn("2026-10-26T00:10:00Z", "agent", 0))
		e.Turns[0].Model = "claude-haiku-5-5"
		if i > 0 {
			e.Command.Name = "/deliver"
		}
		e.Directory = dir
		push(t, s, e)
	}
	q := store.TotalsQuery{Period: "week", From: "2026-10-12", To: "2026-10-26", Group: "kind"}
	got, e := s.Totals(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	check(t, len(got), 3)
	check(t, got[0].Groups, []store.GroupTotals{})
	check(t, got[2].Groups, []store.GroupTotals{})
	check(t, got[1].Start, moment("2026-10-19T00:00:00Z"))
	names := []string{}
	for _, g := range got[1].Groups {
		names = append(names, g.Group)
		check(t, g.Sessions, 1)
		check(t, g.Figures.Dollars.Dollars(), "0.01")
	}
	check(t, names, []string{"plan", "build", "untagged"})
	q.Group = "person"
	got, e = s.Totals(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	check(t, got[1].Groups[0].Group, "owner@example")
	check(t, got[1].Groups[0].Sessions, 3)
	check(t, got[1].Groups[0].Figures.Dollars.String(), "0.015")
	check(t, got[1].Groups[0].Figures.Dollars.Dollars(), "0.02")
	q.Group = "command"
	got, e = s.Totals(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	check(t, []string{got[1].Groups[0].Group, got[1].Groups[1].Group}, []string{"", "/deliver"})
	check(t, got[1].Groups[1].Sessions, 2)
	q.Group = "tool"
	q.Filter = store.Filter{Period: "day", Of: "invalid", Person: "owner@example", Kind: "build", Tool: "claude", Host: "ld01", Command: "/deliver"}
	got, e = s.Totals(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	check(t, got[1].Groups[0].Sessions, 1)
	check(t, got[1].Groups[0].Group, "claude")
	q.Filter.Person = "other"
	got, e = s.Totals(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	check(t, got[1].Groups, []store.GroupTotals{})
	for _, q := range []store.TotalsQuery{{Period: "month", From: "2021-01-15", To: "2025-12-31", Group: "kind"}, {Period: "week", From: "2026-01-01", To: "2027-02-21", Group: "kind"}} {
		got, e = s.Totals(ctx, q)
		if e != nil {
			t.Fatal(e)
		}
		check(t, len(got), 60)
	}
	base := store.TotalsQuery{Period: "week", From: "2026-10-12", Group: "kind"}
	for _, tc := range []struct {
		q    store.TotalsQuery
		want string
	}{{store.TotalsQuery{}, store.UnknownPeriod}, {store.TotalsQuery{Period: "day", From: "bad", Group: "bad"}, store.UnknownPeriod}, {store.TotalsQuery{Period: "week", From: "bad", To: "bad", Group: "bad"}, store.BadDate}, {store.TotalsQuery{Period: "week", From: "2026-10-14", To: "2026-10-13", Group: "bad"}, store.ToBeforeFrom}, {store.TotalsQuery{Period: "month", From: "2021-01-15", To: "2026-01-01", Group: "bad"}, store.TooManyPeriods}, {store.TotalsQuery{Period: "week", From: "2026-01-01", To: "2027-02-22", Group: "kind"}, store.TooManyPeriods}, {store.TotalsQuery{Period: "week", From: "2026-10-12", Group: "bad", Filter: store.Filter{Kind: "bad"}}, store.UnknownGroup}, {store.TotalsQuery{Period: "week", From: "2026-10-12", Group: "kind", Filter: store.Filter{Kind: "bad", Tool: "bad"}}, store.UnknownKind}, {store.TotalsQuery{Period: "week", From: "2026-10-12", Group: "kind", Filter: store.Filter{Tool: "bad"}}, store.UnknownTool}} {
		got, e = s.Totals(ctx, tc.q)
		reason(t, e, tc.want)
		if got != nil {
			t.Fatal(got)
		}
	}
	base.To = "bad"
	_, e = s.Totals(ctx, base)
	reason(t, e, store.BadDate)
}

// R-42LB-G9L0 R-BYZA-UVC1 R-3YXM-AYCX
func TestLandingClockAndExactWeeks(t *testing.T) {
	now := moment("2026-11-01T12:00:00Z")
	calls := 0
	clock := func() time.Time { calls++; return now }
	s, _ := openAt(t, filepath.Join(t.TempDir(), "sessions.db"), sessions.Migrations(), clock)
	calls = 0
	dates := []string{"2026-10-26T00:00:00Z", "2026-10-25T23:59:59.999Z", "2026-10-25T23:50:00Z", "2026-08-30T23:59:59.999Z"}
	for i, at := range dates {
		e := entry(fmt.Sprint(i), turn(at, "agent", 50000))
		e.Turns[0].Model = "claude-haiku-5-5"
		if i == 2 {
			e.Turns = append(e.Turns, turn("2026-10-26T00:10:00Z", "agent", 0))
		}
		push(t, s, e)
	}
	for i := 0; i < 2; i++ {
		e := entry(fmt.Sprintf("extra-%d", i), turn("2026-10-26T10:00:00Z", "agent", 50000))
		e.Turns[0].Model = "claude-haiku-5-5"
		push(t, s, e)
	}
	l, e := s.Landing(ctx)
	if e != nil {
		t.Fatal(e)
	}
	check(t, calls, 1)
	check(t, l.Held, true)
	check(t, l.AwayLimit, 15*time.Minute)
	check(t, len(l.Weeks), 9)
	check(t, l.Weeks[0].Start, moment("2026-10-26T00:00:00Z"))
	check(t, l.Weeks[8].Start, moment("2026-08-31T00:00:00Z"))
	check(t, l.Weeks[0].Total.Dollars.Dollars(), "0.02")
	check(t, l.Weeks[1].Total.Dollars.String(), "0.01")
	for i, w := range l.Weeks {
		check(t, len(w.Kinds), 7)
		var sum figures.Figures
		for j, k := range w.Kinds {
			check(t, k.Kind, figures.Kinds()[j])
			sum = sum.Add(k.Figures)
		}
		check(t, w.Total, sum)
		if i > 1 {
			check(t, w.Total, figures.Figures{})
		}
	}
	now = moment("2026-11-02T00:00:00Z")
	l, e = s.Landing(ctx)
	if e != nil {
		t.Fatal(e)
	}
	check(t, calls, 2)
	check(t, l.Weeks[0].Start, now)
	check(t, l.Weeks[0].Total, figures.Figures{})
	check(t, l.Weeks[1].Total.Dollars.Dollars(), "0.02")
}

// R-4690-LKT3 R-47GW-ZCJS R-1D1U-A9FR R-4B4M-4NRV R-4CCI-IFIK R-4DKE-W799
func TestTablesAndRefusals(t *testing.T) {
	s, _ := fresh(t)
	for i := range 3 {
		e := entry(fmt.Sprint(i), turn("2026-10-13T10:00:00Z", "agent", 1000))
		if i == 0 {
			e.Command.Name = "/deliver"
		}
		push(t, s, e)
	}
	r, n, e := s.AddRule(ctx, "ld01", "/mnt/projects/other", "", "operate")
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
	check(t, r, figures.Rule{Host: "ld01", Directory: "/mnt/projects/other", Command: "", Kind: figures.KindOperate})
	for _, r := range rows(t, s) {
		check(t, r.Summary.Kind, figures.KindOperate)
	}
	_, n, e = s.AddRule(ctx, "ld01", "/mnt/projects/other", "/deliver", "sell")
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
	for _, r := range rows(t, s) {
		want := figures.KindOperate
		if r.Command.Name == "/deliver" {
			want = figures.KindSell
		}
		check(t, r.Summary.Kind, want)
	}
	for _, tc := range []struct{ h, d, c, k, want string }{{"", "bad", "", "untagged", store.MissingHost}, {"ld01", "bad", "", "untagged", store.BadDirectory}, {"ld01", "/valid", "", "untagged", store.UntaggedRule}, {"ld01", "/valid", "", "bad", store.UnknownKind}, {"ld01", "/mnt/projects/biz", "", "build", store.RuleHeld}} {
		r, n, e = s.AddRule(ctx, tc.h, tc.d, tc.c, tc.k)
		reason(t, e, tc.want)
		check(t, r, figures.Rule{})
		check(t, n, 0)
	}
	r, n, e = s.RemoveRule(ctx, "ld01", "/mnt/projects/other", "")
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
	check(t, r.Kind, figures.KindOperate)
	for _, r := range rows(t, s) {
		if r.Command.Name == "" {
			check(t, r.Summary.Kind, figures.KindUntagged)
		}
	}
	r, n, e = s.RemoveRule(ctx, "ld01", "/mnt/projects/biz/", "")
	reason(t, e, store.RuleAbsent)
	check(t, r, figures.Rule{})
	check(t, n, 0)
	p := store.PriceInput{"claude-opus-5-5", "2026-01-01", "5", "0.5", "25", "25"}
	pr, n, e := s.SetPrice(ctx, p)
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
	check(t, pr.Input.String(), "5")
	ps, e := s.Prices(ctx)
	if e != nil {
		t.Fatal(e)
	}
	check(t, len(ps), 12)
	for _, r := range rows(t, s) {
		check(t, r.Summary.Figures.Dollars.String(), "0.005")
	}
	for _, tc := range []struct {
		p    store.PriceInput
		want string
	}{{store.PriceInput{}, store.MissingModel}, {store.PriceInput{Model: "m", Effective: "bad", Input: "bad"}, store.BadDate}, {store.PriceInput{"m", "2026-01-01", "4.1234567", "0", "0", "0"}, store.BadRate}, {store.PriceInput{"m", "2026-01-01", "0", "-1", "0", "0"}, store.BadRate}, {store.PriceInput{"m", "2026-01-01", "0", "0", "bad", "0"}, store.BadRate}, {store.PriceInput{"m", "2026-01-01", "0", "0", "0", "bad"}, store.BadRate}} {
		pr, n, e = s.SetPrice(ctx, tc.p)
		reason(t, e, tc.want)
		check(t, pr, figures.Price{})
		check(t, n, 0)
	}
	n, e = s.RemovePrice(ctx, "m", "bad")
	reason(t, e, store.BadDate)
	check(t, n, 0)
	n, e = s.RemovePrice(ctx, "missing", "2026-01-01")
	reason(t, e, store.PriceAbsent)
	check(t, n, 0)
	n, e = s.RemovePrice(ctx, p.Model, p.Effective)
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
	for _, r := range rows(t, s) {
		check(t, r.Summary.Figures.Dollars.Dollars(), "0.00")
		check(t, r.Summary.Figures.Unpriced, int64(1000))
	}
	// A later effective date is ordered after the earlier date for the same model.
	_, _, e = s.SetPrice(ctx, store.PriceInput{"claude-opus-5-5", "2026-02-01", "0", "0", "0", "0"})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = s.SetPrice(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	ps, e = s.Prices(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for i := 1; i < len(ps); i++ {
		if ps[i].Model < ps[i-1].Model || ps[i].Model == ps[i-1].Model && ps[i].Effective.Before(ps[i-1].Effective) {
			t.Fatal("price order", ps)
		}
	}
}

// R-3SU4-E3NG R-41DF-2HUB R-4H84-1IHC R-C52S-RQ1I R-C3UW-DYAT
func TestPagedRefreshAndTotals(t *testing.T) {
	s, d := fresh(t)
	count := 2*store.BatchSize + 1
	for begin := 0; begin < count; begin += store.MaxPushSessions {
		end := min(begin+store.MaxPushSessions, count)
		es := make([]store.PushSession, 0, end-begin)
		for i := begin; i < end; i++ {
			es = append(es, entry(fmt.Sprintf("paged-%04d", i), turn("2026-10-13T10:00:00Z", "agent", 1)))
		}
		push(t, s, es...)
	}
	verify := func(kind figures.Kind) {
		t.Helper()
		var pos store.Position
		seen := map[string]bool{}
		for {
			r, e := s.Sessions(ctx, store.Filter{}, pos, store.BatchSize)
			if e != nil {
				t.Fatal(e)
			}
			if len(r) == 0 {
				break
			}
			for _, v := range r {
				if seen[v.ID] {
					t.Fatal("duplicate", v.ID)
				}
				seen[v.ID] = true
				check(t, v.Summary.Kind, kind)
			}
			last := r[len(r)-1]
			pos = store.Position{last.First, string(last.Tool), last.ID}
		}
		check(t, len(seen), count)
	}
	verify(figures.KindUntagged)
	_, n, e := s.AddRule(ctx, "ld01", "/mnt/projects/other", "", "operate")
	if e != nil || n != count {
		t.Fatal(n, e)
	}
	verify(figures.KindOperate)
	totals, e := s.Totals(ctx, store.TotalsQuery{Period: "week", From: "2026-10-13", Group: "kind"})
	if e != nil {
		t.Fatal(e)
	}
	check(t, totals[0].Groups[0].Sessions, count)
	// Simulate a crash after the table change but before any refresh page.
	e = d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE rules SET kind='sell' WHERE host='ld01' AND directory='/mnt/projects/other'; UPDATE settings SET refresh_after_tool='',refresh_after_id='' WHERE id=1")
		return err
	})
	if e != nil {
		t.Fatal(e)
	}
	n, e = s.FinishRefresh(ctx)
	if e != nil || n != count {
		t.Fatal(n, e)
	}
	verify(figures.KindSell)
	n, e = s.FinishRefresh(ctx)
	if e != nil || n != 0 {
		t.Fatal(n, e)
	}
	// A subsequent configuration change restarts a pending refresh.
	e = d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE settings SET away_minutes=1,refresh_after_tool='',refresh_after_id='' WHERE id=1")
		return err
	})
	if e != nil {
		t.Fatal(e)
	}
	n, e = s.SetAwayLimit(ctx, 15)
	if e != nil || n != count {
		t.Fatal(n, e)
	}
	verify(figures.KindSell)
	n, e = s.FinishRefresh(ctx)
	if e != nil || n != 0 {
		t.Fatal(n, e)
	}
}

// R-2R18-J7NX
func TestMigrationBytes(t *testing.T) {
	got, err := fs.ReadFile(sessions.Migrations(), "0001_sessions.sql")
	if err != nil {
		t.Fatal(err)
	}
	const want = `CREATE TABLE sessions (tool TEXT NOT NULL, id TEXT NOT NULL, parent_tool TEXT NOT NULL, parent_id TEXT NOT NULL, root_tool TEXT NOT NULL, root_id TEXT NOT NULL, user_id TEXT NOT NULL, person TEXT NOT NULL, host TEXT NOT NULL, directory TEXT NOT NULL, headless INTEGER NOT NULL, command TEXT NOT NULL, argument TEXT NOT NULL, PRIMARY KEY (tool, id)) WITHOUT ROWID;
CREATE INDEX sessions_parent ON sessions (parent_tool, parent_id);
CREATE INDEX sessions_root ON sessions (root_tool, root_id);
CREATE TABLE turns (tool TEXT NOT NULL, id TEXT NOT NULL, moment INTEGER NOT NULL, whose TEXT NOT NULL, model TEXT NOT NULL, input INTEGER NOT NULL, cached INTEGER NOT NULL, output INTEGER NOT NULL, reasoning INTEGER NOT NULL, PRIMARY KEY (tool, id, moment, whose)) WITHOUT ROWID;
CREATE TABLE summaries (tool TEXT NOT NULL, id TEXT NOT NULL, first INTEGER NOT NULL, last INTEGER NOT NULL, person TEXT NOT NULL, host TEXT NOT NULL, command TEXT NOT NULL, kind TEXT NOT NULL, attention INTEGER NOT NULL, wall INTEGER NOT NULL, agent INTEGER NOT NULL, input INTEGER NOT NULL, cached INTEGER NOT NULL, output INTEGER NOT NULL, reasoning INTEGER NOT NULL, dollars TEXT NOT NULL, unpriced INTEGER NOT NULL, subagents INTEGER NOT NULL, PRIMARY KEY (tool, id)) WITHOUT ROWID;
CREATE INDEX summaries_first ON summaries (first, tool, id);
CREATE TABLE rules (host TEXT NOT NULL, directory TEXT NOT NULL, command TEXT NOT NULL, kind TEXT NOT NULL, PRIMARY KEY (host, directory, command)) WITHOUT ROWID;
CREATE TABLE prices (model TEXT NOT NULL, effective TEXT NOT NULL, input TEXT NOT NULL, cached TEXT NOT NULL, output TEXT NOT NULL, reasoning TEXT NOT NULL, PRIMARY KEY (model, effective)) WITHOUT ROWID;
CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK (id = 1), away_minutes INTEGER NOT NULL, refresh_after_tool TEXT, refresh_after_id TEXT);
INSERT INTO rules (host, directory, command, kind) VALUES ('ld01', '/mnt/projects/biz', '', 'plan');
INSERT INTO rules (host, directory, command, kind) VALUES ('ld01', '/mnt/projects/ikigenba', '', 'build');
INSERT INTO rules (host, directory, command, kind) VALUES ('ld01', '/mnt/projects/sites-root', '', 'build');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('claude-opus-5-5', '2026-01-01', '4', '0.20', '20', '20');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('claude-fable-5-1', '2026-01-01', '10', '0.25', '50', '50');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('claude-sonnet-5-5', '2026-01-01', '2', '0.10', '10', '10');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('claude-haiku-5-5', '2026-01-01', '0.10', '0.01', '0.50', '0.50');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('claude-haiku-4-5-20251001', '2026-01-01', '1', '0.10', '5', '5');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('gpt-6.1-sol', '2026-01-01', '2', '0.10', '10', '10');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('gpt-6-astra', '2026-01-01', '10', '1', '50', '50');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('gpt-5.6-luna', '2026-01-01', '0.20', '0.02', '1.20', '1.20');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('gpt-5.4-mini', '2026-01-01', '0.75', '0.075', '4.50', '4.50');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('grok-4.7', '2026-01-01', '2', '0.50', '6', '6');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('grok-4.6', '2026-01-01', '2', '0.50', '6', '6');
INSERT INTO prices (model, effective, input, cached, output, reasoning) VALUES ('grok-4.20-0309-non-reasoning', '2026-01-01', '1.25', '0.20', '2.50', '2.50');
INSERT INTO settings (id, away_minutes, refresh_after_tool, refresh_after_id) VALUES (1, 15, NULL, NULL);
`
	check(t, string(got), want)
}

// R-2S94-WZEM
func TestExpandMigrationCompatibility(t *testing.T) {
	first, e := fs.ReadFile(sessions.Migrations(), "0001_sessions.sql")
	if e != nil {
		t.Fatal(e)
	}
	expanded := fstest.MapFS{"0001_sessions.sql": &fstest.MapFile{Data: first}, "0002_expand.sql": &fstest.MapFile{Data: []byte("ALTER TABLE sessions ADD COLUMN future TEXT;\nALTER TABLE turns ADD COLUMN future TEXT;\nALTER TABLE summaries ADD COLUMN future TEXT;\nALTER TABLE rules ADD COLUMN future TEXT;\nALTER TABLE prices ADD COLUMN future TEXT;\nALTER TABLE settings ADD COLUMN future TEXT;\n")}}
	scenario := func(s *store.Store) []any {
		var all []any
		add := func(value any, err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, value)
		}
		r, e := s.Rules(ctx)
		add(r, e)
		ps, e := s.Prices(ctx)
		add(ps, e)
		a, e := s.AwayLimit(ctx)
		add(a, e)
		n, e := s.FinishRefresh(ctx)
		add(n, e)
		l, e := s.Landing(ctx)
		add(l, e)
		root := entry("root", turn("2026-10-13T10:00:00Z", "person", 0), turn("2026-10-13T10:01:00Z", "agent", 1250), turn("2026-10-13T10:20:00Z", "person", 0))
		child := entry("child", turn("2026-10-13T10:02:00Z", "agent", 500))
		child.Parent = &store.SessionRef{"claude", "root"}
		p, e := s.Push(ctx, "owner", "owner@example", []store.PushSession{child, root})
		add(p, e)
		p, e = s.Push(ctx, "owner", "owner@example", []store.PushSession{root})
		add(p, e)
		ls, e := s.Latest(ctx, []store.SessionRef{{"claude", "root"}, {"claude", "missing"}})
		add(ls, e)
		list, e := s.Sessions(ctx, store.Filter{}, store.Position{}, 10)
		add(list, e)
		tot, e := s.Totals(ctx, store.TotalsQuery{Period: "week", From: "2026-10-12", Group: "kind"})
		add(tot, e)
		l, e = s.Landing(ctx)
		add(l, e)
		rule, n, e := s.AddRule(ctx, "ld01", "/mnt/projects/other", "", "build")
		add([]any{rule, n}, e)
		rule, n, e = s.RemoveRule(ctx, "ld01", "/mnt/projects/other", "")
		add([]any{rule, n}, e)
		price, n, e := s.SetPrice(ctx, store.PriceInput{"claude-opus-5-5", "2026-01-01", "5", "1", "30", "30"})
		add([]any{price, n}, e)
		n, e = s.RemovePrice(ctx, "claude-opus-5-5", "2026-01-01")
		add(n, e)
		n, e = s.SetAwayLimit(ctx, 1)
		add(n, e)
		n, e = s.FinishRefresh(ctx)
		add(n, e)
		l, e = s.Landing(ctx)
		add(l, e)
		r, e = s.Rules(ctx)
		add(r, e)
		ps, e = s.Prices(ctx)
		add(ps, e)
		a, e = s.AwayLimit(ctx)
		add(a, e)
		add(rows(t, s), nil)
		return all
	}
	clock := func() time.Time { return moment("2026-11-01T12:00:00Z") }
	old, _ := openAt(t, filepath.Join(t.TempDir(), "old.db"), sessions.Migrations(), clock)
	newer, _ := openAt(t, filepath.Join(t.TempDir(), "new.db"), expanded, clock)
	check(t, scenario(newer), scenario(old))
}

// R-3XPP-X6M8 R-42LB-G9L0
func TestCompleteGroupedFiguresAndLandingKinds(t *testing.T) {
	s, _ := fresh(t)
	counted := func(at, model string, a, b, c, d int64) store.PushTurn {
		return store.PushTurn{Moment: at, Whose: "agent", Model: model, Input: a, Cached: b, Output: c, Reasoning: d}
	}
	first := entry("plan-first",
		turn("2026-10-27T09:00:00Z", "person", 0),
		counted("2026-10-27T09:05:00Z", "claude-opus-5-5", 1000, 200, 300, 400),
		turn("2026-10-27T09:25:00Z", "person", 0),
		counted("2026-10-27T09:30:00Z", "unpriced", 5, 6, 7, 8))
	first.Directory = "/mnt/projects/biz"
	second := entry("plan-second",
		turn("2026-10-28T10:00:00Z", "person", 0),
		counted("2026-10-28T10:02:00Z", "claude-opus-5-5", 2000, 300, 400, 500),
		turn("2026-10-28T10:06:00Z", "person", 0),
		counted("2026-10-28T10:07:00Z", "unpriced", 10, 20, 30, 40))
	second.Directory = "/mnt/projects/biz"
	third := entry("build",
		turn("2026-10-27T11:00:00Z", "person", 0),
		counted("2026-10-27T11:03:00Z", "claude-opus-5-5", 100, 200, 300, 400),
		turn("2026-10-27T11:07:00Z", "person", 0))
	third.Directory = "/mnt/projects/ikigenba"
	third.Headless = true
	fourth := entry("previous-untagged",
		turn("2026-10-25T09:00:00Z", "person", 0),
		counted("2026-10-25T09:02:00Z", "unpriced", 1, 2, 3, 4),
		turn("2026-10-25T09:03:00Z", "person", 0))
	push(t, s, first, second, third, fourth)
	money := func(text string) figures.Money {
		t.Helper()
		m, ok := figures.ParseMoney(text)
		if !ok {
			t.Fatal(text)
		}
		return m
	}
	plan := figures.Figures{Attention: 19 * time.Minute, Wall: 37 * time.Minute, Agent: 13 * time.Minute, Tokens: figures.Tokens{Input: 3015, Cached: 526, Output: 737, Reasoning: 948}, Dollars: money("0.0441"), Unpriced: 126}
	build := figures.Figures{Attention: 0, Wall: 7 * time.Minute, Agent: 3 * time.Minute, Tokens: figures.Tokens{Input: 100, Cached: 200, Output: 300, Reasoning: 400}, Dollars: money("0.01444"), Unpriced: 0}
	untagged := figures.Figures{Attention: time.Minute, Wall: 3 * time.Minute, Agent: 2 * time.Minute, Tokens: figures.Tokens{Input: 1, Cached: 2, Output: 3, Reasoning: 4}, Dollars: figures.Money{}, Unpriced: 10}
	total := figures.Figures{Attention: 19 * time.Minute, Wall: 44 * time.Minute, Agent: 16 * time.Minute, Tokens: figures.Tokens{Input: 3115, Cached: 726, Output: 1037, Reasoning: 1348}, Dollars: money("0.05854"), Unpriced: 126}
	q := store.TotalsQuery{Period: "week", From: "2026-10-19", To: "2026-11-02", Group: "kind"}
	got, err := s.Totals(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, []store.PeriodTotals{
		{Start: moment("2026-10-19T00:00:00Z"), Groups: []store.GroupTotals{{Group: "untagged", Figures: untagged, Sessions: 1}}},
		{Start: moment("2026-10-26T00:00:00Z"), Groups: []store.GroupTotals{{Group: "plan", Figures: plan, Sessions: 2}, {Group: "build", Figures: build, Sessions: 1}}},
		{Start: moment("2026-11-02T00:00:00Z"), Groups: []store.GroupTotals{}},
	})
	for _, group := range []string{"person", "tool", "command"} {
		q.Group = group
		got, err = s.Totals(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		name := map[string]string{"person": "owner@example", "tool": "claude", "command": ""}[group]
		check(t, got[0].Groups, []store.GroupTotals{{Group: name, Figures: untagged, Sessions: 1}})
		check(t, got[1].Groups, []store.GroupTotals{{Group: name, Figures: total, Sessions: 3}})
		check(t, got[2].Groups, []store.GroupTotals{})
	}
	landing, err := s.Landing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := store.Landing{Held: true, AwayLimit: 15 * time.Minute, Weeks: make([]store.Week, 0, 9)}
	for i := range 9 {
		week := store.Week{Start: moment("2026-10-26T00:00:00Z").AddDate(0, 0, -7*i), Kinds: []store.KindTotals{{Kind: figures.KindPlan}, {Kind: figures.KindBuild}, {Kind: figures.KindPublish}, {Kind: figures.KindOperate}, {Kind: figures.KindSell}, {Kind: figures.KindCustomer}, {Kind: figures.KindUntagged}}}
		switch i {
		case 0:
			week.Kinds[0].Figures = plan
			week.Kinds[1].Figures = build
			week.Total = total
		case 1:
			week.Kinds[6].Figures = untagged
			week.Total = untagged
		}
		want.Weeks = append(want.Weeks, week)
	}
	check(t, landing, want)
}
