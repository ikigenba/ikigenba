package chat

import (
	"io/fs"
	"reflect"
	"testing"
	"time"
)

func TestKindType(t *testing.T) {
	// R-KHCB-MZ8F
	if got, want := reflect.TypeOf(Kind("")), reflect.TypeOf(""); got.Kind() != want.Kind() || got.Name() != "Kind" || got.PkgPath() != "github.com/ikigenba/ikigenba/agent-monitor/internal/chat" {
		t.Fatalf("Kind type = %v", got)
	}
}

func TestKinds(t *testing.T) {
	// R-KIK8-0QZ4
	got := []Kind{KindUser, KindAssistant, KindReasoning, KindAgent, KindTool, KindResultOK, KindResultError}
	want := []Kind{"user", "assistant", "reasoning", "agent", "tool", "result ok", "result error"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %q, want %q", got, want)
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(KindUser), reflect.TypeOf(KindAssistant), reflect.TypeOf(KindReasoning), reflect.TypeOf(KindAgent), reflect.TypeOf(KindTool), reflect.TypeOf(KindResultOK), reflect.TypeOf(KindResultError)} {
		if typ != reflect.TypeOf(Kind("")) {
			t.Fatalf("kind constant type = %v", typ)
		}
	}
}

func checkFields(t *testing.T, typ reflect.Type, names []string, types []reflect.Type) {
	t.Helper()
	if typ.Kind() != reflect.Struct || typ.NumField() != len(names) {
		t.Fatalf("%s fields = %v", typ, typ.NumField())
	}
	for i, name := range names {
		field := typ.Field(i)
		if field.Name != name || field.Type != types[i] || !field.IsExported() {
			t.Fatalf("%s field %d = %v", typ, i, field)
		}
	}
}

func TestEntryShape(t *testing.T) {
	// R-KJS4-EIPT
	checkFields(t, reflect.TypeOf(Entry{}), []string{"Time", "HasTime", "Kind", "Tool", "Text"}, []reflect.Type{reflect.TypeOf(time.Time{}), reflect.TypeOf(false), reflect.TypeOf(Kind("")), reflect.TypeOf(""), reflect.TypeOf("")})
}

func TestUsageShape(t *testing.T) {
	// R-KL00-SAGI
	i64 := reflect.TypeOf(int64(0))
	checkFields(t, reflect.TypeOf(Usage{}), []string{"In", "CacheWrite", "CacheRead", "Out", "Reasoning", "Calls"}, []reflect.Type{i64, i64, i64, i64, i64, i64})
}

func TestRecordedShape(t *testing.T) {
	// R-KNFT-JTXW
	b := reflect.TypeOf(false)
	checkFields(t, reflect.TypeOf(Recorded{}), []string{"In", "CacheWrite", "CacheRead", "Out", "Reasoning", "Calls"}, []reflect.Type{b, b, b, b, b, b})
}

func TestDecoderShape(t *testing.T) {
	// R-6S43-37D6
	typ := reflect.TypeOf((*Decoder)(nil)).Elem()
	if typ.Kind() != reflect.Interface || typ.NumMethod() != 1 {
		t.Fatalf("Decoder methods = %v", typ.NumMethod())
	}
	m := typ.Method(0)
	if m.Name != "Decode" || m.Type.NumIn() != 2 || m.Type.In(0) != reflect.TypeOf((*fs.FS)(nil)).Elem() || m.Type.In(1) != reflect.TypeOf([]byte(nil)) || m.Type.NumOut() != 2 || m.Type.Out(0) != reflect.TypeOf([]Entry(nil)) || m.Type.Out(1) != reflect.TypeOf(Usage{}) {
		t.Fatalf("Decode signature = %v", m.Type)
	}
}

func TestTranscriptShape(t *testing.T) {
	// R-KPVM-BDFA
	typ := reflect.TypeOf(Transcript{})
	if typ.Kind() != reflect.Struct || typ.Name() != "Transcript" {
		t.Fatalf("Transcript type = %v", typ)
	}
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			t.Fatalf("Transcript exported field = %v", typ.Field(i))
		}
	}
}

func TestErrAgentNotFound(t *testing.T) {
	// R-KX70-LZVG
	if reflect.TypeOf(&ErrAgentNotFound).Elem() != reflect.TypeOf((*error)(nil)).Elem() || ErrAgentNotFound == nil {
		t.Fatalf("ErrAgentNotFound type or value invalid")
	}
}
