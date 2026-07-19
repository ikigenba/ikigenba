package prompt

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"prompts/internal/calls"
)

type sessionCallRow struct {
	class, origin, name, groupID, ownerEmail, provider, model string
	inputTokens, outputTokens, totalTokens                    int64
	usageJSON, errMsg                                         string
	costUSD                                                   float64
	requestBody, responseBody                                 sql.NullString
}

func readSessionCalls(t *testing.T, conn *sql.DB) []sessionCallRow {
	t.Helper()
	rows, err := conn.Query(`SELECT class, origin, name, group_id, owner_email, provider, model,
		input_tokens, output_tokens, total_tokens, usage_json, cost_usd, error,
		request_body, response_body FROM calls ORDER BY started_at, id`)
	if err != nil {
		t.Fatalf("query calls: %v", err)
	}
	defer rows.Close()
	var got []sessionCallRow
	for rows.Next() {
		var row sessionCallRow
		if err := rows.Scan(&row.class, &row.origin, &row.name, &row.groupID,
			&row.ownerEmail, &row.provider, &row.model, &row.inputTokens,
			&row.outputTokens, &row.totalTokens, &row.usageJSON, &row.costUSD,
			&row.errMsg, &row.requestBody, &row.responseBody); err != nil {
			t.Fatalf("scan call: %v", err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("calls rows: %v", err)
	}
	return got
}

func TestFinishRunRecordsManualAndTriggeredOrigins(t *testing.T) {
	// R-6KUS-37C5
	store, conn := newProducerStore(t)
	store.Calls = calls.NewStore(conn)
	ctx := context.Background()
	_, manual := seedRunningRun(t, store, "manual")
	_, triggered := seedRunningRunTrig(t, store, "scheduled", "cron", "tick", "/nightly", "ev-1")

	for _, run := range []Run{manual, triggered} {
		if err := store.FinishRun(ctx, FinishRunInput{
			RunID: run.ID, Status: RunSucceeded, EndedAt: store.nowStr(),
			Provider: "anthropic", Model: "claude-haiku-4-5",
		}); err != nil {
			t.Fatalf("FinishRun(%s): %v", run.ID, err)
		}
	}

	rows := readSessionCalls(t, conn)
	if len(rows) != 2 {
		t.Fatalf("calls rows = %+v, want two", rows)
	}
	origins := map[string]string{}
	for _, row := range rows {
		origins[row.groupID] = row.origin
	}
	if got := origins[manual.ID]; got != "user:"+manual.OwnerEmail {
		t.Fatalf("manual origin = %q, want user:%s", got, manual.OwnerEmail)
	}
	if got := origins[triggered.ID]; got != "trigger:cron" {
		t.Fatalf("triggered origin = %q, want trigger:cron", got)
	}
}

func TestFinishRunCallsInsertFailureRollsBackTerminalWrite(t *testing.T) {
	// R-6M2O-GZ2U
	store, conn := newProducerStore(t)
	store.Calls = calls.NewStore(conn)
	ctx := context.Background()
	_, run := seedRunningRun(t, store, "atomic")
	if _, err := conn.Exec(`CREATE TRIGGER fail_session_call BEFORE INSERT ON calls
		BEGIN SELECT RAISE(FAIL, 'injected calls insert failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	err := store.FinishRun(ctx, FinishRunInput{
		RunID: run.ID, Status: RunSucceeded, EndedAt: store.nowStr(),
		Provider: "anthropic", Model: "claude-haiku-4-5",
	})
	if err == nil || !strings.Contains(err.Error(), "injected calls insert failure") {
		t.Fatalf("FinishRun error = %v, want injected calls failure", err)
	}
	got, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != RunRunning || got.EndedAt != "" || got.UsageJSON != "" {
		t.Fatalf("run terminal write committed despite calls failure: %+v", got)
	}
	if rows := readSessionCalls(t, conn); len(rows) != 0 {
		t.Fatalf("calls rows = %+v, want none after rollback", rows)
	}
}
