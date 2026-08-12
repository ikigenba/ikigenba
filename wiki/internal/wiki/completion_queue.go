package wiki

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"wiki/internal/extract"
	"wiki/internal/llm"
)

type pipelineEnvelope struct {
	JobID   string `json:"job_id"`
	Stage   string `json:"stage"`
	Subject string `json:"subject,omitempty"`
	Units   int    `json:"units,omitempty"`
}

type stagedIntegration struct {
	NewSubjects []stagedSubject `json:"new_subjects"`
	Claims      []stagedClaim   `json:"claims"`
	Pages       []stagedPage    `json:"pages"`
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
		out = append(out, Claim{ID: claim.ID, SubjectID: subjectID, JobID: claim.JobID, Body: claim.Body})
	}
	return out
}

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
	_, err = s.jobs.MarkPhase(ctx, job.ID)
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

// ProcessInbox drains all terminal completion items currently visible.
func (s *Service) ProcessInbox(ctx context.Context) (int, error) {
	if s == nil || s.queue == nil {
		return 0, nil
	}
	items, err := s.queue.Inbox(ctx)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, item := range items {
		if item.Status != "done" && item.Status != "failed" {
			continue
		}
		if err := s.applyCompletion(ctx, item); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (s *Service) applyCompletion(ctx context.Context, item llm.Item) error {
	var envelope pipelineEnvelope
	if err := json.Unmarshal(item.Context, &envelope); err != nil || envelope.JobID == "" || (envelope.Stage != "extract" && envelope.Stage != "compile") {
		return s.queue.Ack(ctx, item.ID)
	}
	job, err := s.jobs.Get(ctx, envelope.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return s.queue.Ack(ctx, item.ID)
	}
	if err != nil {
		return err
	}
	if job.Status != JobWorking {
		return s.queue.Ack(ctx, item.ID)
	}
	if envelope.Stage != job.Phase && !(item.Status == "done" && envelope.Stage == "extract" && job.Phase == "compile") {
		return s.queue.Ack(ctx, item.ID)
	}
	ctx, job = s.jobContext(ctx, job)
	ctx, err = s.contextForScope(ctx, job.Scope)
	if err != nil {
		return err
	}
	if valid, err := s.pipelineStageMatches(ctx, job.ID, envelope); err != nil {
		return err
	} else if !valid {
		return s.queue.Ack(ctx, item.ID)
	}
	if item.Status == "failed" {
		if finished, err := s.jobs.FailWaiting(ctx, job.ID, s.now(), item.Error); err != nil {
			return err
		} else if finished {
			s.notify()
		}
		return s.queue.Ack(ctx, item.ID)
	}
	switch envelope.Stage {
	case "extract":
		return s.applyExtract(ctx, job, item)
	case "compile":
		return s.applyCompile(ctx, job, envelope, item)
	default:
		return s.queue.Ack(ctx, item.ID)
	}
}

func (s *Service) pipelineStageMatches(ctx context.Context, jobID string, envelope pipelineEnvelope) (bool, error) {
	if envelope.Stage == "extract" {
		return envelope.Subject == "" && envelope.Units == 0, nil
	}
	plan, found, err := s.loadStagedIntegration(ctx, jobID)
	if err != nil || !found {
		return false, err
	}
	_, found = compilePage(plan, envelope.Subject)
	return found && envelope.Units == compileUnits(plan), nil
}

func (s *Service) applyExtract(ctx context.Context, job Job, item llm.Item) error {
	plan, found, err := s.loadStagedIntegration(ctx, job.ID)
	if err != nil {
		return err
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
			return err
		}
	}
	for _, page := range plan.Pages {
		if page.Delete {
			continue
		}
		staged, err := s.compileUnitStaged(ctx, job.ID, page.Subject.NormName)
		if err != nil {
			return err
		}
		if staged {
			continue
		}
		envelope, err := json.Marshal(pipelineEnvelope{JobID: job.ID, Stage: "compile", Subject: page.Subject.NormName, Units: compileUnits(plan)})
		if err != nil {
			return err
		}
		_, err = s.queue.Ensure(ctx, llm.Request{
			Key:      job.ID + "/compile/" + page.Subject.NormName + "/a1",
			Context:  envelope,
			Site:     s.compileSite,
			Attr:     jobAttribution(job),
			Attempt:  1,
			Messages: []llm.Message{{Role: "user", Text: renderCompile(page.Subject.subject(""), modelClaims(page.Claims, ""))}},
		})
		if err != nil {
			return err
		}
	}
	if compileUnits(plan) == 0 {
		if err := s.integrateStaged(ctx, job, plan); err != nil {
			return err
		}
	}
	return s.queue.Ack(ctx, item.ID)
}

func compileUnits(plan stagedIntegration) int {
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

func (s *Service) stageExtractPlan(ctx context.Context, job Job, extracted []extract.ExtractedSubject) (stagedIntegration, error) {
	affected, err := s.affectedSubjects(ctx, s.subjects, s.claims, job.ID)
	if err != nil {
		return stagedIntegration{}, err
	}
	knownByNorm := map[string]Subject{}
	newByNorm := map[string]bool{}
	claimsByNorm := map[string][]stagedClaim{}
	pageByNorm := map[string]stagedSubject{}
	plan := stagedIntegration{}
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
			claim := stagedClaim{ID: s.newID(), NormName: subject.NormName, JobID: job.ID, Body: strings.TrimSpace(body)}
			plan.Claims = append(plan.Claims, claim)
			claimsByNorm[subject.NormName] = append(claimsByNorm[subject.NormName], claim)
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
		if subject.ID != "" {
			existing, err := s.plannedClaims(ctx, job.ID, subject.ID, nil)
			if err != nil {
				return stagedIntegration{}, err
			}
			for _, claim := range existing {
				claims = append(claims, stagedClaim{ID: claim.ID, NormName: normName, JobID: claim.JobID, Body: claim.Body})
			}
		}
		sort.Slice(claims, func(i, j int) bool { return claims[i].ID < claims[j].ID })
		plan.Pages = append(plan.Pages, stagedPage{Subject: pageByNorm[normName], Claims: claims, Delete: len(claims) == 0})
	}
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
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET phase = 'compile' WHERE id = ? AND status = ? AND phase = 'extract'`, job.ID, JobWorking)
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

func (s *Service) applyCompile(ctx context.Context, job Job, envelope pipelineEnvelope, item llm.Item) error {
	plan, found, err := s.loadStagedIntegration(ctx, job.ID)
	if err != nil {
		return err
	}
	_, ok := compilePage(plan, envelope.Subject)
	if !found || !ok || envelope.Units != compileUnits(plan) {
		return s.queue.Ack(ctx, item.ID)
	}
	title, body, err := parseCompile(item.Result)
	if err != nil {
		return s.correctOrFail(ctx, job, item, "compile", envelope.Subject, envelope.Units, err)
	}
	payload, err := json.Marshal(stagedCompiledPage{Title: title, Body: body})
	if err != nil {
		return err
	}
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO job_staging (job_id, stage, unit, payload, units) VALUES (?, 'compile', ?, ?, ?)
		ON CONFLICT(job_id, stage, unit) DO UPDATE SET payload = excluded.payload, units = excluded.units`,
		job.ID, envelope.Subject, string(payload), envelope.Units)
	if err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_staging WHERE job_id = ? AND stage = 'compile'`, job.ID).Scan(&count); err != nil {
		return err
	}
	var committed stagedCommit
	if count == envelope.Units {
		committed, err = s.integrateStagedTx(ctx, tx, job, plan)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := s.afterStagedCommit(ctx, job, committed); err != nil {
		return err
	}
	return s.queue.Ack(ctx, item.ID)
}

func compilePage(plan stagedIntegration, normName string) (stagedPage, bool) {
	for _, page := range plan.Pages {
		if !page.Delete && page.Subject.NormName == normName {
			return page, true
		}
	}
	return stagedPage{}, false
}

func (s *Service) correctOrFail(ctx context.Context, job Job, item llm.Item, stage, subject string, units int, semanticErr error) error {
	attempt := keyAttempt(item.Key)
	site := s.extractSite
	var original string
	if stage == "extract" {
		original = extract.Render(extract.DocumentHeader{Source: "mcp:ingest_text", Title: job.Title, Tags: job.Tags, ReceivedAt: job.ReceivedAt}, job.SourceText)
	} else {
		site = s.compileSite
		plan, found, err := s.loadStagedIntegration(ctx, job.ID)
		if err != nil {
			return err
		}
		page, ok := compilePage(plan, subject)
		if !found || !ok {
			return s.queue.Ack(ctx, item.ID)
		}
		original = renderCompile(page.Subject.subject(""), modelClaims(page.Claims, ""))
	}
	if attempt >= site.MaxParseRetries+1 {
		if finished, err := s.jobs.FailWaiting(ctx, job.ID, s.now(), semanticErr.Error()); err != nil {
			return err
		} else if finished {
			s.notify()
		}
		return s.queue.Ack(ctx, item.ID)
	}
	next := attempt + 1
	envelope, err := json.Marshal(pipelineEnvelope{JobID: job.ID, Stage: stage, Subject: subject, Units: units})
	if err != nil {
		return err
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
		return err
	}
	return s.queue.Ack(ctx, item.ID)
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
	for _, staged := range plan.Claims {
		subject := resolved[staged.NormName]
		claim := Claim{ID: staged.ID, SubjectID: subject.ID, JobID: staged.JobID, Body: staged.Body}
		if err := claims.Save(ctx, claim); err != nil {
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
	return stagedCommit{changed: true}, nil
}
