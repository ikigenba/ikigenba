package grok_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/grok"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

const encodedCWD = "%2Fwork%2Fshop"

func write(t *testing.T, name, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func source(t *testing.T, root, encoded, name, summary, updates string) string {
	t.Helper()
	path := filepath.Join(root, "sessions", encoded, name)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if summary != "" {
		write(t, filepath.Join(path, "summary.json"), summary)
	}
	write(t, filepath.Join(path, "updates.jsonl"), updates)
	return path
}

func line(moment int64, update string) string {
	return fmt.Sprintf(`{"timestamp":%d,"params":{"update":%s}}`+"\n", moment, update)
}

func person(moment int64) string {
	return line(moment, `{"sessionUpdate":"user_message_chunk"}`)
}

func one(t *testing.T, root string) session.Session {
	t.Helper()
	found := grok.Read(root, time.Time{}, nil)
	if found.Skipped != 0 || len(found.Sessions) != 1 {
		t.Fatalf("Read = %#v, want one session and no skips", found)
	}
	return found.Sessions[0]
}

func chmod(t *testing.T, path string, mode os.FileMode, restore os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, restore); err != nil {
			t.Error(err)
		}
	})
}

// R-9MAQ-3ONO R-SNSO-WMVL R-SP0L-AEMA R-EOVN-ATDP R-ERBG-2CV3
// R-ESJC-G4LS R-F2AJ-IAJC R-FKL1-8UNR
func TestReadSessionDepthAndOnlyNamedFiles(t *testing.T) {
	root := t.TempDir()
	path := source(t, root, encodedCWD, "real", `{}`, person(1))
	write(t, filepath.Join(root, "sessions", "regular-group"), "not a directory")
	write(t, filepath.Join(root, "sessions", encodedCWD, "regular-session"), "not a directory")
	write(t, filepath.Join(path, "deep", "summary.json"), `{}`)
	write(t, filepath.Join(path, "deep", "updates.jsonl"), person(2))
	for _, name := range []string{"chat_history.jsonl", "events.jsonl", "prompt_context.json", "other"} {
		write(t, filepath.Join(path, name), "malformed and content that must not be retained\n")
	}
	reader := struct {
		Read func(string, time.Time, []string) session.Found
	}{Read: grok.Read}
	found := reader.Read(root, time.Time{}, nil)
	if found.Skipped != 0 || len(found.Sessions) != 1 {
		t.Fatalf("Read = %#v, want one session and no skips", found)
	}
	got := found.Sessions[0]
	want := session.Session{Ref: session.Ref{Tool: session.ToolGrok, ID: "real"}, Directory: "/work/shop", Turns: []session.Turn{{Moment: time.Unix(1, 0).UTC(), Whose: session.WhosePerson}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("session = %#v, want %#v", got, want)
	}
}

// R-6EKQ-8U1C R-6FSM-MLS1 R-FATU-6OQ7 R-FC1Q-KGGW
func TestUnreadableDirectoriesAndFiles(t *testing.T) {
	for _, target := range []string{"sessions", "group", "source", "summary.json", "updates.jsonl"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			bad := source(t, root, "%2Fbad", "bad", `{}`, person(1))
			source(t, root, encodedCWD, "good", `{}`, person(2))
			path := bad
			restore := os.FileMode(0o700)
			switch target {
			case "sessions":
				path = filepath.Join(root, "sessions")
			case "group":
				path = filepath.Dir(bad)
			case "summary.json", "updates.jsonl":
				path = filepath.Join(bad, target)
				restore = 0o600
			}
			chmod(t, path, 0, restore)
			got := grok.Read(root, time.Time{}, nil)
			wantCount := 1
			if target == "sessions" {
				wantCount = 0
			}
			if got.Skipped != 1 || len(got.Sessions) != wantCount || (wantCount == 1 && got.Sessions[0].Ref.ID != "good") {
				t.Fatalf("Read = %#v", got)
			}
		})
	}
}

// R-SRGE-1Y3O R-EQ3J-OL4E
func TestMissingSummaryAndEmptySources(t *testing.T) {
	for _, test := range []struct {
		name, summary, updates string
		missingUpdates         bool
		skipped                int
	}{
		{name: "turn", updates: person(1), skipped: 1},
		{name: "empty"},
		{name: "no-files", missingUpdates: true},
		{name: "ignored", updates: line(1, `{"sessionUpdate":"agent_message_chunk"}`)},
		{name: "summary-empty-updates", summary: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := source(t, root, encodedCWD, "session", test.summary, test.updates)
			if test.missingUpdates {
				if err := os.Remove(filepath.Join(path, "updates.jsonl")); err != nil {
					t.Fatal(err)
				}
			}
			got := grok.Read(root, time.Time{}, nil)
			if len(got.Sessions) != 0 || got.Skipped != test.skipped {
				t.Fatalf("Read = %#v, want skip %d", got, test.skipped)
			}
		})
	}
}

// R-SSOA-FPUD R-STW6-THL2 R-SV43-79BR R-SWBZ-L12G R-EW71-LFTV R-F9LX-SWZI
func TestIdentityAndDirectoryFallbacks(t *testing.T) {
	for _, test := range []struct {
		name, encoded, summary, id, directory string
		skipped                               int
	}{
		{name: "explicit", encoded: "invalid%", summary: `{"info":{"id":"other","cwd":"/work/./x/../shop/"}}`, id: "other", directory: "/work/shop"},
		{name: "no-info", summary: `{}`},
		{name: "non-object-info", summary: `{"info":42}`},
		{name: "no-members", summary: `{"info":{}}`},
		{name: "wrong-types", summary: `{"info":{"id":42,"cwd":false}}`},
		{name: "empty-values", summary: `{"info":{"id":"","cwd":""}}`},
		{name: "relative-cwd", summary: `{"info":{"cwd":"relative/path"}}`, skipped: 1},
		{name: "invalid-percent", encoded: "bad%2", summary: `{}`, skipped: 1},
		{name: "relative-encoded", encoded: "relative%2Fpath", summary: `{}`, skipped: 1},
		{name: "encoded-clean", encoded: "%2Fwork%2F.%2Fother%2F..%2Fshop%2F", summary: `{}`},
		{name: "plus-is-literal", encoded: "%2Fwork%2Fa+b", summary: `{}`, directory: "/work/a+b"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			encoded := test.encoded
			if encoded == "" {
				encoded = encodedCWD
			}
			source(t, root, encoded, "from-directory", test.summary, person(1))
			got := grok.Read(root, time.Time{}, nil)
			if got.Skipped != test.skipped {
				t.Fatalf("Read = %#v", got)
			}
			if test.skipped != 0 {
				if len(got.Sessions) != 0 {
					t.Fatal("invalid directory yielded session")
				}
				return
			}
			if len(got.Sessions) != 1 {
				t.Fatalf("Read = %#v", got)
			}
			id, directory := test.id, test.directory
			if id == "" {
				id = "from-directory"
			}
			if directory == "" {
				directory = "/work/shop"
			}
			if got.Sessions[0].Ref.ID != id || got.Sessions[0].Directory != directory {
				t.Fatalf("session = %#v", got.Sessions[0])
			}
		})
	}
}

// R-SXJV-YST5
func TestSessionKinds(t *testing.T) {
	for _, raw := range []string{`"headless"`, `"subagent"`, `"subagent_resume"`, `"interactive"`, `"other"`, `null`, `3`, `true`, `{}`} {
		t.Run(raw, func(t *testing.T) {
			root := t.TempDir()
			source(t, root, encodedCWD, "child", `{"session_kind":`+raw+`}`, person(1))
			want := raw == `"headless"` || raw == `"subagent"` || raw == `"subagent_resume"`
			if got := one(t, root); got.Headless != want {
				t.Fatalf("Headless = %v, want %v", got.Headless, want)
			}
		})
	}
	root := t.TempDir()
	source(t, root, encodedCWD, "child", `{}`, person(1))
	if one(t, root).Headless {
		t.Fatal("missing kind treated as headless")
	}
}

// R-SYRS-CKJU R-6I8F-E59F R-T17L-4418 R-6H0J-0DIQ R-ETR8-TWCH R-EUZ5-7O36
func TestParents(t *testing.T) {
	for _, test := range []struct {
		name, summary, want string
		children            bool
		locked              bool
	}{
		{name: "explicit-wins", summary: `{"parent_session_id":"explicit"}`, children: true, want: "explicit"},
		{name: "lexical-parent", summary: `{}`, children: true, want: "a-parent"},
		{name: "empty-infers", summary: `{"parent_session_id":""}`, children: true, want: "a-parent"},
		{name: "wrong-type-infers", summary: `{"parent_session_id":1}`, children: true, want: "a-parent"},
		{name: "none", summary: `{}`},
		{name: "empty", summary: `{"parent_session_id":""}`},
		{name: "self", summary: `{"parent_session_id":"child"}`, children: true},
		{name: "unreadable-subagents", summary: `{}`, children: true, locked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source(t, root, encodedCWD, "child", test.summary, person(1))
			if test.children {
				for _, parent := range []string{"z-parent", "a-parent", "child"} {
					path := filepath.Join(root, "sessions", "%2Fother", parent, "subagents", "child")
					if err := os.MkdirAll(path, 0o700); err != nil {
						t.Fatal(err)
					}
					if test.locked {
						chmod(t, filepath.Dir(path), 0, 0o700)
					}
				}
			}
			got := one(t, root)
			if test.want == "" {
				if got.Parent != nil {
					t.Fatalf("Parent = %v, want nil", got.Parent)
				}
			} else if got.Parent == nil || *got.Parent != (session.Ref{Tool: session.ToolGrok, ID: test.want}) {
				t.Fatalf("Parent = %v, want %s", got.Parent, test.want)
			}
		})
	}
}

// R-T3ND-VNIM R-T4VA-9F9B R-T636-N700 R-T7B3-0YQP R-T8IZ-EQHE R-T9QV-SI83
// R-F3IF-W2A1
func TestTurnMappingAndUsage(t *testing.T) {
	root := t.TempDir()
	updates := `{"method":"unrelated","timestamp":1,"params":{"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":11,"cachedReadTokens":2,"outputTokens":3,"reasoningTokens":4,"cacheCreationTokens":"ignored","totalTokens":false,"modelUsage":{"grok-x-build":{}}}}}}` + "\n"
	updates += line(2, `{"sessionUpdate":"user_message_chunk","usage":{"inputTokens":-1},"text":"private"}`)
	updates += line(3, `{"sessionUpdate":"tool_call_update","status":"completed","usage":{"inputTokens":-1}}`)
	updates += line(4, `{"sessionUpdate":"tool_call_update","status":"failed"}`)
	for _, update := range []string{`{}`, `{"sessionUpdate":2}`, `{"sessionUpdate":"tool_call"}`, `{"sessionUpdate":"agent_message_chunk"}`, `{"sessionUpdate":"subagent_spawned"}`, `{"sessionUpdate":"tool_call_update"}`, `{"sessionUpdate":"tool_call_update","status":"running"}`} {
		updates += line(9, update)
	}
	source(t, root, encodedCWD, "session", `{}`, updates)
	got := one(t, root).Turns
	want := []session.Turn{
		{Moment: time.Unix(1, 0).UTC(), Whose: session.WhoseAgent, Model: "grok-x", Tokens: session.Tokens{Input: 11, Cached: 2, Output: 3, Reasoning: 4}},
		{Moment: time.Unix(2, 0).UTC(), Whose: session.WhosePerson},
		{Moment: time.Unix(3, 0).UTC(), Whose: session.WhoseTool},
		{Moment: time.Unix(4, 0).UTC(), Whose: session.WhoseTool},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Turns = %#v, want %#v", got, want)
	}
	for _, test := range []struct {
		usage, model string
		tokens       session.Tokens
	}{
		{usage: `null`}, {usage: `42`}, {usage: `{}`},
		{usage: `{"inputTokens":1000000000000}`, tokens: session.Tokens{Input: 1_000_000_000_000}},
		{usage: `{"modelUsage":{}}`}, {usage: `{"modelUsage":3}`},
		{usage: `{"modelUsage":{"a":1,"b":2}}`},
		{usage: `{"modelUsage":{"grok-x-build-build":null}}`, model: "grok-x-build"},
		{usage: `{"modelUsage":{"grok-x":{}}}`, model: "grok-x"},
	} {
		home := t.TempDir()
		source(t, home, encodedCWD, "session", `{}`, line(1, `{"sessionUpdate":"turn_completed","usage":`+test.usage+`}`))
		turn := one(t, home).Turns[0]
		if turn.Model != test.model || turn.Tokens != test.tokens {
			t.Fatalf("usage %s yielded %#v", test.usage, turn)
		}
	}
	home := t.TempDir()
	source(t, home, encodedCWD, "session", `{}`, line(1, `{"sessionUpdate":"turn_completed"}`))
	if got := one(t, home).Turns[0]; got.Model != "" || got.Tokens != (session.Tokens{}) {
		t.Fatalf("absent usage yielded %#v", got)
	}
}

// R-6JGB-RX04 R-6KO8-5OQT R-EZUQ-QR1Y R-F12N-4ISN
func TestMomentFormRangeAndFallback(t *testing.T) {
	for _, test := range []struct {
		meta, seconds string
		want          time.Time
	}{
		{meta: "1767225600123", seconds: "1", want: time.UnixMilli(1767225600123).UTC()},
		{meta: "0", seconds: "1", want: time.Unix(0, 0).UTC()},
		{meta: "253402300799999", seconds: "1", want: time.UnixMilli(253402300799999).UTC()},
		{meta: "253402300800000", seconds: "253402300799", want: time.Unix(253402300799, 0).UTC()},
		{meta: "-1", seconds: "0", want: time.Unix(0, 0).UTC()},
		{meta: "null", seconds: "1767225600", want: time.Unix(1767225600, 0).UTC()},
		{meta: "1.5", seconds: "2", want: time.Unix(2, 0).UTC()},
		{meta: "1e3", seconds: "2", want: time.Unix(2, 0).UTC()},
		{meta: `"123"`, seconds: "2", want: time.Unix(2, 0).UTC()},
	} {
		root := t.TempDir()
		updates := fmt.Sprintf(`{"timestamp":%s,"params":{"_meta":{"agentTimestampMs":%s},"update":{"sessionUpdate":"user_message_chunk"}}}`+"\n", test.seconds, test.meta)
		source(t, root, encodedCWD, "session", `{}`, updates)
		got := one(t, root).Turns[0].Moment
		if got != test.want || got.Location() != time.UTC || got.Nanosecond()%int(time.Millisecond) != 0 {
			t.Fatalf("Moment = %v, want %v", got, test.want)
		}
	}
	for _, bad := range []string{"null", "1.5", "1e3", `"2026-01-01T00:00:00Z"`, `"123"`, "true", "[]", "{}", "-1", "253402300800000", "9223372036854775808"} {
		for _, keepGood := range []bool{true, false} {
			root := t.TempDir()
			updates := fmt.Sprintf(`{"timestamp":%s,"params":{"_meta":{"agentTimestampMs":%s},"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":-1}}}}`+"\n", bad, bad)
			updates += `{"params":{"update":{"sessionUpdate":"user_message_chunk"}}}` + "\n"
			if keepGood {
				updates += person(1)
			}
			source(t, root, encodedCWD, "session", `{}`, updates)
			got := grok.Read(root, time.Time{}, nil)
			wantCount := 0
			if keepGood {
				wantCount = 1
			}
			if got.Skipped != 0 || len(got.Sessions) != wantCount || (keepGood && len(got.Sessions[0].Turns) != 1) {
				t.Fatalf("bad moment %s: %#v", bad, got)
			}
		}
	}
}

// R-TDEK-XTG6 R-OX0X-GWCI R-OY8T-UO37
func TestStableOrderAndMerge(t *testing.T) {
	root := t.TempDir()
	updates := line(3, `{"sessionUpdate":"turn_completed","usage":{"inputTokens":1,"cachedReadTokens":2,"outputTokens":3,"reasoningTokens":4,"modelUsage":{"first":{}}}}`)
	updates += person(1)
	updates += line(3, `{"sessionUpdate":"tool_call_update","status":"completed"}`)
	updates += person(3)
	updates += line(3, `{"sessionUpdate":"turn_completed","usage":{"inputTokens":5,"cachedReadTokens":6,"outputTokens":7,"reasoningTokens":8,"modelUsage":{"last":{}}}}`)
	updates += person(3)
	source(t, root, encodedCWD, "session", `{}`, updates)
	got := one(t, root).Turns
	want := []session.Turn{
		{Moment: time.Unix(1, 0).UTC(), Whose: session.WhosePerson},
		{Moment: time.Unix(3, 0).UTC(), Whose: session.WhoseAgent, Model: "last", Tokens: session.Tokens{Input: 6, Cached: 8, Output: 10, Reasoning: 12}},
		{Moment: time.Unix(3, 0).UTC(), Whose: session.WhoseTool},
		{Moment: time.Unix(3, 0).UTC(), Whose: session.WhosePerson},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Turns = %#v, want %#v", got, want)
	}
}

// R-OOVB-N5KE R-OZGQ-8FTW R-FC1Q-KGGW
func TestInvalidAndMergedCountsSkipWholeSourceOnce(t *testing.T) {
	for _, key := range []string{"inputTokens", "cachedReadTokens", "outputTokens", "reasoningTokens"} {
		for _, value := range []string{"-1", "1000000000001", "9223372036854775808", "1.5", "1e3", `"2"`, "null", "true", "{}"} {
			root := t.TempDir()
			bad := line(2, `{"sessionUpdate":"turn_completed","usage":{"`+key+`":`+value+`}}`)
			source(t, root, encodedCWD, "bad", `{}`, person(1)+bad+bad)
			source(t, root, encodedCWD, "good", `{}`, person(1))
			got := grok.Read(root, time.Time{}, nil)
			if got.Skipped != 1 || len(got.Sessions) != 1 || got.Sessions[0].Ref.ID != "good" {
				t.Fatalf("%s=%s: %#v", key, value, got)
			}
		}
		root := t.TempDir()
		first := line(1, `{"sessionUpdate":"turn_completed","usage":{"`+key+`":1000000000000}}`)
		second := line(1, `{"sessionUpdate":"turn_completed","usage":{"`+key+`":1}}`)
		source(t, root, encodedCWD, "bad", `{}`, first+second)
		if got := grok.Read(root, time.Time{}, nil); got.Skipped != 1 || len(got.Sessions) != 0 {
			t.Fatalf("merged %s overflow: %#v", key, got)
		}
	}
}

// R-OQ38-0XB3 R-ORB4-EP1S R-OSJ0-SGSH R-OTQX-68J6 R-FC1Q-KGGW
func TestJSONDocumentAndLineRules(t *testing.T) {
	for _, malformed := range []string{"not-json", "[]", `"string"`, "1", "true", "null", `{} {}`, `{"bad":`} {
		t.Run(malformed, func(t *testing.T) {
			root := t.TempDir()
			source(t, root, encodedCWD, "complete-bad", `{}`, person(1)+malformed+"\n"+malformed+"\n")
			source(t, root, encodedCWD, "unterminated", `{}`, person(1)+" \t \r\n\n"+malformed)
			source(t, root, encodedCWD, "whole-bad", malformed, person(1))
			got := grok.Read(root, time.Time{}, nil)
			if got.Skipped != 2 || len(got.Sessions) != 1 || got.Sessions[0].Ref.ID != "unterminated" || len(got.Sessions[0].Turns) != 1 {
				t.Fatalf("Read = %#v", got)
			}
		})
	}
	root := t.TempDir()
	source(t, root, encodedCWD, "session", " \n {} \t", person(1)+strings.TrimSuffix(person(2), "\n"))
	if turns := one(t, root).Turns; len(turns) != 1 || turns[0].Moment != time.Unix(1, 0).UTC() {
		t.Fatalf("unterminated valid line retained: %#v", turns)
	}
}

// R-FGXC-3JFO R-OW6P-XS0K R-PEDZ-5S83
func TestSessionsOrderedAndDuplicateIDChoosesLeastPath(t *testing.T) {
	root := t.TempDir()
	source(t, root, "%2Fz", "earlier-name", `{"info":{"id":"shared"}}`, person(20))
	source(t, root, "%2Fa", "z-name", `{"info":{"id":"shared"}}`, person(10))
	source(t, root, "%2Fa", "a-name", `{"info":{"id":"shared"}}`, person(5))
	source(t, root, encodedCWD, "last", `{}`, person(1))
	source(t, root, encodedCWD, "First", `{}`, person(2))
	got := grok.Read(root, time.Time{}, nil)
	if got.Skipped != 0 || len(got.Sessions) != 3 {
		t.Fatalf("Read = %#v", got)
	}
	if got.Sessions[0].Ref.ID != "First" || got.Sessions[1].Ref.ID != "last" || got.Sessions[2].Ref.ID != "shared" || got.Sessions[2].Turns[0].Moment != time.Unix(5, 0).UTC() {
		t.Fatalf("wrong order or duplicate choice: %#v", got.Sessions)
	}
}

// R-OUYT-K09V
func TestMissingRootsDoNotCreateAnything(t *testing.T) {
	root := t.TempDir()
	missingRoot := filepath.Join(root, "missing")
	for _, home := range []string{root, missingRoot} {
		got := grok.Read(home, time.Time{}, nil)
		if got.Skipped != 0 || len(got.Sessions) != 0 {
			t.Fatalf("Read(%q) = %#v", home, got)
		}
	}
	for _, name := range []string{missingRoot, filepath.Join(root, "sessions")} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("reader created %s: %v", name, err)
		}
	}
}

type fileState struct {
	Mode    os.FileMode
	ModTime time.Time
	Data    string
}

func snapshot(t *testing.T, root string) map[string]fileState {
	t.Helper()
	directory, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := directory.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	states := make(map[string]fileState)
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		state := fileState{Mode: info.Mode(), ModTime: info.ModTime()}
		if info.Mode().IsRegular() {
			name, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			data, readErr := directory.ReadFile(name)
			if readErr != nil {
				return readErr
			}
			state.Data = string(data)
		}
		states[path] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return states
}

// R-FFPF-PROZ
func TestReadLeavesAllSourceBytesAndMetadataUnchanged(t *testing.T) {
	root := t.TempDir()
	source(t, root, encodedCWD, "valid", `{}`, person(1))
	source(t, root, encodedCWD, "broken", `{}`, "not JSON\n")
	write(t, filepath.Join(root, "sessions", encodedCWD, "valid", "other"), "private bytes")
	before := snapshot(t, root)
	got := grok.Read(root, time.Time{}, nil)
	if got.Skipped != 1 || len(got.Sessions) != 1 {
		t.Fatalf("Read = %#v", got)
	}
	if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("reader changed files: before %#v, after %#v", before, after)
	}
}
