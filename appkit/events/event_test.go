package events_test

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
)

func record() events.Event {
	return events.Event{"evt_0123456789abcdef", time.Date(2025, 2, 3, 4, 5, 6, 123456789, time.FixedZone("offset", 3600)), "repos", "repo.pushed", "request", "user", events.Attrs{"count": int8(3)}, "", 0, 0, time.Time{}}
}

// R-G3AZ-6IPU R-G4IV-KAGJ R-G5QR-Y278 R-G6YO-BTXX R-G86K-PLOM
// R-GLLG-X2U9 R-GMTD-AUKY R-GWKK-D0II
func TestEventCanonicalForms(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		e := record()
		if delivered {
			e.Seq = 7
			e.Received = time.Date(2025, 3, 4, 5, 6, 7, 987654321, time.FixedZone("offset", -3600))
		}
		data, err := e.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		want := `{"id":"evt_0123456789abcdef","time":"2025-02-03T03:05:06.123456Z","service":"repos","event":"repo.pushed","request_id":"request","user":"user","attrs":{"count":3},"cause":"","depth":0`
		if delivered {
			want += `,"seq":7,"received":"2025-03-04T06:06:07.987654Z"`
		}
		want += `}`
		if string(data) != want {
			t.Fatalf("got %s want %s", data, want)
		}
		var decoded events.Event
		if err = decoded.UnmarshalJSON(data); err != nil {
			t.Fatal(err)
		}
		again, err := decoded.MarshalJSON()
		if err != nil || !bytes.Equal(data, again) {
			t.Fatalf("roundtrip %s %v", again, err)
		}
	}
	e := record()
	e.Attrs = nil
	data, err := e.MarshalJSON()
	if err != nil || !bytes.Contains(data, []byte(`"attrs":{}`)) {
		t.Fatalf("nil attrs %s %v", data, err)
	}
}

// R-GEA2-MGE3 R-GHXR-RRM6 R-GJ5O-5JCV R-SW2V-L459 R-GO19-OMBN
func TestEventValidity(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*events.Event)
	}{
		{"id", func(e *events.Event) { e.ID = "evt_0123456789abcdeF" }}, {"short id", func(e *events.Event) { e.ID = "evt_0" }}, {"service", func(e *events.Event) { e.Service = "" }},
		{"name caps", func(e *events.Event) { e.Name = "Repo.pushed" }}, {"name extra dot", func(e *events.Event) { e.Name = "a..b.c" }}, {"name underscore", func(e *events.Event) { e.Name = "a_.b" }}, {"name digit", func(e *events.Event) { e.Name = "1a.b" }},
		{"key", func(e *events.Event) { e.Attrs = events.Attrs{"a__b": 1} }}, {"year negative", func(e *events.Event) { e.Time = time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC) }}, {"year large", func(e *events.Event) { e.Time = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"cause depth", func(e *events.Event) { e.Depth = 1 }}, {"cause no depth", func(e *events.Event) { e.Cause = e.ID }}, {"bad cause", func(e *events.Event) { e.Cause = "bad"; e.Depth = 1 }}, {"negative depth", func(e *events.Event) { e.Depth = -1 }},
		{"seq no received", func(e *events.Event) { e.Seq = 1 }}, {"received no seq", func(e *events.Event) { e.Received = e.Time }}, {"negative seq", func(e *events.Event) { e.Seq = -1; e.Received = e.Time }},
		{"zero formatted received", func(e *events.Event) { e.Seq = 1; e.Received = time.Date(1, 1, 1, 0, 0, 0, 999, time.UTC) }}, {"received large year", func(e *events.Event) { e.Seq = 1; e.Received = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := record()
			tc.mutate(&e)
			data, err := e.MarshalJSON()
			if err == nil || data != nil {
				t.Fatalf("accepted %+v", e)
			}
		})
	}
	for _, year := range []int{0, 9999} {
		e := record()
		e.Time = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		e.Name = "a1_b2.c3_d4"
		e.Attrs = events.Attrs{"a1_b2": true}
		e.Cause = e.ID
		e.Depth = 1
		if _, err := e.MarshalJSON(); err != nil {
			t.Fatal(err)
		}
		e.Seq = 1
		e.Received = e.Time
		if _, err := e.MarshalJSON(); err != nil {
			t.Fatal(err)
		}
	}
}

// R-KFTJ-ZIG2
func TestEventNameGrammar(t *testing.T) {
	valid := []string{"a.b", "api_key.minted", "a0_1.b2_3", "noun9.verb_0", "a.b.c", "auth.token.minted", "cron.nightly_backup.fired", "a0_1.b2_3.c4_5.d6_7"}
	invalid := []string{"", "minted", "a", "a_b", "Token.Minted", "1a.b", "a.1b", "_a.b", "a._b", "a_.b", "a.b_", "a__x.b", "a.b__x", "é.b", "a.é", "a-b.c", "a.b-c", "a/b.c", " a.b", "a.b ", "a.b\n", "a.b\x00", ".a.b", "a.b.", "a..b", "a.1b.c", "a.B.c", "a.b_.c", "a.b__x.c", "a.b.é", "cron.*.fired", "*.fired", "a.*"}
	for _, names := range []struct {
		values []string
		valid  bool
	}{{valid, true}, {invalid, false}} {
		for _, name := range names.values {
			e := record()
			e.Name = name
			data, err := e.MarshalJSON()
			if (err == nil) != names.valid {
				t.Fatalf("MarshalJSON name %q: %v, valid=%v", name, err, names.valid)
			}
			// Exercise the decoder independently of the encoder's validation.
			base, err := record().MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			encodedName, err := json.Marshal(name)
			if err != nil {
				t.Fatal(err)
			}
			text := bytes.Replace(base, []byte(`"repo.pushed"`), encodedName, 1)
			var decoded events.Event
			if err = decoded.UnmarshalJSON(text); (err == nil) != names.valid {
				t.Fatalf("UnmarshalJSON name %q: %v, valid=%v", name, err, names.valid)
			}
			if names.valid && (!bytes.Contains(data, encodedName) || decoded.Name != name) {
				t.Fatalf("name %q was not preserved", name)
			}
		}
	}
}

// R-KI9C-R1XG R-KH1G-DA6R R-KJH9-4TO5
func TestMatch(t *testing.T) {
	match := events.Match
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"repo.pushed", "repo.pushed", true},
		{"a0_1.b2_3.c4_5", "a0_1.b2_3.c4_5", true},
		{"cron.*.fired", "cron.nightly_backup.fired", true},
		{"cron.*.fired", "cron.hourly.fired", true},
		{"*.pushed", "repo.pushed", true},
		{"repo.*", "repo.pushed", true},
		{"*.*", "repo.pushed", true},
		{"*.*.*", "cron.nightly_backup.fired", true},
		{"*.a0_1.*.*", "repo.a0_1.b2_3.c4_5", true},
		{"repo.pushed", "repo.deleted", false},
		{"cron.*.fired", "cron.fired", false},
		{"cron.*.fired", "cron.a.b.fired", false},
		{"cron.*.fired", "cron.hourly.stopped", false},
		{"cron.*.fired", "other.hourly.fired", false},
		{"repo.*", "repo.pushed.again", false},
		{"*", "repo.pushed", false},
		{"*", "repo", false},
		{"repo", "repo", false},
		{"", "repo.pushed", false},
		{"repo.pushed", "", false},
		{"cron.a*.fired", "cron.ab.fired", false},
		{"cron.*a.fired", "cron.ba.fired", false},
		{"cron.**.fired", "cron.hourly.fired", false},
		{"Cron.*", "cron.fired", false},
		{"1cron.*", "cron.fired", false},
		{"cron_.*", "cron.fired", false},
		{"cron__x.*", "cron_x.fired", false},
		{"cron.*.", "cron.hourly.fired", false},
		{".cron.*", "cron.hourly.fired", false},
		{"cron..*", "cron.hourly.fired", false},
		{"cron.*\n", "cron.fired", false},
		{"cron.*\x00", "cron.fired", false},
		{"cron.é.*", "cron.e.fired", false},
		{"cron.*.fired", "cron.*.fired", false},
		{"*.*", "Repo.pushed", false},
		{"*.*", "1repo.pushed", false},
		{"*.*", "repo.pushed_", false},
		{"*.*", "repo.pushed\n", false},
		{"*.*", "repo.é", false},
		{"*.*", "repo.*", false},
		{"*.*", "repo..pushed", false},
		{"*.*", "repo/pushed", false},
		{"repo.pushed\n", "repo.pushed\n", false},
	}
	for _, tc := range cases {
		if got := match(tc.pattern, tc.name); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

// R-GGPV-DZVH
func TestAttributeKinds(t *testing.T) {
	type namedString string
	type namedInt int16
	type namedBool bool
	type namedFloat float32
	valid := []any{namedString("hello"), namedInt(-3), namedBool(true), namedFloat(1.25), int(1), int8(2), int32(3), int64(4), uint(1), uint8(2), uint16(3), uint32(4), uint64(math.MaxUint64), float64(-1.25)}
	for _, value := range valid {
		e := record()
		e.Attrs = events.Attrs{"value": value}
		data, err := e.MarshalJSON()
		if err != nil {
			t.Fatalf("%T: %v", value, err)
		}
		var got events.Event
		if err = got.UnmarshalJSON(data); err != nil {
			t.Fatal(err)
		}
	}
	var pointer *int
	invalid := []any{nil, pointer, uintptr(1), complex(1, 2), []int{1}, map[string]int{"a": 1}, struct{}{}, func() {}, make(chan int), math.NaN(), math.Inf(1), float32(math.Inf(-1))}
	for _, value := range invalid {
		e := record()
		e.Attrs = events.Attrs{"value": value}
		if data, err := e.MarshalJSON(); err == nil || data != nil {
			t.Fatalf("accepted %T", value)
		}
	}
}

// R-GP96-2E2C R-GQH2-G5T1 R-SXAR-YVVY R-GVCN-Z8RT
func TestStrictEventDecode(t *testing.T) {
	data, _ := record().MarshalJSON()
	base := string(data)
	invalid := []string{"", `null`, `[]`, base + base, base + " x", strings.Replace(base, `"id":`, `"id":"evt_0123456789abcdef","i\u0064":`, 1), strings.Replace(base, `"attrs":{"count":3}`, `"attrs":{"count":3,"c\u006funt":4}`, 1), strings.Replace(base, `"attrs":{"count":3}`, `"attrs":null`, 1), strings.Replace(base, `"attrs":{"count":3}`, `"attrs":{"count":null}`, 1), strings.Replace(base, `"attrs":{"count":3}`, `"attrs":{"count":1e999}`, 1), strings.Replace(base, `"attrs":{"count":3}`, `"attrs":{"Count":1}`, 1), strings.Replace(base, `"depth":0`, `"depth":0.0`, 1), strings.Replace(base, `"depth":0`, `"depth":"0"`, 1), strings.Replace(base, `"depth":0`, `"depth":1`, 1), strings.Replace(base, `"cause":""`, `"cause":"bad"`, 1), strings.Replace(base, `"service":"repos"`, `"service":null`, 1), strings.Replace(base, `"event":"repo.pushed"`, `"event":"bad"`, 1), strings.Replace(base, `.123456Z`, `.123Z`, 1), strings.Replace(base, `.123456Z`, `.123456+00:00`, 1), base[:len(base)-1] + `,"extra":1}`, base[:len(base)-1] + `,"seq":1}`, base[:len(base)-1] + `,"seq":1,"received":"0001-01-01T00:00:00.000000Z"}`, base[:len(base)-1] + `,"seq":1.0,"received":"2025-01-01T00:00:00.000000Z"}`, base[:len(base)-1] + `,"seq":9223372036854775808,"received":"2025-01-01T00:00:00.000000Z"}`, strings.Replace(base, "repos", string([]byte{0xff}), 1)}
	for _, s := range invalid {
		e := record()
		before := record()
		if err := e.UnmarshalJSON([]byte(s)); err == nil {
			t.Fatalf("accepted %q", s)
		}
		if !reflect.DeepEqual(e, before) {
			t.Fatalf("mutated on %q", s)
		}
	}
	for _, key := range []string{"id", "time", "service", "event", "request_id", "user", "attrs", "cause", "depth"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, key)
		bad, _ := json.Marshal(fields)
		e := record()
		if err := e.UnmarshalJSON(bad); err == nil {
			t.Fatalf("missing %s accepted", key)
		}
	}
}

// R-GSWV-7PAF R-GU4R-LH14
func TestAttributeDecodeAndEscapes(t *testing.T) {
	base, _ := record().MarshalJSON()
	cases := []struct {
		text string
		want any
	}{{"-0", math.Copysign(0, -1)}, {"-9223372036854775808", int64(math.MinInt64)}, {"9223372036854775807", int64(math.MaxInt64)}, {"18446744073709551615", uint64(math.MaxUint64)}, {"18446744073709551616", float64(18446744073709551616)}, {"1e2", float64(100)}, {"1.5", float64(1.5)}, {"true", true}, {"false", false}, {`"\ud800"`, "�"}}
	for _, tc := range cases {
		data := bytes.Replace(base, []byte(`"count":3`), []byte(`"count":`+tc.text), 1)
		var e events.Event
		if err := e.UnmarshalJSON(data); err != nil {
			t.Fatal(err)
		}
		got := e.Attrs["count"]
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s -> %#v want %#v", tc.text, got, tc.want)
		}
		if tc.text == "-0" && !math.Signbit(got.(float64)) {
			t.Fatal("lost negative zero")
		}
	}
	data := bytes.Replace(base, []byte(`"user":"user"`), []byte(`"user":"\ud800"`), 1)
	var e events.Event
	if err := e.UnmarshalJSON(data); err != nil || e.User != "�" || e.Attrs == nil || e.Seq != 0 || !e.Received.IsZero() || e.Time.Location() != time.UTC {
		t.Fatalf("decoded %+v %v", e, err)
	}
}

// R-GGPV-DZVH R-GLLG-X2U9 R-GWKK-D0II
func TestBasicAttributesAndCanonicalEscaping(t *testing.T) {
	e := record()
	e.RequestID = "<&\"\n"
	e.User = "é"
	e.Attrs = events.Attrs{"z": customRecordString("<&\"\n"), "a": uint64(math.MaxUint64), "negative": math.Copysign(0, -1), "b": customRecordBool(true), "i": customRecordInt(-7), "f": customRecordFloat(1.25)}
	data, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	expected := struct {
		ID        string         `json:"id"`
		Time      string         `json:"time"`
		Service   string         `json:"service"`
		Event     string         `json:"event"`
		RequestID string         `json:"request_id"`
		User      string         `json:"user"`
		Attrs     map[string]any `json:"attrs"`
		Cause     string         `json:"cause"`
		Depth     int            `json:"depth"`
	}{e.ID, e.Time.UTC().Format("2006-01-02T15:04:05.000000Z"), e.Service, e.Name, e.RequestID, e.User, map[string]any{"z": string(e.Attrs["z"].(customRecordString)), "a": uint64(math.MaxUint64), "negative": math.Copysign(0, -1), "b": true, "i": int64(-7), "f": float64(1.25)}, e.Cause, e.Depth}
	want, err := json.Marshal(expected)
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("got %s want %s err %v", data, want, err)
	}
	for _, delivered := range []bool{false, true} {
		if delivered {
			e.Seq = 9
			e.Received = e.Time
		}
		data, err = e.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var got events.Event
		if err = got.UnmarshalJSON(data); err != nil {
			t.Fatal(err)
		}
		again, err := got.MarshalJSON()
		if err != nil || !bytes.Equal(data, again) {
			t.Fatalf("roundtrip %s %v", again, err)
		}
		if delivered && (got.Seq != e.Seq || !got.Received.Equal(e.Received.Truncate(time.Microsecond))) {
			t.Fatalf("delivered fields %+v", got)
		}
	}
}

type customRecordString string

func (customRecordString) MarshalJSON() ([]byte, error) { return []byte(`"wrong"`), nil }

type customRecordBool bool

func (customRecordBool) MarshalText() ([]byte, error) { return []byte("wrong"), nil }

type customRecordInt int

func (customRecordInt) MarshalJSON() ([]byte, error) { return []byte(`999`), nil }

type customRecordFloat float32

func (customRecordFloat) MarshalJSON() ([]byte, error) { return []byte(`999`), nil }

// R-GP96-2E2C R-GQH2-G5T1 R-SXAR-YVVY R-GVCN-Z8RT
func TestMoreInvalidEventTexts(t *testing.T) {
	e := record()
	e.Seq = 1
	e.Received = e.Time
	data, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	base := string(data)
	cases := []string{strings.Replace(base, `"seq":1`, `"seq":0`, 1), strings.Replace(base, `"seq":1`, `"seq":-1`, 1), strings.Replace(base, `"seq":1`, `"seq":1,"s\u0065q":1`, 1), strings.Replace(base, `"received":`, `"received":"2025-01-01T00:00:00.000000Z","received":`, 1), strings.Replace(base, `"depth":0`, `"depth":99999999999999999999999999`, 1), strings.Replace(base, `"attrs":{"count":3}`, `"attrs":{"count":[]}`, 1), strings.Replace(base, `"attrs":{"count":3}`, `"attrs":{"count":{}}`, 1), strings.Replace(base, `"attrs":{"count":3}`, `"attrs":[]`, 1), strings.Replace(base, `"id":"evt_0123456789abcdef"`, `"id":false`, 1), strings.Replace(base, `"time":"2025-02-03T03:05:06.123456Z"`, `"time":0`, 1), strings.Replace(base, `"request_id":"request"`, `"request_id":null`, 1), strings.Replace(base, `"user":"user"`, `"user":[]`, 1), strings.Replace(base, `"cause":""`, `"cause":false`, 1), strings.Replace(base, `2025-02-03`, `2025-02-30`, 1)}
	for _, text := range cases {
		got := record()
		before := record()
		if err = got.UnmarshalJSON([]byte(text)); err == nil || !reflect.DeepEqual(got, before) {
			t.Fatalf("accepted or mutated on %s: %+v %v", text, got, err)
		}
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "received")
	bad, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.UnmarshalJSON(bad); err == nil {
		t.Fatal("missing received accepted")
	}
}
