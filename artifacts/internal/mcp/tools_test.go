package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appkit"
	appkitdb "appkit/db"
	appkitmcp "appkit/mcp"
	domain "artifacts/internal/artifacts"
	"artifacts/internal/db"
	"artifacts/internal/testutil"
	"eventplane/outbox"
	"registry"
)

// R-4R7O-Q64B
func TestMutationToolsEmitPostChangeAndDeletedFacts(t *testing.T) {
	fx := newFixture(t)
	artifact := fx.storeDescription(t, "facts.txt", "private", []byte("body"), identity("owner"), "before")
	ob, err := outbox.New(fx.conn, outbox.Options{Source: "artifacts", Registry: domain.Events, Now: func() time.Time { return fx.now }})
	if err != nil {
		t.Fatal(err)
	}
	fx.service.Outbox = domain.NewOutboxProducer(ob)

	fx.now = fx.now.Add(time.Minute)
	structured(t, fx.call(t, "update", map[string]any{"id": artifact.ID, "description": "after"}, identity("other")))
	fx.now = fx.now.Add(time.Minute)
	structured(t, fx.call(t, "set_visibility", map[string]any{"id": artifact.ID, "visibility": "public"}, identity("other")))
	structured(t, fx.call(t, "delete", map[string]any{"id": artifact.ID}, identity("other")))

	rows, err := fx.conn.Query(`SELECT kind, subject, payload FROM outbox ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var events []struct {
		Kind, Subject string
		Payload       map[string]any
	}
	for rows.Next() {
		var kind, subject, raw string
		if err := rows.Scan(&kind, &subject, &raw); err != nil {
			t.Fatal(err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatal(err)
		}
		events = append(events, struct {
			Kind, Subject string
			Payload       map[string]any
		}{kind, subject, payload})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Kind != "updated" || events[1].Kind != "updated" || events[2].Kind != "deleted" {
		t.Fatalf("mutation events = %#v", events)
	}
	for i, event := range events {
		if event.Subject != "/"+artifact.ID || event.Payload["id"] != artifact.ID || event.Payload["owner_id"] != artifact.OwnerID || event.Payload["owner_email"] != artifact.OwnerEmail {
			t.Errorf("event %d address/payload = %#v", i, event)
		}
	}
	if events[0].Payload["description"] != "after" || events[0].Payload["visibility"] != "private" || events[1].Payload["description"] != "after" || events[1].Payload["visibility"] != "public" || events[2].Payload["filename"] != artifact.Filename || events[2].Payload["visibility"] != "public" {
		t.Fatalf("post-change/deleted payloads = %#v", events)
	}
}

// R-4CKW-4X7Z
func TestUploadReturnsExpiringCommandReadyLinkAndValidatesFilename(t *testing.T) {
	fx := newFixture(t)
	result := fx.call(t, "upload", map[string]any{"filename": "report.pdf", "visibility": "public"}, identity("one"))
	structured := structured(t, result)
	link := structured["upload_url"].(string)
	if !strings.HasSuffix(link, "/srv/artifacts/u/000000000000000000000000000001") {
		t.Fatalf("upload_url = %q", link)
	}
	if structured["expires_at"] != fx.now.Add(24*time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("expires_at = %v", structured["expires_at"])
	}
	curl := structured["curl"].(string)
	if !strings.Contains(curl, "curl -T") || !strings.Contains(curl, link) {
		t.Fatalf("curl = %q, link = %q", curl, link)
	}
	failure := fx.call(t, "upload", map[string]any{"filename": strings.Repeat("x", 129), "visibility": "public"}, identity("one"))
	assertToolError(t, failure, "validation")
}

// R-4DSS-IOYO
func TestImportReturnsReferenceAndServesExactBytesWithClosedFailures(t *testing.T) {
	fx := newFixture(t)
	content := []byte("import-fixture-contents")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /source", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(content) })
	mux.Handle("/srv/artifacts/f/", http.StripPrefix("/srv/artifacts", fx.service.PublicDownloadHandler()))
	server := startRegistryServer(t, mux)

	result := fx.call(t, "import", map[string]any{
		"source_url": registry.BaseURL("artifacts") + "/source", "filename": "from-dropbox.txt", "visibility": "public",
	}, identity("importer"))
	record := structured(t, result)
	digest := sha256.Sum256(content)
	if record["filename"] != "from-dropbox.txt" || record["size"] != float64(len(content)) || record["content_hash"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("import record = %#v", record)
	}
	if !strings.Contains(record["url"].(string), "/f/") || !strings.HasPrefix(record["content_url"].(string), registry.BaseURL("artifacts")+"/content?id=") {
		t.Fatalf("tier/reference URLs = %q / %q", record["url"], record["content_url"])
	}
	response, err := http.Get(record["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || !bytes.Equal(body, content) {
		t.Fatalf("download = %d %q %v", response.StatusCode, body, readErr)
	}
	assertToolError(t, fx.call(t, "import", map[string]any{"source_url": "https://example.com/file", "filename": "x", "visibility": "public"}, identity("one")), "validation")
	server.Close()
	assertToolError(t, fx.call(t, "import", map[string]any{"source_url": registry.BaseURL("artifacts") + "/source", "filename": "x", "visibility": "public"}, identity("one")), "source_unavailable")
}

// R-4F0O-WGPD
func TestListIsBoxSharedTierCorrectAndNeverContainsBytes(t *testing.T) {
	fx := newFixture(t)
	sentinel := []byte("SENTINEL-BYTES-MUST-NOT-ESCAPE")
	first := fx.store(t, "public.txt", "public", sentinel, identity("first"))
	second := fx.store(t, "private.txt", "private", []byte("other"), identity("second"))
	encoded, err := json.Marshal(fx.call(t, "list", map[string]any{}, identity("third")))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, sentinel) || strings.Contains(string(encoded), hex.EncodeToString(sentinel)) {
		t.Fatalf("list leaked content: %s", encoded)
	}
	root := structuredBytes(t, encoded)
	items := root["artifacts"].([]any)
	if len(items) != 2 {
		t.Fatalf("list count = %d", len(items))
	}
	byID := map[string]map[string]any{}
	for _, item := range items {
		row := item.(map[string]any)
		byID[row["id"].(string)] = row
	}
	if !strings.Contains(byID[first.ID]["url"].(string), "/f/") || !strings.Contains(byID[second.ID]["url"].(string), "/p/") || byID[first.ID]["owner_id"] != "owner-first" || byID[second.ID]["owner_id"] != "owner-second" {
		t.Fatalf("box-shared records = %#v", byID)
	}
	for _, name := range []string{"id", "filename", "description", "visibility", "size", "content_hash", "download_count", "url", "content_url", "created_at", "updated_at", "owner_id", "owner_email"} {
		if _, ok := byID[first.ID][name]; !ok {
			t.Errorf("record missing %q", name)
		}
	}
}

// R-4G8L-A8G2
func TestGetReturnsFullRecordAndNotFound(t *testing.T) {
	fx := newFixture(t)
	artifact := fx.store(t, "get.txt", "public", []byte("get-body"), identity("owner"))
	record := structured(t, fx.call(t, "get", map[string]any{"id": artifact.ID}, identity("reader")))
	if record["id"] != artifact.ID || record["filename"] != artifact.Filename || record["content_hash"] != artifact.ContentHash || record["owner_email"] != artifact.OwnerEmail {
		t.Fatalf("get record = %#v", record)
	}
	assertToolError(t, fx.call(t, "get", map[string]any{"id": "missing"}, identity("reader")), "not_found")
}

// R-4HGH-O06R
func TestUpdateDescriptionHasPatchAndFreshTimestampSemantics(t *testing.T) {
	fx := newFixture(t)
	artifact := fx.storeDescription(t, "update.txt", "public", []byte("body"), identity("owner"), "original")
	created := artifact.CreatedAt.Format(time.RFC3339Nano)
	fx.now = fx.now.Add(time.Minute)
	changed := structured(t, fx.call(t, "update", map[string]any{"id": artifact.ID, "description": "changed"}, identity("other")))
	if changed["description"] != "changed" || changed["created_at"] != created || changed["updated_at"] == created {
		t.Fatalf("changed = %#v", changed)
	}
	fx.now = fx.now.Add(time.Minute)
	omitted := structured(t, fx.call(t, "update", map[string]any{"id": artifact.ID}, identity("other")))
	if omitted["description"] != "changed" {
		t.Fatalf("omitted description = %#v", omitted)
	}
	fx.now = fx.now.Add(time.Minute)
	cleared := structured(t, fx.call(t, "update", map[string]any{"id": artifact.ID, "description": ""}, identity("other")))
	if cleared["description"] != "" || cleared["created_at"] != created || cleared["updated_at"] == omitted["updated_at"] {
		t.Fatalf("cleared = %#v", cleared)
	}
}

// R-4IOE-1RXG
func TestSetVisibilityMovesDownloadTierAndRejectsInvalidValues(t *testing.T) {
	fx := newFixture(t)
	artifact := fx.store(t, "visible.txt", "public", []byte("body"), identity("owner"))
	oldURL := fx.recordURL(artifact)
	updated := structured(t, fx.call(t, "set_visibility", map[string]any{"id": artifact.ID, "visibility": "private"}, identity("other")))
	if !strings.Contains(updated["url"].(string), "/p/") {
		t.Fatalf("private URL = %q", updated["url"])
	}
	request := httptest.NewRequest(http.MethodGet, oldURL, nil)
	response := httptest.NewRecorder()
	fx.service.PublicDownloadHandler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("old public URL status = %d", response.Code)
	}
	assertToolError(t, fx.call(t, "set_visibility", map[string]any{"id": artifact.ID, "visibility": "friends"}, identity("other")), "validation")
	stored, err := fx.storeDB.GetArtifact(context.Background(), artifact.ID)
	if err != nil || stored.Visibility != "private" {
		t.Fatalf("visibility after invalid update = %q, %v", stored.Visibility, err)
	}
}

// R-4JWA-FJO5
func TestDeleteRemovesRowBlobAndDownload(t *testing.T) {
	fx := newFixture(t)
	artifact := fx.store(t, "delete.txt", "public", []byte("body"), identity("owner"))
	link := fx.recordURL(artifact)
	deleted := structured(t, fx.call(t, "delete", map[string]any{"id": artifact.ID}, identity("other")))
	if deleted["deleted"] != true || deleted["id"] != artifact.ID {
		t.Fatalf("delete = %#v", deleted)
	}
	if _, err := fx.service.Blobs.Open(artifact.ID); !os.IsNotExist(err) {
		t.Fatalf("blob open after delete = %v", err)
	}
	assertToolError(t, fx.call(t, "get", map[string]any{"id": artifact.ID}, identity("other")), "not_found")
	request := httptest.NewRequest(http.MethodGet, link, nil)
	response := httptest.NewRecorder()
	fx.service.PublicDownloadHandler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("deleted URL status = %d", response.Code)
	}
	assertToolError(t, fx.call(t, "delete", map[string]any{"id": "missing"}, identity("other")), "not_found")
}

// R-4L46-TBEU
func TestToolDiscoverySchemasAndStructuredResultMirrorThroughChassis(t *testing.T) {
	fx := newFixture(t)
	fx.store(t, "listed.txt", "public", []byte("body"), identity("owner"))
	handler, err := New(fx.service, "test")
	if err != nil {
		t.Fatal(err)
	}
	discovery := rpc(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	tools := discovery["result"].(map[string]any)["tools"].([]any)
	want := map[string]bool{"upload": true, "import": true, "list": true, "get": true, "update": true, "set_visibility": true, "delete": true, "guide": true}
	for _, item := range tools {
		tool := item.(map[string]any)
		name, _ := tool["name"].(string)
		if !want[name] {
			continue
		}
		delete(want, name)
		_, hasSchema := tool["outputSchema"]
		if (name == "guide") == hasSchema {
			t.Errorf("%s outputSchema presence = %v", name, hasSchema)
		}
	}
	if len(want) != 0 {
		t.Fatalf("undiscovered tools = %v", want)
	}
	called := rpc(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list","arguments":{}}}`)
	result := called["result"].(map[string]any)
	structuredContent := result["structuredContent"]
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var mirrored any
	if err := json.Unmarshal([]byte(text), &mirrored); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(structuredContent) != fmt.Sprint(mirrored) {
		t.Fatalf("structured/text mismatch: %#v / %#v", structuredContent, mirrored)
	}
}

// R-4MC3-735J
func TestInstructionsAndGuideProvideTierTwoDiscovery(t *testing.T) {
	fx := newFixture(t)
	handler, err := New(fx.service, "test")
	if err != nil {
		t.Fatal(err)
	}
	initialized := rpc(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	instructions := initialized["result"].(map[string]any)["instructions"].(string)
	for _, phrase := range []string{"upload links", "public/private", "guide"} {
		if !strings.Contains(instructions, phrase) {
			t.Errorf("instructions missing %q: %q", phrase, instructions)
		}
	}
	guide := fx.call(t, "guide", map[string]any{}, identity("owner"))
	if _, exists := guide["structuredContent"]; exists {
		t.Fatalf("guide has structuredContent: %#v", guide)
	}
	encoded, _ := json.Marshal(guide)
	for _, phrase := range []string{"curl -T", "source_url", "127.0.0.1"} {
		if !bytes.Contains(encoded, []byte(phrase)) {
			t.Errorf("guide missing %q: %s", phrase, encoded)
		}
	}
}

type fixture struct {
	conn    *sql.DB
	storeDB *db.Store
	service *domain.Service
	now     time.Time
	next    int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}
	fx := &fixture{now: time.Date(2026, 8, 10, 12, 0, 0, 123, time.UTC)}
	fx.conn = conn
	fx.storeDB = db.NewStore(conn, func() string { fx.next++; return fmt.Sprintf("%030d", fx.next) })
	fx.service = domain.NewService(fx.storeDB, &domain.BlobStore{Root: t.TempDir()}, func() time.Time { return fx.now }, 1024)
	fx.service.UploadOrigin = registry.BaseURL("artifacts")
	return fx
}

func identity(name string) appkit.Identity {
	return appkit.Identity{OwnerID: "owner-" + name, OwnerEmail: name + "@example.test"}
}

func (fx *fixture) call(t *testing.T, name string, args any, id appkit.Identity) map[string]any {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range Tools(fx.service) {
		if tool.Name == name {
			result, err := tool.Handler(context.Background(), raw, id)
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func (fx *fixture) store(t *testing.T, filename, visibility string, body []byte, id appkit.Identity) db.Artifact {
	return fx.storeDescription(t, filename, visibility, body, id, "description")
}

func (fx *fixture) storeDescription(t *testing.T, filename, visibility string, body []byte, id appkit.Identity, description string) db.Artifact {
	t.Helper()
	fx.next++
	artifactID := fmt.Sprintf("artifact-%d", fx.next)
	writer, err := fx.service.Blobs.Create(artifactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	artifact, err := fx.storeDB.CreateArtifact(context.Background(), db.CreateArtifactParams{ID: artifactID, OwnerID: id.OwnerID, OwnerEmail: id.OwnerEmail, Filename: filename, Description: description, Visibility: visibility, Size: int64(len(body)), ContentHash: hex.EncodeToString(digest[:]), CreatedAt: fx.now})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func (fx *fixture) recordURL(artifact db.Artifact) string {
	tier := "p"
	if artifact.Visibility == "public" {
		tier = "f"
	}
	return fx.service.UploadOrigin + "/srv/artifacts/" + tier + "/" + url.PathEscape(artifact.ID) + "/" + url.PathEscape(artifact.Filename)
}

func structured(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return structuredBytes(t, encoded)
}
func structuredBytes(t *testing.T, encoded []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	value, ok := decoded["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("missing structuredContent: %s", encoded)
	}
	return value
}
func assertToolError(t *testing.T, result map[string]any, code string) {
	t.Helper()
	encoded, _ := json.Marshal(result)
	if !bytes.Contains(encoded, []byte(`"isError":true`)) || !bytes.Contains(encoded, []byte(`"code":"`+code+`"`)) {
		t.Fatalf("result = %s, want isError %s", encoded, code)
	}
}

func startRegistryServer(t *testing.T, handler http.Handler) *http.Server {
	t.Helper()
	parsed, err := url.Parse(registry.BaseURL("artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	port := registry.MustPort("artifacts")
	unlock, err := testutil.LockRegistryPort(port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unlock(); err != nil {
			t.Errorf("release artifacts registry port lock: %v", err)
		}
	})
	listener, err := net.Listen("tcp", parsed.Host)
	if err != nil {
		t.Fatalf("listen on artifacts registry port: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func rpc(t *testing.T, handler *appkitmcp.Handler, body string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("RPC status = %d: %s", response.Code, response.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode RPC %q: %v", response.Body.String(), err)
	}
	if rpcErr := decoded["error"]; rpcErr != nil {
		t.Fatalf("RPC error = %#v", rpcErr)
	}
	return decoded
}
