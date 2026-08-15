package wiki_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"wiki/internal/llm"
	wikidomain "wiki/internal/wiki"
)

func TestEveryDecisionIsLoggedAndDeferredOnlyDrainReportsNoProgress(t *testing.T) {
	// R-PG5V-L9NS
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	svc := wikidomain.NewService(conn, nil, nil, time.Now, wikidomain.WithCompletionQueue(q.client(), llm.CallSite{}, llm.CallSite{}), wikidomain.WithLogger(logger))
	q.add(queueItem{ID: "deferred-decision", Status: "done", Context: json.RawMessage(`{"job_id":"missing","stage":"extract"}`), Result: json.RawMessage(`{}`)})
	q.failDeletes(2)
	first := svc.ProcessInbox(ctx)
	drain := svc.ProcessInbox(ctx)
	if !first.NoProgress || !drain.NoProgress || drain.Deferred != 1 || len(drain.Errs) == 0 {
		t.Fatalf("drain = %+v, want explicit no progress", drain)
	}
	text := logs.String()
	for _, field := range []string{`"job_id":"missing"`, `"stage":"extract"`, `"outcome":"deferred"`, `"reason":"ack_unreachable"`, `"duration"`, `"seen":1`, `"no_progress_streak":2`} {
		if !strings.Contains(text, field) {
			t.Fatalf("logs missing %s: %s", field, text)
		}
	}
}

func TestLandingUnitExtendsDeadlineButAbsoluteLifetimeStillReaps(t *testing.T) {
	// R-PBAA-26P0
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := wikidomain.NewService(conn, nil, nil, func() time.Time { return now }, wikidomain.WithCompletionQueue(q.client(), llm.CallSite{}, llm.CallSite{}))
	jobID, request := ingestAndHandoff(t, ctx, svc, q)
	var firstDeadline string
	if err := conn.QueryRowContext(ctx, `SELECT deadline_at FROM jobs WHERE id = ?`, jobID).Scan(&firstDeadline); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	q.add(queueItem{ID: "landed-extract", Status: "done", Context: request.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote code."]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 {
		t.Fatalf("land extract = %+v", drain)
	}
	var extended string
	if err := conn.QueryRowContext(ctx, `SELECT deadline_at FROM jobs WHERE id = ?`, jobID).Scan(&extended); err != nil {
		t.Fatal(err)
	}
	if extended <= firstDeadline {
		t.Fatalf("deadline did not extend: before %q after %q", firstDeadline, extended)
	}
	now = now.Add(24*time.Hour - 10*time.Minute + time.Second)
	if n, err := svc.ReapExpired(ctx, now); err != nil || n != 1 {
		t.Fatalf("absolute lifetime reap = %d/%v", n, err)
	}
}

type queueRequest struct {
	Key      string          `json:"key"`
	Context  json.RawMessage `json:"context"`
	GroupID  string          `json:"group_id"`
	Attempt  int             `json:"attempt"`
	System   string          `json:"system"`
	Messages []llm.Message   `json:"messages"`
}

type queueItem struct {
	ID      string          `json:"id"`
	Key     string          `json:"key"`
	Status  string          `json:"status"`
	Context json.RawMessage `json:"context"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type testQueue struct {
	mu             sync.Mutex
	requests       []queueRequest
	inbox          []queueItem
	acked          []string
	nextID         int
	postFailures   int
	deleteFailures int
	server         *httptest.Server
}

func newTestQueue(t *testing.T) *testQueue {
	t.Helper()
	q := &testQueue{}
	q.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q.mu.Lock()
		defer q.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			if q.postFailures > 0 {
				q.postFailures--
				http.Error(w, "prompts temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			var request queueRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			q.requests = append(q.requests, request)
			q.nextID++
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(queueItem{ID: "ensured-" + request.Key, Key: request.Key, Status: "pending", Context: request.Context})
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(q.inbox)
		case http.MethodDelete:
			if q.deleteFailures > 0 {
				q.deleteFailures--
				http.Error(w, "prompts temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			id := strings.TrimPrefix(r.URL.Path, "/completions/")
			q.acked = append(q.acked, id)
			kept := q.inbox[:0]
			for _, item := range q.inbox {
				if item.ID != id {
					kept = append(kept, item)
				}
			}
			q.inbox = kept
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(q.server.Close)
	return q
}

func (q *testQueue) client() *llm.Client { return llm.New(q.server.URL) }

func (q *testQueue) add(items ...queueItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.inbox = append(q.inbox, items...)
}

func (q *testQueue) failPosts(n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.postFailures = n
}

func (q *testQueue) failDeletes(n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.deleteFailures = n
}

func (q *testQueue) snapshot() ([]queueRequest, []string, int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]queueRequest(nil), q.requests...), append([]string(nil), q.acked...), len(q.inbox)
}

func queueService(conn *sql.DB, q *testQueue) *wikidomain.Service {
	siteExtract := llm.CallSite{Stage: "extract", Config: llm.Config{Model: "extract"}, MaxParseRetries: 1}
	siteCompile := llm.CallSite{Stage: "compile", Config: llm.Config{Model: "compile"}, MaxParseRetries: 1}
	return wikidomain.NewService(conn, nil, nil, time.Now, wikidomain.WithCompletionQueue(q.client(), siteExtract, siteCompile))
}

func ingestAndHandoff(t *testing.T, ctx context.Context, svc *wikidomain.Service, q *testQueue) (string, queueRequest) {
	t.Helper()
	jobID, err := svc.Ingest(ctx, "default", "owner", "owner@example.com", "Ada and Grace wrote compilers.", "Compiler history", nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("ProcessNext = %v, %v", processed, err)
	}
	requests, _, _ := q.snapshot()
	return jobID, requests[len(requests)-1]
}

func TestClaimHandoffUsesDerivedStableExtractKeyAndSourceText(t *testing.T) {
	// R-K73J-J3W3
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)

	jobID, first := ingestAndHandoff(t, ctx, svc, q)
	status, err := svc.JobStatus(ctx, jobID)
	if err != nil || status.Status != wikidomain.JobWorking {
		t.Fatalf("status after handoff = %+v, %v", status, err)
	}
	var phase string
	if err := conn.QueryRowContext(ctx, `SELECT phase FROM jobs WHERE id = ?`, jobID).Scan(&phase); err != nil || phase != "extract" {
		t.Fatalf("phase after handoff = %q, %v", phase, err)
	}
	if first.Key != jobID+"/extract/a1" || !strings.Contains(first.Messages[0].Text, "Ada and Grace wrote compilers.") {
		t.Fatalf("extract request = %+v", first)
	}
	var envelope map[string]any
	if err := json.Unmarshal(first.Context, &envelope); err != nil || envelope["job_id"] != jobID || envelope["stage"] != "extract" {
		t.Fatalf("extract envelope = %s, %v", first.Context, err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM ingest_lease WHERE job_id = ?`, jobID); err != nil {
		t.Fatalf("simulate sweep lease release: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE jobs SET status = 'pending' WHERE id = ?`, jobID); err != nil {
		t.Fatalf("simulate claim crash: %v", err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("reclaim = %v, %v", processed, err)
	}
	requests, _, _ := q.snapshot()
	if requests[len(requests)-1].Key != first.Key {
		t.Fatalf("reclaim key = %q, want %q", requests[len(requests)-1].Key, first.Key)
	}
}

func TestExtractReplayStagesOnceFansOutAndAcksAfterConsequences(t *testing.T) {
	// R-K8BF-WVMS
	// R-OQJZ-K337
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	result := json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote a compiler."]},{"type":"entity","kind":"person","name":"Grace","claims":["Grace wrote a compiler."]}]}`)
	item := queueItem{ID: "extract-done", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: result}
	q.add(item)
	if drain := svc.ProcessInbox(ctx); drain.Seen != 1 || len(drain.Errs) != 0 {
		t.Fatalf("ProcessInbox = %+v", drain)
	}
	var staged int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_staging WHERE job_id = ?`, jobID).Scan(&staged); err != nil || staged != 1 {
		t.Fatalf("staging rows = %d, %v", staged, err)
	}
	requests, acked, _ := q.snapshot()
	compileKeys := map[string]bool{}
	for _, request := range requests {
		if strings.Contains(request.Key, "/compile/") {
			compileKeys[request.Key] = true
			var envelope map[string]any
			_ = json.Unmarshal(request.Context, &envelope)
			if envelope["units"] != float64(2) {
				t.Fatalf("compile envelope = %s, want units 2", request.Context)
			}
		}
	}
	if len(compileKeys) != 2 || len(acked) != 1 {
		t.Fatalf("compile keys/acks = %#v/%#v", compileKeys, acked)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO job_staging(job_id, stage, unit, payload, units)
		VALUES (?, 'compile', 'ada', '{}', 2)`, jobID); err != nil {
		t.Fatalf("stage applied Ada unit: %v", err)
	}
	before := len(requests)
	q.add(item)
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("replay extract after one unit: %v", drain.Errs)
	}
	requests, _, _ = q.snapshot()
	if got := requests[before:]; len(got) != 1 || got[0].Key != jobID+"/compile/grace/a1" {
		t.Fatalf("ensures after one staged unit = %#v, want only Grace", got)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO job_staging(job_id, stage, unit, payload, units)
		VALUES (?, 'compile', 'grace', '{}', 2)`, jobID); err != nil {
		t.Fatalf("stage applied Grace unit: %v", err)
	}
	before = len(requests)
	q.add(item)
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("replay extract after all units: %v", drain.Errs)
	}
	requests, _, _ = q.snapshot()
	if got := requests[before:]; len(got) != 0 {
		t.Fatalf("ensures after all units staged = %#v, want none", got)
	}
}

func TestCompileApplyRollsBackStagedUnitWhenTerminalWriteFails(t *testing.T) {
	// R-OVFL-361Z
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote a compiler."]}]}`)})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("apply extract: %v", drain.Errs)
	}
	requests, _, _ := q.snapshot()
	compileRequest := requests[len(requests)-1]
	if _, err := conn.ExecContext(ctx, `
		CREATE TRIGGER inject_done_failure
		BEFORE UPDATE OF status ON jobs
		WHEN NEW.status = 'done'
		BEGIN SELECT RAISE(ABORT, 'injected terminal failure'); END`); err != nil {
		t.Fatalf("install failure injection: %v", err)
	}
	q.add(queueItem{ID: "compile", Key: compileRequest.Key, Status: "done", Context: compileRequest.Context, Result: json.RawMessage(`{"title":"Ada","body":"Ada wrote a compiler."}`)})
	drain := svc.ProcessInbox(ctx)
	if drain.JobsFailed != 1 || len(drain.Errs) == 0 || !strings.Contains(drain.Errs[0].Error(), "injected terminal failure") {
		t.Fatalf("compile apply = %+v, want permanent injected terminal failure", drain)
	}
	var staged int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_staging WHERE job_id = ? AND stage = 'compile'`, jobID).Scan(&staged); err != nil || staged != 0 {
		t.Fatalf("compile staging rows after rollback = %d, %v; want zero", staged, err)
	}
	status, err := svc.JobStatus(ctx, jobID)
	if err != nil || status.Status != wikidomain.JobFailed {
		t.Fatalf("job after rollback = %+v, %v; want failed", status, err)
	}
}

func TestLastCompileAtomicallyIntegratesAndRedeliveryIsStray(t *testing.T) {
	// R-K9JC-ANDH
	// R-MB7F-ZRE9
	// R-MCFC-DJ4Y
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote a compiler."]},{"type":"entity","kind":"person","name":"Grace","claims":["Grace wrote a compiler."]}]}`)})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("apply extract: %v", drain.Errs)
	}
	requests, _, _ := q.snapshot()
	var compileRequests []queueRequest
	for _, request := range requests {
		if strings.Contains(request.Key, "/compile/") {
			compileRequests = append(compileRequests, request)
		}
	}
	for i, request := range compileRequests {
		name := "Ada"
		if strings.Contains(request.Key, "/grace/") {
			name = "Grace"
		}
		q.add(queueItem{ID: "compile-" + name, Key: request.Key, Status: "done", Context: request.Context, Result: json.RawMessage(`{"title":"` + name + `","body":"` + name + ` wrote a compiler."}`)})
		if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
			t.Fatalf("apply compile %d: %v", i, drain.Errs)
		}
		status, _ := svc.JobStatus(ctx, jobID)
		want := wikidomain.JobWorking
		if i == len(compileRequests)-1 {
			want = wikidomain.JobDone
		}
		if status.Status != want {
			t.Fatalf("status after compile %d = %q, want %q", i, status.Status, want)
		}
	}
	assertTableCount(t, ctx, conn, "claims", 2)
	assertTableCount(t, ctx, conn, "pages", 2)
	assertTableCount(t, ctx, conn, "job_staging", 0)
	last := compileRequests[len(compileRequests)-1]
	q.add(queueItem{ID: "redelivery", Key: last.Key, Status: "done", Context: last.Context, Result: json.RawMessage(`{"title":"Corrupt","body":"Corrupt"}`)})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("redelivery: %v", drain.Errs)
	}
	assertTableCount(t, ctx, conn, "claims", 2)
	assertTableCount(t, ctx, conn, "pages", 2)
}

func TestSemanticFailuresCorrectThenExhaustWithoutPartialWrites(t *testing.T) {
	// R-KAR8-OF46
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, request := ingestAndHandoff(t, ctx, svc, q)
	bad := json.RawMessage(`{"subjects":[{"type":"wrong"}]}`)
	q.add(queueItem{ID: "bad-a1", Key: request.Key, Status: "done", Context: request.Context, Result: bad})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("first bad result: %v", drain.Errs)
	}
	requests, _, _ := q.snapshot()
	corrective := requests[len(requests)-1]
	if corrective.Key != jobID+"/extract/a2" || len(corrective.Messages) != 3 {
		t.Fatalf("corrective request = %+v", corrective)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobWorking {
		t.Fatalf("status after correction = %q", status.Status)
	}
	q.add(queueItem{ID: "bad-a2", Key: corrective.Key, Status: "done", Context: corrective.Context, Result: bad})
	drain := svc.ProcessInbox(ctx)
	if drain.JobsFailed != 1 {
		t.Fatalf("exhausted result = %+v", drain)
	}
	status, _ = svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobFailed || status.Error == "" {
		t.Fatalf("exhausted status = %+v", status)
	}
	for _, table := range []string{"subjects", "claims", "pages"} {
		assertTableCount(t, ctx, conn, table, 0)
	}
}

func TestFailedAndStrayItemsDrainWithoutPartialState(t *testing.T) {
	// R-KBZ5-26UV
	// R-KEEX-TQC9
	// R-KFMU-7I2Y
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, request := ingestAndHandoff(t, ctx, svc, q)
	if _, err := conn.ExecContext(ctx, `INSERT INTO job_staging(job_id, stage, unit, payload, units) VALUES (?, 'extract', '', '{}', 1)`, jobID); err != nil {
		t.Fatalf("seed staging: %v", err)
	}
	unknownContext := json.RawMessage(`{"job_id":"missing","stage":"extract"}`)
	nonPipeline := json.RawMessage(`{"job_id":"` + jobID + `","stage":"ask"}`)
	mismatched := json.RawMessage(`{"job_id":"` + jobID + `","stage":"compile","subject":"missing","units":1}`)
	q.add(
		queueItem{ID: "unknown", Key: "x", Status: "done", Context: unknownContext, Result: json.RawMessage(`{}`)},
		queueItem{ID: "non-pipeline", Key: "y", Status: "done", Context: nonPipeline, Result: json.RawMessage(`{}`)},
		queueItem{ID: "mismatched", Key: "z", Status: "done", Context: mismatched, Result: json.RawMessage(`{}`)},
		queueItem{ID: "failed", Key: request.Key, Status: "failed", Context: request.Context, Error: "provider exploded"},
	)
	if drain := svc.ProcessInbox(ctx); drain.Seen != 4 || drain.JobsFailed != 1 {
		t.Fatalf("mixed inbox drain = %+v", drain)
	}
	_, acked, remaining := q.snapshot()
	if len(acked) != 4 || remaining != 0 {
		t.Fatalf("acked/remaining = %#v/%d", acked, remaining)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobFailed || !strings.Contains(status.Error, "provider exploded") {
		t.Fatalf("failed status = %+v", status)
	}
	assertTableCount(t, ctx, conn, "job_staging", 0)
	if _, err := conn.ExecContext(ctx, `INSERT INTO jobs(id, scope, status, phase, owner_id, owner_email) VALUES ('working-job','default','working','','',''), ('phased-job','default','working','extract','','')`); err != nil {
		t.Fatalf("seed sweep jobs: %v", err)
	}
	if n, err := svc.RequeueWorking(ctx); err != nil || n != 1 {
		t.Fatalf("RequeueWorking = %d, %v", n, err)
	}
	for id, want := range map[string][2]string{"working-job": {wikidomain.JobPending, ""}, "phased-job": {wikidomain.JobWorking, "extract"}} {
		var status, phase string
		_ = conn.QueryRowContext(ctx, `SELECT status, phase FROM jobs WHERE id = ?`, id).Scan(&status, &phase)
		if got := [2]string{status, phase}; got != want {
			t.Fatalf("%s status/phase = %q, want %q", id, got, want)
		}
	}
}

func TestAbortWaitingClearsStagingAndLateResultIsDiscarded(t *testing.T) {
	// R-KGUQ-L9TN
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, request := ingestAndHandoff(t, ctx, svc, q)
	if _, err := conn.ExecContext(ctx, `INSERT INTO job_staging(job_id, stage, unit, payload, units) VALUES (?, 'extract', '', '{}', 1)`, jobID); err != nil {
		t.Fatalf("seed staging: %v", err)
	}
	result, err := svc.Abort(ctx, jobID)
	if err != nil || !result.Aborted || result.Status != wikidomain.JobAborted {
		t.Fatalf("Abort = %+v, %v", result, err)
	}
	assertTableCount(t, ctx, conn, "job_staging", 0)
	q.add(queueItem{ID: "late", Key: request.Key, Status: "done", Context: request.Context, Result: json.RawMessage(`{"subjects":[]}`)})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("late result: %v", drain.Errs)
	}
	for _, table := range []string{"subjects", "claims", "pages"} {
		assertTableCount(t, ctx, conn, table, 0)
	}
}

func TestRerunAbortedJobCompletesQueuePipelineOnce(t *testing.T) {
	// R-KI2M-Z1KC
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, oldRequest := ingestAndHandoff(t, ctx, svc, q)
	if _, err := svc.Abort(ctx, jobID); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if result, err := svc.Rerun(ctx, jobID); err != nil || !result.Requeued {
		t.Fatalf("Rerun = %+v, %v", result, err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("rerun handoff = %v, %v", processed, err)
	}
	requests, _, _ := q.snapshot()
	current := requests[len(requests)-1]
	if current.Key != oldRequest.Key {
		t.Fatalf("rerun extract key = %q, want deduped %q", current.Key, oldRequest.Key)
	}
	q.add(queueItem{ID: "rerun-extract", Key: current.Key, Status: "done", Context: current.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote a compiler."]}]}`)})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("rerun extract apply: %v", drain.Errs)
	}
	requests, _, _ = q.snapshot()
	compileRequest := requests[len(requests)-1]
	q.add(queueItem{ID: "rerun-compile", Key: compileRequest.Key, Status: "done", Context: compileRequest.Context, Result: json.RawMessage(`{"title":"Ada","body":"Ada wrote a compiler."}`)})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("rerun compile apply: %v", drain.Errs)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobDone || len(status.Subjects) != 1 {
		t.Fatalf("rerun status = %+v", status)
	}
	q.add(queueItem{ID: "stale", Key: oldRequest.Key, Status: "done", Context: oldRequest.Context, Result: json.RawMessage(`{"subjects":[]}`)})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("stale delivery: %v", drain.Errs)
	}
	assertTableCount(t, ctx, conn, "claims", 1)
	assertTableCount(t, ctx, conn, "pages", 1)
}

func TestInboxDrainContinuesAfterFirstItemPermanentlyFails(t *testing.T) {
	// R-P56S-5BZJ
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	for _, scope := range []string{"first", "later"} {
		if _, err := wikidomain.NewScopeStore(conn).Create(ctx, scope); err != nil {
			t.Fatalf("create scope %s: %v", scope, err)
		}
		if _, err := svc.Ingest(ctx, scope, "owner", "owner@example.com", scope+" document", scope, nil); err != nil {
			t.Fatalf("ingest %s: %v", scope, err)
		}
		if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
			t.Fatalf("handoff %s = %v, %v", scope, processed, err)
		}
	}
	requests, _, _ := q.snapshot()
	firstJob := requests[len(requests)-2].GroupID
	laterJob := requests[len(requests)-1].GroupID
	q.add(
		queueItem{ID: "poison", Key: requests[len(requests)-2].Key, Status: "failed", Context: requests[len(requests)-2].Context, Error: "provider rejected the document"},
		queueItem{ID: "applicable", Key: requests[len(requests)-1].Key, Status: "done", Context: requests[len(requests)-1].Context, Result: json.RawMessage(`{"subjects":[]}`)},
	)
	drain := svc.ProcessInbox(ctx)
	if drain.Seen != 2 || drain.JobsFailed != 1 || drain.Applied != 1 {
		t.Fatalf("drain = %+v, want failed first item and applied later item", drain)
	}
	firstStatus, _ := svc.JobStatus(ctx, firstJob)
	laterStatus, _ := svc.JobStatus(ctx, laterJob)
	if firstStatus.Status != wikidomain.JobFailed || laterStatus.Status != wikidomain.JobDone {
		t.Fatalf("statuses after one tick = first %q, later %q", firstStatus.Status, laterStatus.Status)
	}
	if next := svc.ProcessInbox(ctx); next.Seen != 0 {
		t.Fatalf("next drain = %+v, want poison item gone", next)
	}
}

func TestPermanentCompletionFailureCleansJobAndAcks(t *testing.T) {
	// R-P6EO-J3Q8
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, request := ingestAndHandoff(t, ctx, svc, q)
	if _, err := conn.ExecContext(ctx, `INSERT INTO job_staging(job_id, stage, unit, payload, units) VALUES (?, 'extract', '', '{}', 1)`, jobID); err != nil {
		t.Fatalf("seed staging: %v", err)
	}
	q.add(queueItem{ID: "permanent", Key: request.Key, Status: "failed", Context: request.Context, Error: "model rejected payload"})
	drain := svc.ProcessInbox(ctx)
	status, err := svc.JobStatus(ctx, jobID)
	if err != nil || drain.JobsFailed != 1 || status.Status != wikidomain.JobFailed || !strings.Contains(status.Error, "model rejected payload") {
		t.Fatalf("drain/status = %+v/%+v, %v", drain, status, err)
	}
	assertTableCount(t, ctx, conn, "job_staging", 0)
	assertTableCount(t, ctx, conn, "ingest_lease", 0)
	_, acked, remaining := q.snapshot()
	if len(acked) != 1 || remaining != 0 || svc.ProcessInbox(ctx).Seen != 0 {
		t.Fatalf("queue after permanent failure = acked %#v, remaining %d", acked, remaining)
	}
}

func TestTransientCompletionDefersThenFailsAtBound(t *testing.T) {
	// R-P7MK-WVGX
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, request := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "transient", Key: request.Key, Status: "done", Context: request.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote a compiler."]}]}`)})
	q.failPosts(maxTestDeferrals)
	for attempt := 1; attempt < maxTestDeferrals; attempt++ {
		drain := svc.ProcessInbox(ctx)
		_, _, remaining := q.snapshot()
		if drain.Deferred != 1 || drain.JobsFailed != 0 || remaining != 1 {
			t.Fatalf("deferral %d = %+v, remaining %d", attempt, drain, remaining)
		}
	}
	drain := svc.ProcessInbox(ctx)
	status, _ := svc.JobStatus(ctx, jobID)
	_, acked, remaining := q.snapshot()
	if drain.JobsFailed != 1 || drain.Deferred != 0 || status.Status != wikidomain.JobFailed || !strings.Contains(status.Error, "temporarily unavailable") || len(acked) != 1 || remaining != 0 {
		t.Fatalf("bounded deferral = %+v, status %+v, acked %#v, remaining %d", drain, status, acked, remaining)
	}
}

const maxTestDeferrals = 3

func TestAckFailureAfterCommitRedeliversAsDiscarded(t *testing.T) {
	// R-P8UH-AN7M
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "extract-for-ack-test", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote a compiler."]}]}`)})
	if drain := svc.ProcessInbox(ctx); len(drain.Errs) != 0 {
		t.Fatalf("apply extract: %v", drain.Errs)
	}
	requests, _, _ := q.snapshot()
	compileRequest := requests[len(requests)-1]
	q.add(queueItem{ID: "commit-before-ack", Key: compileRequest.Key, Status: "done", Context: compileRequest.Context, Result: json.RawMessage(`{"title":"Ada","body":"Committed body."}`)})
	q.failDeletes(1)
	first := svc.ProcessInbox(ctx)
	status, _ := svc.JobStatus(ctx, jobID)
	_, _, remaining := q.snapshot()
	if first.Deferred != 1 || status.Status != wikidomain.JobDone || remaining != 1 {
		t.Fatalf("post-commit ack failure = %+v, status %+v, remaining %d", first, status, remaining)
	}
	assertTableCount(t, ctx, conn, "claims", 1)
	assertTableCount(t, ctx, conn, "pages", 1)
	second := svc.ProcessInbox(ctx)
	if second.Discarded != 1 {
		t.Fatalf("redelivery = %+v, want discarded", second)
	}
	assertTableCount(t, ctx, conn, "claims", 1)
	assertTableCount(t, ctx, conn, "pages", 1)
	_, _, remaining = q.snapshot()
	if remaining != 0 {
		t.Fatalf("remaining after redelivery = %d", remaining)
	}
}
