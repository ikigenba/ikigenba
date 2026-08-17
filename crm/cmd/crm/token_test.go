package main

import (
	"appkit/server"
	"bytes"
	"context"
	"crm/internal/crm"
	"crm/internal/db"
	crmmcp "crm/internal/mcp"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	appkitdb "appkit/db"
)

func TestAssembledMCPMintThenTokenSearch(t *testing.T) {
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	defer conn.Close()
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var router *server.Router
	_, err = server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/crm",
		AuthServer: "https://auth.example.test",
		Version:    "test",
		Service:    "crm",
		Events:     crm.Events,
		DB:         conn,
		Register: func(rt *server.Router) error {
			router = rt
			return nil
		},
	})
	if err != nil {
		t.Fatalf("assemble server: %v", err)
	}
	svc := crm.NewService(conn)
	handler, err := crmmcp.NewHandler(svc, router)
	if err != nil {
		t.Fatalf("assemble MCP handler: %v", err)
	}
	handler = router.RequireIdentity(handler)

	contact := assembledCall(t, handler, "save", map[string]any{
		"type": "contact", "fields": map[string]any{"display_name": "Ada"},
	})
	contactID, _ := contact["id"].(string)
	minted := assembledCall(t, handler, "mint", map[string]any{
		"contact_id": contactID, "campaign": "integration",
	})
	token, _ := minted["token"].(string)
	var storedContact string
	if err := conn.QueryRow(`SELECT contact_id FROM contact_tokens WHERE token = ? AND deleted_at IS NULL`, token).Scan(&storedContact); err != nil {
		t.Fatalf("load minted row: %v", err)
	}
	search := assembledCall(t, handler, "search", map[string]any{
		"type": "contact", "filters": map[string]any{"token": token},
	})
	items, _ := search["items"].([]any)
	// R-9XZS-WD9O
	if token == "" || minted["contact_id"] != contactID || storedContact != contactID || len(items) != 1 || items[0].(map[string]any)["id"] != contactID {
		t.Fatalf("assembled mint/search mismatch: contact=%q minted=%+v stored=%q search=%+v", contactID, minted, storedContact, search)
	}
}

func assembledCall(t *testing.T, handler http.Handler, name string, arguments map[string]any) map[string]any {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments},
	})
	if err != nil {
		t.Fatalf("marshal %s call: %v", name, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(request))
	req.Header.Set("X-Owner-Id", "owner@example.com")
	req.Header.Set("X-Owner-Email", "owner@example.com")
	req.Header.Set("X-Client-Id", "client-test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var response struct {
		Result struct {
			IsError           bool           `json:"isError"`
			StructuredContent map[string]any `json:"structuredContent"`
			Content           []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %s response: %v body=%s", name, err, rec.Body.String())
	}
	if response.Error != nil || response.Result.IsError || response.Result.StructuredContent == nil {
		t.Fatalf("%s failed: %+v body=%s", name, response, rec.Body.String())
	}
	return response.Result.StructuredContent
}
