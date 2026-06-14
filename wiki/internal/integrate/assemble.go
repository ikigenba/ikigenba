package integrate

import (
	"context"
	"fmt"
)

// Assembler is the document pass's manifest producer (P6b2): it consumes P6b's
// per-subject Resolution outcomes, runs match on the shortlists, and assembles the
// in-memory Manifest the document pass hands downstream (the type frozen in P4 —
// never redefined here). It is the Manifest's FIRST real producer; its in-memory,
// never-persisted nature (the run id is its durable identity) is P4's contract.
//
// Assemble fills, per subject: the resolved SubjectID (resolved arm → the found
// id; create arm → a freshly minted ULID; shortlist arm → match's same id, or a
// fresh ULID on no_match), the TargetPage (one page per subject = the subject id,
// design §4.1), and the generalized {text, cites[]} claims extract already stamped
// with the one causing-inbox-row id. The per-subject BaseVersion is part of the
// pinned type but is filled at merge-read time (P7a), NOT here — Assemble leaves it
// unset, per P4's field obligations. Assemble also records the manifest's DupPairs:
// match's candidate-pair side channel plus every many-ids pair P6b handed up, each
// in canonical order (the field P7a's commit reads to write dup_flags).
type Assembler struct {
	matcher *Matcher
	newID   func() string
}

// NewAssembler builds an Assembler over the match stage and a ULID minter. The
// minter is injectable so tests get deterministic ids; production passes the
// suite-standard ULID generator.
func NewAssembler(matcher *Matcher, newID func() string) *Assembler {
	if newID == nil {
		newID = newULID
	}
	return &Assembler{matcher: matcher, newID: newID}
}

// Assemble turns P6b's resolution outcomes into the document pass's Manifest. The
// input resolutions are in extracted-subject order; the output manifest's subjects
// preserve that order (so the manifest is deterministic for a fixed registry state
// and a fixed match verdict).
func (a *Assembler) Assemble(ctx context.Context, resolutions []Resolution) (*Manifest, error) {
	m := &Manifest{Subjects: make([]Subject, 0, len(resolutions))}
	seenPairs := make(map[DupPair]struct{})

	addPairs := func(pairs []DupPair) {
		for _, p := range pairs {
			if _, ok := seenPairs[p]; ok {
				continue
			}
			seenPairs[p] = struct{}{}
			m.DupPairs = append(m.DupPairs, p)
		}
	}

	for i := range resolutions {
		res := resolutions[i]
		subj := res.Subject // carries the extracted fields + claims (cites already stamped)

		switch res.Outcome {
		case OutcomeResolved:
			subj.SubjectID = res.SubjectID

		case OutcomeCreate:
			subj.SubjectID = a.newID()

		case OutcomeShortlist:
			// The many-ids arm already surfaced its colliding pairs; fold them in
			// regardless of the verdict (the collision is a dup signal on its own).
			addPairs(res.DupPairs)

			verdict, err := a.matcher.Match(ctx, subj, res.Candidates)
			if err != nil {
				return nil, fmt.Errorf("assemble: match subject %d (%q): %w", i, subj.Name, err)
			}
			addPairs(verdict.DupPairs)
			if verdict.Same != "" {
				subj.SubjectID = verdict.Same
			} else {
				// Doubt is no_match → a new subject (false-split is cheap and
				// lint-repairable; design §4.3).
				subj.SubjectID = a.newID()
			}

		default:
			return nil, fmt.Errorf("assemble: subject %d (%q): unknown outcome %v", i, subj.Name, res.Outcome)
		}

		// One page per subject (design §4.1): the target page IS the subject id, so
		// the manifest's write set is exactly the subjects' target pages.
		subj.TargetPage = subj.SubjectID
		m.Subjects = append(m.Subjects, subj)
	}

	return m, nil
}
