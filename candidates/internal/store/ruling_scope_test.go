package store_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-A6J7-UP2I R-F7M3-BTV6
func TestRulingLeavesEveryUntouchedCandidateAsStored(t *testing.T) {
	for _, kind := range []string{store.KindExclude, store.KindInclude, store.KindNotSame, store.KindMerge, store.KindSplit} {
		t.Run(kind, func(t *testing.T) {
			f := regNew(t)
			a, b, c := regObs("a", "Shared Inc"), regObs("b", "Shared LLC"), regObs("c", "Shared Co")
			for _, o := range []*store.Observation{&a, &b, &c} {
				o.Reading.Estimate = &sources.Estimate{Source: o.Scraper, Low: 12345, Confidence: .7}
				o.Reading.Evidence = []sources.Evidence{{Name: sources.EvidenceOwnDomain, Source: o.Scraper, Weight: .2}}
			}
			_, ids := regAttach(t, f, "initial", a, b, c)
			r, _, err := f.st.Decide(regCtx, store.Ruling{Kind: store.KindSplit, Candidate: ids[0], Keys: []string{sources.SourceKey(c.Scraper, c.SourceID)}, Why: "separate businesses", UserID: "owner"})
			regNoErr(t, err)
			_, others := regAttach(t, f, "initial", regObs("other", "Different business"), regObs("unrelated", "Unrelated business"))
			_, _, err = f.st.JudgeAll(regCtx, "", 500, regTime(f))
			regNoErr(t, err)
			all := append([]string{ids[0], r.Other}, others...)
			deriveAll(t, f.st)
			for _, id := range all {
				_, err = f.st.AddEnrichments(regCtx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
				regNoErr(t, err)
			}
			// The separated candidate still shares a match key with a. Its stored
			// display is deliberately stale, a supported fixture that a refresh repairs.
			stale := "Owner-maintained display"
			regNoErr(t, f.d.Write(regCtx, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(regCtx, "UPDATE candidates SET name=? WHERE id=?", stale, r.Other); err != nil {
					return err
				}
				if kind == store.KindExclude || kind == store.KindInclude || kind == store.KindNotSame {
					_, err := tx.ExecContext(regCtx, "UPDATE candidates SET name=? WHERE id=?", "Stored named candidate", ids[0])
					return err
				}
				return nil
			}))
			regEqual(t, regCandidate(t, f, r.Other).Name, stale)
			regEqual(t, regLinks(t, f, r.Other)[0].Reading.NameKey, a.Reading.NameKey)
			beforeC := map[string]store.Candidate{}
			beforeL := map[string][]store.Link{}
			beforeA := map[string]store.Judgments{}
			beforeE := map[string][]sources.Estimate{}
			beforeFacts := map[string]store.Facts{}
			beforeResults := map[string][]store.Enrichment{}
			for _, id := range all {
				beforeFacts[id], err = f.st.GetFacts(regCtx, id)
				regNoErr(t, err)
				beforeResults[id], err = f.st.Enrichments(regCtx, id)
				regNoErr(t, err)
				beforeC[id] = regCandidate(t, f, id)
				beforeL[id] = regLinks(t, f, id)
				beforeA[id], err = f.st.GetJudgments(regCtx, id)
				regNoErr(t, err)
				beforeE[id], err = f.st.Estimates(regCtx, id)
				regNoErr(t, err)
			}
			f.now = f.now.Add(3 * time.Hour)
			f.calls = 0
			_, cs, err := f.st.Decide(regCtx, store.Ruling{Kind: kind, Candidate: ids[0], Other: others[0], Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "owner ruling", UserID: "owner"})
			regNoErr(t, err)
			regEqual(t, f.calls, 1)
			touched := map[string]bool{}
			for _, c := range cs {
				touched[c.ID] = true
			}
			for _, id := range all {
				if !touched[id] {
					facts, err := f.st.GetFacts(regCtx, id)
					regNoErr(t, err)
					regEqual(t, facts, beforeFacts[id])
					results, err := f.st.Enrichments(regCtx, id)
					regNoErr(t, err)
					regEqual(t, results, beforeResults[id])
				}
				if !touched[id] || kind == store.KindExclude || kind == store.KindInclude || kind == store.KindNotSame {
					regEqual(t, regCandidate(t, f, id), beforeC[id])
					regEqual(t, regLinks(t, f, id), beforeL[id])
				}
				if !touched[id] || kind == store.KindNotSame {
					got, err := f.st.GetJudgments(regCtx, id)
					regNoErr(t, err)
					regEqual(t, got, beforeA[id])
					es, err := f.st.Estimates(regCtx, id)
					regNoErr(t, err)
					regEqual(t, es, beforeE[id])
				}
			}
		})
	}
}

// R-FCHO-UWTY
func TestMovingRulingsDeriveEveryDisplayFieldAndStampOnlyChanges(t *testing.T) {
	for _, kind := range []string{store.KindSplit, store.KindMerge} {
		t.Run(kind, func(t *testing.T) {
			f := regNew(t)
			a, b := regObs("a", "First Inc"), regObs("b", "Second LLC")
			for i, o := range []*store.Observation{&a, &b} {
				o.Reading.Street = []string{"First street", "Second street"}[i]
				o.Reading.City = []string{"First city", "Second city"}[i]
				o.Reading.County = []string{"MN-001", "MN-002"}[i]
				o.Reading.Phone = []string{"111", "222"}[i]
				o.Reading.Email = []string{"a@example.test", "b@example.test"}[i]
				o.Reading.People = []sources.Person{{Name: o.Reading.Name, Role: "owner"}}
				o.Reading.Class = []string{sources.ClassForHire, sources.ClassPrivate}[i]
				o.Reading.Names = []string{o.Reading.Name + " alias"}
				o.Reading.Cargo = []string{[]string{"alpha", "beta"}[i]}
			}
			b.Reading.Filed = a.Reading.Filed.AddDate(0, 1, 0)
			if kind == store.KindSplit {
				b.Reading.NameKey = a.Reading.NameKey
			}
			_, ids := regAttach(t, f, "initial", a, b)
			original := regCandidate(t, f, ids[0])
			f.now = f.now.Add(time.Hour)
			f.calls = 0
			ruling := store.Ruling{Kind: kind, Candidate: ids[0], Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "move evidence", UserID: "owner"}
			if kind == store.KindMerge {
				ruling.Other = ids[1]
			}
			_, cs, err := f.st.Decide(regCtx, ruling)
			regNoErr(t, err)
			regEqual(t, f.calls, 1)
			display := func(c store.Candidate, o store.Observation, names, cargo []string) store.Candidate {
				rd := o.Reading
				return withProvenance(store.Candidate{ID: c.ID, Status: store.StatusActive, Name: rd.Name, Names: names, Street: rd.Street, City: rd.City, State: rd.State, Zip: rd.Zip, County: rd.County, Phone: rd.Phone, Email: rd.Email, People: sourcedPeople(rd.People, o.Scraper, o.SourceID), Class: rd.Class, Cargo: cargo, Created: c.Created, Updated: regTime(f)}, func() []store.Observation {
					if kind == store.KindMerge {
						return []store.Observation{b, a}
					}
					return []store.Observation{o}
				}()...)
			}
			if kind == store.KindSplit {
				regEqual(t, cs[0], display(cs[0], a, []string{a.Reading.Name, a.Reading.Names[0]}, a.Reading.Cargo))
				regEqual(t, cs[1], display(cs[1], b, []string{b.Reading.Name, b.Reading.Names[0]}, b.Reading.Cargo))
			} else {
				regEqual(t, cs[0], display(cs[0], b, []string{b.Reading.Name, b.Reading.Names[0], a.Reading.Name, a.Reading.Names[0]}, []string{"alpha", "beta"}))
			}
			for _, c := range cs {
				regEqual(t, regCandidate(t, f, c.ID), c)
			}
			if cs[0].Updated.Equal(original.Updated) {
				t.Fatal("changed display retained its earlier stamp")
			}
			// A later ingest of identical evidence returns the active candidate while
			// leaving its display and Updated unchanged.
			before := regCandidate(t, f, cs[0].ID)
			f.now = f.now.Add(time.Hour)
			regAttach(t, f, "unchanged", a)
			regEqual(t, regCandidate(t, f, cs[0].ID), before)
		})
	}
}
