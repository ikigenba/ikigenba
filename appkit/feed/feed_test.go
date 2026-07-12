package feed_test

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"appkit/feed"

	"eventplane/outbox"
	"eventplane/routing"
	_ "modernc.org/sqlite"
)

func TestStartProducerAppendValidatesRegistryByKind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn, dbPath := openOutboxDB(t)
	t.Cleanup(func() { _ = conn.Close() })

	prod, err := feed.Start(ctx, conn, feed.Options{
		Source:         "dropbox",
		DBPath:         dbPath,
		GenerationPath: filepath.Join(t.TempDir(), "generation"),
		Registry:       outbox.Registry{{Kind: "create", Subject: "/<path>", Description: "created"}},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	tx, err := conn.Begin()
	if err != nil {
		t.Fatalf("begin reject tx: %v", err)
	}
	err = prod.Outbox.Append(tx, outbox.Event{Kind: "delete", Subject: "/bills/a.pdf", Payload: json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "create") {
		_ = tx.Rollback()
		t.Fatalf("delete append error = %v, want rejection naming declared kind create", err)
	}
	_ = tx.Rollback()

	tx, err = conn.Begin()
	if err != nil {
		t.Fatalf("begin accept tx: %v", err)
	}
	if err := prod.Outbox.Append(tx, outbox.Event{Kind: "create", Subject: "/bills/a.pdf", Payload: json.RawMessage(`{"path":"/bills/a.pdf"}`)}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("create append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit create: %v", err)
	}

	// R-7JFR-R31S
	var kind, subject string
	if err := conn.QueryRow(`SELECT kind, subject FROM outbox`).Scan(&kind, &subject); err != nil {
		t.Fatalf("read appended row: %v", err)
	}
	if kind != "create" || subject != "/bills/a.pdf" {
		t.Fatalf("stored event = (%q, %q), want (create, /bills/a.pdf)", kind, subject)
	}

	openConn, openDBPath := openOutboxDB(t)
	t.Cleanup(func() { _ = openConn.Close() })
	t.Cleanup(cancel)
	openProd, err := feed.Start(ctx, openConn, feed.Options{
		Source:         "dropbox",
		DBPath:         openDBPath,
		GenerationPath: filepath.Join(t.TempDir(), "open-generation"),
	})
	if err != nil {
		t.Fatalf("Start without registry: %v", err)
	}
	openTx, err := openConn.Begin()
	if err != nil {
		t.Fatalf("begin open tx: %v", err)
	}
	if err := openProd.Outbox.Append(openTx, outbox.Event{Kind: "delete", Subject: "/bills/a.pdf", Payload: json.RawMessage(`{}`)}); err != nil {
		_ = openTx.Rollback()
		t.Fatalf("append with empty registry: %v", err)
	}
	if err := openTx.Commit(); err != nil {
		t.Fatalf("commit open append: %v", err)
	}
}

func TestStartProducerHandlerFramesCanonicalKindSubjectEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn, dbPath := openOutboxDB(t)
	t.Cleanup(func() { _ = conn.Close() })
	t.Cleanup(cancel)

	prod, err := feed.Start(ctx, conn, feed.Options{
		Source:         "dropbox",
		DBPath:         dbPath,
		GenerationPath: filepath.Join(t.TempDir(), "generation"),
		Registry:       outbox.Registry{{Kind: "create", Subject: "/<path>", Description: "created"}},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	tx, err := conn.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := prod.Outbox.Append(tx, outbox.Event{Kind: "create", Subject: "/bills/a.pdf", Payload: json.RawMessage(`{"path":"/bills/a.pdf"}`)}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	srv := httptest.NewServer(prod.Handler)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET feed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("feed status = %d, want 200", resp.StatusCode)
	}

	wantEventLine := "event: " + routing.Key("dropbox", "create", "/bills/a.pdf")
	var eventLine, dataLine string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventLine = line
			continue
		}
		if eventLine == wantEventLine && strings.HasPrefix(line, "data: ") {
			dataLine = line
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan feed: %v", err)
	}

	// R-7LVK-IMJ6
	if eventLine != wantEventLine {
		t.Fatalf("event line = %q, want canonical key", eventLine)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(dataLine, "data: ")), &envelope); err != nil {
		t.Fatalf("decode data envelope %q: %v", dataLine, err)
	}
	if envelope["kind"] != "create" || envelope["subject"] != "/bills/a.pdf" {
		t.Fatalf("envelope kind/subject = (%v, %v), want (create, /bills/a.pdf)", envelope["kind"], envelope["subject"])
	}
	if _, ok := envelope["type"]; ok {
		t.Fatalf("envelope still has type key: %#v", envelope)
	}
}

func openOutboxDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "events.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := conn.Exec(outbox.SchemaSQL); err != nil {
		_ = conn.Close()
		t.Fatalf("create outbox schema: %v", err)
	}
	return conn, dbPath
}
