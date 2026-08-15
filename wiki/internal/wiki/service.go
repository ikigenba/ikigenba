package wiki

import (
	"appkit"
	"appkit/logging"
	"appkit/telemetry"
	"context"
	"database/sql"
	"errors"
	"eventplane/correlation"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"wiki/internal/extract"
	"wiki/internal/llm"
	"wiki/internal/match"
	"wiki/internal/page"
)

const (
	JobPending = "pending"
	JobWorking = "working"
	JobDone    = "done"
	JobFailed  = "failed"
	JobAborted = "aborted"

	ingestLeaseTTL            = 30 * time.Minute
	jobProgressDeadline       = time.Hour
	jobAbsoluteLifetime       = 24 * time.Hour
	noProgressHealthThreshold = 2
)

type AbortResult struct {
	Aborted bool
	Status  string
}

// Extractor is the injected extract-stage dependency.
type Extractor interface {
	Extract(ctx context.Context, attr llm.Attribution, h extract.DocumentHeader, text string) ([]extract.ExtractedSubject, error)
}

// Compiler is the injected compile-stage dependency.
type Compiler interface {
	Compile(ctx context.Context, attr llm.Attribution, subject Subject, claims []Claim) (title, body string, err error)
}

// Service coordinates ingest jobs and the single background integration worker.
type Service struct {
	AskInvalidate     func(scope string)
	write             *sql.DB
	jobs              *JobStore
	scopes            *ScopeStore
	subjects          *SubjectStore
	aliases           *AliasStore
	resolver          *Resolver
	claims            *ClaimStore
	pages             *PageStore
	embeddings        *EmbeddingStore
	merges            *SubjectMergeStore
	pageEmbedder      PageEmbedder
	embedModel        string
	vectorCache       VectorCache
	vectorCacheRemove func(subjectID string)
	extractor         Extractor
	compiler          Compiler
	matcher           *match.Matcher
	queue             *llm.Client
	extractSite       llm.CallSite
	compileSite       llm.CallSite
	matchSite         llm.CallSite
	recorder          *telemetry.Recorder
	now               func() time.Time
	newID             func() string
	wake              chan struct{}
	mu                sync.Mutex
	cancels           map[string]*jobCancel
	deferrals         map[string]int
	noProgressStreak  int
	deferredItems     int
	logger            *slog.Logger
}

// WithMatcher wires the live matcher used by composed operations such as merge.
func WithMatcher(matcher *match.Matcher) ServiceOption {
	return func(s *Service) { s.matcher = matcher }
}

// WithCompletionQueue enables the durable handoff/apply ingest pipeline.
func WithCompletionQueue(client *llm.Client, extractSite llm.CallSite, sites ...llm.CallSite) ServiceOption {
	return func(s *Service) {
		s.queue = client
		s.extractSite = extractSite
		s.matchSite = match.DefaultCallSite()
		if len(sites) == 1 {
			s.compileSite = sites[0]
		}
		if len(sites) >= 2 {
			s.matchSite = sites[0]
			s.compileSite = sites[1]
		}
	}
}

type jobCancel struct {
	cancel context.CancelFunc
}

// NewService builds the ingest service over wiki's read/write SQLite handles.
func NewService(db any, extractor Extractor, compiler Compiler, now func() time.Time, opts ...ServiceOption) *Service {
	if now == nil {
		now = time.Now
	}
	c := mustConns(db)
	s := &Service{
		write:      c.Write,
		jobs:       NewJobStore(c),
		scopes:     NewScopeStore(c),
		subjects:   NewSubjectStore(c.Read),
		aliases:    NewAliasStore(c.Read),
		resolver:   NewResolver(c.Read),
		claims:     NewClaimStore(c.Read),
		pages:      NewPageStore(c.Read),
		embeddings: NewEmbeddingStore(c),
		merges:     NewSubjectMergeStore(c.Read),
		extractor:  extractor,
		compiler:   compiler,
		recorder:   &telemetry.Recorder{},
		now:        now,
		newID:      logging.NewULID,
		wake:       make(chan struct{}, 1),
		cancels:    map[string]*jobCancel{},
		deferrals:  map[string]int{},
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// Ingest records a pending job and returns immediately with its handle.
func (s *Service) Ingest(ctx context.Context, scope, ownerID, ownerEmail, text, title string, tags []string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("wiki: nil service")
	}
	if err := requireScope(ctx, s.write, scope); err != nil {
		return "", err
	}
	jobID := s.newID()
	job := Job{
		ID:            jobID,
		Scope:         scope,
		OwnerID:       strings.TrimSpace(ownerID),
		OwnerEmail:    strings.TrimSpace(ownerEmail),
		SourceText:    text,
		Title:         strings.TrimSpace(title),
		Tags:          append([]string(nil), tags...),
		Status:        JobPending,
		ReceivedAt:    s.now(),
		CorrelationID: correlation.FromContext(ctx),
	}
	if err := s.jobs.InsertIngest(ctx, job); err != nil {
		return "", err
	}
	s.notify()
	return jobID, nil
}

// MergeSubjects queues a background job that folds one subject into another.
func (s *Service) MergeSubjects(ctx context.Context, scope, fromSubjectID, toSubjectID string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("wiki: nil service")
	}
	if err := requireScope(ctx, s.write, scope); err != nil {
		return "", err
	}
	for _, id := range []string{fromSubjectID, toSubjectID} {
		var exists int
		if err := s.write.QueryRowContext(ctx, `SELECT 1 FROM subjects WHERE id = ? AND scope = ?`, strings.TrimSpace(id), scope).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			if err := s.write.QueryRowContext(ctx, `SELECT 1 FROM subjects WHERE id = ?`, strings.TrimSpace(id)).Scan(&exists); err == nil {
				return "", ErrSubjectNotFound
			} else if !errors.Is(err, sql.ErrNoRows) {
				return "", err
			}
		} else if err != nil {
			return "", err
		}
	}
	jobID := s.newID()
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	receivedAt := s.now()
	ownerID, ownerEmail := "", ""
	if id, ok := appkit.IdentityFrom(ctx); ok {
		ownerID = strings.TrimSpace(id.OwnerID)
		ownerEmail = strings.TrimSpace(id.OwnerEmail)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO jobs (
			id, scope, owner_id, owner_email, source_text, title, tags, source_hash, status, received_at, correlation_id
		) VALUES (?, ?, ?, ?, '', 'subject merge', '[]', ?, ?, ?, ?)`,
		jobID, scope, ownerID, ownerEmail, hashText(""), JobPending, formatTime(receivedAt), correlation.FromContext(ctx))
	if err != nil {
		return "", err
	}
	if err := NewSubjectMergeStore(tx).Save(ctx, SubjectMerge{
		JobID:         jobID,
		FromSubjectID: strings.TrimSpace(fromSubjectID),
		ToSubjectID:   strings.TrimSpace(toSubjectID),
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	s.notify()
	return jobID, nil
}

// JobStatus returns the visible lifecycle state and produced subject ids.
func (s *Service) JobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	if s == nil {
		return JobStatus{}, fmt.Errorf("wiki: nil service")
	}
	return s.jobs.Status(ctx, jobID)
}

func (s *Service) Abort(ctx context.Context, jobID string) (AbortResult, error) {
	if s == nil {
		return AbortResult{}, fmt.Errorf("wiki: nil service")
	}
	result, err := s.jobs.Abort(ctx, strings.TrimSpace(jobID), s.now())
	if err != nil {
		return AbortResult{}, err
	}
	if result.Aborted {
		s.cancelJob(strings.TrimSpace(jobID))
		s.notify()
	}
	return result, nil
}

func (s *Service) Rerun(ctx context.Context, jobID string) (RerunResult, error) {
	if s == nil {
		return RerunResult{}, fmt.Errorf("wiki: nil service")
	}
	result, err := s.jobs.Rerun(ctx, strings.TrimSpace(jobID))
	if err != nil {
		return result, err
	}
	if result.Requeued {
		s.notify()
	}
	return result, nil
}

func (s *Service) RequeueWorking(ctx context.Context) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("wiki: nil service")
	}
	n, err := s.jobs.RequeueWorking(ctx)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		s.notify()
	}
	return n, nil
}

func (s *Service) ReapExpired(ctx context.Context, now time.Time) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("wiki: nil service")
	}
	n, err := s.jobs.ReapExpired(ctx, now, jobAbsoluteLifetime, "completion never arrived before the job deadline")
	if n > 0 {
		s.notify()
	}
	return n, err
}

func (s *Service) Health(ctx context.Context) (map[string]any, error) {
	if s == nil {
		return nil, fmt.Errorf("wiki: nil service")
	}
	var running int
	var oldest string
	if err := s.write.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MIN(started_at), '') FROM jobs WHERE status = ?`, JobWorking).Scan(&running, &oldest); err != nil {
		return nil, err
	}
	age := int64(0)
	if at := parseStoredTime(oldest); !at.IsZero() {
		age = max(0, int64(s.now().Sub(at).Seconds()))
	}
	s.mu.Lock()
	streak := s.noProgressStreak
	s.mu.Unlock()
	details := map[string]any{"running_jobs": running, "oldest_running_age_seconds": age, "no_progress_streak": streak}
	s.mu.Lock()
	details["deferred_items"] = s.deferredItems
	s.mu.Unlock()
	details["degraded"] = streak >= noProgressHealthThreshold
	return details, nil
}

// Subjects lists registry subjects, optionally filtered by type and name substring.
func (s *Service) Subjects(ctx context.Context, typ, nameContains string) ([]Subject, error) {
	if s == nil {
		return nil, fmt.Errorf("wiki: nil service")
	}
	return listAllSubjects(ctx, s.subjects, typ, nameContains)
}

// Recent returns the newest subjects in one scope by descending ULID order.
func (s *Service) Recent(ctx context.Context, scope string, limit int) ([]Subject, error) {
	if s == nil {
		return nil, fmt.Errorf("wiki: nil service")
	}
	return s.subjects.Recent(ctx, scope, limit)
}

// ClaimsBySubject returns the stored claims for an existing subject.
func (s *Service) ClaimsBySubject(ctx context.Context, subjectID string) ([]Claim, error) {
	if s == nil {
		return nil, fmt.Errorf("wiki: nil service")
	}
	if _, err := s.subjects.Get(ctx, strings.TrimSpace(subjectID)); err != nil {
		return nil, err
	}
	return listAllClaims(ctx, s.claims, strings.TrimSpace(subjectID))
}

// PageBySubject returns the compiled page for an existing subject.
func (s *Service) PageBySubject(ctx context.Context, subjectID string) (Page, error) {
	if s == nil {
		return Page{}, fmt.Errorf("wiki: nil service")
	}
	return s.pages.GetBySubject(ctx, strings.TrimSpace(subjectID))
}

// ProcessNext integrates one pending job, if any.
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("wiki: nil service")
	}
	job, ok, err := s.jobs.ClaimPending(ctx, s.now(), ingestLeaseTTL)
	if err != nil || !ok {
		return ok, err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	registeredCancel := s.registerJobCancel(job.ID, cancel)
	defer s.unregisterJobCancel(job.ID, registeredCancel)
	if _, merge, mergeErr := s.mergeForJob(jobCtx, job.ID); mergeErr != nil {
		err = mergeErr
	} else if s.queue != nil && !merge {
		err = s.handoffExtract(jobCtx, job)
	} else {
		err = s.processClaimed(jobCtx, job)
	}
	if err != nil {
		if finished, _ := s.jobs.FinishWorking(ctx, job.ID, JobFailed, s.now(), err.Error()); finished {
			s.notify()
		}
		return true, nil
	}
	return true, nil
}

// WaitForWork blocks until work is nudged, the retry floor elapses, or the context is canceled.
func (s *Service) WaitForWork(ctx context.Context) error {
	if s == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	select {
	case <-s.wake:
		return nil
	case <-time.After(time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) processClaimed(ctx context.Context, job Job) error {
	ctx, job = s.jobContext(ctx, job)
	scope, err := s.scopes.Get(ctx, job.Scope)
	if err != nil {
		return err
	}
	ctx = llm.WithSystemComposer(ctx, func(base string) string {
		return ComposeSystem(base, scope.Instructions)
	})
	if _, ok, err := s.mergeForJob(ctx, job.ID); err != nil {
		return err
	} else if ok {
		return s.mergeSubjects(ctx, job)
	}
	return s.integrate(ctx, job)
}

func (s *Service) jobContext(ctx context.Context, job Job) (context.Context, Job) {
	if job.CorrelationID == "" {
		job.CorrelationID = job.ID
	}
	return correlation.WithContext(ctx, job.CorrelationID), job
}

//nolint:funlen,gocyclo // Integration is an auditable transaction pipeline with explicit rollback guards.
func (s *Service) integrate(ctx context.Context, job Job) error {
	if s.extractor == nil {
		return fmt.Errorf("wiki: nil extractor")
	}
	if s.compiler == nil {
		return fmt.Errorf("wiki: nil compiler")
	}
	attr := jobAttribution(job)
	extracted, err := s.extractor.Extract(ctx, attr, extract.DocumentHeader{
		Source:     "mcp:ingest_text",
		Title:      job.Title,
		Tags:       job.Tags,
		ReceivedAt: job.ReceivedAt,
	}, job.SourceText)
	if err != nil {
		return err
	}

	plan, err := s.planIntegration(ctx, attr, job, extracted)
	if err != nil {
		return err
	}

	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, job.ID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if status != JobWorking {
		return nil
	}

	subjects := NewSubjectStore(tx)
	claims := NewClaimStore(tx)
	pages := NewPageStore(tx)
	var pagesToEmbed []Page
	if err := claims.DeleteByJob(ctx, job.ID); err != nil {
		return err
	}
	for _, subject := range plan.newSubjects {
		if err := subjects.Save(ctx, job.Scope, subject); err != nil {
			return err
		}
	}
	for _, claim := range plan.claims {
		if err := claims.Save(ctx, claim); err != nil {
			return err
		}
	}
	for _, stagedPage := range plan.pages {
		if stagedPage.delete {
			if err := pages.DeleteBySubject(ctx, stagedPage.subjectID); err != nil {
				return err
			}
			continue
		}
		pageRow := Page{
			ID:        stagedPage.subjectID,
			SubjectID: stagedPage.subjectID,
			Title:     stagedPage.title,
			Body:      stagedPage.body,
		}
		if err := pages.Upsert(ctx, pageRow); err != nil {
			return err
		}
		pagesToEmbed = append(pagesToEmbed, pageRow)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status = ?, finished_at = ?, error = '' WHERE id = ? AND status = ?`,
		JobDone, formatTime(s.now()), job.ID, JobWorking)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if err := s.jobs.ReleaseLease(ctx, tx, job.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify()
	if s.AskInvalidate != nil {
		s.AskInvalidate(job.Scope)
	}
	for _, page := range pagesToEmbed {
		if err := s.embedAndStore(ctx, attr, job.Scope, page); err != nil {
			return err
		}
	}
	return nil
}

type integrationPlan struct {
	newSubjects []Subject
	claims      []Claim
	pages       []plannedPage
}

type plannedPage struct {
	subjectID string
	title     string
	body      string
	delete    bool
}

func (s *Service) planIntegration(ctx context.Context, attr llm.Attribution, job Job, extracted []extract.ExtractedSubject) (integrationPlan, error) {
	affected, err := s.affectedSubjects(ctx, s.subjects, s.claims, job.ID)
	if err != nil {
		return integrationPlan{}, err
	}

	knownByNorm := map[string]Subject{}
	newByNorm := map[string]bool{}
	claimsBySubject := map[string][]Claim{}
	var plan integrationPlan
	for _, item := range extracted {
		if Normalize(item.Name) == "" {
			continue
		}
		subject, isNew, err := s.plannedSubject(ctx, job.Scope, knownByNorm, item)
		if err != nil {
			return integrationPlan{}, err
		}
		if isNew {
			subject.ID = s.newID()
			knownByNorm[subject.NormName] = subject
		}
		if isNew && !newByNorm[subject.NormName] {
			plan.newSubjects = append(plan.newSubjects, subject)
			newByNorm[subject.NormName] = true
		}
		affected[subject.ID] = subject
		for _, body := range item.Claims {
			claim := Claim{
				ID:        s.newID(),
				SubjectID: subject.ID,
				JobID:     job.ID,
				Body:      strings.TrimSpace(body),
				Kind:      ClaimKind,
			}
			plan.claims = append(plan.claims, claim)
			claimsBySubject[subject.ID] = append(claimsBySubject[subject.ID], claim)
		}
	}

	affectedSubjects := sortedSubjects(affected)
	for _, subject := range affectedSubjects {
		subjectClaims, err := s.plannedClaims(ctx, job.ID, subject.ID, claimsBySubject[subject.ID])
		if err != nil {
			return integrationPlan{}, err
		}
		if len(subjectClaims) == 0 {
			plan.pages = append(plan.pages, plannedPage{subjectID: subject.ID, delete: true})
			continue
		}
		title, body, err := s.compiler.Compile(ctx, attr, subject, subjectClaims)
		if err != nil {
			return integrationPlan{}, err
		}
		plan.pages = append(plan.pages, plannedPage{
			subjectID: subject.ID,
			title:     title,
			body:      body,
		})
	}
	return plan, nil
}

//nolint:funlen,gocyclo // Subject merging keeps all atomic migration and cleanup guards in one transaction.
func (s *Service) mergeSubjects(ctx context.Context, job Job) error {
	if s.compiler == nil {
		return fmt.Errorf("wiki: nil compiler")
	}
	attr := jobAttribution(job)
	merge, ok, err := s.mergeForJob(ctx, job.ID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("wiki: missing merge payload for job %s", job.ID)
	}

	winner, err := s.subjects.Get(ctx, merge.ToSubjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return s.finishStaleMerge(ctx, job.ID)
	}
	if err != nil {
		return err
	}
	loser, err := s.subjects.Get(ctx, merge.FromSubjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return s.finishStaleMerge(ctx, job.ID)
	} else if err != nil {
		return err
	}

	combined, crossEdges, err := s.mergeEffectiveClaims(ctx, attr, loser, winner)
	if err != nil {
		return err
	}
	title, body, err := s.compiler.Compile(ctx, attr, winner, combined)
	if err != nil {
		return err
	}

	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, job.ID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if status != JobWorking {
		return nil
	}

	subjects := NewSubjectStore(tx)
	claims := NewClaimStore(tx)
	pages := NewPageStore(tx)
	aliases := NewAliasStore(tx)
	embeddings := NewEmbeddingStore(tx)
	suppressions := NewSuppressionStore(tx)
	if _, err := subjects.Get(ctx, merge.ToSubjectID); errors.Is(err, sql.ErrNoRows) {
		committed, err := finishDoneInTx(ctx, tx, job.ID, s.now())
		if committed {
			s.notify()
		}
		return err
	} else if err != nil {
		return err
	}
	loser, err = subjects.Get(ctx, merge.FromSubjectID)
	if errors.Is(err, sql.ErrNoRows) {
		committed, err := finishDoneInTx(ctx, tx, job.ID, s.now())
		if committed {
			s.notify()
		}
		return err
	} else if err != nil {
		return err
	}
	if err := pages.DeleteBySubject(ctx, merge.FromSubjectID); err != nil {
		return err
	}
	if err := embeddings.Delete(ctx, merge.FromSubjectID); err != nil {
		return err
	}
	if err := claims.RepointSubject(ctx, merge.FromSubjectID, merge.ToSubjectID); err != nil {
		return err
	}
	for _, edge := range crossEdges {
		if err := suppressions.Insert(ctx, edge); err != nil {
			return err
		}
	}
	if err := aliases.RepointSubject(ctx, merge.FromSubjectID, merge.ToSubjectID); err != nil {
		return err
	}
	if _, err := aliases.GetByNormName(ctx, job.Scope, loser.Name); errors.Is(err, sql.ErrNoRows) {
		if err := aliases.Insert(ctx, job.Scope, Alias{
			NormName:   Normalize(loser.Name),
			SubjectID:  merge.ToSubjectID,
			Name:       loser.Name,
			OwnerID:    job.OwnerID,
			OwnerEmail: job.OwnerEmail,
			CreatedAt:  formatTime(s.now()),
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := subjects.Delete(ctx, merge.FromSubjectID); err != nil {
		return err
	}
	winnerPage := Page{
		ID:        merge.ToSubjectID,
		SubjectID: merge.ToSubjectID,
		Title:     title,
		Body:      body,
	}
	if err := pages.Upsert(ctx, winnerPage); err != nil {
		return err
	}
	committed, err := finishDoneInTx(ctx, tx, job.ID, s.now())
	if err != nil || !committed {
		return err
	}
	s.notify()
	if s.vectorCacheRemove != nil {
		s.vectorCacheRemove(merge.FromSubjectID)
	}
	if s.AskInvalidate != nil {
		s.AskInvalidate(job.Scope)
	}
	return s.embedAndStore(ctx, attr, job.Scope, winnerPage)
}

func jobAttribution(job Job) llm.Attribution {
	origin := "service:wiki"
	if owner := strings.TrimSpace(job.OwnerEmail); owner != "" {
		origin = "user:" + owner
	}
	return llm.Attribution{Origin: origin, GroupID: job.CorrelationID}
}

func (s *Service) mergeForJob(ctx context.Context, jobID string) (SubjectMerge, bool, error) {
	merge, err := s.merges.GetByJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return SubjectMerge{}, false, nil
	}
	if err != nil {
		return SubjectMerge{}, false, err
	}
	return merge, true, nil
}

func (s *Service) mergeClaims(ctx context.Context, fromSubjectID, toSubjectID string) ([]Claim, error) {
	winnerClaims, err := listAllClaims(ctx, s.claims, toSubjectID)
	if err != nil {
		return nil, err
	}
	loserClaims, err := listAllClaims(ctx, s.claims, fromSubjectID)
	if err != nil {
		return nil, err
	}
	combined := append([]Claim(nil), winnerClaims...)
	for _, claim := range loserClaims {
		claim.SubjectID = toSubjectID
		combined = append(combined, claim)
	}
	sort.Slice(combined, func(i, j int) bool {
		return combined[i].ID < combined[j].ID
	})
	return combined, nil
}

//nolint:gocyclo // Claim reconciliation enumerates the required correction and suppression cases explicitly.
func (s *Service) mergeEffectiveClaims(ctx context.Context, attr llm.Attribution, loser, winner Subject) ([]Claim, []Suppression, error) {
	combined, err := s.mergeClaims(ctx, loser.ID, winner.ID)
	if err != nil {
		return nil, nil, err
	}
	winnerRows, err := listAllClaims(ctx, s.claims, winner.ID)
	if err != nil {
		return nil, nil, err
	}
	loserRows, err := listAllClaims(ctx, s.claims, loser.ID)
	if err != nil {
		return nil, nil, err
	}

	var corrections []match.Statement
	var claims []match.Statement
	subjectByRow := make(map[string]string, len(winnerRows)+len(loserRows))
	appendRows := func(rows []Claim, isLoser bool) {
		for _, row := range rows {
			subjectByRow[row.ID] = row.SubjectID
			statement := match.Statement{Claim: row, New: isLoser}
			switch row.Kind {
			case CorrectionKind:
				corrections = append(corrections, statement)
			case ClaimKind:
				claims = append(claims, statement)
			}
		}
	}
	appendRows(winnerRows, false)
	appendRows(loserRows, true)

	hasCrossPairs := hasCorrections(loserRows) && len(winnerRows) > 0 || hasCorrections(winnerRows) && len(loserRows) > 0
	var crossEdges []Suppression
	if hasCrossPairs {
		if s.matcher == nil {
			return nil, nil, fmt.Errorf("wiki: nil matcher")
		}
		judged, err := s.matcher.Match(ctx, attr, match.Unit{Subject: winner, Corrections: corrections, Claims: claims})
		if err != nil {
			return nil, nil, err
		}
		for _, edge := range judged {
			if subjectByRow[edge.Correction] == subjectByRow[edge.Suppressed] {
				continue
			}
			crossEdges = append(crossEdges, Suppression{
				Correction: edge.Correction,
				Suppressed: edge.Suppressed,
				CreatedAt:  s.now().Unix(),
			})
		}
	}

	edges, err := NewSuppressionStore(s.write).ListBySubject(ctx, winner.ID)
	if err != nil {
		return nil, nil, err
	}
	loserEdges, err := NewSuppressionStore(s.write).ListBySubject(ctx, loser.ID)
	if err != nil {
		return nil, nil, err
	}
	edges = append(edges, loserEdges...)
	edges = append(edges, crossEdges...)
	effective, _ := Effective(combined, edges)
	return effective, crossEdges, nil
}

func hasCorrections(rows []Claim) bool {
	for _, row := range rows {
		if row.Kind == CorrectionKind {
			return true
		}
	}
	return false
}

func (s *Service) finishStaleMerge(ctx context.Context, jobID string) error {
	finished, err := s.jobs.FinishWorking(ctx, jobID, JobDone, s.now(), "")
	if finished {
		s.notify()
	}
	return err
}

func finishDoneInTx(ctx context.Context, tx *sql.Tx, jobID string, finishedAt time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status = ?, finished_at = ?, error = '' WHERE id = ? AND status = ?`,
		JobDone, formatTime(finishedAt), jobID, JobWorking)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ingest_lease WHERE job_id = ?`, jobID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

//nolint:gocritic // Explicit returns keep subject resolution, creation status, and errors distinct.
func (s *Service) plannedSubject(ctx context.Context, scope string, known map[string]Subject, item extract.ExtractedSubject) (Subject, bool, error) {
	normName := Normalize(item.Name)
	if subject, ok := known[normName]; ok {
		return subject, false, nil
	}
	subject, err := s.subjects.GetByNormName(ctx, scope, item.Name)
	if err == nil {
		known[normName] = subject
		return subject, false, nil
	}
	if err != sql.ErrNoRows {
		return Subject{}, false, err
	}
	subject, err = s.resolver.ResolveByName(ctx, scope, item.Name)
	if err == nil {
		known[normName] = subject
		return subject, false, nil
	}
	if !errors.Is(err, ErrSubjectNotFound) {
		return Subject{}, false, err
	}
	subject = Subject{
		Name:     strings.TrimSpace(item.Name),
		NormName: normName,
		Type:     item.Type,
	}
	known[normName] = subject
	return subject, true, nil
}

func (s *Service) plannedClaims(ctx context.Context, jobID, subjectID string, newClaims []Claim) ([]Claim, error) {
	claims, err := listAllClaims(ctx, s.claims, subjectID)
	if err != nil {
		return nil, err
	}
	out := claims[:0]
	for _, claim := range claims {
		if claim.JobID != jobID {
			out = append(out, claim)
		}
	}
	out = append(out, newClaims...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func sortedSubjects(subjects map[string]Subject) []Subject {
	out := make([]Subject, 0, len(subjects))
	for _, subject := range subjects {
		out = append(out, subject)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func listAllSubjects(ctx context.Context, store *SubjectStore, typ, nameContains string) ([]Subject, error) {
	return listAllSubjectsInScope(ctx, store, "default", typ, nameContains)
}

func listAllSubjectsInScope(ctx context.Context, store *SubjectStore, scope, typ, nameContains string) ([]Subject, error) {
	var out []Subject
	params := page.Params{Limit: page.MaxLimit}
	for {
		subjects, next, err := store.ListInScope(ctx, scope, typ, nameContains, params)
		if err != nil {
			return nil, err
		}
		out = append(out, subjects...)
		if next == "" {
			return out, nil
		}
		params.Cursor = next
	}
}

func listAllClaims(ctx context.Context, store *ClaimStore, subjectID string) ([]Claim, error) {
	var out []Claim
	params := page.Params{Limit: page.MaxLimit}
	for {
		claims, next, err := store.ListBySubject(ctx, subjectID, params)
		if err != nil {
			return nil, err
		}
		out = append(out, claims...)
		if next == "" {
			return out, nil
		}
		params.Cursor = next
	}
}

func (s *Service) affectedSubjects(ctx context.Context, subjects *SubjectStore, claims *ClaimStore, jobID string) (map[string]Subject, error) {
	affected := map[string]Subject{}
	ids, err := claims.SubjectIDsByJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		subject, err := subjects.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		affected[subject.ID] = subject
	}
	return affected, nil
}

func (s *Service) registerJobCancel(jobID string, cancel context.CancelFunc) *jobCancel {
	registered := &jobCancel{cancel: cancel}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancels[jobID] = registered
	return registered
}

func (s *Service) unregisterJobCancel(jobID string, registered *jobCancel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancels[jobID] == registered {
		delete(s.cancels, jobID)
	}
	registered.cancel()
}

func (s *Service) cancelJob(jobID string) {
	s.mu.Lock()
	registered := s.cancels[jobID]
	s.mu.Unlock()
	if registered != nil {
		registered.cancel()
	}
}

func (s *Service) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
