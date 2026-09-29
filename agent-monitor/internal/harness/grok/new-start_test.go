package grok_test

import (
	"io/fs"
	"testing"
	"time"
)

func TestNewIndexStarts(t *testing.T) {
	// R-3ZJG-I3EF R-40RC-VV54 R-MYFK-S6HX
	cases := []struct {
		raw  string
		want string
	}{
		{`"2026-09-23T20:31:07.123456789Z"`, "2026-09-23T20:31:07.123456789Z"},
		{`"2026-09-23T20:31:07Z"`, "2026-09-23T20:31:07Z"},
		{`"2026-09-23T15:31:07-05:00"`, "2026-09-23T20:31:07Z"},
		{"", ""}, {"null", ""}, {"123", ""}, {"true", ""}, {"{}", ""}, {"[]", ""}, {`""`, ""}, {`"2026-09-23 20:31:07"`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			extra := ""
			if tc.raw != "" {
				extra = `,"opened_at":` + tc.raw
			}
			got := mustList(t, root(`[{"session_id":"a","pid":42`+extra+`}]`))
			if len(got) != 1 {
				t.Fatalf("sessions: %+v", got)
			}
			want, err := time.Parse(time.RFC3339Nano, tc.want)
			if got[0].HasStarted != (err == nil) || got[0].HasStarted && got[0].Started.Compare(want) != 0 {
				t.Fatalf("start: %+v", got[0])
			}
		})
	}
}

func TestNewIndexStartIndependence(t *testing.T) {
	// R-E1R3-XO1J
	for _, raw := range []string{`"2026-09-23T20:31:07Z"`, "null"} {
		for _, variant := range []string{"missing", "content", "unreadable", "different process"} {
			t.Run(raw+variant, func(t *testing.T) {
				m := root(`[{"session_id":"a","pid":42,"opened_at":` + raw + `}]`)
				summary, events := base+"sessions/p/a/summary.json", base+"sessions/p/a/events.jsonl"
				if variant != "missing" {
					m[summary] = file(`{"generated_title":"different","opened_at":"2030-01-01T00:00:00Z"}`)
					m[events] = file(`{"type":"turn_started","ts":"2040-01-01T00:00:00Z","opened_at":"2040-01-01T00:00:00Z"}` + "\n")
				}
				if variant == "different process" {
					m["proc/stat"] = file("btime 2000\n")
				}
				var injected fs.FS = m
				if variant == "unreadable" {
					injected = &observed{files: m, fail: map[string]error{summary: fs.ErrPermission, events: fs.ErrPermission}}
				}
				got := mustList(t, injected)
				if len(got) != 1 {
					t.Fatalf("sessions: %+v", got)
				}
				want, err := time.Parse(time.RFC3339Nano, "2026-09-23T20:31:07Z")
				if err != nil {
					t.Fatal(err)
				}
				if got[0].HasStarted != (raw != "null") || got[0].HasStarted && got[0].Started.Compare(want) != 0 {
					t.Fatalf("start depends on optional source: %+v", got[0])
				}
			})
		}
	}
}
