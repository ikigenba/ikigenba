package wiki_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"wiki/internal/llm"
	wikidomain "wiki/internal/wiki"
)

type queueRequest struct {
	Key      string          `json:"key"`
	Context  json.RawMessage `json:"context"`
	Attempt  int             `json:"attempt"`
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
	mu       sync.Mutex
	requests []queueRequest
	inbox    []queueItem
	acked    []string
	nextID   int
	server   *httptest.Server
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
	if err != nil || status.Status != wikidomain.JobWaiting {
		t.Fatalf("status after handoff = %+v, %v", status, err)
	}
	if first.Key != jobID+"/extract/a1" || !strings.Contains(first.Messages[0].Text, "Ada and Grace wrote compilers.") {
		t.Fatalf("extract request = %+v", first)
	}
	var envelope map[string]any
	if err := json.Unmarshal(first.Context, &envelope); err != nil || envelope["job_id"] != jobID || envelope["stage"] != "extract" {
		t.Fatalf("extract envelope = %s, %v", first.Context, err)
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
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	result := json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote a compiler."]},{"type":"entity","kind":"person","name":"Grace","claims":["Grace wrote a compiler."]}]}`)
	item := queueItem{ID: "extract-done", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: result}
	q.add(item)
	if n, err := svc.ProcessInbox(ctx); err != nil || n != 1 {
		t.Fatalf("ProcessInbox = %d, %v", n, err)
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
	q.add(item)
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("replay extract: %v", err)
	}
	requests, _, _ = q.snapshot()
	replayedKeys := map[string]bool{}
	for _, request := range requests {
		if strings.Contains(request.Key, "/compile/") {
			replayedKeys[request.Key] = true
		}
	}
	if len(replayedKeys) != 2 {
		t.Fatalf("replayed ensured set = %#v", replayedKeys)
	}
}

func TestLastCompileAtomicallyIntegratesAndRedeliveryIsStray(t *testing.T) {
	// R-K9JC-ANDH
	// R-M8RN-87WV
	// R-MB7F-ZRE9
	// R-MCFC-DJ4Y
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := queueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote a compiler."]},{"type":"entity","kind":"person","name":"Grace","claims":["Grace wrote a compiler."]}]}`)})
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("apply extract: %v", err)
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
		if _, err := svc.ProcessInbox(ctx); err != nil {
			t.Fatalf("apply compile %d: %v", i, err)
		}
		status, _ := svc.JobStatus(ctx, jobID)
		want := wikidomain.JobWaiting
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
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("redelivery: %v", err)
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
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("first bad result: %v", err)
	}
	requests, _, _ := q.snapshot()
	corrective := requests[len(requests)-1]
	if corrective.Key != jobID+"/extract/a2" || len(corrective.Messages) != 3 {
		t.Fatalf("corrective request = %+v", corrective)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobWaiting {
		t.Fatalf("status after correction = %q", status.Status)
	}
	q.add(queueItem{ID: "bad-a2", Key: corrective.Key, Status: "done", Context: corrective.Context, Result: bad})
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("exhausted result: %v", err)
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
	if n, err := svc.ProcessInbox(ctx); err != nil || n != 4 {
		t.Fatalf("mixed inbox drain = %d, %v", n, err)
	}
	_, acked, remaining := q.snapshot()
	if len(acked) != 4 || remaining != 0 {
		t.Fatalf("acked/remaining = %#v/%d", acked, remaining)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobFailed || status.Error != "provider exploded" {
		t.Fatalf("failed status = %+v", status)
	}
	assertTableCount(t, ctx, conn, "job_staging", 0)
	if _, err := conn.ExecContext(ctx, `INSERT INTO jobs(id, scope, status, owner_id, owner_email) VALUES ('working-job','default','working','',''), ('waiting-job','default','waiting','','')`); err != nil {
		t.Fatalf("seed sweep jobs: %v", err)
	}
	if n, err := svc.RequeueWorking(ctx); err != nil || n != 1 {
		t.Fatalf("RequeueWorking = %d, %v", n, err)
	}
	for id, want := range map[string]string{"working-job": wikidomain.JobPending, "waiting-job": wikidomain.JobWaiting} {
		var got string
		_ = conn.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, id).Scan(&got)
		if got != want {
			t.Fatalf("%s status = %q, want %q", id, got, want)
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
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("late result: %v", err)
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
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("rerun extract apply: %v", err)
	}
	requests, _, _ = q.snapshot()
	compileRequest := requests[len(requests)-1]
	q.add(queueItem{ID: "rerun-compile", Key: compileRequest.Key, Status: "done", Context: compileRequest.Context, Result: json.RawMessage(`{"title":"Ada","body":"Ada wrote a compiler."}`)})
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("rerun compile apply: %v", err)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobDone || len(status.Subjects) != 1 {
		t.Fatalf("rerun status = %+v", status)
	}
	q.add(queueItem{ID: "stale", Key: oldRequest.Key, Status: "done", Context: oldRequest.Context, Result: json.RawMessage(`{"subjects":[]}`)})
	if _, err := svc.ProcessInbox(ctx); err != nil {
		t.Fatalf("stale delivery: %v", err)
	}
	assertTableCount(t, ctx, conn, "claims", 1)
	assertTableCount(t, ctx, conn, "pages", 1)
}
