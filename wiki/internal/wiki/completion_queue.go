package wiki

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"wiki/internal/extract"
	"wiki/internal/llm"

	matchstage "wiki/internal/match"
)

// OutcomeClass describes the durable disposition of one terminal completion.
type OutcomeClass string

const (
	Applied          OutcomeClass = "applied"
	Discarded        OutcomeClass = "discarded"
	JobFailedOutcome OutcomeClass = "job_failed"
	Deferred         OutcomeClass = "deferred"
)

// Outcome is the disposition of one completion. Err is reporting data, never
// an instruction to stop draining the inbox.
type Outcome struct {
	Class  OutcomeClass
	Reason string
	Err    error
}

// Drain summarizes one inbox tick.
type Drain struct {
	Seen, Applied, Discarded, JobsFailed, Deferred int
	Errs                                           []error
	NoProgress                                     bool
}

const maxConsecutiveDeferrals = 3

type pipelineEnvelope struct {
	JobID   string `json:"job_id"`
	Stage   string `json:"stage"`
	Subject string `json:"subject,omitempty"`
	Units   int    `json:"units,omitempty"`
}

type stagedIntegration struct {
	NewSubjects  []stagedSubject `json:"new_subjects"`
	Claims       []stagedClaim   `json:"claims"`
	Pages        []stagedPage    `json:"pages"`
	MatchUnits   []string        `json:"match_units,omitempty"`
	CompileTotal int             `json:"compile_total,omitempty"`
}

type stagedSubject struct {
	NormName string `json:"norm_name"`
	Name     string `json:"name"`
	Type     string `json:"type"`
}

type stagedClaim struct {
	ID       string `json:"id"`
	NormName string `json:"norm_name"`
	JobID    string `json:"job_id"`
	Body     string `json:"body"`
	Kind     string `json:"kind,omitempty"`
	Ordinal  int    `json:"ordinal,omitempty"`
}

type stagedEdge struct {
	Correction string `json:"correction"`
	Suppressed string `json:"suppressed"`
}

type stagedPage struct {
	Subject stagedSubject `json:"subject"`
	Claims  []stagedClaim `json:"claims,omitempty"`
	Delete  bool          `json:"delete,omitempty"`
}

type stagedCompiledPage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func stageSubject(subject Subject) stagedSubject {
	return stagedSubject{NormName: subject.NormName, Name: subject.Name, Type: subject.Type}
}

func (s stagedSubject) subject(id string) Subject {
	return Subject{ID: id, NormName: s.NormName, Name: s.Name, Type: s.Type}
}

func modelClaims(claims []stagedClaim, subjectID string) []Claim {
	out := make([]Claim, 0, len(claims))
	for _, claim := range claims {
		kind := claim.Kind
		if kind == "" {
			kind = ClaimKind
		}
		id := claim.ID
		if id == "" {
			id = ordinalRef(claim.Ordinal)
		}
		out = append(out, Claim{ID: id, SubjectID: subjectID, JobID: claim.JobID, Body: claim.Body, Kind: kind})
	}
	return out
}

func ordinalRef(ordinal int) string { return fmt.Sprintf("~ordinal:%d", ordinal) }

func (s *Service) handoffExtract(ctx context.Context, job Job) error {
	ctx, job = s.jobContext(ctx, job)
	ctx, err := s.contextForScope(ctx, job.Scope)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(pipelineEnvelope{JobID: job.ID, Stage: "extract"})
	if err != nil {
		return err
	}
	_, err = s.queue.Ensure(ctx, llm.Request{
		Key:     job.ID + "/extract/a1",
		Context: envelope,
		Site:    s.extractSite,
		Attr:    jobAttribution(job),
		Attempt: 1,
		Messages: []llm.Message{{Role: "user", Text: extract.Render(extract.DocumentHeader{
			Source: "mcp:ingest_text", Title: job.Title, Tags: job.Tags, ReceivedAt: job.ReceivedAt,
		}, job.SourceText)}},
	})
	if err != nil {
		return err
	}
	_, err = s.jobs.MarkPhase(ctx, job.ID, s.now().Add(jobProgressDeadline))
	return err
}

func (s *Service) contextForScope(ctx context.Context, scopeName string) (context.Context, error) {
	scope, err := s.scopes.Get(ctx, scopeName)
	if err != nil {
		return ctx, err
	}
	return llm.WithSystemComposer(ctx, func(base string) string {
		return ComposeSystem(base, scope.Instructions)
	}), nil
}

// ProcessInbox classifies every visible terminal completion unless ctx is
// canceled. Individual completion failures never stop the drain.
func (s *Service) ProcessInbox(ctx context.Context) (drain Drain) {
	if s == nil || s.queue == nil {
		return drain
	}
	defer func() { s.finalizeDrain(ctx, &drain) }()
	items, err := s.queue.Inbox(ctx)
	if err != nil {
		drain.Errs = append(drain.Errs, err)
		return drain
	}
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		if item.Status != "done" && item.Status != "failed" {
			continue
		}
		drain.Seen++
		started := time.Now()
		outcome := s.applyCompletion(ctx, item)
		if outcome.Class == Deferred {
			outcome = s.boundDeferral(ctx, item, outcome)
		} else {
			s.clearDeferral(item.ID)
		}
		if outcome.Class != Deferred {
			if err := s.queue.Ack(ctx, item.ID); err != nil {
				outcome = Outcome{Class: Deferred, Reason: "ack_unreachable", Err: err}
			} else {
				s.clearDeferral(item.ID)
			}
		}
		drain.record(outcome)
		var envelope pipelineEnvelope
		_ = json.Unmarshal(item.Context, &envelope)
		if s.logger != nil {
			s.logger.LogAttrs(ctx, slog.LevelInfo, "wiki completion decision", slog.String("job_id", envelope.JobID), slog.String("stage", envelope.Stage), slog.String("unit", envelope.Subject), slog.String("outcome", string(outcome.Class)), slog.String("reason", outcome.Reason), slog.Duration("duration", time.Since(started)))
		}
	}
	return drain
}

func (s *Service) finalizeDrain(ctx context.Context, drain *Drain) {
	s.mu.Lock()
	s.deferredItems = drain.Deferred
	if drain.Seen > 0 && drain.Applied == 0 && drain.Discarded == 0 && drain.JobsFailed == 0 {
		s.noProgressStreak++
		drain.NoProgress = true
		drain.Errs = append(drain.Errs, fmt.Errorf("wiki: drain saw %d items but made no progress", drain.Seen))
	} else {
		s.noProgressStreak = 0
	}
	streak := s.noProgressStreak
	s.mu.Unlock()
	if s.logger != nil {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "wiki completion drain", slog.Int("seen", drain.Seen), slog.Int("applied", drain.Applied), slog.Int("discarded", drain.Discarded), slog.Int("jobs_failed", drain.JobsFailed), slog.Int("deferred", drain.Deferred), slog.Int("no_progress_streak", streak))
	}
}

func (d *Drain) record(outcome Outcome) {
	switch outcome.Class {
	case Applied:
		d.Applied++
	case Discarded:
		d.Discarded++
	case JobFailedOutcome:
		d.JobsFailed++
	case Deferred:
		d.Deferred++
	}
	if outcome.Err != nil {
		d.Errs = append(d.Errs, outcome.Err)
	}
}

//nolint:gocyclo,nestif // Completion routing is a guarded state-machine transition kept together for auditability.
func (s *Service) applyCompletion(ctx context.Context, item llm.Item) Outcome {
	var envelope pipelineEnvelope
	if err := json.Unmarshal(item.Context, &envelope); err != nil || envelope.JobID == "" || (envelope.Stage != "extract" && envelope.Stage != "match" && envelope.Stage != "compile") {
		return Outcome{Class: Discarded, Reason: "non_pipeline"}
	}
	job, err := s.jobs.Get(ctx, envelope.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return Outcome{Class: Discarded, Reason: "unknown_job"}
	}
	if err != nil {
		return s.applyError(ctx, Job{ID: envelope.JobID}, "load_job", err)
	}
	if job.Status != JobWorking {
		return Outcome{Class: Discarded, Reason: "terminal_job"}
	}
	if !completionStageAllowed(envelope.Stage, job.Phase, item.Status) {
		return Outcome{Class: Discarded, Reason: "stale_stage"}
	}
	ctx, job = s.jobContext(ctx, job)
	ctx, err = s.contextForScope(ctx, job.Scope)
	if err != nil {
		return s.applyError(ctx, job, "load_scope", err)
	}
	if valid, err := s.pipelineStageMatches(ctx, job.ID, envelope); err != nil {
		return s.applyError(ctx, job, "staged_payload", err)
	} else if !valid {
		if envelope.Stage == "compile" {
			_, found, loadErr := s.loadStagedIntegration(ctx, job.ID)
			if loadErr != nil {
				return s.applyError(ctx, job, "corrupt_staged_payload", loadErr)
			}
			if !found {
				return s.failCompletion(ctx, job, "missing_staging", errors.New("extract staging row is missing"))
			}
		}
		return Outcome{Class: Discarded, Reason: "stale_units"}
	}
	if item.Status == "failed" {
		return s.failCompletion(ctx, job, "model_failure", errors.New(item.Error))
	}
	switch envelope.Stage {
	case "extract":
		return s.applyExtract(ctx, job, item)
	case "match":
		return s.applyMatch(ctx, job, envelope, item)
	case "compile":
		return s.applyCompile(ctx, job, envelope, item)
	default:
		return Outcome{Class: Discarded, Reason: "non_pipeline"}
	}
}

func (s *Service) applyError(ctx context.Context, job Job, reason string, err error) Outcome {
	if isTransientCompletionError(err) {
		return Outcome{Class: Deferred, Reason: reason, Err: err}
	}
	return s.failCompletion(ctx, job, reason, err)
}

func (s *Service) failCompletion(ctx context.Context, job Job, reason string, cause error) Outcome {
	if job.ID == "" {
		return Outcome{Class: JobFailedOutcome, Reason: reason, Err: cause}
	}
	message := fmt.Sprintf("%s: %v; rerun the job after correcting the cause", strings.ReplaceAll(reason, "_", " "), cause)
	finished, err := s.jobs.FailWaiting(ctx, job.ID, s.now(), message)
	if err != nil {
		// The item cannot be acked until the failed state and its cleanup are
		// durable, regardless of whether the write error itself looks transient.
		return Outcome{Class: Deferred, Reason: "fail_job", Err: errors.Join(cause, err)}
	}
	if finished {
		s.notify()
	}
	return Outcome{Class: JobFailedOutcome, Reason: reason, Err: cause}
}

func isTransientCompletionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "database is busy") ||
		(strings.Contains(text, "llm: prompts") && (strings.Contains(text, "returned 5") || strings.Contains(text, "connection")))
}

func (s *Service) boundDeferral(ctx context.Context, item llm.Item, outcome Outcome) Outcome {
	s.mu.Lock()
	if s.deferrals == nil {
		s.deferrals = make(map[string]int)
	}
	s.deferrals[item.ID]++
	count := s.deferrals[item.ID]
	s.mu.Unlock()
	if count < maxConsecutiveDeferrals || ctx.Err() != nil {
		return outcome
	}
	var envelope pipelineEnvelope
	if json.Unmarshal(item.Context, &envelope) != nil || envelope.JobID == "" {
		return Outcome{Class: JobFailedOutcome, Reason: "deferral_exhausted", Err: outcome.Err}
	}
	job, err := s.jobs.Get(ctx, envelope.JobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != JobWorking) {
		return Outcome{Class: Discarded, Reason: "deferral_exhausted", Err: outcome.Err}
	}
	if err != nil {
		return outcome
	}
	failed := s.failCompletion(ctx, job, "deferral_exhausted", outcome.Err)
	if failed.Class != Deferred {
		s.clearDeferral(item.ID)
	}
	return failed
}

func (s *Service) clearDeferral(id string) {
	s.mu.Lock()
	delete(s.deferrals, id)
	s.mu.Unlock()
}

func (s *Service) pipelineStageMatches(ctx context.Context, jobID string, envelope pipelineEnvelope) (bool, error) {
	if envelope.Stage == "extract" {
		return envelope.Subject == "" && envelope.Units == 0, nil
	}
	plan, found, err := s.loadStagedIntegration(ctx, jobID)
	if err != nil || !found {
		return false, err
	}
	if envelope.Stage == "match" {
		return containsString(plan.MatchUnits, envelope.Subject) && envelope.Units == len(plan.MatchUnits), nil
	}
	_, found = compilePage(plan, envelope.Subject)
	return found && envelope.Units == compileUnits(plan), nil
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func completionStageAllowed(completionStage, jobPhase, itemStatus string) bool {
	if completionStage == jobPhase || completionStage == "compile" && jobPhase == "match" {
		return true
	}
	if itemStatus != "done" {
		return false
	}
	return completionStage == "extract" && (jobPhase == "match" || jobPhase == "compile") ||
		completionStage == "match" && jobPhase == "compile"
}

//nolint:gocyclo // Extraction handling is a linear set of protocol and persistence guards.
func (s *Service) applyExtract(ctx context.Context, job Job, item llm.Item) Outcome {
	plan, found, err := s.loadStagedIntegration(ctx, job.ID)
	if err != nil {
		return s.applyError(ctx, job, "corrupt_staged_payload", err)
	}
	if !found {
		var response struct {
			Subjects []extract.ExtractedSubject `json:"subjects"`
		}
		if err := json.Unmarshal(item.Result, &response); err != nil {
			return s.correctOrFail(ctx, job, item, "extract", "", 0, err)
		}
		if err := extract.Validate(response.Subjects); err != nil {
			return s.correctOrFail(ctx, job, item, "extract", "", 0, err)
		}
		plan, err = s.stageExtractPlan(ctx, job, response.Subjects)
		if err != nil {
			return s.applyError(ctx, job, "stage_extract", err)
		}
	}
	for _, normName := range plan.MatchUnits {
		staged, err := s.matchUnitStaged(ctx, job.ID, normName)
		if err != nil {
			return s.applyError(ctx, job, "read_match_staging", err)
		}
		if staged {
			continue
		}
		if err := s.ensureMatch(ctx, job, plan, normName); err != nil {
			return s.applyError(ctx, job, "ensure_match", err)
		}
	}
	for _, page := range plan.Pages {
		if page.Delete || containsString(plan.MatchUnits, page.Subject.NormName) {
			continue
		}
		if err := s.ensureCompile(ctx, job, plan, page); err != nil {
			return s.applyError(ctx, job, "ensure_compile", err)
		}
	}
	if len(plan.MatchUnits) == 0 && compileUnits(plan) == 0 {
		if err := s.integrateStaged(ctx, job, plan); err != nil {
			return s.applyError(ctx, job, "integrate", err)
		}
	}
	return Outcome{Class: Applied, Reason: "extract_applied"}
}

func (s *Service) ensureMatch(ctx context.Context, job Job, plan stagedIntegration, normName string) error {
	unit, err := s.matchUnit(ctx, job, plan, normName)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(pipelineEnvelope{JobID: job.ID, Stage: "match", Subject: normName, Units: len(plan.MatchUnits)})
	if err != nil {
		return err
	}
	_, err = s.queue.Ensure(ctx, llm.Request{
		Key: job.ID + "/match/" + normName + "/a1", Context: envelope, Site: s.matchSite,
		Attr: jobAttribution(job), Attempt: 1,
		Messages: []llm.Message{{Role: "user", Text: matchstage.Render(unit)}},
	})
	return err
}

func (s *Service) ensureCompile(ctx context.Context, job Job, plan stagedIntegration, page stagedPage) error {
	staged, err := s.compileUnitStaged(ctx, job.ID, page.Subject.NormName)
	if err != nil || staged {
		return err
	}
	envelope, err := json.Marshal(pipelineEnvelope{JobID: job.ID, Stage: "compile", Subject: page.Subject.NormName, Units: compileUnits(plan)})
	if err != nil {
		return err
	}
	_, err = s.queue.Ensure(ctx, llm.Request{
		Key: job.ID + "/compile/" + page.Subject.NormName + "/a1", Context: envelope, Site: s.compileSite,
		Attr: jobAttribution(job), Attempt: 1,
		Messages: []llm.Message{{Role: "user", Text: renderCompile(page.Subject.subject(""), modelClaims(page.Claims, ""))}},
	})
	return err
}

func compileUnits(plan stagedIntegration) int {
	if plan.CompileTotal > 0 {
		return plan.CompileTotal
	}
	n := 0
	for _, page := range plan.Pages {
		if !page.Delete {
			n++
		}
	}
	return n
}

func (s *Service) compileUnitStaged(ctx context.Context, jobID, unit string) (bool, error) {
	var found int
	err := s.write.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM job_staging
			WHERE job_id = ? AND stage = 'compile' AND unit = ?
		)`, jobID, unit).Scan(&found)
	return found != 0, err
}

func (s *Service) matchUnitStaged(ctx context.Context, jobID, unit string) (bool, error) {
	var found int
	err := s.write.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM job_staging WHERE job_id = ? AND stage = 'match' AND unit = ?)`, jobID, unit).Scan(&found)
	return found != 0, err
}

//nolint:funlen,gocyclo // Plan staging validates and normalizes one atomic extraction transaction.
func (s *Service) stageExtractPlan(ctx context.Context, job Job, extracted []extract.ExtractedSubject) (stagedIntegration, error) {
	affected, err := s.affectedSubjects(ctx, s.subjects, s.claims, job.ID)
	if err != nil {
		return stagedIntegration{}, err
	}
	knownByNorm := map[string]Subject{}
	newByNorm := map[string]bool{}
	claimsByNorm := map[string][]stagedClaim{}
	newClaimsByNorm := map[string]bool{}
	newCorrectionsByNorm := map[string]bool{}
	pageByNorm := map[string]stagedSubject{}
	plan := stagedIntegration{}
	nextOrdinal := 1
	for _, subject := range affected {
		knownByNorm[subject.NormName] = subject
		pageByNorm[subject.NormName] = stageSubject(subject)
	}
	for _, item := range extracted {
		normName := Normalize(item.Name)
		if normName == "" {
			continue
		}
		subject, ok := knownByNorm[normName]
		if !ok {
			var err error
			subject, err = s.resolver.ResolveByName(ctx, job.Scope, item.Name)
			if err != nil && !errors.Is(err, ErrSubjectNotFound) {
				return stagedIntegration{}, err
			}
			if errors.Is(err, ErrSubjectNotFound) {
				subject = Subject{Name: strings.TrimSpace(item.Name), NormName: normName, Type: item.Type}
				if !newByNorm[normName] {
					plan.NewSubjects = append(plan.NewSubjects, stageSubject(subject))
					newByNorm[normName] = true
				}
			}
			knownByNorm[normName] = subject
		}
		pageByNorm[subject.NormName] = stageSubject(subject)
		for _, body := range item.Claims {
			claim := stagedClaim{NormName: subject.NormName, JobID: job.ID, Body: strings.TrimSpace(body), Kind: ClaimKind, Ordinal: nextOrdinal}
			nextOrdinal++
			plan.Claims = append(plan.Claims, claim)
			claimsByNorm[subject.NormName] = append(claimsByNorm[subject.NormName], claim)
			newClaimsByNorm[subject.NormName] = true
		}
		for _, body := range item.Corrections {
			claim := stagedClaim{NormName: subject.NormName, JobID: job.ID, Body: strings.TrimSpace(body), Kind: CorrectionKind, Ordinal: nextOrdinal}
			nextOrdinal++
			plan.Claims = append(plan.Claims, claim)
			claimsByNorm[subject.NormName] = append(claimsByNorm[subject.NormName], claim)
			newCorrectionsByNorm[subject.NormName] = true
		}
	}
	normNames := make([]string, 0, len(pageByNorm))
	for normName := range pageByNorm {
		normNames = append(normNames, normName)
	}
	sort.Strings(normNames)
	for _, normName := range normNames {
		subject := knownByNorm[normName]
		claims := claimsByNorm[normName]
		var persistedEdges []Suppression
		standingCorrection := false
		if subject.ID != "" {
			existing, err := s.plannedClaims(ctx, job.ID, subject.ID, nil)
			if err != nil {
				return stagedIntegration{}, err
			}
			for _, claim := range existing {
				claims = append(claims, stagedClaim{ID: claim.ID, NormName: normName, JobID: claim.JobID, Body: claim.Body, Kind: claim.Kind})
				standingCorrection = standingCorrection || claim.Kind == CorrectionKind
			}
			persistedEdges, err = NewSuppressionStore(s.write).ListBySubject(ctx, subject.ID)
			if err != nil {
				return stagedIntegration{}, err
			}
		}
		sort.Slice(claims, func(i, j int) bool {
			return modelClaims(claims[i:i+1], "")[0].ID < modelClaims(claims[j:j+1], "")[0].ID
		})
		if newCorrectionsByNorm[normName] || (newClaimsByNorm[normName] && standingCorrection) {
			plan.MatchUnits = append(plan.MatchUnits, normName)
		}
		effective, _ := Effective(modelClaims(claims, subject.ID), persistedEdges)
		effectiveStaged := stagedClaims(effective, claims)
		plan.Pages = append(plan.Pages, stagedPage{Subject: pageByNorm[normName], Claims: effectiveStaged, Delete: len(effectiveStaged) == 0})
	}
	sort.Strings(plan.MatchUnits)
	plan.CompileTotal = compileUnits(plan)
	raw, err := json.Marshal(plan)
	if err != nil {
		return stagedIntegration{}, err
	}
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return stagedIntegration{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO job_staging (job_id, stage, unit, payload, units) VALUES (?, 'extract', '', ?, ?)
		ON CONFLICT(job_id, stage, unit) DO UPDATE SET payload = excluded.payload, units = excluded.units`,
		job.ID, string(raw), compileUnits(plan)); err != nil {
		return stagedIntegration{}, err
	}
	nextPhase := "compile"
	if len(plan.MatchUnits) > 0 {
		nextPhase = "match"
	}
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET phase = ?, deadline_at = ? WHERE id = ? AND status = ? AND phase = 'extract'`, nextPhase, formatTime(s.now().Add(jobProgressDeadline)), job.ID, JobWorking)
	if err != nil {
		return stagedIntegration{}, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return stagedIntegration{}, err
	}
	if changed == 0 {
		return stagedIntegration{}, fmt.Errorf("wiki: extract phase changed before staging job %s", job.ID)
	}
	if err := tx.Commit(); err != nil {
		return stagedIntegration{}, err
	}
	return plan, nil
}

func stagedClaims(rows []Claim, candidates []stagedClaim) []stagedClaim {
	byRef := make(map[string]stagedClaim, len(candidates))
	for _, candidate := range candidates {
		ref := candidate.ID
		if ref == "" {
			ref = ordinalRef(candidate.Ordinal)
		}
		byRef[ref] = candidate
	}
	out := make([]stagedClaim, 0, len(rows))
	for _, row := range rows {
		if candidate, ok := byRef[row.ID]; ok {
			out = append(out, candidate)
		}
	}
	return out
}

func (s *Service) loadStagedIntegration(ctx context.Context, jobID string) (stagedIntegration, bool, error) {
	var raw string
	err := s.write.QueryRowContext(ctx, `SELECT payload FROM job_staging WHERE job_id = ? AND stage = 'extract' AND unit = ''`, jobID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return stagedIntegration{}, false, nil
	}
	if err != nil {
		return stagedIntegration{}, false, err
	}
	var plan stagedIntegration
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return stagedIntegration{}, false, err
	}
	return plan, true, nil
}

func (s *Service) matchUnit(ctx context.Context, job Job, plan stagedIntegration, normName string) (matchstage.Unit, error) {
	var page stagedPage
	found := false
	for _, candidate := range plan.Pages {
		if candidate.Subject.NormName == normName {
			page, found = candidate, true
			break
		}
	}
	if !found {
		return matchstage.Unit{}, fmt.Errorf("wiki: match subject %q missing from staged plan", normName)
	}
	subject := page.Subject.subject("")
	var rows []Claim
	resolved, err := s.resolver.ResolveByName(ctx, job.Scope, normName)
	if err == nil {
		subject.ID = resolved.ID
		rows, err = s.plannedClaims(ctx, job.ID, resolved.ID, nil)
		if err != nil {
			return matchstage.Unit{}, err
		}
	} else if !errors.Is(err, ErrSubjectNotFound) {
		return matchstage.Unit{}, err
	}
	for _, staged := range plan.Claims {
		if staged.NormName == normName {
			rows = append(rows, modelClaims([]stagedClaim{staged}, subject.ID)[0])
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	unit := matchstage.Unit{Subject: subject}
	for _, row := range rows {
		statement := matchstage.Statement{Claim: row, New: row.JobID == job.ID}
		if row.Kind == CorrectionKind {
			unit.Corrections = append(unit.Corrections, statement)
		} else {
			unit.Claims = append(unit.Claims, statement)
		}
	}
	return unit, nil
}

//nolint:funlen,gocyclo // Match handling is a guarded state-machine transition kept together for auditability.
func (s *Service) applyMatch(ctx context.Context, job Job, envelope pipelineEnvelope, item llm.Item) Outcome {
	plan, found, err := s.loadStagedIntegration(ctx, job.ID)
	if err != nil {
		return s.applyError(ctx, job, "corrupt_staged_payload", err)
	}
	if !found || !containsString(plan.MatchUnits, envelope.Subject) || envelope.Units != len(plan.MatchUnits) {
		return Outcome{Class: Discarded, Reason: "stale_units"}
	}
	unit, err := s.matchUnit(ctx, job, plan, envelope.Subject)
	if err != nil {
		return s.applyError(ctx, job, "load_match_unit", err)
	}
	var result matchstage.Result
	if err := json.Unmarshal(item.Result, &result); err != nil {
		return s.correctOrFail(ctx, job, item, "match", envelope.Subject, envelope.Units, err)
	}
	if err := matchstage.Validate(result, len(unit.Corrections), len(unit.Claims)); err != nil {
		return s.correctOrFail(ctx, job, item, "match", envelope.Subject, envelope.Units, err)
	}
	edges := matchstage.Filter(unit, result)
	payload := make([]stagedEdge, 0, len(edges))
	for _, edge := range edges {
		payload = append(payload, stagedEdge{Correction: edge.Correction, Suppressed: edge.Suppressed})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return s.applyError(ctx, job, "match_payload", err)
	}
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return s.applyError(ctx, job, "begin_match", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_staging(job_id, stage, unit, payload, units) VALUES (?, 'match', ?, ?, ?) ON CONFLICT(job_id, stage, unit) DO UPDATE SET payload=excluded.payload, units=excluded.units`, job.ID, envelope.Subject, string(raw), envelope.Units); err != nil {
		return s.applyError(ctx, job, "stage_match", err)
	}
	var matched int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_staging WHERE job_id = ? AND stage = 'match'`, job.ID).Scan(&matched); err != nil {
		return s.applyError(ctx, job, "count_match", err)
	}
	if matched == len(plan.MatchUnits) {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET phase='compile', deadline_at=? WHERE id=? AND status=? AND phase='match'`, formatTime(s.now().Add(jobProgressDeadline)), job.ID, JobWorking); err != nil {
			return s.applyError(ctx, job, "finish_match", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return s.applyError(ctx, job, "commit_match", err)
	}
	page, err := s.effectiveStagedPage(ctx, job, plan, envelope.Subject)
	if err != nil {
		return s.applyError(ctx, job, "effective_match", err)
	}
	plan, err = s.replaceStagedPage(ctx, job.ID, plan, page)
	if err != nil {
		return s.applyError(ctx, job, "stage_effective_page", err)
	}
	if page.Delete {
		if err := s.stageDeletedCompile(ctx, job.ID, page.Subject.NormName, compileUnits(plan)); err != nil {
			return s.applyError(ctx, job, "stage_empty_compile", err)
		}
	} else if err := s.ensureCompile(ctx, job, plan, page); err != nil {
		return s.applyError(ctx, job, "ensure_compile", err)
	}
	if matched == len(plan.MatchUnits) {
		var compiled int
		if err := s.write.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_staging WHERE job_id=? AND stage='compile'`, job.ID).Scan(&compiled); err != nil {
			return s.applyError(ctx, job, "count_compile", err)
		}
		if compiled == compileUnits(plan) {
			if err := s.integrateStaged(ctx, job, plan); err != nil {
				return s.applyError(ctx, job, "integrate", err)
			}
		}
	}
	return Outcome{Class: Applied, Reason: "match_applied"}
}

func (s *Service) replaceStagedPage(ctx context.Context, jobID string, plan stagedIntegration, page stagedPage) (stagedIntegration, error) {
	for i := range plan.Pages {
		if plan.Pages[i].Subject.NormName == page.Subject.NormName {
			plan.Pages[i] = page
			break
		}
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return stagedIntegration{}, err
	}
	_, err = s.write.ExecContext(ctx, `UPDATE job_staging SET payload=? WHERE job_id=? AND stage='extract' AND unit=''`, string(raw), jobID)
	return plan, err
}

func (s *Service) stagedMatchEdges(ctx context.Context, jobID string) ([]Suppression, error) {
	rows, err := s.write.QueryContext(ctx, `SELECT payload FROM job_staging WHERE job_id=? AND stage='match' ORDER BY unit`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Suppression
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var edges []stagedEdge
		if err := json.Unmarshal([]byte(raw), &edges); err != nil {
			return nil, err
		}
		for _, edge := range edges {
			out = append(out, Suppression{Correction: edge.Correction, Suppressed: edge.Suppressed})
		}
	}
	return out, rows.Err()
}

func (s *Service) effectiveStagedPage(ctx context.Context, job Job, plan stagedIntegration, normName string) (stagedPage, error) {
	unit, err := s.matchUnit(ctx, job, plan, normName)
	if err != nil {
		return stagedPage{}, err
	}
	rows := make([]Claim, 0, len(unit.Claims)+len(unit.Corrections))
	for _, statement := range unit.Claims {
		rows = append(rows, statement.Claim)
	}
	for _, statement := range unit.Corrections {
		rows = append(rows, statement.Claim)
	}
	var edges []Suppression
	if unit.Subject.ID != "" {
		edges, err = NewSuppressionStore(s.write).ListBySubject(ctx, unit.Subject.ID)
		if err != nil {
			return stagedPage{}, err
		}
	}
	staged, err := s.stagedMatchEdges(ctx, job.ID)
	if err != nil {
		return stagedPage{}, err
	}
	edges = append(edges, staged...)
	effective, _ := Effective(rows, edges)
	var subject stagedSubject
	for _, candidate := range plan.Pages {
		if candidate.Subject.NormName == normName {
			subject = candidate.Subject
			break
		}
	}
	claims := make([]stagedClaim, 0, len(effective))
	for _, row := range effective {
		claims = append(claims, stagedClaim{ID: row.ID, NormName: normName, JobID: row.JobID, Body: row.Body, Kind: row.Kind})
	}
	return stagedPage{Subject: subject, Claims: claims, Delete: len(claims) == 0}, nil
}

func (s *Service) stageDeletedCompile(ctx context.Context, jobID, unit string, units int) error {
	_, err := s.write.ExecContext(ctx, `INSERT INTO job_staging(job_id, stage, unit, payload, units) VALUES (?, 'compile', ?, '{}', ?) ON CONFLICT(job_id, stage, unit) DO NOTHING`, jobID, unit, units)
	return err
}

//nolint:gocyclo // Compile handling is a linear set of protocol and persistence guards.
func (s *Service) applyCompile(ctx context.Context, job Job, envelope pipelineEnvelope, item llm.Item) Outcome {
	plan, found, err := s.loadStagedIntegration(ctx, job.ID)
	if err != nil {
		return s.applyError(ctx, job, "corrupt_staged_payload", err)
	}
	_, ok := compilePage(plan, envelope.Subject)
	if !found || !ok || envelope.Units != compileUnits(plan) {
		if !found {
			return s.failCompletion(ctx, job, "missing_staging", errors.New("extract staging row is missing"))
		}
		return Outcome{Class: Discarded, Reason: "stale_units"}
	}
	title, body, err := parseCompile(item.Result)
	if err != nil {
		return s.correctOrFail(ctx, job, item, "compile", envelope.Subject, envelope.Units, err)
	}
	payload, err := json.Marshal(stagedCompiledPage{Title: title, Body: body})
	if err != nil {
		return s.applyError(ctx, job, "compile_payload", err)
	}
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return s.applyError(ctx, job, "begin_compile", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO job_staging (job_id, stage, unit, payload, units) VALUES (?, 'compile', ?, ?, ?)
		ON CONFLICT(job_id, stage, unit) DO UPDATE SET payload = excluded.payload, units = excluded.units`,
		job.ID, envelope.Subject, string(payload), envelope.Units)
	if err != nil {
		_ = tx.Rollback()
		return s.applyError(ctx, job, "stage_compile", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET deadline_at = ? WHERE id = ? AND status = ?`, formatTime(s.now().Add(jobProgressDeadline)), job.ID, JobWorking); err != nil {
		_ = tx.Rollback()
		return s.applyError(ctx, job, "extend_deadline", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_staging WHERE job_id = ? AND stage = 'compile'`, job.ID).Scan(&count); err != nil {
		_ = tx.Rollback()
		return s.applyError(ctx, job, "count_compile", err)
	}
	var committed stagedCommit
	if count == envelope.Units {
		committed, err = s.integrateStagedTx(ctx, tx, job, plan)
		if err != nil {
			_ = tx.Rollback()
			return s.applyError(ctx, job, "integrate", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return s.applyError(ctx, job, "commit_compile", err)
	}
	if err := s.afterStagedCommit(ctx, job, committed); err != nil {
		return Outcome{Class: Applied, Reason: "compile_applied", Err: err}
	}
	if committed.failed {
		return Outcome{Class: JobFailedOutcome, Reason: "stale_plan"}
	}
	return Outcome{Class: Applied, Reason: "compile_applied"}
}

func compilePage(plan stagedIntegration, normName string) (stagedPage, bool) {
	for _, page := range plan.Pages {
		if !page.Delete && page.Subject.NormName == normName {
			return page, true
		}
	}
	return stagedPage{}, false
}

func (s *Service) correctOrFail(ctx context.Context, job Job, item llm.Item, stage, subject string, units int, semanticErr error) Outcome {
	attempt := keyAttempt(item.Key)
	site := s.extractSite
	var original string
	switch stage {
	case "extract":
		original = extract.Render(extract.DocumentHeader{Source: "mcp:ingest_text", Title: job.Title, Tags: job.Tags, ReceivedAt: job.ReceivedAt}, job.SourceText)
	case "match":
		site = s.matchSite
		plan, found, err := s.loadStagedIntegration(ctx, job.ID)
		if err != nil {
			return s.applyError(ctx, job, "corrupt_staged_payload", err)
		}
		if !found || !containsString(plan.MatchUnits, subject) {
			return Outcome{Class: Discarded, Reason: "stale_units"}
		}
		unit, err := s.matchUnit(ctx, job, plan, subject)
		if err != nil {
			return s.applyError(ctx, job, "load_match_unit", err)
		}
		original = matchstage.Render(unit)
	default:
		site = s.compileSite
		plan, found, err := s.loadStagedIntegration(ctx, job.ID)
		if err != nil {
			return s.applyError(ctx, job, "corrupt_staged_payload", err)
		}
		page, ok := compilePage(plan, subject)
		if !found || !ok {
			if !found {
				return s.failCompletion(ctx, job, "missing_staging", errors.New("extract staging row is missing"))
			}
			return Outcome{Class: Discarded, Reason: "stale_units"}
		}
		original = renderCompile(page.Subject.subject(""), modelClaims(page.Claims, ""))
	}
	if attempt >= site.MaxParseRetries+1 {
		return s.failCompletion(ctx, job, "semantic_retries_exhausted", semanticErr)
	}
	next := attempt + 1
	envelope, err := json.Marshal(pipelineEnvelope{JobID: job.ID, Stage: stage, Subject: subject, Units: units})
	if err != nil {
		return s.applyError(ctx, job, "corrective_envelope", err)
	}
	base := strings.TrimSuffix(item.Key, "/a"+strconv.Itoa(attempt))
	_, err = s.queue.Ensure(ctx, llm.Request{
		Key: base + "/a" + strconv.Itoa(next), Context: envelope, Site: site,
		Attr: jobAttribution(job), Attempt: next,
		Messages: []llm.Message{
			{Role: "user", Text: original},
			{Role: "assistant", Text: string(item.Result)},
			{Role: "user", Text: fmt.Sprintf("Return corrected JSON for the original request. Validation error: %v", semanticErr)},
		},
	})
	if err != nil {
		return s.applyError(ctx, job, "ensure_correction", err)
	}
	return Outcome{Class: Applied, Reason: "correction_ensured"}
}

var compileBracketedID = regexp.MustCompile(`\[[0-9A-HJKMNP-TV-Z]{26}\]`)

func renderCompile(subject Subject, claims []Claim) string {
	var b strings.Builder
	b.WriteString("Hard body limit: 12000 characters.\n\nSubject identity:\n")
	fmt.Fprintf(&b, "name: %s\ntype: %s\n\nComplete claim texts:\n", subject.Name, subject.Type)
	if len(claims) == 0 {
		b.WriteString("- none\n")
		return b.String()
	}
	for i, claim := range claims {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(claim.Body))
	}
	return b.String()
}

//nolint:gocritic // Explicit returns keep the title, body, and decode error cases local.
func parseCompile(raw json.RawMessage) (string, string, error) {
	var out struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", err
	}
	out.Title = strings.TrimSpace(out.Title)
	out.Body = strings.TrimSpace(compileBracketedID.ReplaceAllString(out.Body, ""))
	if out.Title == "" {
		return "", "", fmt.Errorf("title required")
	}
	if out.Body == "" {
		return "", "", fmt.Errorf("body required")
	}
	if utf8.RuneCountInString(out.Body) > 12000 {
		out.Body = string([]rune(out.Body)[:12000])
	}
	return out.Title, out.Body, nil
}

func keyAttempt(key string) int {
	index := strings.LastIndex(key, "/a")
	if index < 0 {
		return 1
	}
	attempt, err := strconv.Atoi(key[index+2:])
	if err != nil || attempt < 1 {
		return 1
	}
	return attempt
}

type stagedCommit struct {
	changed bool
	done    bool
	failed  bool
	pages   []Page
}

func (s *Service) integrateStaged(ctx context.Context, job Job, plan stagedIntegration) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	committed, err := s.integrateStagedTx(ctx, tx, job, plan)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.afterStagedCommit(ctx, job, committed)
}

//nolint:funlen,gocyclo // This function is the single auditable transaction boundary for staged integration.
func (s *Service) integrateStagedTx(ctx context.Context, tx *sql.Tx, job Job, plan stagedIntegration) (stagedCommit, error) {
	var status, phase string
	if err := tx.QueryRowContext(ctx, `SELECT status, phase FROM jobs WHERE id = ?`, job.ID).Scan(&status, &phase); err != nil {
		return stagedCommit{}, err
	}
	if status != JobWorking || phase != "compile" {
		return stagedCommit{}, nil
	}
	subjects := NewSubjectStore(tx)
	resolver := NewResolver(tx)
	claims := NewClaimStore(tx)
	pages := NewPageStore(tx)
	newByNorm := make(map[string]stagedSubject, len(plan.NewSubjects))
	for _, subject := range plan.NewSubjects {
		newByNorm[subject.NormName] = subject
	}
	required := make(map[string]bool)
	for _, claim := range plan.Claims {
		required[claim.NormName] = true
	}
	for _, page := range plan.Pages {
		required[page.Subject.NormName] = true
	}
	resolved := make(map[string]Subject, len(required))
	for normName := range required {
		subject, resolveErr := resolver.ResolveByName(ctx, job.Scope, normName)
		if resolveErr == nil {
			resolved[normName] = subject
			continue
		}
		if !errors.Is(resolveErr, ErrSubjectNotFound) {
			return stagedCommit{}, resolveErr
		}
		if _, allowed := newByNorm[normName]; !allowed {
			return s.failStaleIntegrationTx(ctx, tx, job, normName)
		}
	}
	if err := claims.DeleteByJob(ctx, job.ID); err != nil {
		return stagedCommit{}, err
	}
	for _, staged := range plan.NewSubjects {
		normName := staged.NormName
		if _, ok := resolved[normName]; ok {
			continue
		}
		if err := subjects.Save(ctx, job.Scope, staged.subject(s.newID())); err != nil {
			return stagedCommit{}, err
		}
		subject, err := resolver.ResolveByName(ctx, job.Scope, normName)
		if err != nil {
			return stagedCommit{}, err
		}
		resolved[normName] = subject
	}
	resolvedRefs := make(map[string]string, len(plan.Claims))
	for _, staged := range plan.Claims {
		subject := resolved[staged.NormName]
		id := staged.ID
		if id == "" {
			id = s.newID()
			resolvedRefs[ordinalRef(staged.Ordinal)] = id
		}
		kind := staged.Kind
		if kind == "" {
			kind = ClaimKind
		}
		claim := Claim{ID: id, SubjectID: subject.ID, JobID: staged.JobID, Body: staged.Body, Kind: kind}
		if err := claims.Save(ctx, claim); err != nil {
			return stagedCommit{}, err
		}
	}
	matchEdges, err := loadStagedMatchEdgesTx(ctx, tx, job.ID)
	if err != nil {
		return stagedCommit{}, err
	}
	suppressions := NewSuppressionStore(tx)
	for _, edge := range matchEdges {
		correction := edge.Correction
		if resolved := resolvedRefs[correction]; resolved != "" {
			correction = resolved
		}
		suppressed := edge.Suppressed
		if resolved := resolvedRefs[suppressed]; resolved != "" {
			suppressed = resolved
		}
		if err := suppressions.Insert(ctx, Suppression{Correction: correction, Suppressed: suppressed, CreatedAt: s.now().Unix()}); err != nil {
			return stagedCommit{}, err
		}
	}
	var pagesToEmbed []Page
	for _, planned := range plan.Pages {
		subject := resolved[planned.Subject.NormName]
		if planned.Delete {
			if err := pages.DeleteBySubject(ctx, subject.ID); err != nil {
				return stagedCommit{}, err
			}
			continue
		}
		var raw string
		if err := tx.QueryRowContext(ctx, `SELECT payload FROM job_staging WHERE job_id = ? AND stage = 'compile' AND unit = ?`, job.ID, planned.Subject.NormName).Scan(&raw); err != nil {
			return stagedCommit{}, err
		}
		var compiled stagedCompiledPage
		if err := json.Unmarshal([]byte(raw), &compiled); err != nil {
			return stagedCommit{}, err
		}
		page := Page{ID: subject.ID, SubjectID: subject.ID, Title: compiled.Title, Body: compiled.Body}
		if err := pages.Upsert(ctx, page); err != nil {
			return stagedCommit{}, err
		}
		pagesToEmbed = append(pagesToEmbed, page)
	}
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET status = ?, finished_at = ?, error = '' WHERE id = ? AND status = ? AND phase = 'compile'`, JobDone, formatTime(s.now()), job.ID, JobWorking)
	if err != nil {
		return stagedCommit{}, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return stagedCommit{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM job_staging WHERE job_id = ?`, job.ID); err != nil {
		return stagedCommit{}, err
	}
	if err := s.jobs.ReleaseLease(ctx, tx, job.ID); err != nil {
		return stagedCommit{}, err
	}
	return stagedCommit{changed: true, done: true, pages: pagesToEmbed}, nil
}

func loadStagedMatchEdgesTx(ctx context.Context, tx *sql.Tx, jobID string) ([]stagedEdge, error) {
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM job_staging WHERE job_id=? AND stage='match' ORDER BY unit`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stagedEdge
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var edges []stagedEdge
		if err := json.Unmarshal([]byte(raw), &edges); err != nil {
			return nil, err
		}
		out = append(out, edges...)
	}
	return out, rows.Err()
}

func (s *Service) afterStagedCommit(ctx context.Context, job Job, committed stagedCommit) error {
	if !committed.changed {
		return nil
	}
	s.notify()
	if committed.done && s.AskInvalidate != nil {
		s.AskInvalidate(job.Scope)
	}
	for _, page := range committed.pages {
		if err := s.embedAndStore(ctx, jobAttribution(job), job.Scope, page); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) failStaleIntegrationTx(ctx context.Context, tx *sql.Tx, job Job, normName string) (stagedCommit, error) {
	reason := fmt.Sprintf("stale staged plan: subject %q no longer resolves", normName)
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET status = ?, finished_at = ?, error = ? WHERE id = ? AND status = ? AND phase = 'compile'`,
		JobFailed, formatTime(s.now()), reason, job.ID, JobWorking)
	if err != nil {
		return stagedCommit{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return stagedCommit{}, err
	}
	if n == 0 {
		return stagedCommit{}, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM job_staging WHERE job_id = ?`, job.ID); err != nil {
		return stagedCommit{}, err
	}
	if err := s.jobs.ReleaseLease(ctx, tx, job.ID); err != nil {
		return stagedCommit{}, err
	}
	return stagedCommit{changed: true, failed: true}, nil
}
