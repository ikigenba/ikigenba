package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSavepointLifecycleWritesExactlyOneRecordOnSuccess(t *testing.T) {
	// R-86CB-5Z1H
	var output bytes.Buffer
	log := NewLog(&output, func() time.Time { return time.Time{} }, "")
	conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{Log: log})

	sp, err := conversation.Savepoint()
	if err != nil {
		t.Fatalf("Savepoint() error = %v, want nil", err)
	}
	if err := conversation.Restore(sp); err != nil {
		t.Fatalf("Restore() error = %v, want nil", err)
	}
	if err := conversation.Release(sp); err != nil {
		t.Fatalf("Release() error = %v, want nil", err)
	}

	records := decodeLogRecords(t, output.Bytes())
	want := []RecordType{RecordSavepoint, RecordRestore, RecordRelease}
	if len(records) != len(want) {
		t.Fatalf("lifecycle record count = %d, want exactly %d: %#v", len(records), len(want), records)
	}
	for index, record := range records {
		if record.Type != want[index] {
			t.Errorf("record %d type = %q, want %q", index, record.Type, want[index])
		}
		if record.Type == RecordTurnStart || record.Type == RecordTurnEnd {
			t.Errorf("lifecycle record %d unexpectedly introduced a turn boundary: %#v", index, record)
		}
	}
}

func TestSavepointLifecycleWritesNothingOnError(t *testing.T) {
	// R-86CB-5Z1H
	tests := []struct {
		name    string
		prepare func(*Conversation, *bytes.Buffer) func() error
		wantErr error
	}{
		{
			name: "savepoint while turn in flight",
			prepare: func(conversation *Conversation, _ *bytes.Buffer) func() error {
				conversation.state = conversationInFlight
				return func() error { _, err := conversation.Savepoint(); return err }
			},
			wantErr: ErrTurnInFlight,
		},
		{
			name: "savepoint while closed",
			prepare: func(conversation *Conversation, _ *bytes.Buffer) func() error {
				conversation.state = conversationClosed
				return func() error { _, err := conversation.Savepoint(); return err }
			},
			wantErr: ErrClosed,
		},
		{
			name: "savepoint while already active",
			prepare: func(conversation *Conversation, output *bytes.Buffer) func() error {
				if _, err := conversation.Savepoint(); err != nil {
					t.Fatalf("setup Savepoint() error = %v", err)
				}
				output.Reset()
				return func() error { _, err := conversation.Savepoint(); return err }
			},
			wantErr: ErrSavepointActive,
		},
		{
			name: "restore while turn in flight",
			prepare: func(conversation *Conversation, _ *bytes.Buffer) func() error {
				conversation.state = conversationInFlight
				return func() error { return conversation.Restore(Savepoint{}) }
			},
			wantErr: ErrTurnInFlight,
		},
		{
			name: "restore while closed",
			prepare: func(conversation *Conversation, _ *bytes.Buffer) func() error {
				conversation.state = conversationClosed
				return func() error { return conversation.Restore(Savepoint{}) }
			},
			wantErr: ErrClosed,
		},
		{
			name: "restore with invalid handle",
			prepare: func(conversation *Conversation, _ *bytes.Buffer) func() error {
				return func() error { return conversation.Restore(Savepoint{}) }
			},
			wantErr: ErrInvalidArgument,
		},
		{
			name: "release while turn in flight",
			prepare: func(conversation *Conversation, _ *bytes.Buffer) func() error {
				conversation.state = conversationInFlight
				return func() error { return conversation.Release(Savepoint{}) }
			},
			wantErr: ErrTurnInFlight,
		},
		{
			name: "release while closed",
			prepare: func(conversation *Conversation, _ *bytes.Buffer) func() error {
				conversation.state = conversationClosed
				return func() error { return conversation.Release(Savepoint{}) }
			},
			wantErr: ErrClosed,
		},
		{
			name: "release with invalid handle",
			prepare: func(conversation *Conversation, _ *bytes.Buffer) func() error {
				return func() error { return conversation.Release(Savepoint{}) }
			},
			wantErr: ErrInvalidArgument,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			log := NewLog(&output, func() time.Time { return time.Time{} }, "")
			conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{Log: log})
			invoke := test.prepare(conversation, &output)

			if err := invoke(); !errors.Is(err, test.wantErr) {
				t.Fatalf("operation error = %v, want %v", err, test.wantErr)
			}
			if output.Len() != 0 {
				records := decodeLogRecords(t, output.Bytes())
				for _, record := range records {
					if record.Type == RecordSavepoint || record.Type == RecordRestore || record.Type == RecordRelease {
						t.Fatalf("failed operation wrote lifecycle record %#v", record)
					}
				}
				t.Fatalf("failed operation wrote unexpected records: %#v", records)
			}
		})
	}
}

func TestReplayAcrossFailedTurnAndRestoreReconstructsHistory(t *testing.T) {
	// R-6N6M-6P9I
	committed := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "committed"}}}
	temporary := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "temporary"}}}
	provider := &phase15Provider{
		model:        "model",
		responses:    [][]Event{{MessageDone{Message: committed}}, nil, {MessageDone{Message: temporary}}},
		decodeErrors: []error{nil, errors.New("discard this turn")},
	}
	var output bytes.Buffer
	conversation := newConversation(provider, successfulPhase15Client(new(int)), Config{
		Log: NewLog(&output, func() time.Time { return time.Time{} }, ""),
	})

	first := conversation.Send(context.Background(), Text{Text: "keep"})
	drainStream(first)
	if first.Err() != nil {
		t.Fatalf("committed Send error = %v", first.Err())
	}
	failed := conversation.Send(context.Background(), Text{Text: "failed"})
	drainStream(failed)
	if failed.Err() == nil {
		t.Fatal("failed Send error = nil, want terminal error")
	}
	sp, err := conversation.Savepoint()
	if err != nil {
		t.Fatalf("Savepoint() error = %v", err)
	}
	temporaryTurn := conversation.Send(context.Background(), Text{Text: "remove"})
	drainStream(temporaryTurn)
	if temporaryTurn.Err() != nil {
		t.Fatalf("temporary Send error = %v", temporaryTurn.Err())
	}
	if err := conversation.Restore(sp); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if err := conversation.Release(sp); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	replay := func(records []LogRecord) History {
		records = append([]LogRecord(nil), records...)
		sort.Slice(records, func(i, j int) bool { return records[i].Seq < records[j].Seq })
		var history History
		var turn History
		inTurn := false
		discardTurn := false
		savepointLength := -1
		for _, record := range records {
			switch record.Type {
			case RecordTurnStart:
				inTurn = true
				discardTurn = false
				turn = nil
			case RecordMessage:
				if inTurn {
					turn = append(turn, *record.Message)
				} else {
					history = append(history, *record.Message)
				}
			case RecordError, RecordLimit:
				if inTurn {
					discardTurn = true
				}
			case RecordTurnEnd:
				if !discardTurn {
					history = append(history, turn...)
				}
				inTurn = false
				turn = nil
			case RecordSavepoint:
				savepointLength = len(history)
			case RecordRestore:
				if savepointLength >= 0 {
					history = history[:savepointLength]
				}
			case RecordRelease:
				savepointLength = -1
			}
		}
		return history
	}

	records := decodeLogRecords(t, output.Bytes())
	counts := make(map[RecordType]int)
	for _, record := range records {
		counts[record.Type]++
	}
	if counts[RecordTurnStart] != 3 || counts[RecordTurnEnd] != 3 || counts[RecordError] != 1 ||
		counts[RecordSavepoint] != 1 || counts[RecordRestore] != 1 || counts[RecordRelease] != 1 {
		t.Fatalf("scenario did not exercise every replay marker: counts=%#v records=%#v", counts, records)
	}
	if got := replay(records); !reflect.DeepEqual(got, conversation.history) {
		t.Fatalf("replayed History = %#v, want live History %#v", got, conversation.history)
	}
}

func TestLogRecordsUseInjectedTimePerTurnSequenceAndFullIdentity(t *testing.T) {
	// R-TA2N-M36A
	// R-5JH4-YC62
	var output bytes.Buffer
	times := []time.Time{
		time.Date(2031, 2, 3, 4, 5, 6, 7, time.UTC),
		time.Date(2031, 2, 3, 4, 5, 7, 8, time.UTC),
		time.Date(2031, 2, 3, 4, 5, 8, 9, time.UTC),
		time.Date(2031, 2, 3, 4, 5, 9, 10, time.UTC),
		time.Date(2031, 2, 3, 4, 5, 10, 11, time.UTC),
		time.Date(2031, 2, 3, 4, 5, 11, 12, time.UTC),
		time.Date(2031, 2, 3, 4, 5, 12, 13, time.UTC),
	}
	clockCall := 0
	log := NewLog(&output, func() time.Time {
		value := times[clockCall]
		clockCall++
		return value
	}, "")
	identity := Identity{Endpoint: "https://api.example/v1", AuthMode: "oauth", Model: "model-a"}
	log.start(identity)
	log.record(eventRecord{kind: eventRecordMessage, value: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "one"}}}})
	log.usage(Usage{}, 0)
	log.finish()
	log.start(identity)
	log.usage(Usage{}, 0)
	log.finish()

	records := decodeLogRecords(t, output.Bytes())
	if len(records) != 7 {
		t.Fatalf("record count = %d, want exactly 7", len(records))
	}
	wantSeq := []int{0, 1, 2, 3, 4, 5, 6}
	for index := range records {
		if records[index].Time != times[index] || records[index].Seq != wantSeq[index] {
			t.Errorf("record %d time/seq = %s/%d, want %s/%d", index, records[index].Time, records[index].Seq, times[index], wantSeq[index])
		}
	}
	for _, index := range []int{0, 4} {
		if records[index].Type != RecordTurnStart || records[index].Identity == nil || *records[index].Identity != identity {
			t.Fatalf("turn_start %d = %#v, want full identity %#v", index, records[index], identity)
		}
	}
}

func TestNilLogIsSilentSafeAndRecordsCanonicalPayloads(t *testing.T) {
	// R-5KP1-C3WR
	var nilLog *Log
	nilLog.start(Identity{})
	nilLog.record(eventRecord{})
	nilLog.usage(Usage{}, 0)
	nilLog.finish()
	if err := nilLog.Close(); err != nil {
		t.Fatalf("nil receiver Close() = %v", err)
	}
	clockCalls := 0
	log := NewLog(nil, func() time.Time { clockCalls++; return time.Time{} }, "")
	log.start(Identity{})
	log.record(eventRecord{kind: eventRecordToolUse, value: ToolUse{ID: "call", Name: "tool"}})
	log.usage(Usage{}, 0)
	log.finish()
	if err := log.Close(); err != nil || clockCalls != 0 {
		t.Fatalf("nil-writer log Close/clock = %v/%d, want nil/0", err, clockCalls)
	}

	var output bytes.Buffer
	log = NewLog(&output, func() time.Time { return time.Time{} }, "")
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "canonical"}}}
	log.start(Identity{})
	log.record(eventRecord{kind: eventRecordMessage, value: message})
	record := decodeLogRecords(t, output.Bytes())[1]
	if record.Message == nil || !reflect.DeepEqual(*record.Message, message) {
		t.Fatalf("decoded canonical Message = %#v, want %#v", record.Message, message)
	}
	if record.Identity != nil || record.ToolUse != nil || record.ToolResult != nil || record.Usage != nil || record.Cost != nil || record.Err != nil || record.Retry != nil {
		t.Fatalf("message record contains unrelated payloads: %#v", record)
	}
}

// R-TEY9-5652
// R-TG65-IXVR
// R-THE1-WPMG
func TestUsageRecordsArePerRoundTripAndSummedIntoTurnEndAndSummary(t *testing.T) {
	var output bytes.Buffer
	log := NewLog(&output, func() time.Time { return time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC) }, "")
	log.start(Identity{})
	log.usage(Usage{InputTokens: 2, OutputTokens: 3}, Cost(11))
	log.usage(Usage{CachedTokens: 5, ReasoningTokens: 7}, Cost(13))
	log.finish()
	log.start(Identity{})
	log.finish()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	before := output.String()
	if err := log.Close(); err != nil || output.String() != before {
		t.Fatalf("second Close changed output or errored: %v", err)
	}

	records := decodeLogRecords(t, output.Bytes())
	if len(records) != 7 {
		t.Fatalf("record count = %d, want exactly 7", len(records))
	}
	// R-TEY9-5652: two round-trips in the first turn, two distinct usage records.
	if records[1].Type != RecordUsage || records[1].Cost == nil || *records[1].Cost != Cost(11) ||
		records[2].Type != RecordUsage || records[2].Cost == nil || *records[2].Cost != Cost(13) {
		t.Fatalf("per-round usage records = %#v, want two separate records, not one aggregate", records[1:3])
	}
	// R-TG65-IXVR: turn_end sums the usage records written since turn_start.
	firstTurnEnd := records[3]
	wantFirstUsage := Usage{InputTokens: 2, CachedTokens: 5, OutputTokens: 3, ReasoningTokens: 7}
	if firstTurnEnd.Type != RecordTurnEnd || firstTurnEnd.Usage == nil || *firstTurnEnd.Usage != wantFirstUsage ||
		firstTurnEnd.Cost == nil || *firstTurnEnd.Cost != Cost(24) {
		t.Fatalf("first turn_end = %#v, want summed usage %+v and cost 24", firstTurnEnd, wantFirstUsage)
	}
	// R-TG65-IXVR: a turn with no round-trip gets zero values.
	secondTurnEnd := records[5]
	if secondTurnEnd.Type != RecordTurnEnd || secondTurnEnd.Usage == nil || *secondTurnEnd.Usage != (Usage{}) ||
		secondTurnEnd.Cost == nil || *secondTurnEnd.Cost != Cost(0) {
		t.Fatalf("no-round-trip turn_end = %#v, want zero Usage and Cost", secondTurnEnd)
	}
	// R-THE1-WPMG: summary sums every turn_end record.
	summary := records[6]
	if summary.Type != RecordSummary || summary.Usage == nil || *summary.Usage != wantFirstUsage ||
		summary.Cost == nil || *summary.Cost != Cost(24) {
		t.Fatalf("summary = %#v, want usage %+v cost 24", summary, wantFirstUsage)
	}
	if summary.Identity != nil || summary.Message != nil || summary.ToolUse != nil || summary.ToolResult != nil || summary.Err != nil || summary.Retry != nil {
		t.Fatalf("summary contains unrelated payloads: %#v", summary)
	}
}

func decodeLogRecords(t *testing.T, data []byte) []LogRecord {
	t.Helper()
	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) == 0 || len(lines[len(lines)-1]) != 0 {
		t.Fatalf("log does not end at a JSON-lines boundary: %q", data)
	}
	lines = lines[:len(lines)-1]
	records := make([]LogRecord, len(lines))
	for index, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			t.Fatalf("line %d is blank", index)
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		if err := decoder.Decode(&records[index]); err != nil {
			t.Fatalf("line %d decode: %v", index, err)
		}
		var extra any
		if err := decoder.Decode(&extra); err == nil {
			t.Fatalf("line %d contains multiple JSON values", index)
		}
	}
	return records
}

func TestRecordTypeIsClosedEnumeration(t *testing.T) {
	// R-8B7W-P209
	recordType := reflect.TypeFor[RecordType]()
	if recordType.Name() != "RecordType" || recordType.Kind() != reflect.String {
		t.Fatalf("RecordType = %q/%s, want defined string type", recordType.Name(), recordType.Kind())
	}

	want := map[string]string{
		"RecordTurnStart":  "turn_start",
		"RecordMessage":    "message",
		"RecordToolUse":    "tool_use",
		"RecordToolResult": "tool_result",
		"RecordOutput":     "output",
		"RecordUsage":      "usage",
		"RecordLimit":      "limit",
		"RecordError":      "error",
		"RecordRetry":      "retry",
		"RecordTurnEnd":    "turn_end",
		"RecordSummary":    "summary",
		"RecordSavepoint":  "savepoint",
		"RecordRestore":    "restore",
		"RecordRelease":    "release",
	}
	got := exportedConstantsOfType(t, "RecordType")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exported RecordType constants = %#v, want exactly %#v", got, want)
	}

	values := map[string]RecordType{
		"RecordTurnStart": RecordTurnStart, "RecordMessage": RecordMessage,
		"RecordToolUse": RecordToolUse, "RecordToolResult": RecordToolResult,
		"RecordOutput": RecordOutput,
		"RecordUsage":  RecordUsage, "RecordLimit": RecordLimit,
		"RecordError": RecordError,
		"RecordRetry": RecordRetry, "RecordTurnEnd": RecordTurnEnd,
		"RecordSummary":   RecordSummary,
		"RecordSavepoint": RecordSavepoint, "RecordRestore": RecordRestore,
		"RecordRelease": RecordRelease,
	}
	for name, value := range values {
		if string(value) != want[name] {
			t.Errorf("%s = %q, want %q", name, value, want[name])
		}
	}
}

func TestLogRecordDeclarationIsExact(t *testing.T) {
	// R-T6EY-GRY7
	typeOf := reflect.TypeFor[LogRecord]()
	if typeOf.Name() != "LogRecord" || typeOf.Kind() != reflect.Struct {
		t.Fatalf("LogRecord = %q/%s, want defined struct", typeOf.Name(), typeOf.Kind())
	}
	want := []struct {
		name   string
		typeOf reflect.Type
		tag    string
	}{
		{"Type", reflect.TypeFor[RecordType](), `json:"type"`},
		{"ID", reflect.TypeFor[string](), `json:"id,omitempty"`},
		{"Time", reflect.TypeFor[time.Time](), `json:"time"`},
		{"Seq", reflect.TypeFor[int](), `json:"seq"`},
		{"Identity", reflect.TypeFor[*Identity](), `json:"identity,omitempty"`},
		{"Message", reflect.TypeFor[*Message](), `json:"message,omitempty"`},
		{"ToolUse", reflect.TypeFor[*ToolUse](), `json:"tool_use,omitempty"`},
		{"ToolResult", reflect.TypeFor[*ToolResult](), `json:"tool_result,omitempty"`},
		{"Output", reflect.TypeFor[json.RawMessage](), `json:"output,omitempty"`},
		{"Usage", reflect.TypeFor[*Usage](), `json:"usage,omitempty"`},
		{"Cost", reflect.TypeFor[*Cost](), `json:"cost,omitempty"`},
		{"Limit", reflect.TypeFor[*LimitInfo](), `json:"limit,omitempty"`},
		{"Err", reflect.TypeFor[*Error](), `json:"error,omitempty"`},
		{"Retry", reflect.TypeFor[*RetryInfo](), `json:"retry,omitempty"`},
	}
	assertExactStruct(t, typeOf, want)

	record := LogRecord{Type: RecordLimit, ID: "agent-7", Limit: &LimitInfo{}}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded LogRecord
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, record) {
		t.Fatalf("LogRecord JSON round trip = %#v, want %#v", decoded, record)
	}
}

func TestLogRecordOutputJSONCodec(t *testing.T) {
	output := json.RawMessage(`{"answer":[1,true],"nested":{"value":"ok"}}`)
	record := LogRecord{Type: RecordOutput, Output: output}

	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(object["output"], output) {
		t.Fatalf("encoded output = %s, want raw document %s", object["output"], output)
	}
	for _, key := range []string{"identity", "message", "tool_use", "tool_result", "usage", "cost", "error", "retry"} {
		if _, present := object[key]; present {
			t.Errorf("unrelated payload %q is present in %s", key, encoded)
		}
	}

	var decoded LogRecord
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != RecordOutput || !bytes.Equal(decoded.Output, output) {
		t.Fatalf("decoded output record = %#v, want type %q and output %s", decoded, RecordOutput, output)
	}
	if decoded.Identity != nil || decoded.Message != nil || decoded.ToolUse != nil || decoded.ToolResult != nil || decoded.Usage != nil || decoded.Cost != nil || decoded.Err != nil || decoded.Retry != nil {
		t.Fatalf("decoded output record contains unrelated payloads: %#v", decoded)
	}

	empty, err := json.Marshal(LogRecord{Type: RecordOutput})
	if err != nil {
		t.Fatal(err)
	}
	var emptyObject map[string]json.RawMessage
	if err := json.Unmarshal(empty, &emptyObject); err != nil {
		t.Fatal(err)
	}
	if _, present := emptyObject["output"]; present {
		t.Fatalf("empty output was not omitted: %s", empty)
	}
}

func TestRetryInfoDeclarationIsExact(t *testing.T) {
	// R-0NE8-TO91
	typeOf := reflect.TypeFor[RetryInfo]()
	if typeOf.Name() != "RetryInfo" || typeOf.Kind() != reflect.Struct {
		t.Fatalf("RetryInfo = %q/%s, want defined struct", typeOf.Name(), typeOf.Kind())
	}
	want := []struct {
		name   string
		typeOf reflect.Type
		tag    string
	}{
		{"Attempt", reflect.TypeFor[int](), `json:"attempt"`},
		{"Delay", reflect.TypeFor[time.Duration](), `json:"delay"`},
		{"Reason", reflect.TypeFor[string](), `json:"reason"`},
	}
	assertExactStruct(t, typeOf, want)
}

func TestLogIsOpaqueAndCallable(t *testing.T) {
	// R-T7MU-UJOW
	typeOf := reflect.TypeFor[Log]()
	if typeOf.Name() != "Log" || typeOf.Kind() != reflect.Struct {
		t.Fatalf("Log = %q/%s, want defined opaque struct", typeOf.Name(), typeOf.Kind())
	}
	for index := range typeOf.NumField() {
		if typeOf.Field(index).IsExported() {
			t.Fatalf("Log field %q is exported", typeOf.Field(index).Name)
		}
	}

	wantConstructor := reflect.TypeOf(func(io.Writer, func() time.Time, string) *Log { return nil })
	if got := reflect.TypeOf(NewLog); got != wantConstructor {
		t.Fatalf("NewLog = %s, want %s", got, wantConstructor)
	}
	wantClose := reflect.TypeOf(func(*Log) error { return nil })
	closeMethod, ok := reflect.TypeFor[*Log]().MethodByName("Close")
	if !ok || closeMethod.Type != wantClose {
		t.Fatalf("(*Log).Close = %v (present=%t), want %s", closeMethod.Type, ok, wantClose)
	}

	log := NewLog(nil, func() time.Time { return time.Time{} }, "")
	if log == nil {
		t.Fatal("NewLog returned nil, want callable *Log")
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil scaffolding result", err)
	}
}

func TestLogStampsIDOnEveryRecordAndOmitsWhenEmpty(t *testing.T) {
	// R-T8UR-8BFL
	var withID bytes.Buffer
	log := NewLog(&withID, func() time.Time { return time.Time{} }, "agent-7")
	log.start(Identity{})
	log.record(eventRecord{kind: eventRecordMessage, value: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "x"}}}})
	log.usage(Usage{}, 0)
	log.finish()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	records := decodeLogRecords(t, withID.Bytes())
	if len(records) == 0 {
		t.Fatal("log wrote no records")
	}
	for index, record := range records {
		if record.ID != "agent-7" {
			t.Fatalf("record %d ID = %q, want %q", index, record.ID, "agent-7")
		}
	}
	for index, line := range bytes.Split(bytes.TrimSuffix(withID.Bytes(), []byte{'\n'}), []byte{'\n'}) {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			t.Fatalf("line %d decode: %v", index, err)
		}
		if _, present := raw["id"]; !present {
			t.Fatalf("line %d missing id: %s", index, line)
		}
	}

	var withoutID bytes.Buffer
	empty := NewLog(&withoutID, func() time.Time { return time.Time{} }, "")
	empty.start(Identity{})
	empty.record(eventRecord{kind: eventRecordMessage, value: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "x"}}}})
	empty.usage(Usage{}, 0)
	empty.finish()
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}
	for index, line := range bytes.Split(bytes.TrimSuffix(withoutID.Bytes(), []byte{'\n'}), []byte{'\n'}) {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			t.Fatalf("empty-id line %d decode: %v", index, err)
		}
		if _, present := raw["id"]; present {
			t.Fatalf("empty-id line %d has an id key: %s", index, line)
		}
	}
}

func assertExactStruct(t *testing.T, got reflect.Type, want []struct {
	name   string
	typeOf reflect.Type
	tag    string
}) {
	t.Helper()
	if got.NumField() != len(want) {
		t.Fatalf("%s field count = %d, want exactly %d", got.Name(), got.NumField(), len(want))
	}
	for index, expected := range want {
		field := got.Field(index)
		if field.Name != expected.name || field.Type != expected.typeOf || string(field.Tag) != expected.tag || !field.IsExported() {
			t.Errorf("%s field %d = %s %s tag %q exported=%t, want %s %s tag %q exported", got.Name(), index, field.Name, field.Type, field.Tag, field.IsExported(), expected.name, expected.typeOf, expected.tag)
		}
	}
}

func exportedConstantsOfType(t *testing.T, typeName string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := make([]*ast.File, 0)
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fileSet, entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		files = append(files, parsed)
	}
	information := &types.Info{Defs: make(map[*ast.Ident]types.Object)}
	checked, err := (&types.Config{Importer: importer.Default()}).Check("github.com/ikigenba/ikigenba/agentkit", fileSet, files, information)
	if err != nil {
		t.Fatal(err)
	}
	wantedType := checked.Scope().Lookup(typeName).Type()
	constants := make(map[string]string)
	for identifier, object := range information.Defs {
		constantObject, ok := object.(*types.Const)
		if !ok || !identifier.IsExported() || !types.Identical(constantObject.Type(), wantedType) {
			continue
		}
		constants[identifier.Name] = constant.StringVal(constantObject.Val())
	}
	return constants
}
