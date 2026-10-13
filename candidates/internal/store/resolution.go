package store

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func match(a, b sources.Reading) bool {
	return a.NameKey != "" && a.State != "" && validZip(a.Zip) && b.NameKey == a.NameKey && b.State == a.State && b.Zip == a.Zip
}
func sortedIDs(m map[string]bool) []string {
	out := []string{}
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func refresh(r *records, t time.Time) {
	for id, c := range r.candidates {
		if c.Status != StatusActive {
			continue
		}
		ls := linksFor(r, id)
		if len(ls) == 0 {
			continue
		}
		old := c
		d := ls[0].Reading
		c.Name = d.Name
		c.Street = d.Street
		c.City = d.City
		c.State = d.State
		c.Zip = d.Zip
		c.County = d.County
		c.Phone = d.Phone
		c.Email = d.Email
		c.Class = d.Class
		c.People = append([]sources.Person{}, d.People...)
		c.Names = []string{}
		names := map[string]bool{}
		add := func(n string) {
			if n != "" && !names[n] {
				names[n] = true
				c.Names = append(c.Names, n)
			}
		}
		add(d.Name)
		cargo := map[string]bool{}
		for _, l := range ls {
			add(l.Reading.Name)
			for _, n := range l.Reading.Names {
				add(n)
			}
			for _, v := range l.Reading.Cargo {
				cargo[v] = true
			}
		}
		c.Cargo = sortedIDs(cargo)
		if !reflect.DeepEqual(old, c) {
			c.Updated = t
		}
		r.candidates[id] = c
	}
}

// Attach attaches a batch of readings atomically.
func (s *Store) Attach(ctx context.Context, run string, rebuild bool, obs []Observation) (Counts, []string, error) {
	var total Counts
	var ids []string
	err := s.transaction(ctx, true, func(r *records) error {
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
		for _, o := range obs {
			total.Rows++
			key := sources.SourceKey(o.Scraper, o.SourceID)
			l, ok := r.links[key]
			if ok {
				if !rebuild && l.Digest == o.Digest {
					total.Unchanged++
				} else {
					total.Changed++
					total.Linked++
					l.Digest = o.Digest
					l.Reading = o.Reading
				}
				l.Run = run
				l.LastSeen = t
				l.Gone = false
				r.links[key] = l
				touched[l.Candidate] = true
				refresh(r, t)
				continue
			}
			total.New++
			var chosen Candidate
			for _, existing := range r.links {
				c := r.candidates[existing.Candidate]
				if c.Status == StatusActive && match(o.Reading, existing.Reading) && (chosen.ID == "" || c.Created.Before(chosen.Created) || c.Created.Equal(chosen.Created) && c.ID < chosen.ID) {
					chosen = c
				}
			}
			reason := LinkMatchKey
			confidence := 0.8
			if chosen.ID == "" {
				id, e := s.mint(r, CandidateIDPrefix)
				if e != nil {
					return e
				}
				chosen = Candidate{ID: id, Status: StatusActive, Names: []string{}, People: []sources.Person{}, Cargo: []string{}, Created: t, Updated: t}
				r.candidates[id] = chosen
				reason = LinkSourceKey
				confidence = 1
				total.Created++
			} else {
				total.Linked++
			}
			r.links[key] = Link{Candidate: chosen.ID, Scraper: o.Scraper, SourceID: o.SourceID, Run: run, Digest: o.Digest, Reading: o.Reading, Reason: reason, Confidence: confidence, FirstSeen: t, LastSeen: t}
			touched[chosen.ID] = true
			refresh(r, t)
		}
		ids = sortedIDs(touched)
		return nil
	})
	if err != nil {
		return Counts{}, nil, err
	}
	return total, ids, nil
}

// MarkGone marks rows absent from a scraper’s latest run.
func (s *Store) MarkGone(ctx context.Context, scraper, run string) (Counts, []string, error) {
	var c Counts
	var out []string
	err := s.transaction(ctx, true, func(r *records) error {
		if scraper == "" || run == "" {
			return ErrUnreachable
		}
		t := s.now()
		ids := map[string]bool{}
		for key, l := range r.links {
			if l.Scraper == scraper && l.Run != run && !l.Gone {
				l.Gone = true
				r.links[key] = l
				c.Gone++
				ids[l.Candidate] = true
			}
		}
		refresh(r, t)
		out = sortedIDs(ids)
		return nil
	})
	if err != nil {
		return Counts{}, nil, err
	}
	return c, out, nil
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
	err := s.transaction(ctx, true, func(r *records) error {
		if d.UserID == "" {
			return ErrUnreachable
		}
		if malformed(d) {
			return ErrInvalid
		}
		c, ok := r.candidates[d.Candidate]
		if !ok {
			return ErrNotFound
		}
		var other Candidate
		if d.Kind == KindMerge || d.Kind == KindNotSame {
			other, ok = r.candidates[d.Other]
			if !ok {
				return ErrNotFound
			}
		}
		if c.Status == StatusMerged || (d.Kind == KindMerge || d.Kind == KindNotSame) && (other.Status == StatusMerged || other.ID == c.ID) {
			return ErrInvalid
		}
		keys := map[string]bool{}
		if d.Kind == KindSplit {
			for _, key := range d.Keys {
				l, exists := r.links[key]
				if !exists || l.Candidate != c.ID {
					return ErrInvalid
				}
				keys[key] = true
			}
			if len(keys) == len(linksFor(r, c.ID)) {
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
			id, e := s.mint(r, CandidateIDPrefix)
			if e != nil {
				return e
			}
			out.Other = id
			out.Keys = sortedIDs(keys)
			ids = append(ids, id)
		}
		t := s.now()
		out.At = t
		r.rulings = append(r.rulings, out)
		switch d.Kind {
		case KindMerge:
			for key, l := range r.links {
				if l.Candidate == other.ID {
					l.Candidate = c.ID
					l.Reason = LinkDecision
					l.Confidence = 1
					r.links[key] = l
				}
			}
			other.Status = StatusMerged
			other.MergedInto = c.ID
			other.Updated = t
			r.candidates[other.ID] = other
		case KindSplit:
			r.candidates[out.Other] = Candidate{ID: out.Other, Status: StatusActive, Names: []string{}, People: []sources.Person{}, Cargo: []string{}, Created: t, Updated: t}
			for key := range keys {
				l := r.links[key]
				l.Candidate = out.Other
				l.Reason = LinkDecision
				l.Confidence = 1
				r.links[key] = l
			}
			r.rulings = append(r.rulings, Ruling{Kind: KindNotSame, Candidate: c.ID, Other: out.Other, Keys: []string{}, Why: d.Why, UserID: d.UserID, UserEmail: d.UserEmail, At: t})
		}
		refresh(r, t)
		if d.Kind != KindNotSame {
			rescore(r, ids, t)
		}
		cs = []Candidate{}
		for _, id := range ids {
			cs = append(cs, r.candidates[id])
		}
		return nil
	})
	if err != nil {
		return Ruling{}, nil, err
	}
	return out, cs, nil
}
