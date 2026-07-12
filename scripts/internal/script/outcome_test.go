package script

import (
	"encoding/json"
	"testing"
)

func intp(i int) *int { return &i }

func TestCompletionEvent(t *testing.T) {
	exit0 := intp(0)
	exit1 := intp(1)

	tests := []struct {
		name        string
		in          FinishRunInput
		wantEmit    bool
		wantKind    string
		wantErrText string // "" means error key must be absent
	}{
		{
			name: "cancelled emits nothing",
			in: FinishRunInput{
				RunID: "r1", ScriptID: "s1", Status: RunCancelled,
			},
			wantEmit: false,
		},
		{
			name: "succeeded has no error key",
			in: FinishRunInput{
				RunID: "r1", ScriptID: "s1", ScriptName: "exporter",
				Status: RunSucceeded, ExitCode: exit0,
				StdoutTail: "ok\n", StdoutTrunc: false,
				// ErrMsg present but must be dropped on success.
				ErrMsg: "should be discarded",
			},
			wantEmit: true, wantKind: EventSucceeded, wantErrText: "",
		},
		{
			name: "failed carries error",
			in: FinishRunInput{
				RunID: "r2", ScriptID: "s1", ScriptName: "exporter",
				Status: RunFailed, ExitCode: exit1,
				StderrTail: "boom\n", StderrTrunc: true,
				ErrMsg: "run TTL exceeded",
			},
			wantEmit: true, wantKind: EventFailed, wantErrText: "run TTL exceeded",
		},
		{
			name: "manual run has empty trigger fields",
			in: FinishRunInput{
				RunID: "r3", ScriptID: "s1", ScriptName: "manual",
				Status: RunSucceeded, ExitCode: exit0,
			},
			wantEmit: true, wantKind: EventSucceeded,
		},
		{
			name: "triggered run populates trigger fields",
			in: FinishRunInput{
				RunID: "r4", ScriptID: "s1", ScriptName: "reactor",
				Status:        RunSucceeded,
				ExitCode:      exit0,
				TriggerSource: "crm", TriggerKind: "contact.created", TriggerSubject: "/c1", TriggerEventID: "evt-123",
			},
			wantEmit: true, wantKind: EventSucceeded,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev, emit, err := completionEvent(tc.in)
			if err != nil {
				t.Fatalf("completionEvent error: %v", err)
			}
			if emit != tc.wantEmit {
				t.Fatalf("shouldEmit = %v, want %v", emit, tc.wantEmit)
			}
			if !emit {
				if ev.Kind != "" || ev.Payload != nil {
					t.Fatalf("no-emit must return zero Event, got %+v", ev)
				}
				return
			}
			if ev.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", ev.Kind, tc.wantKind)
			}
			if want := "/" + tc.in.ScriptName; ev.Subject != want {
				t.Errorf("Subject = %q, want %q", ev.Subject, want)
			}

			// Decode into a generic map to assert exact JSON key presence.
			var m map[string]any
			if err := json.Unmarshal(ev.Payload, &m); err != nil {
				t.Fatalf("payload not valid JSON: %v", err)
			}

			wantKeys := []string{
				"script_id", "script_name", "run_id", "status", "exit_code",
				"trigger", "stdout", "stdout_truncated", "stderr", "stderr_truncated",
			}
			for _, k := range wantKeys {
				if _, ok := m[k]; !ok {
					t.Errorf("payload missing key %q; got keys %v", k, keysOf(m))
				}
			}

			if m["script_id"] != tc.in.ScriptID {
				t.Errorf("script_id = %v, want %v", m["script_id"], tc.in.ScriptID)
			}
			if m["status"] != tc.in.Status {
				t.Errorf("status = %v, want %v", m["status"], tc.in.Status)
			}

			// exit_code round-trips as a number.
			if tc.in.ExitCode != nil {
				ec, ok := m["exit_code"].(float64)
				if !ok || int(ec) != *tc.in.ExitCode {
					t.Errorf("exit_code = %v, want %d", m["exit_code"], *tc.in.ExitCode)
				}
			}

			// error key handling.
			gotErr, hasErr := m["error"]
			if tc.wantErrText == "" {
				if hasErr {
					t.Errorf("error key present on success/empty, value %v", gotErr)
				}
			} else {
				if gotErr != tc.wantErrText {
					t.Errorf("error = %v, want %q", gotErr, tc.wantErrText)
				}
			}

			// trigger sub-object source/kind/subject/event_id.
			tr, ok := m["trigger"].(map[string]any)
			if !ok {
				t.Fatalf("trigger not an object: %v", m["trigger"])
			}
			for _, k := range []string{"source", "kind", "subject", "event_id"} {
				if _, ok := tr[k]; !ok {
					t.Errorf("trigger missing key %q", k)
				}
			}
			if tr["source"] != tc.in.TriggerSource {
				t.Errorf("trigger.source = %v, want %v", tr["source"], tc.in.TriggerSource)
			}
			if tr["kind"] != tc.in.TriggerKind {
				t.Errorf("trigger.kind = %v, want %v", tr["kind"], tc.in.TriggerKind)
			}
			if tr["event_id"] != tc.in.TriggerEventID {
				t.Errorf("trigger.event_id = %v, want %v", tr["event_id"], tc.in.TriggerEventID)
			}
		})
	}
}

func TestCompletionEventNormalizesRoutingSubject(t *testing.T) {
	// R-82AG-F74Z
	zero := 0
	for _, tc := range []struct{ name, scriptName, want string }{
		{"empty", "", ""},
		{"newlines", "nightly\nexport\rfinal", "/nightly export final"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, emit, err := completionEvent(FinishRunInput{ScriptName: tc.scriptName, Status: RunSucceeded, ExitCode: &zero})
			if err != nil || !emit {
				t.Fatalf("completionEvent = (%+v, %v, %v), want emitted event", ev, emit, err)
			}
			if ev.Subject != tc.want {
				t.Fatalf("subject = %q, want %q", ev.Subject, tc.want)
			}
		})
	}
}

func TestEventsRegistry(t *testing.T) {
	if len(Events) != 2 {
		t.Fatalf("Events len = %d, want 2", len(Events))
	}
	want := map[string]bool{EventSucceeded: false, EventFailed: false}
	for _, et := range Events {
		if _, ok := want[et.Kind]; !ok {
			t.Errorf("unexpected registered kind %q", et.Kind)
			continue
		}
		want[et.Kind] = true
		if et.Sample == nil {
			t.Errorf("kind %q has nil Sample", et.Kind)
		}
		if et.Subject != "/<script name>" {
			t.Errorf("kind %q Subject = %q, want /<script name>", et.Kind, et.Subject)
		}
	}
	for typ, seen := range want {
		if !seen {
			t.Errorf("registry missing type %q", typ)
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
