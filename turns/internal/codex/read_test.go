package codex_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/codex"
	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/klauspost/compress/zstd"
)

type obj = map[string]any

const start = "2026-10-21T09:00:00Z"

func record(kind string, payload obj, moment any) obj {
	return obj{"type": kind, "payload": payload, "timestamp": moment}
}

func metadata(id string) obj {
	return record("session_meta", obj{"id": id, "cwd": "/work/project"}, start)
}

func agent(moment any, usage obj) obj {
	return record("token_usage_record", obj{"usage": usage}, moment)
}

func person(moment any) obj {
	return record("event_msg", obj{"type": "item_completed", "item": obj{"type": "UserMessage"}}, moment)
}

func lines(t *testing.T, values ...obj) []byte {
	t.Helper()
	var data bytes.Buffer
	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	return data.Bytes()
}

func put(t *testing.T, root, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func live(name string) string { return "sessions/year/month/day/rollout-" + name + ".jsonl" }

func single(t *testing.T, records ...obj) session.Session {
	t.Helper()
	root := t.TempDir()
	put(t, root, live("sample"), lines(t, records...))
	found := codex.Read(root, time.Time{}, nil)
	if found.Skipped != 0 || len(found.Sessions) != 1 {
		t.Fatalf("Read = %+v", found)
	}
	return found.Sessions[0]
}

func wantSkipped(t *testing.T, data []byte) {
	t.Helper()
	root := t.TempDir()
	put(t, root, live("bad"), data)
	found := codex.Read(root, time.Time{}, nil)
	if found.Skipped != 1 || len(found.Sessions) != 0 {
		t.Fatalf("Read = %+v", found)
	}
}

func compress(t *testing.T, data []byte) []byte {
	t.Helper()
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := encoder.Close(); err != nil {
			t.Error(err)
		}
	}()
	return encoder.EncodeAll(data, nil)
}

// R-NX6L-ZCG0 R-1ZVP-3CPY R-213L-H4GN R-22BH-UW7C
func TestSources(t *testing.T) {
	reader := struct {
		read func(string, time.Time, []string) session.Found
	}{read: codex.Read}
	root := t.TempDir()
	valid := []string{live("live"), "archived_sessions/rollout-flat.jsonl", "archived_sessions/a/b/c/d/rollout-deep.jsonl", "sessions/a/b/c/rollout-compressed.jsonl.zst", "archived_sessions/x/rollout-compressed.jsonl.zst"}
	for _, name := range valid {
		data := lines(t, metadata(name), person(start))
		if filepath.Ext(name) == ".zst" {
			data = compress(t, data)
		}
		put(t, root, name, data)
	}
	invalid := []string{"sessions/rollout-shallow.jsonl", "sessions/a/b/rollout-two.jsonl", "sessions/a/b/c/d/rollout-four.jsonl", "sessions/a/b/c/other.jsonl", "sessions/a/b/c/rollout-other.json", "elsewhere/rollout-other.jsonl", "rollout-root.jsonl"}
	for _, name := range invalid {
		put(t, root, name, []byte("broken\n"))
	}
	if err := os.MkdirAll(filepath.Join(root, "sessions/a/b/c/rollout-directory.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	found := reader.read(root, time.Time{}, nil)
	if len(found.Sessions) != len(valid) || found.Skipped != 0 {
		t.Fatalf("Read = %+v", found)
	}
}

// R-23JE-8NY1 R-24RA-MFOQ
func TestCompressedParityAndInvalidStreams(t *testing.T) {
	for _, data := range [][]byte{lines(t, metadata("thread"), agent(start, obj{"input_tokens": 7})), {}, []byte("not json\n"), lines(t, metadata("thread"))} {
		plain, zipped := t.TempDir(), t.TempDir()
		put(t, plain, live("data"), data)
		put(t, zipped, live("data")+".zst", compress(t, data))
		if a, b := codex.Read(plain, time.Time{}, nil), codex.Read(zipped, time.Time{}, nil); !reflect.DeepEqual(a, b) {
			t.Fatalf("plain=%+v zipped=%+v", a, b)
		}
	}
	good := compress(t, lines(t, metadata("thread"), person(start)))
	for _, data := range [][]byte{nil, []byte("{}\n"), good[:len(good)-1], append(append([]byte{}, good...), 0xff)} {
		root := t.TempDir()
		put(t, root, live("bad")+".zst", data)
		if got := codex.Read(root, time.Time{}, nil); got.Skipped != 1 || len(got.Sessions) != 0 {
			t.Fatalf("invalid stream result %+v", got)
		}
	}
}

// R-25Z7-07FF R-28EZ-RQWT R-EW71-LFTV
func TestFirstMetadataOwnsIdentityAndDirectory(t *testing.T) {
	first := metadata("first")
	first["payload"].(obj)["cwd"] = "/work/./other/../project/"
	first["payload"].(obj)["originator"] = "codex_exec"
	first["payload"].(obj)["thread_source"] = "subagent"
	first["payload"].(obj)["parent_thread_id"] = "parent"
	later := metadata("later")
	later["payload"].(obj)["subagent_history_start_ordinal"] = 1000
	got := single(t, first, later, person(start))
	if got.Ref.ID != "first" || got.Directory != filepath.Clean("/work/./other/../project/") || !filepath.IsAbs(got.Directory) || !got.Headless || got.Parent == nil || got.Parent.ID != "parent" {
		t.Fatalf("session %+v", got)
	}
}

// R-2773-DZ64 R-ERBG-2CV3 R-F9LX-SWZI
func TestInvalidSessionIdentityAndDirectory(t *testing.T) {
	wantSkipped(t, lines(t, person(start)))
	for _, key := range []string{"id", "cwd"} {
		meta := metadata("thread")
		delete(meta["payload"].(obj), key)
		wantSkipped(t, lines(t, meta, person(start)))
	}
	for _, bad := range []any{nil, "", 42} {
		meta := metadata("thread")
		meta["payload"].(obj)["id"] = bad
		wantSkipped(t, lines(t, meta, metadata("valid-later"), person(start)))
	}
	for _, bad := range []any{nil, "", 42, "relative/path"} {
		meta := metadata("thread")
		meta["payload"].(obj)["cwd"] = bad
		wantSkipped(t, lines(t, meta, person(start)))
	}
}

// R-29MW-5INI
func TestHeadless(t *testing.T) {
	for _, value := range []any{nil, "codex_exec", "codex-tui", "Codex Desktop", "other", 17} {
		meta := metadata("thread")
		if value != nil {
			meta["payload"].(obj)["originator"] = value
		}
		got := single(t, meta, person(start))
		if got.Headless != (value == "codex_exec") {
			t.Fatalf("originator %v Headless=%v", value, got.Headless)
		}
	}
}

// R-1CNQ-0MG4 R-1DVM-EE6T R-2DAL-ATVL R-ETR8-TWCH R-EUZ5-7O36
func TestParents(t *testing.T) {
	cases := []struct {
		source any
		direct any
		nested any
		want   string
	}{
		{"subagent", "direct", "nested", "direct"}, {"subagent", nil, "nested", "nested"}, {"subagent", 22, "nested", "nested"}, {"subagent", "", "nested", "nested"},
		{"subagent", nil, nil, ""}, {"subagent", "", nil, ""}, {"subagent", nil, 22, ""}, {"subagent", "thread", "other", ""}, {"subagent", nil, "thread", ""},
		{nil, "parent", "nested", ""}, {"other", "parent", "nested", ""}, {23, "parent", "nested", ""},
	}
	for _, test := range cases {
		meta := metadata("thread")
		payload := meta["payload"].(obj)
		payload["thread_source"], payload["parent_thread_id"] = test.source, test.direct
		payload["source"] = obj{"subagent": obj{"thread_spawn": obj{"parent_thread_id": test.nested}}}
		got := single(t, meta, person(start))
		if test.want == "" {
			if got.Parent != nil {
				t.Fatalf("unexpected parent %+v", got.Parent)
			}
			continue
		}
		if got.Parent == nil || *got.Parent != (session.Ref{Tool: session.ToolCodex, ID: test.want}) {
			t.Fatalf("parent=%+v want=%q", got.Parent, test.want)
		}
	}
}

// R-2FQE-2DCZ R-2GYA-G53O R-2I66-TWUD R-ESJC-G4LS R-F2AJ-IAJC R-F3IF-W2A1 R-FKL1-8UNR
func TestAgentMappingAndContexts(t *testing.T) {
	usage := obj{"input_tokens": 11, "cached_input_tokens": 12, "output_tokens": 13, "reasoning_output_tokens": 14, "cache_write_input_tokens": 99, "total_tokens": 999, "other": 999}
	got := single(t, metadata("thread"), agent(start, usage), record("turn_context", obj{"model": "first"}, start), agent("2026-10-21T09:00:01Z", obj{}), record("turn_context", obj{"model": "latest"}, start), agent("2026-10-21T09:00:02Z", nil), record("turn_context", obj{"model": 4}, start), agent("2026-10-21T09:00:03Z", obj{}), record("turn_context", obj{}, start), agent("2026-10-21T09:00:04Z", obj{}))
	if got.Ref.Tool != session.ToolCodex || got.Command != nil || len(got.Turns) != 5 {
		t.Fatalf("session %+v", got)
	}
	if got.Turns[0].Tokens != (session.Tokens{Input: 11, Cached: 12, Output: 13, Reasoning: 14}) {
		t.Fatalf("tokens %+v", got.Turns[0])
	}
	for i, model := range []string{"", "first", "latest", "", ""} {
		turn := got.Turns[i]
		if turn.Whose != session.WhoseAgent || turn.Model != model || !turn.Moment.Equal(time.Date(2026, 10, 21, 9, 0, i, 0, time.UTC)) {
			t.Fatalf("turn %d %+v", i, turn)
		}
		if i > 0 && turn.Tokens != (session.Tokens{}) {
			t.Fatalf("nonzero absent usage: %+v", turn)
		}
	}
}

// R-2N1S-CZT5 R-2O9O-QRJU R-2PHL-4JAJ
func TestPersonToolAndIgnoredRecords(t *testing.T) {
	tool := func(kind string, moment string) obj {
		return record("response_item", obj{"type": kind, "model": "ignored", "usage": obj{"input_tokens": 4}}, moment)
	}
	got := single(t, metadata("thread"), person(start), tool("function_call_output", "2026-10-21T09:00:01Z"), tool("custom_tool_call_output", "2026-10-21T09:00:02Z"), record("response_item", obj{"type": "message", "role": "user"}, start), record("event_msg", obj{"type": "token_count"}, start), record("event_msg", obj{"type": "item_completed", "item": obj{"type": "AssistantMessage"}}, start), record("unknown", obj{}, start), record("turn_context", obj{"model": "ignored"}, start))
	if len(got.Turns) != 3 {
		t.Fatalf("turns %+v", got.Turns)
	}
	for i, whose := range []session.Whose{session.WhosePerson, session.WhoseTool, session.WhoseTool} {
		turn := got.Turns[i]
		if turn.Whose != whose || turn.Model != "" || turn.Tokens != (session.Tokens{}) || !turn.Moment.Equal(time.Date(2026, 10, 21, 9, 0, i, 0, time.UTC)) {
			t.Fatalf("turn %+v", turn)
		}
	}
}

// R-1BFT-MUPF
func TestResponseMergeUsesLastCountsFirstUsableMomentAndPosition(t *testing.T) {
	agentID := func(moment any, input int, id string) obj {
		value := agent(moment, obj{"input_tokens": input})
		value["payload"].(obj)["response_id"] = id
		return value
	}
	got := single(t, metadata("thread"), agentID("bad", 1, "response"), record("turn_context", obj{"model": "first"}, start), person(start), agentID(start, 2, "response"), record("response_item", obj{"type": "function_call_output"}, start), record("turn_context", obj{"model": "last"}, start), agentID("2026-10-21T09:00:05Z", 9, "response"), agentID(nil, 7, "never"), agentID("bad", 8, "never"))
	if len(got.Turns) != 3 {
		t.Fatalf("turns %+v", got.Turns)
	}
	if got.Turns[0].Whose != session.WhosePerson || got.Turns[1].Whose != session.WhoseAgent || got.Turns[2].Whose != session.WhoseTool {
		t.Fatalf("equal moment position %+v", got.Turns)
	}
	if got.Turns[1].Model != "last" || got.Turns[1].Tokens.Input != 9 || !got.Turns[1].Moment.Equal(got.Turns[0].Moment) {
		t.Fatalf("merged turn %+v", got.Turns[1])
	}
	got = single(t, metadata("thread"), agentID(start, 2, "response"), record("turn_context", obj{"model": "last"}, start), agentID("invalid", 9, "response"))
	if len(got.Turns) != 1 || got.Turns[0].Tokens.Input != 9 || got.Turns[0].Model != "last" {
		t.Fatalf("last response with unusable moment: %+v", got.Turns)
	}
}

// R-2KLZ-LGBR R-1900-VB81
func TestMissingResponseIDsAndThreadFilters(t *testing.T) {
	var values []obj
	values = append(values, metadata("thread"))
	for i, response := range []any{nil, 4, "", "same", "same"} {
		value := agent(time.Date(2026, 10, 21, 9, 0, i, 0, time.UTC).Format(time.RFC3339), obj{"input_tokens": i + 1})
		value["payload"].(obj)["response_id"] = response
		if i == 4 {
			value["payload"].(obj)["thread_id"] = "other"
		}
		values = append(values, value)
	}
	for i, thread := range []any{nil, 42, "thread"} {
		value := agent(time.Date(2026, 10, 21, 9, 1, i, 0, time.UTC).Format(time.RFC3339), obj{})
		value["payload"].(obj)["thread_id"] = thread
		values = append(values, value)
	}
	got := single(t, values...)
	if len(got.Turns) != 7 || got.Turns[3].Tokens.Input != 4 {
		t.Fatalf("turns %+v", got.Turns)
	}
}

// R-1A7X-92YQ
func TestHistoryUsesOrdinalAndExcludesMergeContributions(t *testing.T) {
	meta := metadata("thread")
	meta["payload"].(obj)["subagent_history_start_ordinal"] = 100
	oldContext := record("turn_context", obj{"model": "history"}, start)
	oldContext["ordinal"] = 3
	old := agent(start, obj{"input_tokens": 900})
	old["ordinal"] = 99
	old["payload"].(obj)["response_id"] = "same"
	boundary := agent("2026-10-21T09:00:01Z", obj{"input_tokens": 2})
	boundary["ordinal"] = 100
	boundary["payload"].(obj)["response_id"] = "same"
	oldLate := agent(start, obj{"input_tokens": 999})
	oldLate["ordinal"] = 5
	oldLate["payload"].(obj)["response_id"] = "same"
	oldPerson := person(start)
	oldPerson["ordinal"] = 10
	oldTool := record("response_item", obj{"type": "function_call_output"}, start)
	oldTool["ordinal"] = 11
	context := record("turn_context", obj{"model": "new"}, start)
	context["ordinal"] = 101
	got := single(t, meta, oldContext, old, boundary, oldLate, oldPerson, oldTool, context, agent("2026-10-21T09:00:02Z", nil))
	if len(got.Turns) != 2 || got.Turns[0].Tokens.Input != 2 || got.Turns[0].Model != "" || got.Turns[1].Model != "new" {
		t.Fatalf("turns %+v", got.Turns)
	}
	for _, threshold := range []any{"100", 1.5, nil} {
		meta["payload"].(obj)["subagent_history_start_ordinal"] = threshold
		if got := single(t, meta, old); len(got.Turns) != 1 {
			t.Fatalf("noninteger threshold %+v", got)
		}
	}
}

// R-1A7X-92YQ
func TestHistoryOrdinalsHaveNoInt64Bound(t *testing.T) {
	for _, test := range []struct {
		name      string
		threshold json.Number
		before    json.Number
	}{
		{"threshold above int64", "9223372036854775808", "1"},
		{"ordinal below int64", "0", "-9223372036854775809"},
	} {
		t.Run(test.name, func(t *testing.T) {
			meta := metadata("thread")
			meta["payload"].(obj)["subagent_history_start_ordinal"] = test.threshold
			history := agent(start, obj{"input_tokens": 99})
			history["ordinal"] = test.before
			context := record("turn_context", obj{"model": "history"}, start)
			context["ordinal"] = test.before
			boundary := agent("2026-10-21T09:00:01Z", obj{"input_tokens": 2})
			boundary["ordinal"] = test.threshold
			got := single(t, meta, context, history, boundary)
			if len(got.Turns) != 1 || got.Turns[0].Tokens.Input != 2 || got.Turns[0].Model != "" {
				t.Fatalf("unbounded ordinal history result %+v", got.Turns)
			}
		})
	}
}

// R-2RXD-W2RX R-EZUQ-QR1Y R-F12N-4ISN R-OX0X-GWCI
func TestMomentsAndStableOrdering(t *testing.T) {
	got := single(t, metadata("thread"), person("2026-10-21T11:00:00.0009+02:00"), record("response_item", obj{"type": "function_call_output"}, start), agent("2026-10-21T09:00:02Z", nil), person("1970-01-01T00:00:00Z"), person(nil), person(5), person("2026-10-21 09:00:00"), person("1969-12-31T23:59:59.9999Z"))
	if len(got.Turns) != 4 || got.Turns[0].Moment != time.Unix(0, 0).UTC() || got.Turns[1].Whose != session.WhosePerson || got.Turns[2].Whose != session.WhoseTool {
		t.Fatalf("turns %+v", got.Turns)
	}
	for _, turn := range got.Turns {
		if turn.Moment.Location() != time.UTC || turn.Moment.Nanosecond()%1_000_000 != 0 {
			t.Fatalf("moment %v", turn.Moment)
		}
	}
	root := t.TempDir()
	put(t, root, live("invalid-time"), lines(t, metadata("thread"), person("invalid")))
	if got := codex.Read(root, time.Time{}, nil); len(got.Sessions) != 0 || got.Skipped != 0 {
		t.Fatalf("all bad moments %+v", got)
	}
	for _, invalid := range []string{"2026-10-21T9:00:00Z", "2026-10-21T09:00:00,1Z", "2026-10-21T09:00:00+24:00", "2026-10-21T09:00:00+00:60"} {
		got := single(t, metadata("thread"), person(start), person(invalid))
		if len(got.Turns) != 1 {
			t.Fatalf("invalid RFC3339 timestamp %q returned %+v", invalid, got.Turns)
		}
	}
}

// R-OY8T-UO37 R-OZGQ-8FTW
func TestMomentWhoseMergeAndOverflow(t *testing.T) {
	first := agent("2026-10-21T09:00:00.0001Z", obj{"input_tokens": 1, "cached_input_tokens": 2, "output_tokens": 3, "reasoning_output_tokens": 4})
	last := agent("2026-10-21T09:00:00.0009Z", obj{"input_tokens": 5, "cached_input_tokens": 6, "output_tokens": 7, "reasoning_output_tokens": 8})
	got := single(t, metadata("thread"), record("turn_context", obj{"model": "first"}, start), first, person(start), record("turn_context", obj{"model": "last"}, start), last)
	if len(got.Turns) != 2 || got.Turns[0].Whose != session.WhoseAgent || got.Turns[1].Whose != session.WhosePerson || got.Turns[0].Model != "last" || got.Turns[0].Tokens != (session.Tokens{Input: 6, Cached: 8, Output: 10, Reasoning: 12}) {
		t.Fatalf("merged %+v", got)
	}
	for _, key := range []string{"input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens"} {
		wantSkipped(t, lines(t, metadata("thread"), agent(start, obj{key: int64(1_000_000_000_000)}), agent(start, obj{key: 1})))
	}
}

// R-OOVB-N5KE R-F3IF-W2A1
func TestCountValidation(t *testing.T) {
	for _, key := range []string{"input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens"} {
		for _, value := range []any{-1, 1.5, "4", nil, int64(1_000_000_000_001), json.Number("9223372036854775808")} {
			wantSkipped(t, lines(t, metadata("thread"), agent(start, obj{key: value})))
		}
		got := single(t, metadata("thread"), agent(start, obj{key: int64(1_000_000_000_000)}))
		counts := got.Turns[0].Tokens
		total := counts.Input + counts.Cached + counts.Output + counts.Reasoning
		if total != 1_000_000_000_000 {
			t.Fatalf("boundary counts %+v", counts)
		}
	}
}

// R-OQ38-0XB3 R-ORB4-EP1S R-OSJ0-SGSH R-FC1Q-KGGW
func TestLineRulesAndSkipIsolation(t *testing.T) {
	for _, bad := range []string{"{", "[]", `"text"`, "3", "true", "null", "{} {}"} {
		wantSkipped(t, append(lines(t, metadata("thread"), person(start)), []byte(bad+"\n")...))
	}
	for _, tail := range []string{"{", "[]", string(lines(t, person("2026-10-21T09:00:01Z"))[:len(lines(t, person("2026-10-21T09:00:01Z")))-1])} {
		root := t.TempDir()
		data := append([]byte("\n \t\r\n"), lines(t, metadata("thread"), person(start))...)
		put(t, root, live("line"), append(data, []byte(tail)...))
		got := codex.Read(root, time.Time{}, nil)
		if got.Skipped != 0 || len(got.Sessions) != 1 || len(got.Sessions[0].Turns) != 1 {
			t.Fatalf("tail %q result %+v", tail, got)
		}
	}
	root := t.TempDir()
	put(t, root, live("bad"), []byte("null\n[]\n{\n"))
	put(t, root, live("good"), lines(t, metadata("good"), person(start)))
	got := codex.Read(root, time.Time{}, nil)
	if got.Skipped != 1 || len(got.Sessions) != 1 || got.Sessions[0].Ref.ID != "good" {
		t.Fatalf("isolation %+v", got)
	}
}

// R-EOVN-ATDP R-EQ3J-OL4E R-OUYT-K09V
func TestEmptyAndMissingRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	if got := codex.Read(root, time.Time{}, nil); len(got.Sessions) != 0 || got.Skipped != 0 {
		t.Fatalf("missing %+v", got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("root created: %v", err)
	}
	root = t.TempDir()
	if got := codex.Read(root, time.Time{}, nil); len(got.Sessions) != 0 || got.Skipped != 0 {
		t.Fatalf("missing children %+v", got)
	}
	for _, name := range []string{"sessions", "archived_sessions"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("child created: %v", err)
		}
	}
	put(t, root, live("empty"), nil)
	put(t, root, live("meta"), lines(t, metadata("thread")))
	put(t, root, live("ignored"), lines(t, record("unknown", obj{}, start)))
	if got := codex.Read(root, time.Time{}, nil); len(got.Sessions) != 0 || got.Skipped != 0 {
		t.Fatalf("empty sources %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "archived_sessions")); !os.IsNotExist(err) {
		t.Fatalf("missing archive created: %v", err)
	}
}

// R-2T5A-9UIM R-2UD6-NM9B R-2VL3-1E00 R-FGXC-3JFO R-OW6P-XS0K
func TestDuplicatePrecedenceAndSorting(t *testing.T) {
	root := t.TempDir()
	data := func(id, directory string) []byte {
		meta := metadata(id)
		meta["payload"].(obj)["cwd"] = directory
		return lines(t, meta, person(start))
	}
	put(t, root, "archived_sessions/rollout-a.jsonl", data("same", "/archived"))
	put(t, root, live("b"), data("same", "/live-b"))
	put(t, root, live("a"), data("same", "/live-a"))
	put(t, root, live("a")+".zst", compress(t, data("same", "/compressed")))
	put(t, root, live("bad"), []byte("null\n"))
	put(t, root, "archived_sessions/rollout-fallback.jsonl", data("fallback", "/fallback"))
	put(t, root, live("fallback"), append(data("fallback", "/bad"), []byte("[]\n")...))
	put(t, root, "archived_sessions/rollout-z.jsonl", data("Z", "/z"))
	put(t, root, "archived_sessions/rollout-z-other.jsonl", data("Z", "/other"))
	got := codex.Read(root, time.Time{}, nil)
	if got.Skipped != 2 || len(got.Sessions) != 3 {
		t.Fatalf("result %+v", got)
	}
	for i, want := range []struct{ id, dir string }{{"Z", "/other"}, {"fallback", "/fallback"}, {"same", "/live-a"}} {
		if got.Sessions[i].Ref.ID != want.id || got.Sessions[i].Directory != want.dir {
			t.Fatalf("session %d %+v", i, got.Sessions[i])
		}
	}
}

// R-FATU-6OQ7
func TestUnreadableSource(t *testing.T) {
	root := t.TempDir()
	path := put(t, root, live("unreadable"), lines(t, metadata("thread"), person(start)))
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Error(err)
		}
	})
	if got := codex.Read(root, time.Time{}, nil); got.Skipped != 1 || len(got.Sessions) != 0 {
		t.Fatalf("unreadable result %+v", got)
	}
}

// R-FFPF-PROZ
func TestReadLeavesTreeUnchanged(t *testing.T) {
	root := t.TempDir()
	put(t, root, live("good"), lines(t, metadata("thread"), person(start)))
	put(t, root, "archived_sessions/rollout-bad.jsonl.zst", []byte("bad"))
	put(t, root, "other/ignored", []byte("preserve"))
	directory, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	})
	type snapshot struct {
		Mode     fs.FileMode
		Modified time.Time
		Contents string
	}
	take := func() map[string]snapshot {
		result := make(map[string]snapshot)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			value := snapshot{Mode: info.Mode(), Modified: info.ModTime()}
			if !entry.IsDir() {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				data, err := directory.ReadFile(relative)
				if err != nil {
					return err
				}
				value.Contents = string(data)
			}
			result[path] = value
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := take()
	_ = codex.Read(root, time.Time{}, nil)
	after := take()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("tree changed: before=%+v after=%+v", before, after)
	}
}
