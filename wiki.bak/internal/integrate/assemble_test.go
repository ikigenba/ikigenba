package integrate

import (
	"context"
	"testing"

	"wiki/internal/config"
	"wiki/internal/page"
)

// seqMinter is a deterministic ULID stand-in for assemble tests: it hands out
// fixed ids in order so the manifest is assertable.
func seqMinter(ids ...string) func() string {
	i := 0
	return func() string {
		id := ids[i%len(ids)]
		i++
		return id
	}
}

func newTestAssembler(caller structuredCaller, reg excerptReader, mint func() string) *Assembler {
	site := config.CallSite{Name: "match", Prompt: config.DefaultMatchPrompt, Model: "claude-haiku-4-5"}
	m := NewMatcher(caller, reg, site, 600)
	return NewAssembler(m, mint)
}

// TestAssembleResolvedAndCreate covers the two zero-LLM arms: a resolved subject
// gets its found id, a create subject gets a fresh minted id; both get a target
// page == subject id, and match is never called.
func TestAssembleResolvedAndCreate(t *testing.T) {
	mock := &mockCaller{err: context.Canceled} // must NOT be called
	a := newTestAssembler(mock, &fakeExcerptReader{}, seqMinter("MINTED1"))

	resolutions := []Resolution{
		{Subject: Subject{Type: TypeEntity, Name: "Acme", Claims: []Claim{{Text: "Acme ships widgets."}}}, Outcome: OutcomeResolved, SubjectID: "FOUND-A"},
		{Subject: Subject{Type: TypeEntity, Name: "Brandnew", Claims: []Claim{{Text: "Brandnew was founded in 2020."}}}, Outcome: OutcomeCreate},
	}
	m, err := a.Assemble(context.Background(), resolutions)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(m.Subjects) != 2 {
		t.Fatalf("got %d subjects, want 2", len(m.Subjects))
	}
	if m.Subjects[0].SubjectID != "FOUND-A" || m.Subjects[0].TargetPage != "FOUND-A" {
		t.Errorf("resolved subject = %+v, want id+page FOUND-A", m.Subjects[0])
	}
	if m.Subjects[1].SubjectID != "MINTED1" || m.Subjects[1].TargetPage != "MINTED1" {
		t.Errorf("create subject = %+v, want id+page MINTED1", m.Subjects[1])
	}
	// BaseVersion stays unset (filled at merge-read time in P7a).
	for _, s := range m.Subjects {
		if s.BaseVersion != 0 {
			t.Errorf("BaseVersion must be unset in P6b2, got %d for %q", s.BaseVersion, s.Name)
		}
	}
	// Write set == the subjects' target pages, exactly.
	ws := m.WriteSet()
	if len(ws) != 2 {
		t.Errorf("write set = %v, want 2 pages", ws)
	}
}

// TestAssembleShortlistSame: match says same → the subject takes the matched id.
func TestAssembleShortlistSame(t *testing.T) {
	mock := &mockCaller{resp: `{"verdict":{"same":"01A"},"dup_pairs":[]}`}
	reg := &fakeExcerptReader{excerpts: map[string]page.Excerpt{
		"01A": {SubjectID: "01A", CanonicalName: "Acme Corp", Body: "Acme Corp ships widgets."},
	}}
	a := newTestAssembler(mock, reg, seqMinter("SHOULD-NOT-MINT"))

	resolutions := []Resolution{{
		Subject:    Subject{Type: TypeEntity, Name: "Acme", Claims: []Claim{{Text: "Acme ships widgets."}}},
		Outcome:    OutcomeShortlist,
		Candidates: []page.Candidate{{SubjectID: "01A", Type: TypeEntity}},
	}}
	m, err := a.Assemble(context.Background(), resolutions)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if m.Subjects[0].SubjectID != "01A" {
		t.Errorf("shortlist+same id = %q, want 01A", m.Subjects[0].SubjectID)
	}
}

// TestAssembleShortlistNoMatchMints: match says no_match → a fresh id is minted
// (false-split is cheap, design §4.3).
func TestAssembleShortlistNoMatchMints(t *testing.T) {
	mock := &mockCaller{resp: `{"verdict":{"no_match":true},"dup_pairs":[]}`}
	reg := &fakeExcerptReader{excerpts: map[string]page.Excerpt{
		"01A": {SubjectID: "01A", CanonicalName: "Other Acme"},
	}}
	a := newTestAssembler(mock, reg, seqMinter("MINTED2"))

	resolutions := []Resolution{{
		Subject:    Subject{Type: TypeEntity, Name: "Acme", Claims: []Claim{{Text: "x."}}},
		Outcome:    OutcomeShortlist,
		Candidates: []page.Candidate{{SubjectID: "01A", Type: TypeEntity}},
	}}
	m, err := a.Assemble(context.Background(), resolutions)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if m.Subjects[0].SubjectID != "MINTED2" {
		t.Errorf("no_match id = %q, want freshly minted MINTED2", m.Subjects[0].SubjectID)
	}
}

// TestAssembleDupPairs: both the many-ids pairs handed up from P6b AND match's
// side-channel pairs land in the manifest's DupPairs, de-duped.
func TestAssembleDupPairs(t *testing.T) {
	mock := &mockCaller{resp: `{"verdict":{"no_match":true},"dup_pairs":[{"a":"01C","b":"01D"}]}`}
	reg := &fakeExcerptReader{}
	a := newTestAssembler(mock, reg, seqMinter("MINTED3"))

	resolutions := []Resolution{{
		Subject:    Subject{Type: TypeEntity, Name: "Bridge", Claims: []Claim{{Text: "x."}}},
		Outcome:    OutcomeShortlist,
		Candidates: []page.Candidate{{SubjectID: "01C"}, {SubjectID: "01D"}},
		DupPairs:   []DupPair{{SubjectA: "01C", SubjectB: "01D"}},
	}}
	m, err := a.Assemble(context.Background(), resolutions)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	// The many-ids pair and match's side channel name the same pair → de-duped to one.
	if len(m.DupPairs) != 1 || m.DupPairs[0] != (DupPair{SubjectA: "01C", SubjectB: "01D"}) {
		t.Errorf("DupPairs = %v, want single canonical [01C,01D]", m.DupPairs)
	}
}

// TestAssemblePreservesOrderAndClaims: the manifest's subjects preserve input
// order and the extract-stamped claim cites survive.
func TestAssemblePreservesOrderAndClaims(t *testing.T) {
	a := newTestAssembler(&mockCaller{}, &fakeExcerptReader{}, seqMinter("M1", "M2"))
	resolutions := []Resolution{
		{Subject: Subject{Name: "first", Claims: []Claim{{Text: "a", Cites: []string{"01HXINBOX"}}}}, Outcome: OutcomeCreate},
		{Subject: Subject{Name: "second"}, Outcome: OutcomeCreate},
	}
	m, err := a.Assemble(context.Background(), resolutions)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if m.Subjects[0].Name != "first" || m.Subjects[1].Name != "second" {
		t.Errorf("order not preserved: %+v", m.Subjects)
	}
	if len(m.Subjects[0].Claims) != 1 || m.Subjects[0].Claims[0].Cites[0] != "01HXINBOX" {
		t.Errorf("claim cites not preserved: %+v", m.Subjects[0].Claims)
	}
}
