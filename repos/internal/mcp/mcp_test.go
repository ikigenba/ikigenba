package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	appdb "appkit/db"
	appkitmcp "appkit/mcp"
	"appkit/server"
	"eventplane/outbox"
	reposdb "repos/internal/db"
	"repos/internal/repos"
)

type testService struct {
	repositories []repos.Repository
	lastCreated  repos.Repository
}

func (s *testService) CreateRepository(_ context.Context, repository repos.Repository) (repos.Repository, error) {
	if repository.OwnerID == "" || !contains(repos.Kinds, repository.Kind) || !repos.ValidName(repository.Name) {
		return repos.Repository{}, repos.ErrValidation
	}
	for _, existing := range s.repositories {
		if existing.Kind == repository.Kind && existing.Name == repository.Name && existing.ArchivedAt == nil {
			return repos.Repository{}, repos.ErrConflict
		}
	}
	repository.DefaultBranch = "main"
	repository.CreatedAt = time.Unix(1, 0).UTC()
	s.repositories = append(s.repositories, repository)
	s.lastCreated = repository
	return repository, nil
}

func (s *testService) ListRepositories(_ context.Context, owner string, kind *string) ([]repos.Repository, error) {
	var result []repos.Repository
	for _, repository := range s.repositories {
		if repository.OwnerID == owner && repository.ArchivedAt == nil && (kind == nil || repository.Kind == *kind) {
			result = append(result, repository)
		}
	}
	return result, nil
}

func (s *testService) GetRepository(_ context.Context, owner, kind, name string) (repos.RepositoryDetail, error) {
	for _, repository := range s.repositories {
		if repository.OwnerID == owner && repository.Kind == kind && repository.Name == name && repository.ArchivedAt == nil {
			return repos.RepositoryDetail{Repository: repository, Head: "abc", Branches: []string{"main"}}, nil
		}
	}
	return repos.RepositoryDetail{}, repos.ErrNotFound
}

func (s *testService) RenameRepository(ctx context.Context, owner, kind, name, to string) (repos.Repository, error) {
	detail, err := s.GetRepository(ctx, owner, kind, name)
	if err != nil {
		return repos.Repository{}, err
	}
	detail.Repository.Name = to
	return detail.Repository, nil
}

func (s *testService) DeleteRepository(ctx context.Context, owner, kind, name string) (repos.Repository, error) {
	detail, err := s.GetRepository(ctx, owner, kind, name)
	if err != nil {
		return repos.Repository{}, err
	}
	now := time.Unix(2, 0).UTC()
	detail.Repository.ArchivedAt = &now
	return detail.Repository, nil
}

func (*testService) Merge(_ context.Context, _, name, _, _ string) (repos.MergeResult, error) {
	if name == "blocked" {
		return repos.MergeResult{}, repos.ErrConflict
	}
	return repos.MergeResult{Merged: true, Rev: "def", Strategy: "fast-forward"}, nil
}

func (*testService) SetStatus(_ context.Context, status repos.Status) (repos.Status, error) {
	status.UpdatedAt = time.Unix(3, 0).UTC()
	return status, nil
}

func (*testService) ListStatuses(_ context.Context, kind, name, sha string) ([]repos.Status, error) {
	return []repos.Status{{Kind: kind, Name: name, SHA: sha, CheckName: "tests", State: "success", Actor: "agent", UpdatedAt: time.Unix(3, 0).UTC()}}, nil
}

func TestToolDiscoveryIsExactAndCanonical(t *testing.T) {
	// R-KSPC-80ID
	// R-JGIF-9RRG
	handler, err := appkitmcp.New(appkitmcp.Options{Service: "repos", Version: "test", Instructions: Instructions, Tools: Tools(&testService{})})
	if err != nil {
		t.Fatal(err)
	}
	response := callRPC(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result := response["result"].(map[string]any)
	descriptors := result["tools"].([]any)
	var names []string
	for _, raw := range descriptors {
		descriptor := raw.(map[string]any)
		name := descriptor["name"].(string)
		names = append(names, name)
		if name == "guide" {
			if _, ok := descriptor["outputSchema"]; ok {
				t.Errorf("guide unexpectedly declares outputSchema")
			}
		} else if schema, ok := descriptor["outputSchema"].(map[string]any); !ok || len(schema) == 0 {
			t.Errorf("%s outputSchema = %#v, want non-empty object", name, descriptor["outputSchema"])
		}
		walkNoKey(t, descriptor["inputSchema"], "additionalProperties")
	}
	sort.Strings(names)
	want := []string{"create", "delete", "get", "guide", "health", "list", "merge", "reflection", "rename", "status_list", "status_set"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tools/list names = %v, want exactly %v", names, want)
	}
	initialize := callRPC(t, handler, `{"jsonrpc":"2.0","id":2,"method":"initialize"}`)
	instructions := initialize["result"].(map[string]any)["instructions"].(string)
	for _, word := range []string{"git", "repository", "version history", "branch", "commit", "clone", "merge", "guide"} {
		if !strings.Contains(strings.ToLower(instructions), word) {
			t.Errorf("initialize instructions omit %q: %q", word, instructions)
		}
	}
}

func TestDomainSuccessResultsMatchAdvertisedSchemasAndText(t *testing.T) {
	// R-L00Q-IMYJ
	svc := &testService{repositories: []repos.Repository{{Kind: "code", Name: "demo", OwnerID: "owner", OwnerEmail: "o@example.test", DefaultBranch: "main"}, {Kind: "code", Name: "blocked", OwnerID: "owner", DefaultBranch: "main"}}}
	id := server.Identity{OwnerID: "owner", OwnerEmail: "o@example.test", ClientID: "agent"}
	calls := map[string]string{
		"create": `{"name":"new"}`, "list": `{}`, "get": `{"name":"demo"}`, "rename": `{"name":"demo","to":"renamed"}`,
		"delete": `{"name":"demo"}`, "merge": `{"name":"demo","branch":"work"}`,
		"status_set":  `{"name":"demo","sha":"abc","check":"tests","state":"success"}`,
		"status_list": `{"name":"demo","sha":"abc"}`,
	}
	for _, tool := range Tools(svc) {
		arguments, domain := calls[tool.Name]
		if !domain {
			continue
		}
		result, err := tool.Handler(context.Background(), json.RawMessage(arguments), id)
		if err != nil {
			t.Fatalf("%s handler: %v", tool.Name, err)
		}
		if result["isError"] == true {
			t.Fatalf("%s returned error: %#v", tool.Name, result)
		}
		structured := result["structuredContent"]
		assertSchemaShape(t, tool.OutputSchema, structured, tool.Name)
		text := result["content"].([]map[string]any)[0]["text"].(string)
		encoded, _ := json.Marshal(structured)
		if text != string(encoded) {
			t.Errorf("%s text = %q, structured JSON = %q", tool.Name, text, encoded)
		}
	}
}

func TestErrorsUseOnlyClosedCodes(t *testing.T) {
	// R-L2GJ-A6FX
	svc := &testService{repositories: []repos.Repository{{Kind: "code", Name: "demo", OwnerID: "owner", DefaultBranch: "main"}, {Kind: "code", Name: "blocked", OwnerID: "owner", DefaultBranch: "main"}}}
	id := server.Identity{OwnerID: "owner", ClientID: "agent"}
	cases := []struct{ tool, args, want string }{
		{"create", `{"kind":"bogus","name":"x"}`, "validation"},
		{"get", `{"name":"missing"}`, "not_found"},
		{"create", `{"name":"demo"}`, "conflict"},
		{"merge", `{"name":"blocked","branch":"work"}`, "conflict"},
	}
	allowed := map[string]bool{"validation": true, "not_found": true, "conflict": true, "too_large": true, "source_unavailable": true}
	byName := map[string]appkitmcp.Tool{}
	for _, tool := range Tools(svc) {
		byName[tool.Name] = tool
	}
	for _, test := range cases {
		result, err := byName[test.tool].Handler(context.Background(), json.RawMessage(test.args), id)
		if err != nil {
			t.Fatal(err)
		}
		if result["isError"] != true {
			t.Fatalf("%s %s was not an MCP error: %#v", test.tool, test.args, result)
		}
		code := string(result["structuredContent"].(map[string]any)["code"].(appkitmcp.ErrorCode))
		if code != test.want || !allowed[code] {
			t.Errorf("%s code = %q, want %q in closed set", test.tool, code, test.want)
		}
	}
}

func TestIdentityComesOnlyFromRequestIdentity(t *testing.T) {
	// R-JFAI-W00R
	svc := &testService{}
	create := Tools(svc)[0]
	result, err := create.Handler(context.Background(), json.RawMessage(`{"name":"demo","owner_id":"bogus","owner_email":"bogus@example.test"}`), server.Identity{OwnerID: "asserted", OwnerEmail: "asserted@example.test", ClientID: "sites:home"})
	if err != nil || result["isError"] == true {
		t.Fatalf("asserted service caller create failed: result=%#v err=%v", result, err)
	}
	if svc.lastCreated.OwnerID != "asserted" || svc.lastCreated.OwnerEmail != "asserted@example.test" {
		t.Fatalf("stored identity = %#v, want asserted headers and ignored argument owner", svc.lastCreated)
	}
	result, _ = create.Handler(context.Background(), json.RawMessage(`{"name":"empty-owner"}`), server.Identity{})
	if result["isError"] != true {
		t.Fatalf("empty owner was accepted: %#v", result)
	}
	get := Tools(svc)[2]
	result, _ = get.Handler(context.Background(), json.RawMessage(`{"name":"demo"}`), server.Identity{OwnerID: "different"})
	if result["structuredContent"].(map[string]any)["code"] != appkitmcp.ErrNotFound {
		t.Fatalf("different owner result = %#v, want not_found", result)
	}
}

func TestLoopbackServiceCallerIdentityReachesRealStore(t *testing.T) {
	// R-JFAI-W00R
	ctx := context.Background()
	migrations, err := reposdb.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := appdb.Migrate(ctx, conn, migrations); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	custody, err := repos.NewCustody(root, repos.NewCommandGit("git", root), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := repos.NewStore(conn)
	domain := repos.NewService(store)
	domain.SetCustody(custody)
	producer, err := outbox.New(conn, outbox.Options{Source: "repos", Registry: repos.Events})
	if err != nil {
		t.Fatal(err)
	}
	domain.SetProducer(producer)
	mcpHandler, err := appkitmcp.New(appkitmcp.Options{Service: "repos", Version: "test", Instructions: Instructions, Tools: Tools(domain)})
	if err != nil {
		t.Fatal(err)
	}
	assembled, err := server.New(server.Options{
		Addr: "127.0.0.1:0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "http://127.0.0.1/repos", AuthServer: "http://127.0.0.1/dashboard",
		Version: "test", Service: "repos", MCP: mcpHandler,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create","arguments":{"name":"owned","owner_id":"bogus","owner_email":"bogus@example.test"}}}`
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	request.Header.Set("X-Owner-Id", "asserted-owner")
	request.Header.Set("X-Owner-Email", "asserted@example.test")
	request.Header.Set("X-Client-Id", "sites:home")
	response := httptest.NewRecorder()
	assembled.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"isError":true`) {
		t.Fatalf("loopback service create status=%d body=%s", response.Code, response.Body.String())
	}
	stored, err := store.GetLiveRepository(ctx, "asserted-owner", "code", "owned")
	if err != nil || stored.OwnerEmail != "asserted@example.test" {
		t.Fatalf("stored repository = %#v, err=%v, want asserted identity", stored, err)
	}
	missingIdentity := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	missingResponse := httptest.NewRecorder()
	assembled.Handler.ServeHTTP(missingResponse, missingIdentity)
	if missingResponse.Code != http.StatusUnauthorized {
		t.Fatalf("empty owner status = %d, want 401", missingResponse.Code)
	}
	getBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get","arguments":{"name":"owned"}}}`
	other := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(getBody))
	other.Header.Set("X-Owner-Id", "other-owner")
	otherResponse := httptest.NewRecorder()
	assembled.Handler.ServeHTTP(otherResponse, other)
	if otherResponse.Code != http.StatusOK || !strings.Contains(otherResponse.Body.String(), `"code":"not_found"`) {
		t.Fatalf("different owner response status=%d body=%s", otherResponse.Code, otherResponse.Body.String())
	}
}

func callRPC(t *testing.T, handler http.Handler, body string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var value map[string]any
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &value) != nil {
		t.Fatalf("rpc status=%d body=%s", response.Code, response.Body.String())
	}
	return value
}

func walkNoKey(t *testing.T, value any, forbidden string) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		if _, ok := value[forbidden]; ok {
			t.Errorf("schema contains forbidden key %q: %#v", forbidden, value)
		}
		for _, child := range value {
			walkNoKey(t, child, forbidden)
		}
	case []any:
		for _, child := range value {
			walkNoKey(t, child, forbidden)
		}
	}
}

func assertSchemaShape(t *testing.T, schema map[string]any, value any, path string) {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Errorf("%s result is %T, want object", path, value)
		return
	}
	properties := schema["properties"].(map[string]any)
	for _, raw := range schema["required"].([]string) {
		if _, ok := object[raw]; !ok {
			t.Errorf("%s result misses required %s", path, raw)
		}
	}
	for key := range object {
		if _, ok := properties[key]; !ok {
			t.Errorf("%s result has undeclared key %s", path, key)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

var _ Service = (*testService)(nil)
