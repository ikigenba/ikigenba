package repos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"eventplane/outbox"
)

// Events is the complete set of event families published by repos.
var Events = outbox.Registry{
	{
		Kind:        "session.succeeded",
		Subject:     "/<repo name>",
		Description: "A repository session completed successfully and its work landed.",
		Sample: sessionOutcomePayload{
			Repo: "example", SessionID: "01JEXAMPLE", IssueNumber: intPointer(42),
			Branch: "ikigenba/issue-42", PRURL: "https://github.com/example/example/pull/1",
			EndedAt: "2026-07-15T12:00:00Z",
		},
	},
	{
		Kind:        "session.failed",
		Subject:     "/<repo name>",
		Description: "A repository session ended without landing, including cancellation.",
		Sample: sessionOutcomePayload{
			Repo: "example", SessionID: "01JEXAMPLE", IssueNumber: intPointer(42),
			Branch: "ikigenba/issue-42", Error: "check failed",
			EndedAt: "2026-07-15T12:00:00Z",
		},
	},
}

type sessionOutcomePayload struct {
	Repo        string `json:"repo"`
	SessionID   string `json:"session_id"`
	IssueNumber *int   `json:"issue_number,omitempty"`
	Branch      string `json:"branch"`
	PRURL       string `json:"pr_url,omitempty"`
	Error       string `json:"error,omitempty"`
	EndedAt     string `json:"ended_at"`
}

// AppendOutcome returns the atomic store callback for session outcome events.
func AppendOutcome(producer *outbox.Outbox) OutcomeAppender {
	return func(_ context.Context, tx *sql.Tx, session Session) error {
		if producer == nil {
			return errors.New("append outcome: producer is required")
		}
		if session.EndedAt == nil {
			return errors.New("append outcome: terminal session has no ended_at")
		}
		payload := sessionOutcomePayload{
			Repo: session.RepoName, SessionID: session.ID, IssueNumber: session.IssueNumber,
			Branch: session.Branch, EndedAt: session.EndedAt.UTC().Format(time.RFC3339Nano),
		}
		kind := "session.failed"
		switch session.Status {
		case StatusSucceeded:
			kind = "session.succeeded"
			if session.PRURL != nil {
				payload.PRURL = *session.PRURL
			}
		case StatusCancelled:
			payload.Error = "cancelled"
		case StatusFailed:
			if session.Error != nil {
				payload.Error = *session.Error
			}
		default:
			return fmt.Errorf("append outcome: non-terminal status %q", session.Status)
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("append outcome: marshal: %w", err)
		}
		return producer.Append(tx, outbox.Event{Kind: kind, Subject: "/" + session.RepoName, Payload: encoded})
	}
}

func intPointer(value int) *int { return &value }
