package wiki_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"wiki/internal/llm"
	matchstage "wiki/internal/match"
	wikidomain "wiki/internal/wiki"
)

func matchQueueService(db *sql.DB, q *testQueue) *wikidomain.Service {
	extractSite := llm.CallSite{Stage: "extract", MaxParseRetries: 1}
	matchSite := matchstage.DefaultCallSite()
	matchSite.MaxParseRetries = 2
	compileSite := llm.CallSite{Stage: "compile", MaxParseRetries: 1}
	return wikidomain.NewService(db, nil, nil, time.Now, wikidomain.WithCompletionQueue(q.client(), extractSite, matchSite, compileSite))
}

func TestMatchHandoffPayloadAndScopeComposition(t *testing.T) {
	// R-7GJQ-2PN1
	// R-7HRM-GHDQ
	ctx := context.Background()
	for _, tc := range []struct{ name, instructions string }{{"base", ""}, {"composed", "Prefer primary evidence."}} {
		t.Run(tc.name, func(t *testing.T) {
			conn := migratedWikiDB(t, ctx)
			defer conn.Close()
			if _, err := conn.ExecContext(ctx, `UPDATE scopes SET instructions=? WHERE name='default'`, tc.instructions); err != nil {
				t.Fatal(err)
			}
			q := newTestQueue(t)
			svc := matchQueueService(conn, q)
			jobID, request := ingestAndHandoff(t, ctx, svc, q)
			result := json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada published notes in 1843."],"corrections":["Ada did not publish notes in 1842."]}]}`)
			q.add(queueItem{ID: "extract", Key: request.Key, Status: "done", Context: request.Context, Result: result})
			if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
				t.Fatalf("extract drain = %+v", drain)
			}
			requests, _, _ := q.snapshot()
			got := requests[len(requests)-1]
			if got.Key != jobID+"/match/ada/a1" || len(got.Messages) != 1 || got.Messages[0].Role != "user" {
				t.Fatalf("match request = %+v", got)
			}
			for _, want := range []string{"Subject identity:", "name: Ada", "Corrections:", "[new] Ada did not", "Claims:", "[new] Ada published"} {
				if !strings.Contains(got.Messages[0].Text, want) {
					t.Fatalf("match user missing %q: %s", want, got.Messages[0].Text)
				}
			}
			if strings.Contains(got.Messages[0].Text, jobID) || strings.Contains(got.Messages[0].Text, "~ordinal:") {
				t.Fatalf("match user leaked internal id: %s", got.Messages[0].Text)
			}
			wantSystem := wikidomain.ComposeSystem(matchstage.DefaultPromptInstructions, tc.instructions)
			if got.System != wantSystem {
				t.Fatalf("system mismatch: got %q want %q", got.System, wantSystem)
			}
			var phase string
			if err := conn.QueryRowContext(ctx, `SELECT phase FROM jobs WHERE id=?`, jobID).Scan(&phase); err != nil || phase != "match" {
				t.Fatalf("phase = %q, %v", phase, err)
			}
		})
	}
}

func TestExtractMatchNeedAndStraightCompileHandoff(t *testing.T) {
	// R-7LFB-LSLT
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := matchQueueService(conn, q)
	jobID, request := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "extract", Key: request.Key, Status: "done", Context: request.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","corrections":["Ada was not born in 1900."]},{"type":"entity","kind":"person","name":"Grace","claims":["Grace designed COBOL."]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 {
		t.Fatalf("extract = %+v", drain)
	}
	requests, _, _ := q.snapshot()
	matchCount, compileCount := 0, 0
	for _, got := range requests {
		if strings.Contains(got.Key, "/match/") {
			matchCount++
			if got.Key != jobID+"/match/ada/a1" || !strings.Contains(string(got.Context), `"stage":"match"`) || !strings.Contains(string(got.Context), `"units":1`) {
				t.Fatalf("match fanout = %+v", got)
			}
		}
		if strings.Contains(got.Key, "/compile/") {
			compileCount++
		}
	}
	if matchCount != 1 || compileCount != 1 {
		t.Fatalf("match/compile counts = %d/%d, want 1/1", matchCount, compileCount)
	}

	conn2 := migratedWikiDB(t, ctx)
	defer conn2.Close()
	q2 := newTestQueue(t)
	svc2 := matchQueueService(conn2, q2)
	job2, request2 := ingestAndHandoff(t, ctx, svc2, q2)
	q2.add(queueItem{ID: "extract2", Key: request2.Key, Status: "done", Context: request2.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Katherine","claims":["Katherine calculated trajectories."]}]}`)})
	if drain := svc2.ProcessInbox(ctx); drain.Applied != 1 {
		t.Fatalf("straight extract = %+v", drain)
	}
	requests, _, _ = q2.snapshot()
	last := requests[len(requests)-1]
	var phase string
	if err := conn2.QueryRowContext(ctx, `SELECT phase FROM jobs WHERE id=?`, job2).Scan(&phase); err != nil || phase != "compile" || !strings.Contains(last.Key, job2+"/compile/katherine/") {
		t.Fatalf("straight handoff phase/request = %q/%+v (%v)", phase, last, err)
	}
}

func TestMatchSemanticCorrectionAndExhaustionWritesNothing(t *testing.T) {
	// R-7IZI-U94F
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := matchQueueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada wrote notes."],"corrections":["Ada did not write them in 1842."]}]}`)})
	svc.ProcessInbox(ctx)
	requests, _, _ := q.snapshot()
	matchRequest := requests[len(requests)-1]
	bad := json.RawMessage(`{"judgments":[{"correction":9,"refutes":[0]}]}`)
	for attempt := 1; attempt <= 3; attempt++ {
		key := matchRequest.Key
		contextRaw := matchRequest.Context
		if attempt > 1 {
			requests, _, _ = q.snapshot()
			key, contextRaw = requests[len(requests)-1].Key, requests[len(requests)-1].Context
		}
		q.add(queueItem{ID: "bad-match", Key: key, Status: "done", Context: contextRaw, Result: bad})
		drain := svc.ProcessInbox(ctx)
		if attempt < 3 && drain.Applied != 1 {
			t.Fatalf("attempt %d drain = %+v", attempt, drain)
		}
	}
	requests, _, _ = q.snapshot()
	if !strings.HasSuffix(requests[len(requests)-1].Key, "/a3") || len(requests[len(requests)-1].Messages) != 3 {
		t.Fatalf("corrective chain = %+v", requests[len(requests)-1])
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobFailed || !strings.Contains(status.Error, "out of range") {
		t.Fatalf("status = %+v", status)
	}
	for _, table := range []string{"subjects", "claims", "suppressions", "pages"} {
		assertTableCount(t, ctx, conn, table, 0)
	}
}

func TestMatchApplyIsIdempotentAndCompileUsesEffectiveClaims(t *testing.T) {
	// R-7MN7-ZKCI
	// R-7NV4-DC37
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := matchQueueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada published in 1842.","Ada published in 1843."],"corrections":["Ada did not publish in 1842."]},{"type":"entity","kind":"person","name":"Grace","claims":["Grace designed COBOL."]}]}`)})
	svc.ProcessInbox(ctx)
	requests, _, _ := q.snapshot()
	var matchRequest queueRequest
	for _, request := range requests {
		if strings.Contains(request.Key, "/match/ada/") {
			matchRequest = request
		}
	}
	item := queueItem{ID: "match", Key: matchRequest.Key, Status: "done", Context: matchRequest.Context, Result: json.RawMessage(`{"judgments":[{"correction":0,"refutes":[0]}]}`)}
	q.add(item)
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("match apply = %+v", drain)
	}
	requests, _, _ = q.snapshot()
	var adaCompile queueRequest
	for _, request := range requests {
		if strings.Contains(request.Key, "/compile/ada/") {
			adaCompile = request
		}
	}
	if strings.Contains(adaCompile.Messages[0].Text, "published in 1842") || !strings.Contains(adaCompile.Messages[0].Text, "published in 1843") {
		t.Fatalf("effective compile user = %s", adaCompile.Messages[0].Text)
	}
	var before string
	if err := conn.QueryRowContext(ctx, `SELECT payload FROM job_staging WHERE job_id=? AND stage='match' AND unit='ada'`, jobID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO job_staging(job_id,stage,unit,payload,units) VALUES (?, 'compile','ada','{"title":"Ada","body":"Ada published in 1843."}',2)`, jobID); err != nil {
		t.Fatal(err)
	}
	requestCount := len(requests)
	q.add(item)
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("match replay = %+v", drain)
	}
	requests, _, _ = q.snapshot()
	var after string
	if err := conn.QueryRowContext(ctx, `SELECT payload FROM job_staging WHERE job_id=? AND stage='match' AND unit='ada'`, jobID).Scan(&after); err != nil || before != after || len(requests) != requestCount {
		t.Fatalf("replay payload/requests = %q/%q/%d want unchanged (%v)", before, after, len(requests), err)
	}
}

func TestIntegrateResolvesEdgesAndDeletesFullySuppressedPage(t *testing.T) {
	// R-7QAX-4VKL
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `INSERT INTO subjects(id,scope,name,norm_name,type) VALUES ('subject-ada','default','Ada','ada','entity'); INSERT INTO claims(id,subject_id,job_id,body,kind) VALUES ('100-old','subject-ada','old-job','Ada was born in 1900.','claim')`); err != nil {
		t.Fatal(err)
	}
	if err := wikidomain.NewPageStore(conn).Upsert(ctx, wikidomain.Page{ID: "subject-ada", SubjectID: "subject-ada", Title: "Ada", Body: "Old page"}); err != nil {
		t.Fatal(err)
	}
	q := newTestQueue(t)
	svc := matchQueueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","corrections":["Ada was not born in 1900."]}]}`)})
	svc.ProcessInbox(ctx)
	requests, _, _ := q.snapshot()
	matchRequest := requests[len(requests)-1]
	q.add(queueItem{ID: "match", Key: matchRequest.Key, Status: "done", Context: matchRequest.Context, Result: json.RawMessage(`{"judgments":[{"correction":0,"refutes":[0]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("match/integrate = %+v", drain)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobDone {
		t.Fatalf("job = %+v", status)
	}
	var corrections, edges, subjects, pages int
	_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM claims WHERE job_id=? AND kind='correction'`, jobID).Scan(&corrections)
	_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppressions`).Scan(&edges)
	_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM subjects WHERE id='subject-ada'`).Scan(&subjects)
	_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM pages WHERE subject_id='subject-ada'`).Scan(&pages)
	if corrections != 1 || edges != 1 || subjects != 1 || pages != 0 {
		t.Fatalf("corrections/edges/subjects/pages = %d/%d/%d/%d", corrections, edges, subjects, pages)
	}
}

func TestStandingCorrectionSuppressesLaterReassertion(t *testing.T) {
	// R-7RIT-INBA
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	seedCorrectedAda(t, ctx, conn, "old-claim-job")

	q := newTestQueue(t)
	svc := matchQueueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "reassert-extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada was born in 1900."]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("extract drain = %+v", drain)
	}
	matchRequest := latestQueueRequest(t, q, "/match/ada/")
	q.add(queueItem{ID: "reassert-match", Key: matchRequest.Key, Status: "done", Context: matchRequest.Context, Result: json.RawMessage(`{"judgments":[{"correction":0,"refutes":[1]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("match drain = %+v", drain)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobDone {
		t.Fatalf("job = %+v, want done", status)
	}
	var newClaimID string
	if err := conn.QueryRowContext(ctx, `SELECT id FROM claims WHERE job_id=? AND body='Ada was born in 1900.'`, jobID).Scan(&newClaimID); err != nil || newClaimID == "0001-old-claim" {
		t.Fatalf("new claim id = %q, %v", newClaimID, err)
	}
	var edgeCount int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppressions WHERE correction='0002-standing-correction' AND suppressed IN ('0001-old-claim', ?)`, newClaimID).Scan(&edgeCount); err != nil || edgeCount != 2 {
		t.Fatalf("standing correction edge count = %d, %v; want old and reasserted claims suppressed", edgeCount, err)
	}
	if _, err := wikidomain.NewPageStore(conn).GetBySubject(ctx, "subject-ada"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("page lookup = %v, want no page containing the reasserted falsehood", err)
	}
}

func TestNewCorrectionRevivesClaimsSuppressedByOlderCorrection(t *testing.T) {
	// R-7SQP-WF1Z
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	seedCorrectedAda(t, ctx, conn, "old-claim-job")

	q := newTestQueue(t)
	svc := matchQueueService(conn, q)
	jobID, extractRequest := ingestAndHandoff(t, ctx, svc, q)
	q.add(queueItem{ID: "revive-extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada was born in 1900, not 1901."],"corrections":["The earlier correction was wrong."]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("extract drain = %+v", drain)
	}
	matchRequest := latestQueueRequest(t, q, "/match/ada/")
	q.add(queueItem{ID: "revive-match", Key: matchRequest.Key, Status: "done", Context: matchRequest.Context, Result: json.RawMessage(`{"judgments":[{"correction":1,"contradicts":[0]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("match drain = %+v", drain)
	}
	compileRequest := latestQueueRequest(t, q, "/compile/ada/")
	for _, want := range []string{"Ada was born in 1900.", "Ada was born in 1900, not 1901."} {
		if !strings.Contains(compileRequest.Messages[0].Text, want) {
			t.Fatalf("effective compile prompt missing revived/carried claim %q: %s", want, compileRequest.Messages[0].Text)
		}
	}
	q.add(queueItem{ID: "revive-compile", Key: compileRequest.Key, Status: "done", Context: compileRequest.Context, Result: json.RawMessage(`{"title":"Ada","body":"Ada was born in 1900. Ada was born in 1900, not 1901."}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("compile drain = %+v", drain)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobDone {
		t.Fatalf("job = %+v, want done", status)
	}
	var newerCorrection string
	if err := conn.QueryRowContext(ctx, `SELECT id FROM claims WHERE job_id=? AND kind='correction'`, jobID).Scan(&newerCorrection); err != nil {
		t.Fatalf("new correction: %v", err)
	}
	var overrule int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppressions WHERE correction=? AND suppressed='0002-standing-correction'`, newerCorrection).Scan(&overrule); err != nil || overrule != 1 {
		t.Fatalf("new-to-old correction edge = %d, %v", overrule, err)
	}
	page, err := wikidomain.NewPageStore(conn).GetBySubject(ctx, "subject-ada")
	if err != nil || !strings.Contains(page.Body, "Ada was born in 1900.") || !strings.Contains(page.Body, "not 1901") {
		t.Fatalf("revived page = %+v, %v", page, err)
	}
}

func TestRerunReplacesAndRematchesSuppressedClaim(t *testing.T) {
	// R-7TYM-A6SO
	ctx := context.Background()
	conn := migratedWikiDB(t, ctx)
	defer conn.Close()
	q := newTestQueue(t)
	svc := matchQueueService(conn, q)
	jobID, _ := ingestAndHandoff(t, ctx, svc, q)
	if _, err := svc.Abort(ctx, jobID); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	seedCorrectedAda(t, ctx, conn, jobID)

	if result, err := svc.Rerun(ctx, jobID); err != nil || !result.Requeued {
		t.Fatalf("Rerun = %+v, %v", result, err)
	}
	if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("rerun handoff = %v, %v", processed, err)
	}
	extractRequest := latestQueueRequest(t, q, "/extract/")
	q.add(queueItem{ID: "rerun-rematch-extract", Key: extractRequest.Key, Status: "done", Context: extractRequest.Context, Result: json.RawMessage(`{"subjects":[{"type":"entity","kind":"person","name":"Ada","claims":["Ada was born in 1900."]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("extract drain = %+v", drain)
	}
	matchRequest := latestQueueRequest(t, q, "/match/ada/")
	q.add(queueItem{ID: "rerun-rematch", Key: matchRequest.Key, Status: "done", Context: matchRequest.Context, Result: json.RawMessage(`{"judgments":[{"correction":0,"refutes":[0]}]}`)})
	if drain := svc.ProcessInbox(ctx); drain.Applied != 1 || len(drain.Errs) != 0 {
		t.Fatalf("match drain = %+v", drain)
	}
	status, _ := svc.JobStatus(ctx, jobID)
	if status.Status != wikidomain.JobDone {
		t.Fatalf("job = %+v, want done", status)
	}
	var freshID string
	if err := conn.QueryRowContext(ctx, `SELECT id FROM claims WHERE job_id=? AND kind='claim'`, jobID).Scan(&freshID); err != nil || freshID == "0001-old-claim" {
		t.Fatalf("fresh replacement id = %q, %v", freshID, err)
	}
	var oldRows, oldEdges, freshEdges int
	_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM claims WHERE id='0001-old-claim'`).Scan(&oldRows)
	_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppressions WHERE suppressed='0001-old-claim'`).Scan(&oldEdges)
	_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppressions WHERE correction='0002-standing-correction' AND suppressed=?`, freshID).Scan(&freshEdges)
	if oldRows != 0 || oldEdges != 0 || freshEdges != 1 {
		t.Fatalf("old rows/edges and fresh edge = %d/%d/%d, want 0/0/1", oldRows, oldEdges, freshEdges)
	}
	if _, err := wikidomain.NewPageStore(conn).GetBySubject(ctx, "subject-ada"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("page lookup = %v, want rerun page still free of falsehood", err)
	}
}

func seedCorrectedAda(t *testing.T, ctx context.Context, conn *sql.DB, claimJobID string) {
	t.Helper()
	if err := wikidomain.NewSubjectStore(conn).Save(ctx, wikidomain.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}); err != nil {
		t.Fatalf("Save Ada: %v", err)
	}
	claims := wikidomain.NewClaimStore(conn)
	for _, claim := range []wikidomain.Claim{
		{ID: "0001-old-claim", SubjectID: "subject-ada", JobID: claimJobID, Body: "Ada was born in 1900.", Kind: wikidomain.ClaimKind},
		{ID: "0002-standing-correction", SubjectID: "subject-ada", JobID: "standing-correction-job", Body: "Ada was not born in 1900.", Kind: wikidomain.CorrectionKind},
	} {
		if err := claims.Save(ctx, claim); err != nil {
			t.Fatalf("Save seed claim: %v", err)
		}
	}
	if err := wikidomain.NewSuppressionStore(conn).Insert(ctx, wikidomain.Suppression{Correction: "0002-standing-correction", Suppressed: "0001-old-claim", CreatedAt: 1}); err != nil {
		t.Fatalf("Insert standing edge: %v", err)
	}
	if err := wikidomain.NewPageStore(conn).Upsert(ctx, wikidomain.Page{ID: "subject-ada", SubjectID: "subject-ada", Title: "Ada", Body: "No falsehood."}); err != nil {
		t.Fatalf("Upsert seed page: %v", err)
	}
}

func latestQueueRequest(t *testing.T, q *testQueue, keyContains string) queueRequest {
	t.Helper()
	requests, _, _ := q.snapshot()
	for i := len(requests) - 1; i >= 0; i-- {
		if strings.Contains(requests[i].Key, keyContains) {
			return requests[i]
		}
	}
	t.Fatalf("no queue request containing %q in %+v", keyContains, requests)
	return queueRequest{}
}
