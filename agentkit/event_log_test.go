package agentkit

import (
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

func TestRecordTypeIsClosedEnumeration(t *testing.T) {
	// R-0KYG-24RN
	recordType := reflect.TypeFor[RecordType]()
	if recordType.Name() != "RecordType" || recordType.Kind() != reflect.String {
		t.Fatalf("RecordType = %q/%s, want defined string type", recordType.Name(), recordType.Kind())
	}

	want := map[string]string{
		"RecordTurnStart":  "turn_start",
		"RecordMessage":    "message",
		"RecordToolUse":    "tool_use",
		"RecordToolResult": "tool_result",
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
		"RecordUsage": RecordUsage, "RecordError": RecordError,
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
		{"Usage", reflect.TypeFor[*Usage](), `json:"usage,omitempty"`},
		{"Cost", reflect.TypeFor[*Cost](), `json:"cost,omitempty"`},
		{"Err", reflect.TypeFor[*Error](), `json:"error,omitempty"`},
		{"Retry", reflect.TypeFor[*RetryInfo](), `json:"retry,omitempty"`},
	}
	assertExactStruct(t, typeOf, want)
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
