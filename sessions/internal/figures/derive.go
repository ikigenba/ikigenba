package figures

import (
	"slices"
	"strings"
	"time"
)

// Match returns the most specific candidate rule’s kind, or untagged.
func Match(rules []Rule, host, directory, command string) Kind {
	best := -1
	for i, r := range rules {
		if r.Host != host || (r.Command != "" && r.Command != command) {
			continue
		}
		if r.Directory != directory && (r.Directory != "/" || !strings.HasPrefix(directory, "/")) && !strings.HasPrefix(directory, r.Directory+"/") {
			continue
		}
		if best < 0 || len(r.Directory) > len(rules[best].Directory) || (len(r.Directory) == len(rules[best].Directory) && r.Command != "" && rules[best].Command == "") {
			best = i
		}
	}
	if best < 0 {
		return KindUntagged
	}
	return rules[best].Kind
}
func date(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// PriceAt returns the first price at the latest applicable UTC date.
func PriceAt(prices []Price, model string, moment time.Time) (Price, bool) {
	var best Price
	found := false
	d := date(moment)
	for _, p := range prices {
		pd := date(p.Effective)
		if p.Model == model && !pd.After(d) && (!found || pd.After(date(best.Effective))) {
			best = p
			found = true
		}
	}
	return best, found
}
func ordered(s Session) []Turn {
	turns := slices.Clone(s.Turns)
	rank := map[Whose]int{WhosePerson: 0, WhoseAgent: 1, WhoseTool: 2}
	slices.SortStableFunc(turns, func(a, b Turn) int {
		if c := a.Moment.Compare(b.Moment); c != 0 {
			return c
		}
		return rank[a.Whose] - rank[b.Whose]
	})
	return turns
}

// Attention sums capped person gaps from the latest preceding agent turn.
func Attention(s Session, away time.Duration) time.Duration {
	if s.Headless || away <= 0 || len(s.Turns) == 0 {
		return 0
	}
	turns := ordered(s)
	last := turns[0].Moment
	var total int64
	for _, t := range turns {
		switch t.Whose {
		case WhoseAgent:
			last = t.Moment
		case WhosePerson:
			gap := t.Moment.Sub(last)
			if gap > away {
				gap = away
			}
			total = sat(total, int64(gap))
		}
	}
	return time.Duration(total)
}

// AgentTime sums each person span’s time through its final agent turn.
func AgentTime(s Session) time.Duration {
	turns := ordered(s)
	if len(turns) == 0 {
		return 0
	}
	start := turns[0].Moment
	var span time.Duration
	var total int64
	for _, t := range turns {
		switch t.Whose {
		case WhosePerson:
			total = sat(total, int64(span))
			start = t.Moment
			span = 0
		case WhoseAgent:
			span = t.Moment.Sub(start)
		}
	}
	return time.Duration(sat(total, int64(span)))
}

// Wall returns the duration between the earliest and latest turns.
func Wall(s Session) time.Duration {
	if len(s.Turns) == 0 {
		return 0
	}
	first, last := s.Turns[0].Moment, s.Turns[0].Moment
	for _, t := range s.Turns[1:] {
		if t.Moment.Before(first) {
			first = t.Moment
		}
		if t.Moment.After(last) {
			last = t.Moment
		}
	}
	return last.Sub(first)
}

// Root follows known parent links until a missing parent or cycle.
func Root(key SessionKey, parents map[SessionKey]SessionKey) SessionKey {
	seen := map[SessionKey]bool{key: true}
	for {
		p, ok := parents[key]
		if !ok || p == (SessionKey{}) || seen[p] {
			return key
		}
		if _, ok := parents[p]; !ok {
			return key
		}
		key = p
		seen[key] = true
	}
}

// Summarize rolls root and descendant usage into the root’s figures.
func Summarize(root Session, subagents []Session, rules []Rule, prices []Price, away time.Duration) Summary {
	f := Figures{Attention: Attention(root, away), Wall: Wall(root), Agent: AgentTime(root)}
	turns := slices.Clone(root.Turns)
	for _, s := range subagents {
		f.Agent = time.Duration(sat(int64(f.Agent), int64(AgentTime(s))))
		turns = append(turns, s.Turns...)
	}
	for _, t := range turns {
		f.Tokens = f.Tokens.Add(t.Tokens)
	}
	f.Dollars, f.Unpriced = Cost(turns, prices)
	return Summary{Match(rules, root.Host, root.Directory, root.Command), f, len(subagents)}
}
