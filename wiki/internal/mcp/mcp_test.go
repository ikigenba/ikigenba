package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"appkit/server"
)

func TestHealthToolReturnsAppkitEnvelope(t *testing.T) {
	h := NewHandler("test-version", "wiki", nil)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ikigenba_wiki_health"}}`)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if len(got.Result.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(got.Result.Content))
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(got.Result.Content[0].Text), &env); err != nil {
		t.Fatalf("health text JSON: %v", err)
	}
	if env["service"] != "wiki" {
		t.Fatalf("service = %v, want wiki", env["service"])
	}
	if env["version"] != "test-version" {
		t.Fatalf("version = %v, want test-version", env["version"])
	}
	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok", env["status"])
	}
}

func TestInitializeAdvertisesWikiMCPServer(t *testing.T) {
	// R-6RVX-P1IG
	h := NewHandler("test-version", "wiki", nil)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":"init","method":"initialize"}`)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools map[string]any `json:"tools"`
			} `json:"capabilities"`
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if got.Result.ProtocolVersion != "2025-03-26" {
		t.Fatalf("protocolVersion = %q, want 2025-03-26", got.Result.ProtocolVersion)
	}
	if got.Result.Capabilities.Tools == nil {
		t.Fatal("capabilities.tools is nil")
	}
	if got.Result.ServerInfo.Name != "Wiki" {
		t.Fatalf("serverInfo.name = %q, want Wiki", got.Result.ServerInfo.Name)
	}
	if got.Result.ServerInfo.Version != "test-version" {
		t.Fatalf("serverInfo.version = %q, want test-version", got.Result.ServerInfo.Version)
	}
}

func TestToolsListAdvertisesConfiguredWikiSurface(t *testing.T) {
	// R-MUQ4-K1JS
	h := gatedHandler(t, NewHandler("test-version", "wiki", nil,
		WithIngestService(&capturingWiki{}),
		WithJobStatusService(&capturingWiki{}),
		WithAskFunc((&capturingAsker{}).Ask),
	))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, rec.Body.Bytes(), &got)
	names := map[string]bool{}
	for _, tool := range got.Result.Tools {
		names[tool.Name] = true
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("%s schema type = %v, want object", tool.Name, tool.InputSchema["type"])
		}
	}
	for _, name := range []string{
		"ikigenba_wiki_health",
		"ikigenba_wiki_ingest_text",
		"ikigenba_wiki_job_status",
		"ikigenba_wiki_ask",
	} {
		if !names[name] {
			t.Fatalf("tools/list missing %s in %#v", name, names)
		}
	}
}

func TestIngestToolUsesAuthenticatedIdentity(t *testing.T) {
	// R-MVY0-XTAH
	wiki := &capturingWiki{ingestID: "job-123"}
	h := gatedHandler(t, NewHandler("test-version", "wiki", nil, WithIngestService(wiki)))
	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"ingest",
		"method":"tools/call",
		"params":{
			"name":"ikigenba_wiki_ingest_text",
			"arguments":{"text":"source text","title":"Source","tags":["one","two"]}
		}
	}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if wiki.ingestOwner != "owner@example.com" {
		t.Fatalf("ingest owner = %q, want authenticated owner", wiki.ingestOwner)
	}
	if wiki.ingestText != "source text" || wiki.ingestTitle != "Source" {
		t.Fatalf("ingest captured text/title = %q/%q", wiki.ingestText, wiki.ingestTitle)
	}
	if len(wiki.ingestTags) != 2 || wiki.ingestTags[0] != "one" || wiki.ingestTags[1] != "two" {
		t.Fatalf("ingest tags = %#v, want [one two]", wiki.ingestTags)
	}
	var body struct {
		JobID string `json:"job_id"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if body.JobID != "job-123" {
		t.Fatalf("job_id = %q, want job-123", body.JobID)
	}
}

func TestJobStatusToolReturnsDomainStatus(t *testing.T) {
	// R-MX5X-BL16
	received := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	finished := received.Add(3 * time.Second)
	wiki := &capturingWiki{status: jobStatus{
		ID:         "job-123",
		Status:     "done",
		ReceivedAt: received,
		FinishedAt: &finished,
		Subjects:   []string{"subject-1"},
	}}
	h := gatedHandler(t, NewHandler("test-version", "wiki", nil, WithJobStatusService(wiki)))
	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"status",
		"method":"tools/call",
		"params":{"name":"ikigenba_wiki_job_status","arguments":{"job_id":"job-123"}}
	}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if wiki.statusJobID != "job-123" {
		t.Fatalf("status job id = %q, want job-123", wiki.statusJobID)
	}
	var body jobStatus
	decodeToolText(t, rec.Body.Bytes(), &body)
	if body.ID != "job-123" || body.Status != "done" {
		t.Fatalf("job status = %#v, want job-123 done", body)
	}
	if len(body.Subjects) != 1 || body.Subjects[0] != "subject-1" {
		t.Fatalf("subjects = %#v, want [subject-1]", body.Subjects)
	}
}

func TestAskToolUsesAuthenticatedIdentity(t *testing.T) {
	// R-MYDT-PCRV
	asker := &capturingAsker{answer: answer{
		Found: true,
		Text:  "Ada wrote the note.",
		Citations: []citation{{
			Subject: "subject-1",
			Title:   "Ada",
		}},
		Sources: []string{"job-123"},
	}}
	h := gatedHandler(t, NewHandler("test-version", "wiki", nil, WithAskFunc(asker.Ask)))
	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"ask",
		"method":"tools/call",
		"params":{"name":"ikigenba_wiki_ask","arguments":{"question":"Who wrote it?"}}
	}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if asker.owner != "owner@example.com" {
		t.Fatalf("ask owner = %q, want authenticated owner", asker.owner)
	}
	if asker.question != "Who wrote it?" {
		t.Fatalf("ask question = %q, want forwarded question", asker.question)
	}
	var body answer
	decodeToolText(t, rec.Body.Bytes(), &body)
	if !body.Found || body.Text != "Ada wrote the note." {
		t.Fatalf("answer = %#v, want found Ada answer", body)
	}
	if len(body.Citations) != 1 || body.Citations[0].Subject != "subject-1" {
		t.Fatalf("citations = %#v, want subject-1 citation", body.Citations)
	}
}

func TestMCPToolsAreBehindRequireIdentity(t *testing.T) {
	// R-MZLQ-34IK
	h := gatedHandler(t, NewHandler("test-version", "wiki", nil, WithIngestService(&capturingWiki{})))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without authenticated identity", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("WWW-Authenticate challenge is empty")
	}
	var body map[string]any
	decodeJSON(t, rec.Body.Bytes(), &body)
	if body["error"] != "unauthorized" {
		t.Fatalf("error = %v, want unauthorized", body["error"])
	}
}

type capturingWiki struct {
	ingestID    string
	ingestOwner string
	ingestText  string
	ingestTitle string
	ingestTags  []string
	statusJobID string
	status      jobStatus
}

func (w *capturingWiki) Ingest(_ context.Context, owner, text, title string, tags []string) (string, error) {
	w.ingestOwner = owner
	w.ingestText = text
	w.ingestTitle = title
	w.ingestTags = append([]string(nil), tags...)
	if w.ingestID == "" {
		return "job-1", nil
	}
	return w.ingestID, nil
}

func (w *capturingWiki) JobStatus(_ context.Context, jobID string) (jobStatus, error) {
	w.statusJobID = jobID
	return w.status, nil
}

type jobStatus struct {
	ID         string
	Status     string
	ReceivedAt time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      string
	Subjects   []string
}

type answer struct {
	Found     bool
	Text      string
	Citations []citation
	Sources   []string
}

type citation struct {
	Subject string
	Title   string
}

type capturingAsker struct {
	owner    string
	question string
	answer   answer
}

func (a *capturingAsker) Ask(_ context.Context, owner, question string) (answer, error) {
	a.owner = owner
	a.question = question
	return a.answer, nil
}

func gatedHandler(t *testing.T, mcp http.Handler) http.Handler {
	t.Helper()
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test-version",
		Service:    "wiki",
		MCP:        mcp,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return srv.Handler
}

func callMCP(t *testing.T, h http.Handler, body, owner string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	if owner != "" {
		req.Header.Set("X-Owner-Email", owner)
		req.Header.Set("X-Client-Id", "client-1")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeToolText(t *testing.T, raw []byte, dst any) {
	t.Helper()
	var got struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	decodeJSON(t, raw, &got)
	if len(got.Result.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(got.Result.Content))
	}
	decodeJSON(t, []byte(got.Result.Content[0].Text), dst)
}

func decodeJSON(t *testing.T, raw []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decode JSON %s: %v", string(raw), err)
	}
}
