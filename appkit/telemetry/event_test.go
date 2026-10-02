package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"
)

type eventTestString string

func (eventTestString) MarshalJSON() ([]byte, error) { panic("must not call custom marshaler") }

type eventTestBool bool
type eventTestInt int64
type eventTestUint uint64
type eventTestFloat float64

func contractEvent() Event {
	return Event{time.Date(2025, 2, 3, 4, 5, 6, 123456789, time.FixedZone("offset", 3600)), "test", "token.minted", "request", "user", Attrs{}}
}

// R-UTJ9-KA7E R-UUR5-Y1Y3 R-UVZ2-BTOS
func TestEventPublicContract(t *testing.T) {
	var raw map[string]any = Attrs{"value": "text"}
	attrs := Attrs(raw)
	// An unkeyed literal also requires exactly these fields, in this order.
	event := Event{time.Time{}, "service", "service.started", "", "", attrs}
	marshal := event.MarshalJSON
	data, err := marshal()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["service"] != "service" || decoded["event"] != "service.started" {
		t.Fatalf("wrong envelope: %s", data)
	}
}

// R-UX6Y-PLFH
func TestEventNameGrammar(t *testing.T) {
	valid := []string{"a.b", "api_key.minted", "a0_1.b2_3", "token.minted", "noun9.verb_0"}
	invalid := []string{"", "Token.Minted", "minted", "auth.token.minted", "token..minted", "a.b\n", "1a.b", "a.1b", "_a.b", "a._b", "a_.b", "a.b_", "a__x.b", "a.b__x", "é.b", "a.é", "a-b.c", "a.b-c", "a/b.c", " a.b", "a.b ", "a.b\x00"}
	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			e := contractEvent()
			e.Name = name
			if _, err := e.MarshalJSON(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, name := range invalid {
		t.Run("invalid/"+name, func(t *testing.T) {
			e := contractEvent()
			e.Name = name
			data, err := e.MarshalJSON()
			if err == nil || data != nil {
				t.Fatalf("accepted %q: %s, %v", name, data, err)
			}
		})
	}
}

// R-UZMR-H4WV
func TestAttributeKeyGrammar(t *testing.T) {
	for _, key := range []string{"a", "a0", "a_0", "duration_us", "a0_b1_2"} {
		e := contractEvent()
		e.Attrs = Attrs{key: true}
		if _, err := e.MarshalJSON(); err != nil {
			t.Fatalf("%q: %v", key, err)
		}
	}
	for _, key := range []string{"", "A", "0a", "_a", "a_", "a__b", "a.b", "a-b", "é", "a\n", "a b", "a\x00"} {
		e := contractEvent()
		e.Attrs = Attrs{key: true}
		data, err := e.MarshalJSON()
		if err == nil || data != nil {
			t.Fatalf("accepted %q: %s, %v", key, data, err)
		}
	}
}

// R-V0UN-UWNK
func TestAttributeBasicForms(t *testing.T) {
	cases := []struct{ value, want any }{
		{true, true}, {"text", "text"}, {int(-1), int64(-1)}, {int8(-2), int64(-2)}, {int16(-3), int64(-3)}, {int32(-4), int64(-4)}, {int64(math.MinInt64), int64(math.MinInt64)},
		{uint(1), uint64(1)}, {uint8(2), uint64(2)}, {uint16(3), uint64(3)}, {uint32(4), uint64(4)}, {uint64(math.MaxUint64), uint64(math.MaxUint64)},
		{float32(1.25), float64(1.25)}, {float64(-2.5), float64(-2.5)},
		{eventTestString("named"), "named"}, {eventTestBool(true), true}, {eventTestInt(-5), int64(-5)}, {eventTestUint(6), uint64(6)}, {eventTestFloat(7.5), float64(7.5)},
	}
	for _, tc := range cases {
		var capture Capture
		w := testWriter(t, writerConfig(&capture, nil))
		w.Emit(context.Background(), "value.recorded", Attrs{"value": tc.value})
		flushWriter(t, w)
		got := capture.Events()[0].Attrs["value"]
		if got != tc.want {
			t.Fatalf("%T normalized to %T(%v), want %T(%v)", tc.value, got, got, tc.want, tc.want)
		}
		w.Shutdown(context.Background(), "done")
		e := contractEvent()
		e.Attrs = Attrs{"value": tc.value}
		data, err := e.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		expected := contractEvent()
		expected.Attrs = Attrs{"value": tc.want}
		want, err := expected.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("basic encoding differs: %s vs %s", data, want)
		}
	}
}

// R-V22K-8OE9
func TestEventCanonicalJSON(t *testing.T) {
	cases := []Event{contractEvent(), {time.Time{}, "\xff<&>", "a.b", "\xff<&>", "\xff<&>", nil}, {time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), "service", "a.b", "", "", Attrs{}}, {time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), "service", "a.b", "", "", Attrs{}}}
	cases[0].Attrs = Attrs{"z": float64(1e-9), "a": "\xff<&>\n", "b": true, "n": int64(-17), "u": uint64(math.MaxUint64)}
	for _, e := range cases {
		attrs := map[string]any{}
		for k, v := range e.Attrs {
			attrs[k] = v
		}
		envelope := struct {
			Time      string         `json:"time"`
			Service   string         `json:"service"`
			Event     string         `json:"event"`
			RequestID string         `json:"request_id"`
			User      string         `json:"user"`
			Attrs     map[string]any `json:"attrs"`
		}{e.Time.UTC().Format("2006-01-02T15:04:05.000000Z"), e.Service, e.Name, e.RequestID, e.User, attrs}
		want, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("got %s, want %s", got, want)
		}
	}
}

// R-V3AG-MG4Y R-V0UN-UWNK
func TestEventMalformed(t *testing.T) {
	var pointer *int
	invalid := []any{nil, pointer, new(int), []int{}, []byte{}, map[string]any{}, struct{}{}, make(chan int), func() {}, complex64(1), complex128(2), uintptr(3), math.NaN(), math.Inf(1), math.Inf(-1), float32(math.Inf(1)), float32(math.NaN()), eventTestFloat(math.Inf(-1))}
	for _, value := range invalid {
		e := contractEvent()
		e.Attrs = Attrs{"value": value}
		data, err := e.MarshalJSON()
		if err == nil || data != nil {
			t.Fatalf("accepted %T: %s, %v", value, data, err)
		}
	}
	events := []Event{contractEvent(), contractEvent(), contractEvent(), contractEvent()}
	events[0].Service = ""
	events[1].Time = time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)
	events[2].Time = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	// UTC conversion crosses the lower year boundary.
	events[3].Time = time.Date(0, 1, 1, 0, 0, 0, 0, time.FixedZone("offset", 3600))
	for _, e := range events {
		data, err := e.MarshalJSON()
		if err == nil || data != nil {
			t.Fatalf("accepted malformed event: %s, %v", data, err)
		}
	}
}
