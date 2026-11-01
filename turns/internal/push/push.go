// Package push selects, batches and counts content-free turns for a space.
package push

import (
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/ikigenba/ikigenba/turns/internal/space"
)

// Pending pairs a selected session with the latest moment held by the space.
type Pending struct {
	Session session.Session
	Latest  time.Time
}

// Tally counts sessions and new turns for one tool.
type Tally struct {
	Sessions int
	Turns    int
}

// Select keeps each new session and the held boundary plus later turns.
func Select(sessions []session.Session, held []space.Held) []Pending {
	latest := make(map[session.Ref]time.Time, len(held))
	for _, h := range held {
		latest[h.Ref] = h.Latest
	}
	var result []Pending
	for _, s := range sessions {
		l := latest[s.Ref]
		turns := make([]session.Turn, 0, len(s.Turns))
		for _, turn := range s.Turns {
			if l.IsZero() || !turn.Moment.Before(l) {
				turns = append(turns, turn)
			}
		}
		if !l.IsZero() && len(turns) == 0 {
			continue
		}
		s.Turns = turns
		result = append(result, Pending{Session: s, Latest: l})
	}
	return result
}

// Batches partitions selected turns at the space's entry and turn limits.
func Batches(pending []Pending) [][]Pending {
	var result [][]Pending
	var batch []Pending
	count := 0
	for _, p := range pending {
		for len(p.Session.Turns) > 0 {
			if len(batch) == space.MaxPushSessions || count == space.MaxPushTurns {
				result = append(result, batch)
				batch = nil
				count = 0
			}
			n := min(len(p.Session.Turns), space.MaxPushTurns-count)
			part := p
			part.Session.Turns = p.Session.Turns[:n:n]
			batch = append(batch, part)
			count += n
			p.Session.Turns = p.Session.Turns[n:]
		}
	}
	if len(batch) > 0 {
		result = append(result, batch)
	}
	return result
}

// Count counts new turns and distinct sessions across accepted batches.
func Count(batches [][]Pending) map[session.Tool]Tally {
	result := make(map[session.Tool]Tally)
	seen := make(map[session.Ref]bool)
	for _, tool := range session.Tools() {
		result[tool] = Tally{}
	}
	for _, batch := range batches {
		for _, p := range batch {
			tally := result[p.Session.Ref.Tool]
			counted := 0
			for _, turn := range p.Session.Turns {
				if p.Latest.IsZero() || turn.Moment.After(p.Latest) {
					counted++
				}
			}
			tally.Turns += counted
			if counted > 0 && !seen[p.Session.Ref] {
				tally.Sessions++
				seen[p.Session.Ref] = true
			}
			result[p.Session.Ref.Tool] = tally
		}
	}
	return result
}
