package repos

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"eventplane/outbox"
)

func TestEventsDeclareOnlyOutcomeFamiliesAndRejectUndeclaredKinds(t *testing.T) {
	// R-FT54-LW5D
	if len(Events) != 2 {
		t.Fatalf("event family count = %d, want 2", len(Events))
	}
	wantKinds := []string{"session.succeeded", "session.failed"}
	for index, family := range Events {
		if family.Kind != wantKinds[index] || family.Subject != "/<repo name>" || family.Description == "" || family.Sample == nil {
			t.Errorf("Events[%d] = %#v", index, family)
		}
		detail, err := Events.Detail(family.Kind)
		if err != nil || detail["subject"] != "/<repo name>" {
			t.Errorf("Detail(%q) = %#v, %v", family.Kind, detail, err)
		}
	}

	_, conn := migratedStore(t)
	producer := newTestOutbox(t, conn, time.Now)
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := producer.Append(tx, outbox.Event{Kind: "repo.deleted", Subject: "/fixture", Payload: json.RawMessage(`{}`)}); err == nil || !strings.Contains(err.Error(), "not in the registry") {
		t.Fatalf("undeclared Append error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("outbox count = %d, %v; want 0", count, err)
	}
}

func TestFinishSessionAppendsExactOutcomePayloadsAtomically(t *testing.T) {
	// R-FUD0-ZNW2
	store, conn := migratedStore(t)
	now := time.Date(2026, 7, 15, 15, 4, 5, 123456789, time.UTC)
	producer := newTestOutbox(t, conn, func() time.Time { return now })
	issue := 17
	for _, session := range []Session{
		{ID: "success-event", RepoName: "alpha", OwnerEmail: "owner@example.com", IssueNumber: &issue, Attempt: 1, Branch: "ikigenba/issue-17", Instructions: "land", Status: StatusRunning, CreatedAt: now.Add(-time.Hour), LogPath: "success.jsonl"},
		{ID: "failed-event", RepoName: "beta", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "ikigenba/session-failed", Instructions: "fail", Status: StatusRunning, CreatedAt: now.Add(-time.Hour), LogPath: "failed.jsonl"},
		{ID: "cancelled-event", RepoName: "gamma", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "ikigenba/session-cancelled", Instructions: "cancel", Status: StatusRunning, CreatedAt: now.Add(-time.Hour), LogPath: "cancelled.jsonl"},
	} {
		insertSession(t, store, session)
	}
	prURL := "https://github.com/example/alpha/pull/9"
	failure := "check failed"
	finishes := []struct {
		id, status string
		err, pr    *string
	}{
		{"success-event", StatusSucceeded, nil, &prURL},
		{"failed-event", StatusFailed, &failure, nil},
		{"cancelled-event", StatusCancelled, nil, nil},
	}
	for _, finish := range finishes {
		if err := store.FinishSession(context.Background(), finish.id, finish.status, finish.err, finish.pr, now, AppendOutcome(producer)); err != nil {
			t.Fatalf("finish %s: %v", finish.id, err)
		}
	}

	rows, err := conn.Query(`SELECT kind, subject, payload FROM outbox ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := []struct {
		kind, subject string
		payload       map[string]any
	}{
		{"session.succeeded", "/alpha", map[string]any{"repo": "alpha", "session_id": "success-event", "issue_number": float64(17), "branch": "ikigenba/issue-17", "pr_url": prURL, "ended_at": now.Format(time.RFC3339Nano)}},
		{"session.failed", "/beta", map[string]any{"repo": "beta", "session_id": "failed-event", "branch": "ikigenba/session-failed", "error": failure, "ended_at": now.Format(time.RFC3339Nano)}},
		{"session.failed", "/gamma", map[string]any{"repo": "gamma", "session_id": "cancelled-event", "branch": "ikigenba/session-cancelled", "error": "cancelled", "ended_at": now.Format(time.RFC3339Nano)}},
	}
	rowCount := 0
	for index := 0; rows.Next(); index++ {
		rowCount++
		if index >= len(want) {
			t.Fatal("more outbox rows than expected")
		}
		var kind, subject string
		var encoded []byte
		if err := rows.Scan(&kind, &subject, &encoded); err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(encoded, &payload); err != nil {
			t.Fatal(err)
		}
		if kind != want[index].kind || subject != want[index].subject || !reflect.DeepEqual(payload, want[index].payload) {
			t.Errorf("row %d = %s %s %#v, want %#v", index, kind, subject, payload, want[index])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if rowCount != len(want) {
		t.Fatalf("outbox row count = %d, want %d", rowCount, len(want))
	}

	rollback := Session{ID: "rollback-event", RepoName: "delta", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "rollback", Instructions: "rollback", Status: StatusRunning, CreatedAt: now, LogPath: "rollback.jsonl"}
	insertSession(t, store, rollback)
	forced := errors.New("forced append failure")
	err = store.FinishSession(context.Background(), rollback.ID, StatusFailed, &failure, nil, now,
		func(context.Context, *sql.Tx, Session) error { return forced })
	if !errors.Is(err, forced) {
		t.Fatalf("forced finish error = %v", err)
	}
	got, err := store.GetSession(context.Background(), rollback.ID)
	if err != nil || got.Status != StatusRunning {
		t.Fatalf("rolled-back session = %#v, %v", got, err)
	}
}

func TestFeedHandlerFramesAppendedOutcomeEnvelope(t *testing.T) {
	// R-FVKX-DFMR
	_, conn := migratedStore(t)
	producer := newTestOutbox(t, conn, time.Now)
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"repo":"alpha","session_id":"feed","branch":"main","ended_at":"2026-07-15T12:00:00Z"}`)
	if err := producer.Append(tx, outbox.Event{Kind: "session.succeeded", Subject: "/alpha", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	producer.Ring()

	server := httptest.NewServer(producer.FeedHandler())
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/feed", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	foundEvent := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "event: repos:session.succeeded/alpha" {
			foundEvent = true
		}
		if foundEvent && strings.HasPrefix(line, "data: ") {
			var envelope map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope["kind"] != "session.succeeded" || envelope["subject"] != "/alpha" {
				t.Fatalf("envelope = %#v", envelope)
			}
			return
		}
	}
	t.Fatalf("feed ended without outcome frame: %v", scanner.Err())
}

func newTestOutbox(t *testing.T, conn *sql.DB, now func() time.Time) *outbox.Outbox {
	t.Helper()
	producer, err := outbox.New(conn, outbox.Options{Source: "repos", Registry: Events, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return producer
}
