package script

import (
	"encoding/json"
	"fmt"
	"strings"

	"eventplane/outbox"
)

// Completion event kinds. scripts emits exactly two terminal, static,
// compile-time-known kinds. A cancelled run emits neither.
const (
	EventSucceeded = "succeeded" // exit 0
	EventFailed    = "failed"    // non-zero / TTL / spawn error
)

// completionPayload is the terminal completion payload. error is present only
// on the failed event (omitempty).
type completionPayload struct {
	ScriptID        string         `json:"script_id"`
	ScriptName      string         `json:"script_name"`
	RunID           string         `json:"run_id"`
	Status          string         `json:"status"`
	ExitCode        *int           `json:"exit_code"`
	Trigger         triggerPayload `json:"trigger"`
	Stdout          string         `json:"stdout"`
	StdoutTruncated bool           `json:"stdout_truncated"`
	Stderr          string         `json:"stderr"`
	StderrTruncated bool           `json:"stderr_truncated"`
	Error           string         `json:"error,omitempty"`
}

type triggerPayload struct {
	Source  string `json:"source"`
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	EventID string `json:"event_id"`
}

var sampleSuccess = completionPayload{
	ScriptID:   "01J9Z2K7P3QC8M4R6T0V2X5YA",
	ScriptName: "nightly export",
	RunID:      "01J9Z2K7P3QC8M4R6T0V2X5YB",
	Status:     RunSucceeded,
	Trigger:    triggerPayload{Source: "cron", Kind: "tick", Subject: "/nightly", EventID: "01J9Z2K7P3QC8M4R6T0V2X5YC"},
	Stdout:     "exported 42 rows\n",
}

var sampleFailure = completionPayload{
	ScriptID:   "01J9Z2K7P3QC8M4R6T0V2X5YA",
	ScriptName: "nightly export",
	RunID:      "01J9Z2K7P3QC8M4R6T0V2X5YB",
	Status:     RunFailed,
	Trigger:    triggerPayload{Source: "cron", Kind: "tick", Subject: "/nightly", EventID: "01J9Z2K7P3QC8M4R6T0V2X5YC"},
	Stderr:     "Traceback ...\n",
	Error:      "run TTL exceeded",
}

// Events is scripts' published-event Registry, wired via Spec.Events (STATIC —
// the completion types are fixed at build time). Single source of truth for both
// the reflection tool and Append-time validation: the runner can only Append a
// type that appears here.
var Events = outbox.Registry{
	{
		Kind:        EventSucceeded,
		Subject:     "/<script name>",
		Description: "A scripts run finished successfully (exit 0). Carries the script identity, the captured output tails, and the trigger context that started it (empty for a manual run).",
		Sample:      sampleSuccess,
	},
	{
		Kind:        EventFailed,
		Subject:     "/<script name>",
		Description: "A scripts run terminated in failure (non-zero exit / TTL / spawn error). Same shape as the succeeded event plus an error string.",
		Sample:      sampleFailure,
	},
}

// completionEvent builds the outbox.Event from a FinishRunInput. Returns
// (event, shouldEmit, err). shouldEmit=false ONLY for status==cancelled.
func completionEvent(in FinishRunInput) (outbox.Event, bool, error) {
	var kind string
	errMsg := in.ErrMsg
	switch in.Status {
	case RunSucceeded:
		kind = EventSucceeded
		errMsg = "" // success never carries an error
	case RunFailed:
		kind = EventFailed
	default:
		// cancelled (or any non-outcome terminal state) emits no event — an
		// operator abort is not a script outcome and must not fire chains.
		return outbox.Event{}, false, nil
	}
	raw, err := json.Marshal(completionPayload{
		ScriptID:   in.ScriptID,
		ScriptName: in.ScriptName,
		RunID:      in.RunID,
		Status:     in.Status,
		ExitCode:   in.ExitCode,
		Trigger: triggerPayload{
			Source:  in.TriggerSource,
			Kind:    in.TriggerKind,
			Subject: in.TriggerSubject,
			EventID: in.TriggerEventID,
		},
		Stdout:          in.StdoutTail,
		StdoutTruncated: in.StdoutTrunc,
		Stderr:          in.StderrTail,
		StderrTruncated: in.StderrTrunc,
		Error:           errMsg,
	})
	if err != nil {
		return outbox.Event{}, false, fmt.Errorf("script: marshal %s payload: %w", kind, err)
	}
	subject := ""
	if in.ScriptName != "" {
		subject = "/" + strings.NewReplacer("\r", " ", "\n", " ").Replace(in.ScriptName)
	}
	return outbox.Event{Kind: kind, Subject: subject, Payload: raw}, true, nil
}
