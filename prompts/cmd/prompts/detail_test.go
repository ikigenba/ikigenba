package main

import (
	"context"
	"html"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appkitdb "appkit/db"
	appserver "appkit/server"

	"prompts/internal/calls"
	promptsdb "prompts/internal/db"
	"prompts/internal/prompt"
)

type detailFixture struct {
	prompts  []prompt.Prompt
	runs     []prompt.Run
	triggers []prompt.Trigger
	calls    []calls.Row
}

func TestPromptDetailRendersFullFieldsTriggersAndRunsLink(t *testing.T) {
	// R-0C2E-79JM
	row := prompt.Prompt{
		ID: "prompt-detail", OwnerEmail: "owner@example.com", Name: "Detail prompt",
		UserPrompt: "full user prompt\nsecond line", SystemPrompt: "full system prompt",
		Config:    prompt.Config{Provider: "anthropic", Model: "claude-test", MaxTokens: 321},
		CreatedAt: "2026-07-01T00:00:00Z", UpdatedAt: "2026-07-02T00:00:00Z",
	}
	triggers := []prompt.Trigger{
		{PromptID: row.ID, Source: "cron", Filter: "cron:tick/nightly", CreatedAt: "2026-07-02T00:00:00Z"},
		{PromptID: row.ID, Source: "dropbox", Filter: "dropbox:create/reports/**", CreatedAt: "2026-07-02T00:00:01Z"},
	}
	rec := serveUI(t, newDetailUIHandler(t, detailFixture{prompts: []prompt.Prompt{row}, triggers: triggers}), "/ui/prompts/"+row.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{
		row.UserPrompt, row.SystemPrompt, `"provider": "anthropic"`, `"max_tokens": 321`,
		"cron", "cron:tick/nightly", "dropbox", "dropbox:create/reports/**",
		`href="/srv/prompts/ui/runs?prompt_id=prompt-detail"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt detail missing %q:\n%s", want, body)
		}
	}
}

func TestPromptDetailMissingRendersStyledNotFound(t *testing.T) {
	// R-0DAA-L1AB
	rec := serveUI(t, newDetailUIHandler(t, detailFixture{}), "/ui/prompts/deleted-prompt")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	for _, want := range []string{"prompts-test", "This prompt does not exist or was deleted.", `/srv/prompts/static/tokens.css`, `class="home"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("styled prompt 404 missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestRunDetailRendersOrderedCallsAndExcludesOtherGroups(t *testing.T) {
	// R-0EI6-YT10
	run := prompt.Run{
		ID: "run-detail", PromptID: "prompt-for-run", PromptName: "Run prompt", OwnerEmail: "owner@example.com",
		Status: prompt.RunFailed, StartedAt: "2026-07-03T10:00:00Z", EndedAt: "2026-07-03T10:02:00Z",
		UsageJSON: `{"input_tokens":17,"output_tokens":9}`, Error: "run failed visibly",
		TriggerEventID: "event-789", LogPath: "/state/runs/run-detail/output.log",
	}
	request1, response1 := `{"step":1,"nested":{"ok":true}}`, `{"answer":"first"}`
	request2, response2 := "not-json request", `{"answer":"second"}`
	otherRequest, otherResponse := "other-run-secret", "other-run-response"
	rows := []calls.Row{
		callRow("call-second", run.ID, "prompts.second", request2, response2, time.Date(2026, 7, 3, 10, 1, 0, 0, time.UTC)),
		callRow("call-first", run.ID, "prompts.first", request1, response1, time.Date(2026, 7, 3, 10, 0, 1, 0, time.UTC)),
		callRow("call-other", "another-run", "prompts.other", otherRequest, otherResponse, time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)),
	}
	rec := serveUI(t, newDetailUIHandler(t, detailFixture{runs: []prompt.Run{run}, calls: rows}), "/ui/runs/"+run.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{
		run.ID, `href="/srv/prompts/ui/prompts/prompt-for-run"`, run.Error, `"input_tokens": 17`,
		run.TriggerEventID, run.LogPath, "class=session", "name=prompts.first", "attempt=2",
		"provider=anthropic", "model=claude-detail", "input_tokens=11", "output_tokens=7",
		"total_tokens=18", "cost_usd=1.25", `"nested": {`, request2, `"answer": "second"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("run detail missing %q:\n%s", want, body)
		}
	}
	if strings.Index(body, "call-first") > strings.Index(body, "call-second") {
		t.Fatalf("calls are not in started_at order:\n%s", body)
	}
	if strings.Contains(body, otherRequest) || strings.Contains(body, "call-other") {
		t.Fatalf("another run's call leaked into detail:\n%s", body)
	}
}

func TestRunDetailMakesMissingBodiesExplicit(t *testing.T) {
	// R-0FQ3-CKRP
	run := prompt.Run{ID: "run-bodies", PromptID: "p", OwnerEmail: "o@example.com", Status: prompt.RunSucceeded, StartedAt: "2026-07-03T10:00:00Z", LogPath: "/tmp/log"}
	presentRequest, presentResponse := "present request", "present response"
	present := callRow("present-call", run.ID, "prompts.present", presentRequest, presentResponse, time.Now().UTC())
	missing := callRow("missing-call", run.ID, "prompts.missing", "unused", "unused", time.Now().UTC().Add(time.Second))
	missing.RequestBody, missing.ResponseBody = nil, nil
	body := serveUI(t, newDetailUIHandler(t, detailFixture{runs: []prompt.Run{run}, calls: []calls.Row{present, missing}}), "/ui/runs/"+run.ID).Body.String()
	missingArticle := callArticle(t, body, "missing-call")
	if got := strings.Count(missingArticle, "Body pruned by retention or no body recorded."); got != 2 {
		t.Fatalf("missing call notes = %d, want 2:\n%s", got, missingArticle)
	}
	presentArticle := callArticle(t, body, "present-call")
	if strings.Contains(presentArticle, "Body pruned") || !strings.Contains(presentArticle, presentRequest) || !strings.Contains(presentArticle, presentResponse) {
		t.Fatalf("present call incorrectly degraded:\n%s", presentArticle)
	}
}

func TestOversizedBodyTruncatesInlineAndRawEndpointReturnsBodies(t *testing.T) {
	// R-0GXZ-QCIE
	// R-0I5W-4493
	run := prompt.Run{ID: "run-large", PromptID: "p", OwnerEmail: "o@example.com", Status: prompt.RunSucceeded, StartedAt: "2026-07-03T10:00:00Z", LogPath: "/tmp/log"}
	large := strings.Repeat("A", uiBodyInlineLimit) + "TAIL-MUST-ONLY-BE-RAW"
	small := `{"small":true}`
	row := callRow("large-call", run.ID, "prompts.large", large, small, time.Now().UTC())
	h := newDetailUIHandler(t, detailFixture{runs: []prompt.Run{run}, calls: []calls.Row{row}})
	page := serveUI(t, h, "/ui/runs/"+run.ID)
	body := html.UnescapeString(page.Body.String())
	if strings.Contains(body, "TAIL-MUST-ONLY-BE-RAW") {
		t.Fatalf("oversized tail rendered inline")
	}
	for _, want := range []string{strings.Repeat("A", uiBodyInlineLimit), "Body truncated to the first 64 KiB.", `/srv/prompts/ui/calls/large-call/raw?side=request`, `"small": true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("large body page missing %q", want)
		}
	}
	if strings.Count(body, "Body truncated to the first 64 KiB.") != 1 || strings.Contains(body, "raw?side=response") {
		t.Fatalf("small response was marked truncated:\n%s", body[len(body)-2000:])
	}

	for _, tc := range []struct{ side, want string }{{"request", large}, {"response", small}} {
		rec := serveUI(t, h, "/ui/calls/large-call/raw?side="+tc.side)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" || rec.Body.String() != tc.want {
			t.Fatalf("raw %s = status %d type %q len %d, want exact len %d", tc.side, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len(), len(tc.want))
		}
	}
	if rec := serveUI(t, h, "/ui/calls/large-call/raw?side=wrong"); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid side status = %d, want 400", rec.Code)
	}
	if rec := serveUI(t, h, "/ui/calls/unknown/raw?side=request"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown call status = %d, want 404", rec.Code)
	}
	missing := callRow("pruned-call", run.ID, "prompts.pruned", "x", "y", time.Now().UTC())
	missing.RequestBody = nil
	hMissing := newDetailUIHandler(t, detailFixture{runs: []prompt.Run{run}, calls: []calls.Row{missing}})
	rec := serveUI(t, hMissing, "/ui/calls/pruned-call/raw?side=request")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "body pruned") {
		t.Fatalf("pruned raw response = %d %q, want explanatory 404", rec.Code, rec.Body.String())
	}
}

func TestRunDetailMissingRendersStyledNotFound(t *testing.T) {
	// R-0JDS-HVZS
	rec := serveUI(t, newDetailUIHandler(t, detailFixture{}), "/ui/runs/unknown-run")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	for _, want := range []string{"prompts-test", "This run does not exist.", `/srv/prompts/static/tokens.css`, `class="home"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("styled run 404 missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func callRow(id, groupID, name, request, response string, started time.Time) calls.Row {
	return calls.Row{
		ID: id, Class: calls.ClassSession, Origin: "user:owner@example.com", Name: name, GroupID: groupID,
		Attempt: 2, OwnerEmail: "owner@example.com", Provider: "anthropic", Model: "claude-detail",
		InputTokens: 11, OutputTokens: 7, TotalTokens: 18, CostUSD: 1.25,
		RequestBody: &request, ResponseBody: &response, StartedAt: started, EndedAt: started.Add(2 * time.Second),
	}
}

func callArticle(t *testing.T, body, id string) string {
	t.Helper()
	start := strings.Index(body, `data-call-id="`+id+`"`)
	if start < 0 {
		t.Fatalf("body missing call %s", id)
	}
	rest := body[start:]
	end := strings.Index(rest, "</article>")
	if end < 0 {
		t.Fatalf("call %s article is not closed", id)
	}
	return rest[:end]
}

func newDetailUIHandler(t *testing.T, fixture detailFixture) http.Handler {
	t.Helper()
	ctx := context.Background()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := appkitdb.LoadMigrations(promptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(ctx, conn, migrations); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	promptStore := prompt.NewStore(conn)
	callStore := calls.NewStore(conn)
	for _, row := range fixture.prompts {
		if err := promptStore.InsertPrompt(ctx, row); err != nil {
			t.Fatalf("insert prompt %s: %v", row.ID, err)
		}
	}
	for _, row := range fixture.runs {
		if err := promptStore.InsertRun(ctx, row); err != nil {
			t.Fatalf("insert run %s: %v", row.ID, err)
		}
	}
	for _, row := range fixture.triggers {
		if err := promptStore.SetTrigger(ctx, row); err != nil {
			t.Fatalf("insert trigger %s: %v", row.Filter, err)
		}
	}
	for _, row := range fixture.calls {
		if err := callStore.Insert(ctx, row); err != nil {
			t.Fatalf("insert call %s: %v", row.ID, err)
		}
	}
	srv, err := appserver.New(appserver.Options{
		Addr: "127.0.0.1:0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/prompts/", AuthServer: "https://example.test/",
		Version: "v46-test", Service: "prompts-test", WWW: loadPromptsSite(t), DB: conn,
		Register: func(rt *appserver.Router) error {
			registerUIRoutes(rt, promptStore, callStore)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return srv.Handler
}
