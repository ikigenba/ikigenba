package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/sessions/internal/figures"
)

func validateFilter(f Filter) error {
	if f.Period != "" {
		if _, ok := figures.ParsePeriod(f.Period); !ok {
			return refused(UnknownPeriod)
		}
	}
	if (f.Period == "") != (f.Of == "") {
		return refused(UnpairedPeriod)
	}
	if f.Of != "" {
		if _, ok := figures.ParseDate(f.Of); !ok {
			return refused(BadDate)
		}
	}
	if f.Kind != "" {
		if _, ok := figures.ParseKind(f.Kind); !ok {
			return refused(UnknownKind)
		}
	}
	if f.Tool != "" {
		if _, ok := figures.ParseTool(f.Tool); !ok {
			return refused(UnknownTool)
		}
	}
	return nil
}
func whereFilter(f Filter) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0)
	if f.Period != "" {
		p, _ := figures.ParsePeriod(f.Period)
		d, _ := figures.ParseDate(f.Of)
		start := p.Start(d)
		clauses = append(clauses, "a.first>=? AND a.first<?")
		args = append(args, start.UnixMilli(), p.Next(start).UnixMilli())
	}
	for _, v := range []struct{ col, val string }{{"person", f.Person}, {"kind", f.Kind}, {"tool", f.Tool}, {"host", f.Host}, {"command", f.Command}} {
		if v.val != "" {
			clauses = append(clauses, "a."+v.col+"=?")
			args = append(args, v.val)
		}
	}
	return strings.Join(clauses, " AND "), args
}
func sessionRowsIn(ctx context.Context, tx *sql.Tx, f Filter, after Position, n int, start, end *time.Time) ([]SessionRow, error) {
	out := make([]SessionRow, 0)
	if n < 1 {
		return out, nil
	}
	where, args := whereFilter(f)
	if after.ID != "" {
		where += " AND (a.first,a.tool,a.id)<(?,?,?)"
		args = append(args, after.First.UnixMilli(), after.Tool, after.ID)
	}
	if start != nil {
		where += " AND a.first>=? AND a.first<?"
		args = append(args, start.UnixMilli(), end.UnixMilli())
	}
	args = append(args, n)
	rows, err := tx.QueryContext(ctx, strings.Join([]string{`SELECT a.tool,a.id,a.host,a.person,s.directory,s.headless,s.command,s.argument,a.first,a.last,a.kind,a.attention,a.wall,a.agent,a.input,a.cached,a.output,a.reasoning,a.dollars,a.unpriced,a.subagents FROM summaries a JOIN sessions s ON s.tool=a.tool AND s.id=a.id WHERE `, where, " ORDER BY a.first DESC,a.tool DESC,a.id DESC LIMIT ?"}, ""), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r SessionRow
		var first, last int64
		var dollars string
		f := &r.Summary.Figures
		if err = rows.Scan(&r.Tool, &r.ID, &r.Host, &r.Person, &r.Directory, &r.Headless, &r.Command.Name, &r.Command.Argument, &first, &last, &r.Summary.Kind, &f.Attention, &f.Wall, &f.Agent, &f.Tokens.Input, &f.Tokens.Cached, &f.Tokens.Output, &f.Tokens.Reasoning, &dollars, &f.Unpriced, &r.Summary.Subagents); err != nil {
			return nil, err
		}
		r.First = instant(first)
		r.Last = instant(last)
		f.Dollars, _ = figures.ParseMoney(dollars)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Sessions returns a database-filtered window in descending list order.
func (s *Store) Sessions(ctx context.Context, f Filter, after Position, n int) ([]SessionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validateFilter(f); err != nil {
		return nil, err
	}
	var out []SessionRow
	err := s.d.Read(ctx, func(tx *sql.Tx) error { var e error; out, e = sessionRowsIn(ctx, tx, f, after, n, nil, nil); return e })
	if err != nil {
		return nil, inaccessible(err)
	}
	return out, nil
}
func totalPlan(q TotalsQuery) (figures.Period, time.Time, time.Time, error) {
	p, ok := figures.ParsePeriod(q.Period)
	if !ok {
		return "", time.Time{}, time.Time{}, refused(UnknownPeriod)
	}
	from, ok := figures.ParseDate(q.From)
	if !ok {
		return "", time.Time{}, time.Time{}, refused(BadDate)
	}
	to := from
	if q.To != "" {
		to, ok = figures.ParseDate(q.To)
		if !ok {
			return "", time.Time{}, time.Time{}, refused(BadDate)
		}
	}
	if to.Before(from) {
		return "", time.Time{}, time.Time{}, refused(ToBeforeFrom)
	}
	start, end := p.Start(from), p.Next(p.Start(to))
	count := 0
	for d := start; d.Before(end); d = p.Next(d) {
		count++
		if count > MaxPeriods {
			return "", time.Time{}, time.Time{}, refused(TooManyPeriods)
		}
	}
	switch q.Group {
	case "kind", "person", "tool", "command":
	default:
		return "", time.Time{}, time.Time{}, refused(UnknownGroup)
	}
	f := q.Filter
	f.Period = ""
	f.Of = ""
	if err := validateFilter(f); err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	return p, start, end, nil
}
func totalsIn(ctx context.Context, tx *sql.Tx, q TotalsQuery, p figures.Period, start, end time.Time) ([]PeriodTotals, error) {
	out := make([]PeriodTotals, 0)
	indices := make(map[int64]int)
	groups := make([]map[string]GroupTotals, 0)
	for d := start; d.Before(end); d = p.Next(d) {
		indices[d.UnixMilli()] = len(out)
		out = append(out, PeriodTotals{Start: d, Groups: make([]GroupTotals, 0)})
		groups = append(groups, make(map[string]GroupTotals))
	}
	f := q.Filter
	f.Period = ""
	f.Of = ""
	var after Position
	for {
		rows, err := sessionRowsIn(ctx, tx, f, after, BatchSize, &start, &end)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			index := indices[p.Start(r.First).UnixMilli()]
			var group string
			switch q.Group {
			case "kind":
				group = string(r.Summary.Kind)
			case "person":
				group = r.Person
			case "tool":
				group = string(r.Tool)
			case "command":
				group = r.Command.Name
			}
			g := groups[index][group]
			g.Group = group
			g.Sessions++
			g.Figures = g.Figures.Add(r.Summary.Figures)
			groups[index][group] = g
		}
		if len(rows) < BatchSize {
			break
		}
		last := rows[len(rows)-1]
		after = Position{First: last.First, Tool: string(last.Tool), ID: last.ID}
	}
	for i, gs := range groups {
		if q.Group == "kind" {
			for _, k := range figures.Kinds() {
				if g, ok := gs[string(k)]; ok {
					out[i].Groups = append(out[i].Groups, g)
				}
			}
		} else {
			names := make([]string, 0, len(gs))
			for name := range gs {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				out[i].Groups = append(out[i].Groups, gs[name])
			}
		}
	}
	return out, nil
}

// Totals folds matching root summaries into UTC periods and groups.
func (s *Store) Totals(ctx context.Context, q TotalsQuery) ([]PeriodTotals, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	p, start, end, err := totalPlan(q)
	if err != nil {
		return nil, err
	}
	var out []PeriodTotals
	err = s.d.Read(ctx, func(tx *sql.Tx) error { var e error; out, e = totalsIn(ctx, tx, q, p, start, end); return e })
	if err != nil {
		return nil, inaccessible(err)
	}
	return out, nil
}

// Landing returns nine business weeks placed by the configured clock.
func (s *Store) Landing(ctx context.Context) (Landing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Landing{}, err
	}
	var out Landing
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sessions").Scan(&count); err != nil {
			return err
		}
		var err error
		out.AwayLimit, err = awayIn(ctx, tx)
		if err != nil {
			return err
		}
		out.Held = count > 0
		if !out.Held {
			return nil
		}
		now := s.cfg.Now()
		first := figures.PeriodWeek.Start(now)
		start := first.AddDate(0, 0, -56)
		end := first.AddDate(0, 0, 7)
		periods, err := totalsIn(ctx, tx, TotalsQuery{Group: "kind"}, figures.PeriodWeek, start, end)
		if err != nil {
			return err
		}
		out.Weeks = make([]Week, 0, 9)
		for i := len(periods) - 1; i >= 0; i-- {
			w := Week{Start: periods[i].Start, Kinds: make([]KindTotals, 0, 7)}
			for _, k := range figures.Kinds() {
				kt := KindTotals{Kind: k}
				for _, g := range periods[i].Groups {
					if g.Group == string(k) {
						kt.Figures = g.Figures
						break
					}
				}
				w.Total = w.Total.Add(kt.Figures)
				w.Kinds = append(w.Kinds, kt)
			}
			out.Weeks = append(out.Weeks, w)
		}
		return nil
	})
	if err != nil {
		return Landing{}, inaccessible(err)
	}
	return out, nil
}
