package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"appkit/server"

	gh "github/internal/gh"
)

type fakeClient struct {
	calls []string
	err   error

	lastRepo   string
	lastNumber int
	lastPath   string
	lastBody   string
	lastEvent  string
	lastMethod string
	lastTitle  string
	lastHead   string
	lastBase   string
	lastLabel  string
	lastLabels []string
	lastPatch  gh.IssuePatch
	lastFile   gh.FilePut
}

func (f *fakeClient) record(call string) {
	f.calls = append(f.calls, call)
}

func (f *fakeClient) ReposList(context.Context) ([]gh.Repo, error) {
	f.record("repos_list")
	if f.err != nil {
		return nil, f.err
	}
	return []gh.Repo{{Name: "repo"}}, nil
}

func (f *fakeClient) RepoGet(_ context.Context, repo string) (gh.Repo, error) {
	f.record("repo_get")
	f.lastRepo = repo
	return gh.Repo{Name: repo}, f.err
}

func (f *fakeClient) PRList(_ context.Context, repo, state string) ([]gh.PR, error) {
	f.record("pr_list")
	f.lastRepo = repo
	return []gh.PR{{Number: 7, State: state}}, f.err
}

func (f *fakeClient) PRCreate(_ context.Context, repo, title, head, base, body string) (gh.PR, error) {
	f.record("pr_create")
	f.lastRepo, f.lastTitle, f.lastHead, f.lastBase, f.lastBody = repo, title, head, base, body
	return gh.PR{Number: 8, Title: title, State: "open", Body: body}, f.err
}

func (f *fakeClient) PRGet(_ context.Context, repo string, number int) (gh.PRDetail, error) {
	f.record("pr_get")
	f.lastRepo, f.lastNumber = repo, number
	return gh.PRDetail{PR: gh.PR{Number: number}}, f.err
}

func (f *fakeClient) PRComment(_ context.Context, repo string, number int, body string) (gh.Comment, error) {
	f.record("pr_comment")
	f.lastRepo, f.lastNumber, f.lastBody = repo, number, body
	return gh.Comment{ID: 1, Body: body}, f.err
}

func (f *fakeClient) PRReview(_ context.Context, repo string, number int, event, body string) (gh.Review, error) {
	f.record("pr_review")
	f.lastRepo, f.lastNumber, f.lastEvent, f.lastBody = repo, number, event, body
	return gh.Review{ID: 2, State: event, Body: body}, f.err
}

func (f *fakeClient) PRMerge(_ context.Context, repo string, number int, method string) (gh.MergeResult, error) {
	f.record("pr_merge")
	f.lastRepo, f.lastNumber, f.lastMethod = repo, number, method
	return gh.MergeResult{SHA: "abc", Merged: true}, f.err
}

func (f *fakeClient) IssueList(_ context.Context, repo, state string) ([]gh.Issue, error) {
	f.record("issue_list")
	f.lastRepo = repo
	return []gh.Issue{{Number: 3, State: state}}, f.err
}

func (f *fakeClient) IssueGet(_ context.Context, repo string, number int) (gh.Issue, error) {
	f.record("issue_get")
	f.lastRepo, f.lastNumber = repo, number
	return gh.Issue{Number: number}, f.err
}

func (f *fakeClient) IssueCreate(_ context.Context, repo, title, body string) (gh.Issue, error) {
	f.record("issue_create")
	f.lastRepo, f.lastTitle, f.lastBody = repo, title, body
	return gh.Issue{Number: 4, Title: title, Body: body}, f.err
}

func (f *fakeClient) IssueComment(_ context.Context, repo string, number int, body string) (gh.Comment, error) {
	f.record("issue_comment")
	f.lastRepo, f.lastNumber, f.lastBody = repo, number, body
	return gh.Comment{ID: 5, Body: body}, f.err
}

func (f *fakeClient) IssueComments(_ context.Context, repo string, number int) ([]gh.Comment, error) {
	f.record("issue_comments")
	f.lastRepo, f.lastNumber = repo, number
	return []gh.Comment{{ID: 1, Body: "first"}, {ID: 2, Body: "second"}}, f.err
}

func (f *fakeClient) IssueUpdate(_ context.Context, repo string, number int, patch gh.IssuePatch) (gh.Issue, error) {
	f.record("issue_update")
	f.lastRepo, f.lastNumber, f.lastPatch = repo, number, patch
	return gh.Issue{Number: number, State: patch.State}, f.err
}

func (f *fakeClient) LabelAdd(_ context.Context, repo string, number int, labels []string) ([]gh.Label, error) {
	f.record("label_add")
	f.lastRepo, f.lastNumber, f.lastLabels = repo, number, labels
	return []gh.Label{{Name: labels[0], Color: "ff0000"}}, f.err
}

func (f *fakeClient) LabelRemove(_ context.Context, repo string, number int, label string) error {
	f.record("label_remove")
	f.lastRepo, f.lastNumber, f.lastLabel = repo, number, label
	return f.err
}

func (f *fakeClient) FileGet(_ context.Context, repo, path, ref string) (gh.FileContent, error) {
	f.record("file_get")
	f.lastRepo, f.lastPath = repo, path
	return gh.FileContent{Path: path, SHA: ref}, f.err
}

func (f *fakeClient) FilePut(_ context.Context, repo, path string, in gh.FilePut) (gh.FileCommit, error) {
	f.record("file_put")
	f.lastRepo, f.lastPath, f.lastFile = repo, path, in
	return gh.FileCommit{Content: gh.FileContent{Path: path}, Commit: gh.Commit{Message: in.Message}}, f.err
}

type captureHandler struct {
	mu      sync.Mutex
	records []map[string]any
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "github write" {
		return nil
	}
	attrs := map[string]any{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, attrs)
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func newTestHandler(fc *fakeClient, cap *captureHandler, health func(context.Context) (map[string]any, error)) http.Handler {
	logger := slog.New(cap)
	srv, err := server.New(server.Options{
		Addr:    "127.0.0.1:0",
		Logger:  logger,
		Apex:    true,
		Version: "v-test",
		Service: "github",
		Health:  health,
		Register: func(rt *server.Router) error {
			h, err := NewHandler(fc, rt)
			if err != nil {
				return err
			}
			rt.Handle("POST /mcp", rt.RequireIdentity(h))
			return nil
		},
	})
	if err != nil {
		panic(err)
	}
	return srv.Handler
}

func rpc(t *testing.T, h http.Handler, method, params string) (map[string]any, any, int) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("X-Owner-Id", "owner-456")
	req.Header.Set("X-Owner-Email", "owner@example.com")
	req.Header.Set("X-Client-Id", "client-123")
	req.Header.Set("Authorization", "Bearer ignored-by-service")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var env struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s: decode envelope: %v\n%s", method, err, rec.Body.String())
	}
	return env.Result, env.Error, rec.Code
}

func callToolText(t *testing.T, h http.Handler, name, args string) (string, bool, any, int) {
	t.Helper()
	res, rpcErr, code := rpc(t, h, "tools/call", `{"name":"`+name+`","arguments":`+args+`}`)
	if rpcErr != nil {
		return "", false, rpcErr, code
	}
	isErr, _ := res["isError"].(bool)
	content, ok := res["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("%s: missing content: %v", name, res)
	}
	return content[0].(map[string]any)["text"].(string), isErr, nil, code
}

func callTool(t *testing.T, h http.Handler, name, args string) (map[string]any, bool, any, int) {
	t.Helper()
	text, isErr, rpcErr, code := callToolText(t, h, name, args)
	if rpcErr != nil {
		return nil, isErr, rpcErr, code
	}
	var payload map[string]any
	if json.Unmarshal([]byte(text), &payload) != nil {
		payload = map[string]any{"_text": text}
	}
	return payload, isErr, nil, code
}

func TestInitializeAndToolsListR_EEWI_J569(t *testing.T) {
	// R-EEWI-J569
	// R-FI1O-9E44
	h := newTestHandler(&fakeClient{}, &captureHandler{}, nil)

	init, rpcErr, code := rpc(t, h, "initialize", `{}`)
	if code != http.StatusOK || rpcErr != nil {
		t.Fatalf("initialize status=%d error=%v", code, rpcErr)
	}
	if init["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %v, want 2025-06-18", init["protocolVersion"])
	}
	serverInfo, _ := init["serverInfo"].(map[string]any)
	if serverInfo["name"] != "github" || serverInfo["version"] != "v-test" {
		t.Fatalf("serverInfo = %v", serverInfo)
	}

	res, rpcErr, code := rpc(t, h, "tools/list", `{}`)
	if code != http.StatusOK || rpcErr != nil {
		t.Fatalf("tools/list status=%d error=%v", code, rpcErr)
	}
	tools, _ := res["tools"].([]any)
	names := map[string]bool{}
	for _, raw := range tools {
		tl := raw.(map[string]any)
		name := tl["name"].(string)
		if strings.Contains(name, "ikigenba") || strings.Contains(name, "github_") {
			t.Fatalf("tool name is not bare: %q", name)
		}
		schema, ok := tl["inputSchema"].(map[string]any)
		if !ok || len(schema) == 0 || schema["type"] != "object" {
			t.Fatalf("%s has empty/non-object schema: %v", name, tl["inputSchema"])
		}
		names[name] = true
	}
	want := []string{
		"repos_list", "repo_get",
		"pr_list", "pr_get", "pr_create", "pr_comment", "pr_review", "pr_merge",
		"issue_list", "issue_get", "issue_create", "issue_comment", "issue_update", "issue_comments",
		"label_add", "label_remove",
		"file_get", "file_put",
		"health", "reflection",
	}
	for _, name := range want {
		if !names[name] {
			t.Errorf("missing tool %q", name)
		}
	}
	if len(names) != len(want) {
		t.Fatalf("got %d tools, want %d: %v", len(names), len(want), names)
	}
}

func TestMissingOrMalformedArgsDoNotCallClientR_EHCB_AONN(t *testing.T) {
	// R-EHCB-AONN
	fc := &fakeClient{}
	h := newTestHandler(fc, &captureHandler{}, nil)

	if _, isErr, rpcErr, _ := callTool(t, h, "pr_get", `{"repo":"repo"}`); rpcErr != nil || !isErr {
		t.Fatalf("missing number should be tool error, rpcErr=%v isErr=%v", rpcErr, isErr)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("client called for missing number: %v", fc.calls)
	}
	if _, isErr, rpcErr, _ := callTool(t, h, "pr_get", `{"repo":"repo","number":"bad"}`); rpcErr != nil || !isErr {
		t.Fatalf("malformed number should be tool error, rpcErr=%v isErr=%v", rpcErr, isErr)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("client called for malformed number: %v", fc.calls)
	}
}

func TestIdentityFromHeadersAndNoBearerParsingR_EIK7_OGEC(t *testing.T) {
	// R-EIK7-OGEC
	fc := &fakeClient{}
	cap := &captureHandler{}
	h := newTestHandler(fc, cap, nil)
	if _, isErr, rpcErr, _ := callTool(t, h, "issue_comment", `{"repo":"repo","number":9,"body":"hi"}`); rpcErr != nil || isErr {
		t.Fatalf("issue_comment failed rpcErr=%v isErr=%v", rpcErr, isErr)
	}
	if len(cap.records) != 1 {
		t.Fatalf("expected one provenance log, got %d", len(cap.records))
	}
	rec := cap.records[0]
	if rec["owner_id"] != "owner-456" || rec["owner_email"] != "owner@example.com" || rec["client_id"] != "client-123" {
		t.Fatalf("identity not read from headers: %v", rec)
	}
	if rec["verb"] != "issue_comment" || rec["repo"] != "repo" || rec["number"] != int64(9) && rec["number"] != 9 {
		t.Fatalf("dispatch target not logged: %v", rec)
	}
}

func TestWriteProvenanceIncludesOwnerIDR_X3XX_6BNN(t *testing.T) {
	// R-X3XX-6BNN
	cap := &captureHandler{}
	h := newTestHandler(&fakeClient{}, cap, nil)
	if _, isErr, rpcErr, _ := callTool(t, h, "issue_comment", `{"repo":"repo","number":9,"body":"hi"}`); rpcErr != nil || isErr {
		t.Fatalf("issue_comment failed rpcErr=%v isErr=%v", rpcErr, isErr)
	}
	if len(cap.records) != 1 {
		t.Fatalf("expected one provenance log, got %d", len(cap.records))
	}
	if got := cap.records[0]; got["owner_id"] != "owner-456" || got["owner_email"] != "owner@example.com" {
		t.Fatalf("owner provenance = %v, want distinct owner_id beside owner_email", got)
	}
}

func TestWriteProvenanceAndNoOwnerInClientRequestR_EJS4_2851(t *testing.T) {
	// R-EJS4-2851
	writes := []struct {
		name   string
		args   string
		number int
		path   string
	}{
		{"pr_comment", `{"repo":"repo","number":1,"body":"body"}`, 1, ""},
		{"pr_review", `{"repo":"repo","number":2,"event":"APPROVE","body":"body"}`, 2, ""},
		{"pr_merge", `{"repo":"repo","number":3,"method":"squash"}`, 3, ""},
		{"issue_create", `{"repo":"repo","title":"title","body":"body"}`, 0, ""},
		{"issue_comment", `{"repo":"repo","number":4,"body":"body"}`, 4, ""},
		{"issue_update", `{"repo":"repo","number":5,"state":"closed","labels":["bug"],"assignees":["octo"]}`, 5, ""},
		{"file_put", `{"repo":"repo","path":"README.md","message":"msg","content":"hello","sha":"old"}`, 0, "README.md"},
	}
	for _, tc := range writes {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeClient{}
			cap := &captureHandler{}
			h := newTestHandler(fc, cap, nil)
			if _, isErr, rpcErr, _ := callTool(t, h, tc.name, tc.args); rpcErr != nil || isErr {
				t.Fatalf("%s failed rpcErr=%v isErr=%v", tc.name, rpcErr, isErr)
			}
			if len(cap.records) != 1 {
				t.Fatalf("expected exactly one log record, got %d", len(cap.records))
			}
			rec := cap.records[0]
			if rec["owner_email"] != "owner@example.com" || rec["verb"] != tc.name || rec["repo"] != "repo" {
				t.Fatalf("provenance attrs wrong: %v", rec)
			}
			if tc.number != 0 && rec["number"] != int64(tc.number) {
				t.Fatalf("number target = %v, want %d in record %v", rec["number"], tc.number, rec)
			}
			if tc.path != "" && rec["path"] != tc.path {
				t.Fatalf("path target missing: %v", rec)
			}
			if len(fc.calls) != 1 || fc.calls[0] != tc.name {
				t.Fatalf("client calls = %v, want only %q", fc.calls, tc.name)
			}
			requestFields := map[string][]string{
				"repo":                  {fc.lastRepo},
				"path":                  {fc.lastPath},
				"body":                  {fc.lastBody},
				"event":                 {fc.lastEvent},
				"method":                {fc.lastMethod},
				"title":                 {fc.lastTitle},
				"issue_patch_state":     {fc.lastPatch.State},
				"issue_patch_labels":    fc.lastPatch.Labels,
				"issue_patch_assignees": fc.lastPatch.Assignees,
				"file_message":          {fc.lastFile.Message},
				"file_content":          {string(fc.lastFile.Content)},
				"file_sha":              {fc.lastFile.SHA},
			}
			for name, values := range requestFields {
				for _, value := range values {
					for _, identity := range []string{"owner-456", "owner@example.com", "client-123"} {
						if strings.Contains(value, identity) {
							t.Fatalf("identity %q leaked into client request field %s=%q", identity, name, value)
						}
					}
				}
			}
		})
	}

	fc := &fakeClient{}
	cap := &captureHandler{}
	h := newTestHandler(fc, cap, nil)
	if _, isErr, rpcErr, _ := callTool(t, h, "pr_get", `{"repo":"repo","number":1}`); rpcErr != nil || isErr {
		t.Fatalf("pr_get failed rpcErr=%v isErr=%v", rpcErr, isErr)
	}
	if len(cap.records) != 0 {
		t.Fatalf("read verb emitted provenance log: %v", cap.records)
	}
}

func TestPRCreateToolValidationResultLoggingAndErrorsR_GNMM_65OQ(t *testing.T) {
	// R-GNMM-65OQ
	t.Run("validation never calls client", func(t *testing.T) {
		for _, args := range []string{
			`{"title":"title","head":"feature","base":"main"}`,
			`{"repo":"repo","head":"feature","base":"main"}`,
			`{"repo":"repo","title":"title","base":"main"}`,
			`{"repo":"repo","title":"title","head":"feature"}`,
		} {
			fc := &fakeClient{}
			res, rpcErr, _ := rpc(t, newTestHandler(fc, &captureHandler{}, nil), "tools/call", `{"name":"pr_create","arguments":`+args+`}`)
			if rpcErr != nil || res["isError"] != true || res["structuredContent"].(map[string]any)["code"] != "validation" || len(fc.calls) != 0 {
				t.Fatalf("args=%s result=%v rpcErr=%v calls=%v", args, res, rpcErr, fc.calls)
			}
		}
	})

	t.Run("structured success and one write log", func(t *testing.T) {
		fc := &fakeClient{}
		cap := &captureHandler{}
		res, rpcErr, _ := rpc(t, newTestHandler(fc, cap, nil), "tools/call", `{"name":"pr_create","arguments":{"repo":"repo","title":"title","head":"feature","base":"main","body":"details"}}`)
		if rpcErr != nil || res["isError"] == true {
			t.Fatalf("result=%v rpcErr=%v", res, rpcErr)
		}
		got := res["structuredContent"].(map[string]any)
		if got["number"] != float64(8) || got["title"] != "title" || fc.lastHead != "feature" || fc.lastBase != "main" || fc.lastBody != "details" {
			t.Fatalf("structured=%v fake=%+v", got, fc)
		}
		if len(cap.records) != 1 || cap.records[0]["owner_email"] != "owner@example.com" || cap.records[0]["client_id"] != "client-123" || cap.records[0]["verb"] != "pr_create" {
			t.Fatalf("write records = %v, want one caller-attributed pr_create", cap.records)
		}
	})

	t.Run("client error uses codeFor", func(t *testing.T) {
		res, rpcErr, _ := rpc(t, newTestHandler(&fakeClient{err: gh.ErrInvalid}, &captureHandler{}, nil), "tools/call", `{"name":"pr_create","arguments":{"repo":"repo","title":"title","head":"bad","base":"main"}}`)
		if rpcErr != nil || res["structuredContent"].(map[string]any)["code"] != "validation" {
			t.Fatalf("result=%v rpcErr=%v, want validation", res, rpcErr)
		}
	})
}

func TestIssueCommentsToolWrappedResultValidationAndNoLoggingR_F88E_1JKY(t *testing.T) {
	// R-F88E-1JKY
	fc := &fakeClient{}
	cap := &captureHandler{}
	h := newTestHandler(fc, cap, nil)
	for _, args := range []string{`{"number":4}`, `{"repo":"repo"}`} {
		res, rpcErr, _ := rpc(t, h, "tools/call", `{"name":"issue_comments","arguments":`+args+`}`)
		if rpcErr != nil || res["structuredContent"].(map[string]any)["code"] != "validation" {
			t.Fatalf("args=%s result=%v rpcErr=%v", args, res, rpcErr)
		}
	}
	if len(fc.calls) != 0 {
		t.Fatalf("client called during validation: %v", fc.calls)
	}

	res, rpcErr, _ := rpc(t, h, "tools/call", `{"name":"issue_comments","arguments":{"repo":"repo","number":4}}`)
	if rpcErr != nil || res["isError"] == true {
		t.Fatalf("result=%v rpcErr=%v", res, rpcErr)
	}
	items := res["structuredContent"].(map[string]any)["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["body"] != "first" || items[1].(map[string]any)["body"] != "second" {
		t.Fatalf("items = %v, want wrapped comments in order", items)
	}
	if len(cap.records) != 0 {
		t.Fatalf("read emitted write logs: %v", cap.records)
	}
}

func TestLabelAddToolNonEmptyValidationResultAndLoggingR_GOUI_JXFF(t *testing.T) {
	// R-GOUI-JXFF
	fc := &fakeClient{}
	cap := &captureHandler{}
	h := newTestHandler(fc, cap, nil)
	for _, args := range []string{`{"repo":"repo","number":4}`, `{"repo":"repo","number":4,"labels":[]}`, `{"repo":"repo","number":4,"labels":[""]}`} {
		res, rpcErr, _ := rpc(t, h, "tools/call", `{"name":"label_add","arguments":`+args+`}`)
		if rpcErr != nil || res["structuredContent"].(map[string]any)["code"] != "validation" {
			t.Fatalf("args=%s result=%v rpcErr=%v", args, res, rpcErr)
		}
	}
	if len(fc.calls) != 0 {
		t.Fatalf("client called during validation: %v", fc.calls)
	}

	res, rpcErr, _ := rpc(t, h, "tools/call", `{"name":"label_add","arguments":{"repo":"repo","number":4,"labels":["bug"]}}`)
	if rpcErr != nil || res["isError"] == true {
		t.Fatalf("result=%v rpcErr=%v", res, rpcErr)
	}
	labels := res["structuredContent"].(map[string]any)["labels"].([]any)
	if len(labels) != 1 || labels[0].(map[string]any)["name"] != "bug" || !reflect.DeepEqual(fc.lastLabels, []string{"bug"}) {
		t.Fatalf("labels=%v client labels=%v", labels, fc.lastLabels)
	}
	if len(cap.records) != 1 || cap.records[0]["verb"] != "label_add" || cap.records[0]["owner_email"] != "owner@example.com" {
		t.Fatalf("write records = %v", cap.records)
	}
}

func TestLabelRemoveToolValidationSuccessLoggingAndNotFoundR_GQ2E_XP64(t *testing.T) {
	// R-GQ2E-XP64
	fc := &fakeClient{}
	cap := &captureHandler{}
	h := newTestHandler(fc, cap, nil)
	res, rpcErr, _ := rpc(t, h, "tools/call", `{"name":"label_remove","arguments":{"repo":"repo","number":4}}`)
	if rpcErr != nil || res["structuredContent"].(map[string]any)["code"] != "validation" || len(fc.calls) != 0 {
		t.Fatalf("validation result=%v rpcErr=%v calls=%v", res, rpcErr, fc.calls)
	}

	res, rpcErr, _ = rpc(t, h, "tools/call", `{"name":"label_remove","arguments":{"repo":"repo","number":4,"label":"bug"}}`)
	if rpcErr != nil || res["structuredContent"].(map[string]any)["removed"] != true || fc.lastLabel != "bug" {
		t.Fatalf("success result=%v rpcErr=%v label=%q", res, rpcErr, fc.lastLabel)
	}
	if len(cap.records) != 1 || cap.records[0]["verb"] != "label_remove" || cap.records[0]["client_id"] != "client-123" {
		t.Fatalf("write records = %v", cap.records)
	}

	res, rpcErr, _ = rpc(t, newTestHandler(&fakeClient{err: gh.ErrNotFound}, &captureHandler{}, nil), "tools/call", `{"name":"label_remove","arguments":{"repo":"repo","number":4,"label":"missing"}}`)
	if rpcErr != nil || res["structuredContent"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("not-found result=%v rpcErr=%v", res, rpcErr)
	}
}

func TestHealthEnvelopeReflectsAuthCallR_EL00_FZVQ(t *testing.T) {
	// R-EL00-FZVQ
	calls := 0
	h := newTestHandler(&fakeClient{}, &captureHandler{}, func(context.Context) (map[string]any, error) {
		calls++
		return map[string]any{"github_auth": "ok"}, nil
	})
	payload, isErr, rpcErr, _ := callTool(t, h, "health", `{}`)
	if rpcErr != nil || isErr {
		t.Fatalf("health failed rpcErr=%v isErr=%v", rpcErr, isErr)
	}
	if calls != 1 || payload["status"] != "ok" || payload["service"] != "github" {
		t.Fatalf("health did not use faked auth reporter: calls=%d payload=%v", calls, payload)
	}
	details := payload["details"].(map[string]any)
	if details["github_auth"] != "ok" {
		t.Fatalf("health details = %v", details)
	}

	h = newTestHandler(&fakeClient{}, &captureHandler{}, func(context.Context) (map[string]any, error) {
		return nil, gh.ErrAppAuth
	})
	payload, isErr, rpcErr, _ = callTool(t, h, "health", `{}`)
	if rpcErr != nil || isErr {
		t.Fatalf("health auth failure should be an envelope, rpcErr=%v isErr=%v", rpcErr, isErr)
	}
	details = payload["details"].(map[string]any)
	if !strings.Contains(details["error"].(string), gh.ErrAppAuth.Error()) {
		t.Fatalf("auth error not surfaced: %v", payload)
	}
}

func TestRepoGetStructuredSuccessR_FJ9K_N5UT(t *testing.T) {
	// R-FJ9K-N5UT
	h := newTestHandler(&fakeClient{}, &captureHandler{}, nil)
	res, rpcErr, code := rpc(t, h, "tools/call", `{"name":"repo_get","arguments":{"repo":"known"}}`)
	if code != http.StatusOK || rpcErr != nil || res["isError"] == true {
		t.Fatalf("repo_get status=%d rpcErr=%v result=%v", code, rpcErr, res)
	}
	want := map[string]any{"name": "known", "full_name": "", "private": false, "default_branch": ""}
	if got := res["structuredContent"]; !mapsEqualJSON(got, want) {
		t.Fatalf("structuredContent = %v, want %v", got, want)
	}
	content := res["content"].([]any)
	var textValue any
	if err := json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &textValue); err != nil {
		t.Fatal(err)
	}
	if !mapsEqualJSON(textValue, want) {
		t.Fatalf("text JSON = %v, want %v", textValue, want)
	}
}

func TestAllToolsDeclareOutputSchemasR_FKHH_0XLI(t *testing.T) {
	// R-FKHH-0XLI
	h := newTestHandler(&fakeClient{}, &captureHandler{}, nil)
	res, rpcErr, _ := rpc(t, h, "tools/list", `{}`)
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	tools := res["tools"].([]any)
	want := map[string]bool{
		"repos_list": false, "repo_get": false, "pr_list": false, "pr_get": false,
		"pr_create": false, "pr_comment": false, "pr_review": false, "pr_merge": false,
		"issue_list": false, "issue_get": false, "issue_create": false,
		"issue_comment": false, "issue_update": false, "issue_comments": false,
		"label_add": false, "label_remove": false, "file_get": false, "file_put": false,
		"health": false, "reflection": false,
	}
	for _, raw := range tools {
		got := raw.(map[string]any)
		name := got["name"].(string)
		if _, expected := want[name]; !expected {
			t.Fatalf("unexpected tool %q", name)
		}
		if got["outputSchema"] == nil {
			t.Errorf("%s has nil outputSchema", name)
		}
		want[name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestListAndFileShapesR_FLPD_EPC7(t *testing.T) {
	// R-FLPD-EPC7
	h := newTestHandler(&fakeClient{}, &captureHandler{}, nil)
	listed, rpcErr, _ := rpc(t, h, "tools/list", `{}`)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	schemas := map[string]map[string]any{}
	for _, raw := range listed["tools"].([]any) {
		entry := raw.(map[string]any)
		schemas[entry["name"].(string)] = entry["outputSchema"].(map[string]any)
	}
	for _, name := range []string{"repos_list", "pr_list", "issue_list"} {
		schema := schemas[name]
		if schema["type"] != "object" || !mapsEqualJSON(schema["required"], []any{"items"}) {
			t.Errorf("%s outputSchema = %v", name, schema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s outputSchema properties = %v, want object", name, schema["properties"])
		}
		itemsSchema, ok := properties["items"].(map[string]any)
		if !ok || itemsSchema["type"] != "array" {
			t.Errorf("%s items schema = %v, want array", name, properties["items"])
		}
		res, callErr, _ := rpc(t, h, "tools/call", `{"name":"`+name+`","arguments":{"repo":"repo"}}`)
		if callErr != nil {
			t.Fatalf("%s error: %v", name, callErr)
		}
		structured, ok := res["structuredContent"].(map[string]any)
		if !ok {
			t.Fatalf("%s structuredContent = %v, want object", name, res["structuredContent"])
		}
		items, ok := structured["items"].([]any)
		if !ok || len(items) != 1 {
			t.Errorf("%s structuredContent items = %T(%v), want one-element array", name, structured["items"], structured["items"])
		}
	}

	fileSchema := schemas["file_get"]
	props := fileSchema["properties"].(map[string]any)
	if len(props) != 3 || props["path"] == nil || props["sha"] == nil || props["encoding"] == nil || props["content"] != nil {
		t.Fatalf("file_get properties = %v, want exactly path/sha/encoding", props)
	}
	res, callErr, _ := rpc(t, h, "tools/call", `{"name":"file_get","arguments":{"repo":"repo","path":"README.md","ref":"abc"}}`)
	if callErr != nil {
		t.Fatal(callErr)
	}
	structured := res["structuredContent"].(map[string]any)
	if structured["content"] != nil || len(structured) != 3 {
		t.Fatalf("file_get structuredContent = %v, want metadata only", structured)
	}
}

func TestTypedClientErrorCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
		id   string
	}{
		{"not found", gh.ErrNotFound, "not_found", "R-FMX9-SH2W"},
		{"invalid", gh.ErrInvalid, "validation", "R-FO56-68TL"},
		{"app auth", gh.ErrAppAuth, "source_unavailable", "R-FPD2-K0KA"},
		{"transport", errors.New("transport failed"), "source_unavailable", "R-FPD2-K0KA"},
	}
	// R-FMX9-SH2W
	// R-FO56-68TL
	// R-FPD2-K0KA
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(&fakeClient{err: tc.err}, &captureHandler{}, nil)
			res, rpcErr, code := rpc(t, h, "tools/call", `{"name":"repos_list","arguments":{}}`)
			if code != http.StatusOK || rpcErr != nil || res["isError"] != true {
				t.Fatalf("%s status=%d rpcErr=%v result=%v", tc.id, code, rpcErr, res)
			}
			structured := res["structuredContent"].(map[string]any)
			if structured["code"] != tc.want {
				t.Fatalf("%s code = %v, want %s", tc.id, structured["code"], tc.want)
			}
		})
	}
}

func TestValidationCodeAndNoClientCallR_FQKY_XSAZ(t *testing.T) {
	// R-FQKY-XSAZ
	for _, args := range []string{`{"repo":"repo"}`, `{"repo":"repo","number":"bad"}`} {
		fc := &fakeClient{}
		h := newTestHandler(fc, &captureHandler{}, nil)
		res, rpcErr, _ := rpc(t, h, "tools/call", `{"name":"pr_get","arguments":`+args+`}`)
		if rpcErr != nil || res["isError"] != true {
			t.Fatalf("arguments %s result=%v rpcErr=%v", args, res, rpcErr)
		}
		structured := res["structuredContent"].(map[string]any)
		if structured["code"] != "validation" || len(fc.calls) != 0 {
			t.Fatalf("arguments %s code=%v calls=%v", args, structured["code"], fc.calls)
		}
	}
}

func mapsEqualJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	var av, bv any
	_ = json.Unmarshal(ab, &av)
	_ = json.Unmarshal(bb, &bv)
	return reflect.DeepEqual(av, bv)
}

func TestReflectionReportsEmptyGraphR_EM7W_TRMF(t *testing.T) {
	// R-EM7W-TRMF
	h := newTestHandler(&fakeClient{}, &captureHandler{}, nil)
	payload, isErr, rpcErr, _ := callTool(t, h, "reflection", `{}`)
	if rpcErr != nil || isErr {
		t.Fatalf("reflection failed rpcErr=%v isErr=%v", rpcErr, isErr)
	}
	if publishes := payload["publishes"].([]any); len(publishes) != 0 {
		t.Fatalf("publishes not empty: %v", publishes)
	}
	if subscribes := payload["subscribes"].([]any); len(subscribes) != 0 {
		t.Fatalf("subscribes not empty: %v", subscribes)
	}
}

func TestClientErrorsBecomeToolResultsR_ENFT_7JD4(t *testing.T) {
	// R-ENFT-7JD4
	errs := []error{
		gh.ErrNotFound,
		gh.ErrInvalid,
		gh.ErrAppAuth,
		errors.New("github: transport failed"),
	}
	for _, err := range errs {
		t.Run(err.Error(), func(t *testing.T) {
			h := newTestHandler(&fakeClient{err: err}, &captureHandler{}, nil)
			payload, isErr, rpcErr, code := callTool(t, h, "repos_list", `{}`)
			if code != http.StatusOK || rpcErr != nil {
				t.Fatalf("transport not up: status=%d rpcErr=%v", code, rpcErr)
			}
			if !isErr {
				t.Fatalf("expected isError result, got %v", payload)
			}
			if !strings.Contains(payload["_text"].(string), err.Error()) {
				t.Fatalf("tool error not descriptive: %v", payload)
			}
		})
	}
}
