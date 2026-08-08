package repos

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	appkitmcp "appkit/mcp"
)

func TestEventsRegistryAndChassisReflectionAdvertiseOnlyProducers(t *testing.T) {
	// R-JDHK-5ND7
	want := []map[string]any{
		{"kind": "push", "subject": "/<kind>/<name>", "description": "a branch of a repository moved"},
		{"kind": "archived", "subject": "/<kind>/<name>", "description": "a repository was archived"},
	}
	if got := Events.Index(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Events.Index() = %#v, want %#v", got, want)
	}
	handler, err := appkitmcp.New(appkitmcp.Options{Service: "repos", Events: Events})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"reflection","method":"tools/call","params":{"name":"reflection"}}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reflection status = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Result.Content) != 1 {
		t.Fatalf("reflection content = %#v", envelope.Result.Content)
	}
	var reflected struct {
		Publishes  []map[string]any `json:"publishes"`
		Subscribes []map[string]any `json:"subscribes"`
	}
	if err := json.Unmarshal([]byte(envelope.Result.Content[0].Text), &reflected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reflected.Publishes, want) {
		t.Fatalf("reflection publishes = %#v, want %#v", reflected.Publishes, want)
	}
	if len(reflected.Subscribes) != 0 {
		t.Fatalf("reflection subscribes = %#v, want none", reflected.Subscribes)
	}
}

func TestArchiveRepositoryStoresPathAndAppendsOnlyArchivedEvent(t *testing.T) {
	// R-JH59-AYLA
	ctx := context.Background()
	conn, store := newTestStore(t)
	custody, root := newTestCustody(t)
	if err := custody.Init(ctx, "prompts", "daily"); err != nil {
		t.Fatal(err)
	}
	created := time.Unix(1700000000, 0).UTC()
	inTx(t, conn, func(tx *sql.Tx) error {
		return store.InsertRepository(ctx, tx, Repository{Kind: "prompts", Name: "daily", OwnerID: "owner", OwnerEmail: "owner@example.com", DefaultBranch: "main", CreatedAt: created})
	})
	service := serviceWithOutbox(t, conn, store, custody)
	var archivedPath string
	inTx(t, conn, func(tx *sql.Tx) error {
		var err error
		archivedPath, err = service.ArchiveRepository(ctx, tx, "prompts", "daily")
		return err
	})
	repository, err := store.GetRepository(ctx, "prompts", "daily")
	if err != nil {
		t.Fatal(err)
	}
	if repository.ArchivedPath == nil || *repository.ArchivedPath != archivedPath {
		t.Fatalf("stored archived_path = %#v, want %q", repository.ArchivedPath, archivedPath)
	}
	if info, err := osStat(filepath.Join(root, filepath.FromSlash(archivedPath))); err != nil || !info {
		t.Fatalf("archived directory missing: isDir=%v err=%v", info, err)
	}
	rows, err := conn.QueryContext(ctx, `SELECT kind, subject, payload FROM outbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var events []struct{ kind, subject, payload string }
	for rows.Next() {
		var event struct{ kind, subject, payload string }
		if err := rows.Scan(&event.kind, &event.subject, &event.payload); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if len(events) != 1 || events[0].kind != "archived" || events[0].subject != "/prompts/daily" {
		t.Fatalf("events = %#v, want one archived event", events)
	}
	var payload ArchivedPayload
	if err := json.Unmarshal([]byte(events[0].payload), &payload); err != nil {
		t.Fatal(err)
	}
	wantPayload := ArchivedPayload{Kind: "prompts", Name: "daily", ArchivedPath: archivedPath}
	if payload != wantPayload {
		t.Fatalf("archived payload = %#v, want %#v", payload, wantPayload)
	}
}

func osStat(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
