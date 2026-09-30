package chat

import (
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
	"time"
)

// typed returns v as a T; a call compiles only when v is assignable to T.
func typed[T any](v T) T { return v }

func TestKindType(t *testing.T) {
	// R-KHCB-MZ8F
	k := typed[Kind]("user")
	if k+" ok" != Kind("user ok") || len(k) != 4 {
		t.Fatalf("Kind = %q", k)
	}
}

func TestKinds(t *testing.T) {
	// R-KIK8-0QZ4
	got := []Kind{KindUser, KindAssistant, KindReasoning, KindAgent, KindTool, KindResultOK, KindResultError}
	want := []Kind{"user", "assistant", "reasoning", "agent", "tool", "result ok", "result error"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %q, want %q", got, want)
	}
	for _, constant := range []any{KindUser, KindAssistant, KindReasoning, KindAgent, KindTool, KindResultOK, KindResultError} {
		if _, ok := constant.(Kind); !ok {
			t.Fatalf("kind constant type = %T", constant)
		}
	}
}

func TestEntryShape(t *testing.T) {
	// R-YQET-N0M4
	when := typed[time.Time](time.Date(2026, 9, 23, 1, 2, 3, 0, time.UTC))
	hasTime := typed[bool](true)
	kind := typed[Kind](KindTool)
	tool := typed[string]("shell")
	text := typed[string]("body")
	e := Entry{Time: when, HasTime: hasTime, Kind: kind, Tool: tool, Text: text}
	if !e.Time.Equal(when) || !e.HasTime || e.Kind != kind || e.Tool != tool || e.Text != text {
		t.Fatalf("Entry = %+v", e)
	}
}

func TestUsageShape(t *testing.T) {
	// R-YRMQ-0SCT
	u := Usage{In: typed[int64](1), CacheWrite: typed[int64](2), CacheRead: typed[int64](3), Out: typed[int64](4), Reasoning: typed[int64](5), Calls: typed[int64](6)}
	if u.In != 1 || u.CacheWrite != 2 || u.CacheRead != 3 || u.Out != 4 || u.Reasoning != 5 || u.Calls != 6 {
		t.Fatalf("Usage = %+v", u)
	}
}

func TestRecordedShape(t *testing.T) {
	// R-YSUM-EK3I
	r := Recorded{In: typed[bool](true), CacheWrite: typed[bool](false), CacheRead: typed[bool](true), Out: typed[bool](false), Reasoning: typed[bool](true), Calls: typed[bool](false)}
	if !r.In || r.CacheWrite || !r.CacheRead || r.Out || !r.Reasoning || r.Calls {
		t.Fatalf("Recorded = %+v", r)
	}
}

type shapeDecoder struct{}

func (shapeDecoder) Decode(_ fs.FS, record []byte) ([]Entry, Usage) {
	return []Entry{{Kind: KindUser, Text: string(record)}}, Usage{Calls: 1}
}

func TestDecoderShape(t *testing.T) {
	// R-YU2I-SBU7
	d := typed[Decoder](shapeDecoder{})
	decode := typed[func(fs.FS, []byte) ([]Entry, Usage)](d.Decode)
	entries, usage := decode(fstest.MapFS{}, []byte("record"))
	if len(entries) != 1 || entries[0].Text != "record" || usage.Calls != 1 {
		t.Fatalf("Decode = %v %+v", entries, usage)
	}
}

func TestTranscriptShape(t *testing.T) {
	// R-Z2LT-GQ12
	tr := NewTranscript("/log", Recorded{}, func() Decoder { return shapeDecoder{} })
	if typed[*Transcript](tr).Path() != "/log" {
		t.Fatal("Transcript lost its path")
	}
}

func TestErrAgentNotFound(t *testing.T) {
	// R-KX70-LZVG
	errPtr := typed[*error](&ErrAgentNotFound)
	if *errPtr == nil {
		t.Fatalf("ErrAgentNotFound is nil")
	}
}
