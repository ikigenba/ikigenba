package store_test

import (
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func keyedObservation(id, ein string, keys ...string) store.Observation {
	o := regObs(id, id)
	o.Reading.EINKey = ein
	o.Reading.NameKeys = keys
	if len(keys) > 0 {
		o.Reading.NameKey = keys[0]
	} else {
		o.Reading.NameKey = ""
	}
	return o
}

func assertKeyAttach(t *testing.T, f *regFixture, o store.Observation, id, reason string, confidence float64) {
	t.Helper()
	n, ids := regAttach(t, f, "key-run", o)
	regEqual(t, n, store.Counts{Rows: 1, New: 1, Linked: 1})
	regEqual(t, ids, []string{id})
	links := regLinks(t, f, id)
	for _, l := range links {
		if l.Scraper == o.Scraper && l.SourceID == o.SourceID {
			regEqual(t, l, store.Link{Candidate: id, Scraper: o.Scraper, SourceID: o.SourceID, Run: "key-run", Digest: o.Digest, Reading: o.Reading, Reason: reason, Confidence: confidence, FirstSeen: regTime(f), LastSeen: regTime(f)})
			return
		}
	}
	t.Fatal("attached link missing")
}

// R-ILGO-Z84K R-IMOL-CZV9 R-INWH-QRLY R-106L-DY5P
func TestLinkKeysOrderPlaceGoneAndReopen(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "reopen"}[reopen], func(t *testing.T) {
			f := regNew(t)
			older := keyedObservation("older", "111111111", "OLD", "SHARED")
			_, oldIDs := regAttach(t, f, "initial", older)
			f.now = f.now.Add(time.Hour)
			newer := keyedObservation("newer", "222222222", "NEW", "ALIAS")
			_, newIDs := regAttach(t, f, "initial", newer)
			if reopen {
				f.reopen(t)
			}
			byEIN := keyedObservation("ein", "222222222", "OLD")
			byEIN.Reading.Zip = "99999"
			assertKeyAttach(t, f, byEIN, newIDs[0], store.LinkEIN, .9)
			assertKeyAttach(t, f, keyedObservation("ordered", "333333333", "ALIAS", "OLD"), newIDs[0], store.LinkMatchKey, .8)
			assertKeyAttach(t, f, keyedObservation("later-name", "444444444", "UNHELD", "SHARED"), oldIDs[0], store.LinkMatchKey, .8)
			_, _, err := f.st.MarkGone(regCtx, "fmcsa", "absent-run", "", 500)
			regNoErr(t, err)
			assertKeyAttach(t, f, keyedObservation("gone-ein", "111111111", "OTHER"), oldIDs[0], store.LinkEIN, .9)
			assertKeyAttach(t, f, keyedObservation("gone-name", "", "ALIAS"), newIDs[0], store.LinkMatchKey, .8)
			for i, place := range []struct{ state, zip string }{{"MN", "99998"}, {"WI", "55072"}, {"", "55072"}, {"MN", "5507x"}} {
				o := keyedObservation(string(rune('a'+i))+"place", "", "SHARED")
				o.Reading.State = place.state
				o.Reading.Zip = place.zip
				n, ids := regAttach(t, f, "create", o)
				regEqual(t, n, store.Counts{Rows: 1, New: 1, Created: 1})
				regEqual(t, len(ids), 1)
				if ids[0] == oldIDs[0] || ids[0] == newIDs[0] {
					t.Fatal("matched a different place")
				}
			}
		})
	}
}

// R-IP4E-4JCN
func TestLinkKeysLegacyFallbackAndReplacement(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "digest", true: "forced"}[force], func(t *testing.T) {
			f := regNew(t)
			legacy := regObs("legacy", "Legacy business")
			_, ids := regAttach(t, f, "initial", legacy)
			assertKeyAttach(t, f, keyedObservation("legacy-alias", "111111111", "UNKNOWN", legacy.Reading.NameKey), ids[0], store.LinkMatchKey, .8)
			f.reopen(t)
			assertKeyAttach(t, f, keyedObservation("legacy-reopen", "222222222", "UNHELD", legacy.Reading.NameKey), ids[0], store.LinkMatchKey, .8)
			original := keyedObservation("replace", "333333333", "A", "B")
			_, other := regAttach(t, f, "initial", original)
			next := keyedObservation("replace", "444444444", "A")
			if !force {
				next.Digest = "new-digest"
			}
			_, _, err := f.st.Attach(regCtx, "update", force, []store.Observation{next})
			regNoErr(t, err)
			f.reopen(t)
			for _, o := range []store.Observation{keyedObservation("old-ein", "333333333", "REMOVED EIN ONLY"), keyedObservation("old-alias", "", "B")} {
				n, newIDs := regAttach(t, f, "after", o)
				regEqual(t, n, store.Counts{Rows: 1, New: 1, Created: 1})
				if newIDs[0] == other[0] {
					t.Fatal("retained removed key")
				}
			}
			assertKeyAttach(t, f, keyedObservation("new-ein", "444444444", "UNHELD"), other[0], store.LinkEIN, .9)
		})
	}
}

// R-0YYP-06F0 R-RGR7-9XKL R-ILGO-Z84K R-IMOL-CZV9
func TestLinkKeysMoveWithRulingsAndNotSameDoesNotVeto(t *testing.T) {
	f := regNew(t)
	a := keyedObservation("a", "111111111", "A", "ALIAS")
	b := keyedObservation("b", "111111111", "A", "ALIAS")
	_, ids := regAttach(t, f, "initial", a, b)
	f.now = f.now.Add(time.Hour)
	r, _, err := f.st.Decide(regCtx, store.Ruling{Kind: store.KindSplit, Candidate: ids[0], Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "separate", UserID: "owner"})
	regNoErr(t, err)
	assertKeyAttach(t, f, keyedObservation("oldest-ein", "111111111", "OTHER"), ids[0], store.LinkEIN, .9)
	assertKeyAttach(t, f, keyedObservation("oldest-name", "", "ALIAS"), ids[0], store.LinkMatchKey, .8)
	// Remove the original's held keys, so the split's new candidate owns both.
	for _, id := range []string{"a", "oldest-ein", "oldest-name"} {
		o := keyedObservation(id, "", "REPLACED")
		o.Digest = "replacement"
		_, _, err = f.st.Attach(regCtx, "rewrite", true, []store.Observation{o})
		regNoErr(t, err)
	}
	assertKeyAttach(t, f, keyedObservation("split-ein", "111111111", "OTHER"), r.Other, store.LinkEIN, .9)
	assertKeyAttach(t, f, keyedObservation("split-name", "", "ALIAS"), r.Other, store.LinkMatchKey, .8)
	_, _, err = f.st.Decide(regCtx, store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: r.Other, Why: "combine", UserID: "owner"})
	regNoErr(t, err)
	f.reopen(t)
	assertKeyAttach(t, f, keyedObservation("merged-ein", "111111111", "OTHER"), ids[0], store.LinkEIN, .9)
	assertKeyAttach(t, f, keyedObservation("merged-name", "", "ALIAS"), ids[0], store.LinkMatchKey, .8)
}

// R-IHSZ-TWWH R-IMOL-CZV9
func TestLinkKeysIncomingOrderAndHeldPrimary(t *testing.T) {
	f := regNew(t)
	held := keyedObservation("held", "", "ALIAS")
	held.Reading.NameKey = "PRIMARY"
	_, ids := regAttach(t, f, "initial", held)
	assertKeyAttach(t, f, keyedObservation("primary", "", "PRIMARY"), ids[0], store.LinkMatchKey, .8)
	fallback := keyedObservation("empty-elements", "", "", "")
	fallback.Reading.NameKey = "PRIMARY"
	assertKeyAttach(t, f, fallback, ids[0], store.LinkMatchKey, .8)
	incoming := keyedObservation("ignore-primary", "", "UNHELD")
	incoming.Reading.NameKey = "PRIMARY"
	n, newIDs := regAttach(t, f, "initial", incoming)
	regEqual(t, n, store.Counts{Rows: 1, New: 1, Created: 1})
	if newIDs[0] == ids[0] {
		t.Fatal("used incoming primary despite a nonempty ordered key")
	}
}
