package lint

import (
	"context"
	"fmt"

	"wiki/internal/config"
	"wiki/internal/integrate"
	"wiki/internal/page"
)

// JobName is lint-dups' stable job name (runs.job; the cron/lint TryLock key — at
// most one in-flight lint-dups run, design §6).
const JobName = "lint-dups"

// dupStore is the slice of *page.Store lint-dups needs: the dup-queue work list,
// the per-subject judge evidence, and the three terminal writes (stamp-judged,
// dismiss, the one-transaction subject merge). Narrowed to an interface so the job
// is unit-testable with a fake.
type dupStore interface {
	OpenDupPairs(ctx context.Context) ([]page.DupPair, error)
	ReadDupSubject(ctx context.Context, subjectID string) (page.DupSubject, error)
	StampJudged(ctx context.Context, a, b string, versionA, versionB int) error
	DismissDup(ctx context.Context, a, b, run string) error
	MergeSubjects(ctx context.Context, m page.MergePlan) error
}

// Job is the lint-dups maintenance job (design §6). It is constructed once at the
// composition root with the dup-judge + fold call-site triples and the page store,
// and implements the P4 integrate.Integrator interface so the worker spine selects
// and runs it exactly like any other job (one runs row per attempt, failure policy
// verbatim). It does its OWN per-pair transactions (page.MergeSubjects), so its
// Integrate returns an EMPTY manifest — the worker's end-of-run Commit is then a
// harmless no-op stamp (the real work already landed, per-pair).
type Job struct {
	caller    caller
	store     dupStore
	judgeSite config.CallSite
	foldSite  config.CallSite
}

// NewDupsJob builds the lint-dups job over a structured caller, the page store, and
// the two config-injected call-site triples (judge + fold). The triples are
// injected — the calls never read a constant or env (design §10 / obligation 1).
func NewDupsJob(c caller, store dupStore, judgeSite, foldSite config.CallSite) *Job {
	return &Job{caller: c, store: store, judgeSite: judgeSite, foldSite: foldSite}
}

// Job is the integrate.Integrator job name (runs.job). lint-dups runs under it.
func (j *Job) Job() string { return JobName }

// Integrate runs one lint-dups sweep (design §6): one run per trigger sweeping the
// eligible open pairs SERIALLY, ONE TRANSACTION PER PAIR (per-pair recovery via the
// queue itself — a failure on pair k leaves pairs <k settled and pair k open for a
// later run). The returned manifest is empty: lint-dups owns its writes per-pair,
// so the worker's end-of-run Commit is a no-op stamp. unit identifies the causing
// trigger row (runs.caused_by); lint takes no inbox payload.
func (j *Job) Integrate(ctx context.Context, unit integrate.Unit) (*integrate.Manifest, error) {
	runID := unit.CausedBy // provenance for the dup-row run_id stamp (the lint run's causing row)
	pairs, err := j.store.OpenDupPairs(ctx)
	if err != nil {
		return nil, fmt.Errorf("lint-dups: work list: %w", err)
	}
	for _, p := range pairs {
		if err := j.handlePair(ctx, runID, p); err != nil {
			// One pair's failure fails the run cleanly (its causing trigger row stays
			// pending; the failure policy applies). Pairs already settled this run are
			// durable (per-pair transactions); the queue retries the rest.
			return nil, fmt.Errorf("lint-dups: pair (%s,%s): %w", p.SubjectA, p.SubjectB, err)
		}
	}
	return &integrate.Manifest{}, nil
}

// handlePair runs the judge → (merge|dismiss|stamp) decision for one open pair in
// its own transaction(s) (design §6). A flag is evidence, never a verdict, so the
// judge decides; older ULID wins MECHANICALLY on a merge (the judge picks only the
// canonical name).
func (j *Job) handlePair(ctx context.Context, runID string, p page.DupPair) error {
	a, err := j.store.ReadDupSubject(ctx, p.SubjectA)
	if err != nil {
		return err
	}
	b, err := j.store.ReadDupSubject(ctx, p.SubjectB)
	if err != nil {
		return err
	}

	res, err := j.Judge(ctx, a, b)
	if err != nil {
		return err
	}

	switch res.Verdict {
	case VerdictDismiss:
		// Permanent: row → dismissed (blocks re-flagging).
		return j.store.DismissDup(ctx, p.SubjectA, p.SubjectB, runID)

	case VerdictCantTell:
		// The only write: stamp the examined page versions so the re-judge gate skips
		// this pair until a page advances (design §6).
		return j.store.StampJudged(ctx, p.SubjectA, p.SubjectB, a.Version, b.Version)

	case VerdictMerge:
		return j.merge(ctx, runID, res.CanonicalName, a, b)
	}
	return fmt.Errorf("lint-dups: unreachable verdict %q", res.Verdict)
}

// merge applies a confirmed-same pair (design §6): the winner is the OLDER ULID
// (smaller id — pairs are stored canonically, so SubjectA < SubjectB and SubjectA
// is the older/winner); the fold produces the one merged body (inheriting the §6.1
// gate); the subject merge lands in ONE transaction (page.MergeSubjects). The judge
// picks the canonical NAME only; which id survives is mechanical.
func (j *Job) merge(ctx context.Context, runID, canonicalName string, a, b page.DupSubject) error {
	// Older ULID wins. ULIDs sort lexicographically by mint time, and the pair is
	// stored with subject_a < subject_b, so subject A (a) is the older winner.
	winner, loser := a, b
	if loser.SubjectID < winner.SubjectID {
		winner, loser = loser, winner
	}

	fold, err := j.Fold(ctx, canonicalName, winner, loser)
	if err != nil {
		return err
	}
	title := fold.Title
	if title == "" {
		title = canonicalName
	}
	return j.store.MergeSubjects(ctx, page.MergePlan{
		Winner:        winner.SubjectID,
		Loser:         loser.SubjectID,
		CanonicalName: canonicalName,
		Title:         title,
		Body:          fold.Body,
		Run:           runID,
	})
}
