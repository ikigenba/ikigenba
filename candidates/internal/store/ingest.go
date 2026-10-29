package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func sortedIDs(m map[string]bool) []string {
	out := []string{}
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Attach attaches a batch of readings atomically.
func (s *Store) Attach(ctx context.Context, run string, force bool, obs []Observation) (Counts, []string, error) {
	var total Counts
	var ids []string
	err := unreachable(s.retryUnit(ctx, func(ctx context.Context, tx *sql.Tx) error {
		total = Counts{}
		ids = nil
		if run == "" {
			return ErrUnreachable
		}
		for _, o := range obs {
			if o.Scraper == "" || o.SourceID == "" {
				return ErrUnreachable
			}
		}
		t := s.now()
		touched := map[string]bool{}
		views := map[string]*displayTracker{}
		view := func(id string) (*displayTracker, error) {
			if d := views[id]; d != nil {
				return d, nil
			}
			c, err := candidateKey(ctx, tx, id)
			if err != nil {
				return nil, err
			}
			r, err := candidateRecords(ctx, tx, c)
			if err != nil {
				return nil, err
			}
			d := newDisplayTracker(r, id)
			views[id] = d
			return d, nil
		}
		for _, o := range obs {
			total.Rows++
			ls, err := readRecords[Link](ctx, tx, "links", linkColumns, " WHERE scraper=? AND source_id=?", o.Scraper, o.SourceID)
			if err != nil {
				return err
			}
			var l Link
			if len(ls) > 0 {
				l = ls[0]
				unchanged := !force && l.Digest == o.Digest
				if force {
					var raw string
					if err = scanSQL(ctx, tx, "SELECT reading FROM links WHERE scraper=? AND source_id=?", []any{o.Scraper, o.SourceID}, &raw); err != nil {
						return err
					}
					encoded, err := json.Marshal(o.Reading)
					if err != nil {
						return err
					}
					unchanged = bytes.Equal([]byte(raw), encoded)
				}
				if unchanged {
					total.Unchanged++
					d, err := view(l.Candidate)
					if err != nil {
						return err
					}
					if err = execSQL(ctx, tx, "UPDATE links SET run=?,last_seen=?,gone=0 WHERE scraper=? AND source_id=?", run, t.Format(time.RFC3339), o.Scraper, o.SourceID); err != nil {
						return err
					}
					l.Run = run
					l.LastSeen = t
					l.Gone = false
					touched[l.Candidate] = true
					d.apply(l)
					if err = s.refreshRecords(ctx, tx, d.records, l.Candidate, t); err != nil {
						return err
					}
					continue
				}
				total.Changed++
				total.Linked++
				l.Digest = o.Digest
				l.Reading = o.Reading
			} else {
				total.New++
				id, reason, confidence, err := matchCandidate(ctx, tx, o.Reading)
				if err != nil {
					return err
				}
				if id == "" {
					id, err = s.mintKey(ctx, tx, CandidateIDPrefix)
					if err != nil {
						return err
					}
					c := Candidate{ID: id, Status: StatusActive, Names: []string{}, People: []sources.Sourced[sources.Person]{}, Cargo: []string{}, Created: t, Updated: t}
					r := emptyRecords()
					r.candidates[id] = c
					views[id] = newDisplayTracker(r, id)
					if err = putCandidate(ctx, tx, c); err != nil {
						return err
					}
					reason, confidence = LinkSourceKey, 1
					total.Created++
				} else {
					total.Linked++
				}
				l = Link{Candidate: id, Scraper: o.Scraper, SourceID: o.SourceID, Digest: o.Digest, Reading: o.Reading, Reason: reason, Confidence: confidence, FirstSeen: t}
			}
			l.Run = run
			l.LastSeen = t
			l.Gone = false
			d, err := view(l.Candidate)
			if err != nil {
				return err
			}
			if err = putLink(ctx, tx, l); err != nil {
				return err
			}
			touched[l.Candidate] = true
			d.apply(l)
			if err = s.refreshRecords(ctx, tx, d.records, l.Candidate, t); err != nil {
				return err
			}
		}
		ids = sortedIDs(touched)
		for _, id := range ids {
			d := views[id]
			if err := s.refreshRecords(ctx, tx, d.records, id, t); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return Counts{}, nil, err
	}
	return total, ids, nil
}

// MarkGone examines one page of the scraper's links.
func (s *Store) MarkGone(ctx context.Context, scraper, run, after string, n int) (string, Counts, error) {
	var count Counts
	var last string
	err := unreachable(s.retryUnit(ctx, func(ctx context.Context, tx *sql.Tx) error {
		count, last = Counts{}, ""
		if scraper == "" || scraper == places.Name || run == "" || n < 1 {
			return ErrUnreachable
		}
		ls, err := readRecords[Link](ctx, tx, "links", linkColumns, " WHERE scraper=? AND source_id>? ORDER BY source_id LIMIT ?", scraper, after, n)
		if err != nil {
			return err
		}
		touched := map[string]bool{}
		t := s.now()
		for _, l := range ls {
			last = l.SourceID
			if l.Run == run || l.Gone {
				continue
			}
			if err = execSQL(ctx, tx, "UPDATE links SET gone=1 WHERE scraper=? AND source_id=?", scraper, l.SourceID); err != nil {
				return err
			}
			touched[l.Candidate] = true
			count.Gone++
		}
		for id := range touched {
			if err = s.refreshKey(ctx, tx, id, t); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return "", Counts{}, err
	}
	return last, count, nil
}
func malformed(d Ruling) bool {
	switch d.Kind {
	case KindMerge, KindNotSame:
		return strings.TrimSpace(d.Why) == "" || d.Other == ""
	case KindSplit:
		return strings.TrimSpace(d.Why) == "" || len(d.Keys) == 0
	case KindExclude, KindInclude:
		return strings.TrimSpace(d.Why) == ""
	default:
		return true
	}
}

// Decide records and applies an owner ruling atomically.
func (s *Store) Decide(ctx context.Context, d Ruling) (Ruling, []Candidate, error) {
	var out Ruling
	var cs []Candidate
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if d.UserID == "" {
			return ErrUnreachable
		}
		if malformed(d) {
			return ErrInvalid
		}
		c, err := candidateKey(ctx, tx, d.Candidate)
		if err != nil {
			return err
		}
		var other Candidate
		if d.Kind == KindMerge || d.Kind == KindNotSame {
			other, err = candidateKey(ctx, tx, d.Other)
			if err != nil {
				return err
			}
		}
		if c.Status == StatusMerged || (d.Kind == KindMerge || d.Kind == KindNotSame) && (other.Status == StatusMerged || other.ID == c.ID) {
			return ErrInvalid
		}
		keys := map[string]bool{}
		var split *records
		if d.Kind == KindSplit {
			split, err = candidateRecords(ctx, tx, c)
			if err != nil {
				return err
			}
			for _, key := range d.Keys {
				_, exists := split.links[key]
				if !exists {
					return ErrInvalid
				}
				keys[key] = true
			}
			founded := false
			for _, o := range split.owners {
				if o.Candidate == c.ID && o.Founded {
					founded = true
				}
			}
			if len(keys) == len(split.links) && !founded {
				return ErrInvalid
			}
		}
		out = Ruling{Kind: d.Kind, Candidate: d.Candidate, Keys: []string{}, Why: d.Why, UserID: d.UserID, UserEmail: d.UserEmail}
		ids := []string{c.ID}
		switch d.Kind {
		case KindMerge, KindNotSame:
			out.Other = d.Other
			ids = append(ids, d.Other)
		case KindSplit:
			id, e := s.mintKey(ctx, tx, CandidateIDPrefix)
			if e != nil {
				return e
			}
			out.Other = id
			out.Keys = sortedIDs(keys)
			ids = append(ids, id)
		}
		t := s.now()
		out.At = t
		if err = insertRecord(ctx, tx, "decisions", rulingColumns, out, nil); err != nil {
			return err
		}
		switch d.Kind {
		case KindMerge:
			if err = mergeCampaignUses(ctx, tx, c.ID, other.ID); err != nil {
				return err
			}
			if err = execSQL(ctx, tx, "UPDATE links SET candidate=?,reason=?,confidence=1 WHERE candidate=?", c.ID, LinkDecision, other.ID); err != nil {
				return err
			}
			if err = execSQL(ctx, tx, "UPDATE pages SET candidate=? WHERE candidate=?", c.ID, other.ID); err != nil {
				return err
			}
			if err = execSQL(ctx, tx, "UPDATE enrichments SET candidate=? WHERE candidate=?", c.ID, other.ID); err != nil {
				return err
			}
			if err = execSQL(ctx, tx, "DELETE FROM facts WHERE candidate=?", other.ID); err != nil {
				return err
			}
			other.Status = StatusMerged
			other.MergedInto = c.ID
			other.Updated = t
			if err = putCandidate(ctx, tx, other); err != nil {
				return err
			}
		case KindSplit:
			n := Candidate{ID: out.Other, Status: StatusActive, Names: []string{}, People: []sources.Sourced[sources.Person]{}, Cargo: []string{}, Created: t, Updated: t}
			if err = putCandidate(ctx, tx, n); err != nil {
				return err
			}
			for key := range keys {
				l := split.links[key]
				l.Candidate = out.Other
				l.Reason = LinkDecision
				l.Confidence = 1
				if err = putLink(ctx, tx, l); err != nil {
					return err
				}
			}
			pair := Ruling{Kind: KindNotSame, Candidate: c.ID, Other: out.Other, Keys: []string{}, Why: d.Why, UserID: d.UserID, UserEmail: d.UserEmail, At: t}
			if err = insertRecord(ctx, tx, "decisions", rulingColumns, pair, nil); err != nil {
				return err
			}
		}
		if d.Kind == KindMerge || d.Kind == KindSplit {
			for _, id := range ids {
				if err = s.refreshKey(ctx, tx, id, t); err != nil {
					return err
				}
			}
		}
		if d.Kind == KindMerge || d.Kind == KindSplit {
			for _, id := range ids {
				c, err := candidateKey(ctx, tx, id)
				if err != nil {
					return err
				}
				if c.Status != StatusActive {
					continue
				}
				for _, en := range enrichers.New(enrichers.Config{}).All() {
					if der, ok := en.(enrichers.Deriver); ok {
						if _, err = s.deriveCandidate(ctx, tx, c, der, true, t); err != nil {
							return err
						}
					}
				}
				if err = s.projectFacts(ctx, tx, id); err != nil {
					return err
				}
			}
		}
		if d.Kind != KindNotSame {
			if _, err = judgeKeys(ctx, tx, ids, t); err != nil {
				return err
			}
		}
		cs = []Candidate{}
		for _, id := range ids {
			c, err := candidateKey(ctx, tx, id)
			if err != nil {
				return err
			}
			cs = append(cs, c)
		}
		return nil
	}))
	if err != nil {
		return Ruling{}, nil, err
	}
	return out, cs, nil
}

func displayLinks(ls []Link) []Link {
	out := []Link{}
	for _, l := range ls {
		if l.Scraper != places.Name {
			out = append(out, l)
		}
	}
	if len(out) > 0 {
		return out
	}
	return ls
}
