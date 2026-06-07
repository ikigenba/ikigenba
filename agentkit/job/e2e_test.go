package job_test

// End-to-end agentkit wiring test (Task 1.3). It drives the full chain a
// consumer (wiki's ingest, agent's runner) relies on — agent loop → tool
// dispatch → path confinement → job-runner completion — with NO network: the
// provider is a deterministic stub that returns canned turns, never an HTTP
// call. It is the reference for how wiki's Task 4.1 should wrap agent.Run inside
// a job.Job and spawn it through job.Runner.
//
// The slice exercised:
//   1. a stub provider returns one Write tool-use turn, then a final text turn;
//   2. agent.Run dispatches the Write through tools.Dispatch, confined to a temp
//      sandbox root (the real agentkit confinement), and loops to the text turn;
//   3. the whole agent.Run is the body of a job.Job, spawned via Runner.Spawn
//      over the reference MemStore, reaching a terminal succeeded record;
//   4. assertions: the file landed inside the sandbox with the expected bytes,
//      the loop dispatched the tool then terminated, the record is succeeded,
//      and a sandbox-escape Write is rejected by confinement end-to-end.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentkit/agent"
	"agentkit/job"
	"agentkit/provider"
	"agentkit/tools"
	"agentkit/tools/write"
	"agentkit/wire"
)

// stubProvider is a no-network provider.Client. Each Stream call replays the
// next canned event sequence in order, so a two-element sequence drives one
// tool-use turn followed by one final text turn. It records the number of calls
// so the test can prove the loop actually round-tripped the tool.
type stubProvider struct {
	sequences [][]provider.Event
	calls     int
}

func (s *stubProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	if s.calls >= len(s.sequences) {
		return nil, &provider.Error{Kind: provider.ErrUnknown, Msg: "stubProvider exhausted"}
	}
	evs := s.sequences[s.calls]
	s.calls++
	ch := make(chan provider.Event, len(evs))
	for _, ev := range evs {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// agentJob is the unit of work the runner spawns: it runs the real agent loop
// against the stub provider, confined to sandboxRoot, capturing the wire stream
// so it can return a usage blob (mirroring agent's runner.captureUsage and what
// wiki's ingest job will do). This is the shape a wiki consumer's Job.Run takes.
type agentJob struct {
	client      provider.Client
	req         provider.Request
	sandboxRoot string

	// stream captures the agent's wire output for post-run assertions and usage
	// extraction; in production this is a log file + a tee buffer.
	stream bytes.Buffer
}

func (j *agentJob) Run(ctx context.Context) (string, error) {
	sess := wire.NewSession(&j.stream)
	if err := agent.Run(ctx, j.client, sess, j.req, nil /* freeform */, j.sandboxRoot, nil); err != nil {
		return "", err
	}
	return captureUsage(j.stream.Bytes()), nil
}

// captureUsage extracts the accounting blob from the last result event in the
// wire stream — the same best-effort scan agent's runner does and wiki's ingest
// job will reuse to populate Record.UsageJSON.
func captureUsage(streamed []byte) string {
	var out json.RawMessage
	for _, line := range bytes.Split(streamed, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev struct {
			Type  string          `json:"type"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal(line, &ev); err != nil || ev.Type != "result" {
			continue
		}
		if len(ev.Usage) > 0 && !bytes.Equal(bytes.TrimSpace(ev.Usage), []byte("null")) {
			out = ev.Usage
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(map[string]json.RawMessage{"usage": out})
	return string(b)
}

// mustToolUseEvent builds a provider EventToolUse for the named tool with input
// marshalled from a map, the shape the real backend emits to the loop.
func mustToolUseEvent(t *testing.T, id, name string, input map[string]any) provider.EventToolUse {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal tool input: %v", err)
	}
	return provider.EventToolUse{ID: id, Name: name, Input: raw}
}

// buildToolset is how a consumer (wiki) advertises a restricted toolset: build
// []provider.Tool from selected descriptors. Here we advertise only Write, the
// way wiki's ingest agent advertises a write-enabled, bash-free surface.
func buildToolset(descs []tools.Descriptor) []provider.Tool {
	out := make([]provider.Tool, 0, len(descs))
	for _, d := range descs {
		out = append(out, provider.Tool{Name: d.Name, InputSchema: d.InputSchema})
	}
	return out
}

// TestE2E_AgentJobWritesConfinedFile drives loop → tool dispatch → confinement →
// job completion with no network. A stub provider asks the agent to Write a file
// at a relative path; the agent dispatches the Write confined to a temp sandbox;
// the whole run is a job.Job spawned to a terminal succeeded record; the file is
// asserted to exist inside the sandbox with the expected bytes.
func TestE2E_AgentJobWritesConfinedFile(t *testing.T) {
	sandbox := t.TempDir()
	const (
		relPath  = "notes/page.md"
		contents = "# integrated page\n\nfiled by the ingest agent\n"
	)

	// Turn 1: the model asks to Write the file at a relative path (resolved under
	// the sandbox by confinement). Turn 2: the model finishes with a text turn.
	stub := &stubProvider{sequences: [][]provider.Event{
		{
			mustToolUseEvent(t, "toolu_write_01", write.Name, map[string]any{
				"file_path": relPath,
				"content":   contents,
			}),
			provider.EventDone{StopReason: "tool_use"},
		},
		{
			provider.EventTextDelta{Text: "done"},
			provider.EventUsage{InputTokens: 12, OutputTokens: 3},
			provider.EventDone{StopReason: "end_turn"},
		},
	}}

	// The Write tool needs its parent dir to exist (it does not mkdir), so create
	// the in-sandbox subdir the agent will write into.
	if err := os.MkdirAll(filepath.Join(sandbox, "notes"), 0o755); err != nil {
		t.Fatalf("mkdir sandbox subdir: %v", err)
	}

	jb := &agentJob{
		client:      stub,
		sandboxRoot: sandbox,
		req: provider.Request{
			Model:        "claude-sonnet-4-6",
			SystemPrompt: "file this content into the wiki",
			Messages: []provider.Message{{
				Role:   provider.RoleUser,
				Blocks: []provider.Block{provider.TextBlock{Text: "ingest: " + contents}},
			}},
			Tools: buildToolset([]tools.Descriptor{{Name: write.Name, InputSchema: write.InputSchema}}),
		},
	}

	store := job.NewMemStore()
	runner := job.New(store, 5*time.Second)

	rec, err := runner.Spawn(job.Record{ID: "run-e2e-1", FlightKey: "sandbox-" + sandbox}, jb)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if rec.Status != job.StatusRunning {
		t.Fatalf("spawned record status = %q, want running", rec.Status)
	}

	final := awaitTerminal(t, store, "run-e2e-1")

	// The job record reached succeeded.
	if final.Status != job.StatusSucceeded {
		t.Fatalf("terminal status = %q (err=%q), want succeeded", final.Status, final.Error)
	}
	if final.Error != "" {
		t.Fatalf("terminal error = %q, want empty on success", final.Error)
	}
	if final.EndedAt.IsZero() {
		t.Fatalf("EndedAt is zero, want a terminal timestamp")
	}
	// Usage was captured from the result event and threaded into the record.
	if final.UsageJSON == "" || !strings.Contains(final.UsageJSON, "input_tokens") {
		t.Fatalf("UsageJSON = %q, want a captured usage blob with input_tokens", final.UsageJSON)
	}

	// The loop dispatched the tool then terminated: two provider turns total.
	if stub.calls != 2 {
		t.Fatalf("provider Stream calls = %d, want 2 (tool-use turn + final turn)", stub.calls)
	}

	// The file was actually written INSIDE the sandbox with the expected bytes.
	written := filepath.Join(sandbox, relPath)
	got, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read written file %s: %v", written, err)
	}
	if string(got) != contents {
		t.Fatalf("written contents = %q, want %q", got, contents)
	}

	// And the wire stream shows the loop actually round-tripped: an assistant
	// tool_use, a user tool_result, then a terminal result.
	wireOut := jb.stream.String()
	for _, want := range []string{`"type":"assistant"`, `"type":"tool_use"`, `"type":"user"`, `"type":"tool_result"`, `"type":"result"`} {
		if !strings.Contains(wireOut, want) {
			t.Fatalf("wire stream missing %s; got:\n%s", want, wireOut)
		}
	}
}

// TestE2E_ConfinementRejectsEscape proves the confinement boundary holds end to
// end: when the stub provider asks the agent to Write outside the sandbox, the
// loop dispatches it, confinement rejects it as an is_error tool_result, no file
// is created outside the sandbox, and the job still reaches a terminal succeeded
// record (the escape is surfaced to the model, not a transport failure).
func TestE2E_ConfinementRejectsEscape(t *testing.T) {
	parent := t.TempDir()
	sandbox := filepath.Join(parent, "box")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatalf("mkdir sandbox: %v", err)
	}
	escapeTarget := filepath.Join(parent, "escape.md") // sibling of the sandbox

	stub := &stubProvider{sequences: [][]provider.Event{
		{
			// "../escape.md" resolves outside the sandbox root → must be rejected.
			mustToolUseEvent(t, "toolu_write_escape", write.Name, map[string]any{
				"file_path": "../escape.md",
				"content":   "this must never land",
			}),
			provider.EventDone{StopReason: "tool_use"},
		},
		{
			provider.EventTextDelta{Text: "could not write outside sandbox"},
			provider.EventDone{StopReason: "end_turn"},
		},
	}}

	jb := &agentJob{
		client:      stub,
		sandboxRoot: sandbox,
		req: provider.Request{
			Messages: []provider.Message{{
				Role:   provider.RoleUser,
				Blocks: []provider.Block{provider.TextBlock{Text: "try to escape"}},
			}},
			Tools: buildToolset([]tools.Descriptor{{Name: write.Name, InputSchema: write.InputSchema}}),
		},
	}

	store := job.NewMemStore()
	runner := job.New(store, 5*time.Second)
	if _, err := runner.Spawn(job.Record{ID: "run-escape", FlightKey: "escape"}, jb); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	final := awaitTerminal(t, store, "run-escape")
	// The run completes (the escape is a tool-level is_error, not a fatal error).
	if final.Status != job.StatusSucceeded {
		t.Fatalf("terminal status = %q (err=%q), want succeeded", final.Status, final.Error)
	}

	// No file landed outside the sandbox.
	if _, err := os.Stat(escapeTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escape file %s exists (stat err=%v); confinement boundary breached", escapeTarget, err)
	}

	// The wire stream proves confinement reported the escape as an is_error
	// tool_result naming the sandbox boundary.
	wireOut := jb.stream.String()
	if !strings.Contains(wireOut, "escapes sandbox") {
		t.Fatalf("wire stream missing 'escapes sandbox' tool_result; got:\n%s", wireOut)
	}
}

// awaitTerminal polls the store until id reaches a terminal status, failing the
// test on timeout. The runner writes terminal state from its goroutine, so the
// test waits for that rather than racing it.
func awaitTerminal(t *testing.T, store *job.MemStore, id string) job.Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec, err := store.Load(context.Background(), id)
		if err == nil && rec.Status.Terminal() {
			return rec
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for run %s to reach terminal state (last err=%v, status=%q)", id, err, rec.Status)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
