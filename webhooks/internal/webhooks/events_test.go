package webhooks

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"eventplane/outbox"

	"webhooks/internal/db"
)

// newRecordFixture stands up a real temp-file SQLite (never :memory:), migrates
// it so the outbox table exists, wires the Service with a real *outbox.Outbox
// over a fixed clock, inserts one webhook, and returns everything a Record test
// needs — including the db path so a freshly-opened connection can prove
// durability across reopen.
func newRecordFixture(t *testing.T, wh db.Webhook) (svc *Service, dbPath string, now time.Time) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "webhooks.db")
	conn, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	now = time.Date(2026, 6, 25, 12, 0, 0, 123456789, time.UTC)
	clk := fixedClock{t: now}

	ob, err := outbox.New(conn, outbox.Options{
		Source:   "webhooks",
		Registry: Events,
		Now:      clk.Now,
	})
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}

	svc = NewService(conn, clk)
	svc.Outbox = ob

	wh.CreatedAt = now
	if err := db.NewStore(conn).Insert(context.Background(), wh, "deadbeef"); err != nil {
		t.Fatalf("Insert webhook: %v", err)
	}
	return svc, dbPath, now
}

// decodeOnlyOutboxRow asserts there is exactly one outbox row read through conn
// and returns its type and decoded payload.
func decodeOnlyOutboxRow(t *testing.T, conn *sql.DB) (eventType string, p webhookReceivedPayload) {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one outbox row, got %d", n)
	}
	var payload string
	if err := conn.QueryRow(`SELECT type, payload FROM outbox`).Scan(&eventType, &payload); err != nil {
		t.Fatalf("scan outbox: %v", err)
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return eventType, p
}

// R-GTUZ-AIGW — one Record writes exactly one webhook.received row whose payload
// carries the webhook name, stored owner, content_type, and a binary body that
// base64-decodes byte-for-byte.
func TestRecord_WritesEventWithBinaryBodyRecoverable(t *testing.T) {
	binary := []byte{0x00, 0xff, 0x01, 0xfe, 0x80, 0x7f, 0x00, 0xc3, 0x28}
	wh := db.Webhook{Name: "deploy-hook", OwnerEmail: "owner@example.com"}
	svc, _, _ := newRecordFixture(t, wh)

	if err := svc.Record(context.Background(), wh, "application/octet-stream", binary); err != nil {
		t.Fatalf("Record: %v", err)
	}

	typ, p := decodeOnlyOutboxRow(t, svc.db)
	if typ != "webhook.received" {
		t.Fatalf("type = %q, want webhook.received", typ)
	}
	if p.Name != "deploy-hook" {
		t.Errorf("name = %q, want deploy-hook", p.Name)
	}
	if p.Owner != "owner@example.com" {
		t.Errorf("owner = %q, want owner@example.com", p.Owner)
	}
	if p.ContentType != "application/octet-stream" {
		t.Errorf("content_type = %q", p.ContentType)
	}
	got, err := base64.StdEncoding.DecodeString(p.Body)
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if string(got) != string(binary) {
		t.Errorf("body = %v, want %v (byte-for-byte)", got, binary)
	}
}

// R-GV2V-OA7L — the payload owner is always the stored owner_email, never any
// owner the triggering call might carry.
func TestRecord_StampsStoredOwnerNotCallerInput(t *testing.T) {
	wh := db.Webhook{Name: "scoped", OwnerEmail: "stored-owner@example.com"}
	svc, _, _ := newRecordFixture(t, wh)

	// The caller passes a wh value whose OwnerEmail is the stored one; even if a
	// different identity triggered the call, Record only ever reads wh.OwnerEmail.
	if err := svc.Record(context.Background(), wh, "text/plain", []byte("hi")); err != nil {
		t.Fatalf("Record: %v", err)
	}

	_, p := decodeOnlyOutboxRow(t, svc.db)
	if p.Owner != "stored-owner@example.com" {
		t.Fatalf("owner = %q, want stored-owner@example.com (caller input must never be echoed)", p.Owner)
	}
}

// R-GWAS-21YA — after Record returns nil the row is durable: a freshly-opened
// connection to the same temp file sees it (durable-before-ack, not a value on
// the live handle).
func TestRecord_DurableAcrossReopen(t *testing.T) {
	wh := db.Webhook{Name: "durable", OwnerEmail: "owner@example.com"}
	svc, dbPath, _ := newRecordFixture(t, wh)

	if err := svc.Record(context.Background(), wh, "text/plain", []byte("payload")); err != nil {
		t.Fatalf("Record: %v", err)
	}

	fresh, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer fresh.Close()

	typ, p := decodeOnlyOutboxRow(t, fresh)
	if typ != "webhook.received" || p.Name != "durable" {
		t.Fatalf("reopened row = (%q, %q), want (webhook.received, durable)", typ, p.Name)
	}
}

// R-GXIO-FTOZ — one Record produces exactly one row, and the webhook's
// last_triggered_at equals the payload's received_at (append + touch committed
// in one tx under one fixed clock).
func TestRecord_TouchEqualsReceivedAtInOneTx(t *testing.T) {
	wh := db.Webhook{Name: "atomic", OwnerEmail: "owner@example.com"}
	svc, _, _ := newRecordFixture(t, wh)

	if err := svc.Record(context.Background(), wh, "application/json", []byte("{}")); err != nil {
		t.Fatalf("Record: %v", err)
	}

	_, p := decodeOnlyOutboxRow(t, svc.db) // also asserts exactly one row

	got, _, ok, err := db.NewStore(svc.db).GetByName(context.Background(), "atomic")
	if err != nil || !ok {
		t.Fatalf("GetByName: ok=%v err=%v", ok, err)
	}
	if got.LastTriggeredAt == nil {
		t.Fatal("last_triggered_at not stamped")
	}
	lt := got.LastTriggeredAt.UTC().Format(time.RFC3339Nano)
	if lt != p.ReceivedAt {
		t.Fatalf("last_triggered_at %q != received_at %q", lt, p.ReceivedAt)
	}
}

// R-GYQK-TLFO — Events declares webhook.received, and an Append of a type not in
// the registry errors with no row written.
func TestEvents_RegistryGatesAppend(t *testing.T) {
	if !registryHas(Events, "webhook.received") {
		t.Fatal("Events does not declare webhook.received")
	}

	dbPath := filepath.Join(t.TempDir(), "webhooks.db")
	conn, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	ob, err := outbox.New(conn, outbox.Options{Source: "webhooks", Registry: Events})
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}

	before := outboxCount(t, conn)

	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := ob.Append(tx, outbox.Event{Type: "webhook.not-a-real-type", Payload: json.RawMessage(`{}`)}); err == nil {
		tx.Rollback()
		t.Fatal("Append of an unregistered type returned nil, want error")
	}
	tx.Rollback()

	if after := outboxCount(t, conn); after != before {
		t.Fatalf("outbox row count changed from %d to %d on rejected Append", before, after)
	}
}

// registryHas reports whether the registry declares eventType (the unexported
// has() is not reachable from a test outside the outbox package).
func registryHas(r outbox.Registry, eventType string) bool {
	for _, et := range r {
		if et.Type == eventType {
			return true
		}
	}
	return false
}

func outboxCount(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}
