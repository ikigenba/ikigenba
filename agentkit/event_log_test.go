package agentkit

import (
	"bytes"
	"encoding/json"
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
	"strings"
	"testing"
	"time"
)

func TestLogRecordsUseInjectedTimePerTurnSequenceAndFullIdentity(t *testing.T) {
	// R-5FTF-T0XZ
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
	})
	identity := Identity{Endpoint: "https://api.example/v1", AuthMode: "oauth", Model: "model-a"}
	log.start(identity)
	log.record(eventRecord{kind: eventRecordMessage, value: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "one"}}}})
	log.finish(Usage{}, Cost{})
	log.start(identity)
	log.finish(Usage{}, Cost{})

	records := decodeLogRecords(t, output.Bytes())
	if len(records) != 7 {
		t.Fatalf("record count = %d, want exactly 7", len(records))
	}
	wantSeq := []int{0, 1, 2, 3, 0, 1, 2}
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
	nilLog.finish(Usage{}, Cost{})
	if err := nilLog.Close(); err != nil {
		t.Fatalf("nil receiver Close() = %v", err)
	}
	clockCalls := 0
	log := NewLog(nil, func() time.Time { clockCalls++; return time.Time{} })
	log.start(Identity{})
	log.record(eventRecord{kind: eventRecordToolUse, value: ToolUse{ID: "call", Name: "tool"}})
	log.finish(Usage{}, Cost{})
	if err := log.Close(); err != nil || clockCalls != 0 {
		t.Fatalf("nil-writer log Close/clock = %v/%d, want nil/0", err, clockCalls)
	}

	var output bytes.Buffer
	log = NewLog(&output, func() time.Time { return time.Time{} })
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

func TestCloseWritesOneCumulativeSummaryAndPropagatesUnknownCost(t *testing.T) {
	var output bytes.Buffer
	log := NewLog(&output, func() time.Time { return time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC) })
	log.start(Identity{})
	log.finish(Usage{InputTokens: 2, OutputTokens: 3}, Cost{Amount: 11, Known: true})
	log.start(Identity{})
	log.finish(Usage{CachedTokens: 5, ReasoningTokens: 7}, Cost{Amount: 13, Known: false})
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	before := output.String()
	if err := log.Close(); err != nil || output.String() != before {
		t.Fatalf("second Close changed output or errored: %v", err)
	}
	records := decodeLogRecords(t, output.Bytes())
	if len(records) != 7 || records[1].Type != RecordUsage || records[1].Cost == nil || records[4].Type != RecordUsage || records[4].Cost == nil {
		t.Fatalf("usage records missing mandatory costs: %#v", records)
	}
	summary := records[6]
	wantUsage := Usage{InputTokens: 2, CachedTokens: 5, OutputTokens: 3, ReasoningTokens: 7}
	wantCost := Cost{Amount: 24, Known: false}
	if summary.Type != RecordSummary || summary.Usage == nil || *summary.Usage != wantUsage || summary.Cost == nil || *summary.Cost != wantCost {
		t.Fatalf("summary = %#v, want usage %+v cost %+v", summary, wantUsage, wantCost)
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
	// R-0KYG-24RN
	// R-URVJ-1JCJ
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
		"RecordError":      "error",
		"RecordRetry":      "retry",
		"RecordTurnEnd":    "turn_end",
		"RecordSummary":    "summary",
	}
	got := exportedConstantsOfType(t, "RecordType")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exported RecordType constants = %#v, want exactly %#v", got, want)
	}

	values := map[string]RecordType{
		"RecordTurnStart": RecordTurnStart, "RecordMessage": RecordMessage,
		"RecordToolUse": RecordToolUse, "RecordToolResult": RecordToolResult,
		"RecordOutput": RecordOutput,
		"RecordUsage":  RecordUsage, "RecordError": RecordError,
		"RecordRetry": RecordRetry, "RecordTurnEnd": RecordTurnEnd,
		"RecordSummary": RecordSummary,
	}
	for name, value := range values {
		if string(value) != want[name] {
			t.Errorf("%s = %q, want %q", name, value, want[name])
		}
	}
}

func TestLogRecordDeclarationIsExact(t *testing.T) {
	// R-0M6C-FWIC
	// R-UT3F-FB38
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
		{"Time", reflect.TypeFor[time.Time](), `json:"time"`},
		{"Seq", reflect.TypeFor[int](), `json:"seq"`},
		{"Identity", reflect.TypeFor[*Identity](), `json:"identity,omitempty"`},
		{"Message", reflect.TypeFor[*Message](), `json:"message,omitempty"`},
		{"ToolUse", reflect.TypeFor[*ToolUse](), `json:"tool_use,omitempty"`},
		{"ToolResult", reflect.TypeFor[*ToolResult](), `json:"tool_result,omitempty"`},
		{"Output", reflect.TypeFor[json.RawMessage](), `json:"output,omitempty"`},
		{"Usage", reflect.TypeFor[*Usage](), `json:"usage,omitempty"`},
		{"Cost", reflect.TypeFor[*Cost](), `json:"cost,omitempty"`},
		{"Err", reflect.TypeFor[*Error](), `json:"error,omitempty"`},
		{"Retry", reflect.TypeFor[*RetryInfo](), `json:"retry,omitempty"`},
	}
	assertExactStruct(t, typeOf, want)
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
	// R-0OM5-7FZQ
	typeOf := reflect.TypeFor[Log]()
	if typeOf.Name() != "Log" || typeOf.Kind() != reflect.Struct {
		t.Fatalf("Log = %q/%s, want defined opaque struct", typeOf.Name(), typeOf.Kind())
	}
	for index := range typeOf.NumField() {
		if typeOf.Field(index).IsExported() {
			t.Fatalf("Log field %q is exported", typeOf.Field(index).Name)
		}
	}

	wantConstructor := reflect.TypeOf(func(io.Writer, func() time.Time) *Log { return nil })
	if got := reflect.TypeOf(NewLog); got != wantConstructor {
		t.Fatalf("NewLog = %s, want %s", got, wantConstructor)
	}
	wantClose := reflect.TypeOf(func(*Log) error { return nil })
	closeMethod, ok := reflect.TypeFor[*Log]().MethodByName("Close")
	if !ok || closeMethod.Type != wantClose {
		t.Fatalf("(*Log).Close = %v (present=%t), want %s", closeMethod.Type, ok, wantClose)
	}

	log := NewLog(nil, func() time.Time { return time.Time{} })
	if log == nil {
		t.Fatal("NewLog returned nil, want callable *Log")
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil scaffolding result", err)
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
