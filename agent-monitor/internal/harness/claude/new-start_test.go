package claude_test

import (
	"io/fs"
	"testing"
	"time"
)

func TestNewRegistrationStarts(t *testing.T) {
	// R-NP8G-9BZ6 R-NQGC-N3PV R-MYFK-S6HX
	cases := []struct {
		raw    string
		valid  bool
		millis int64
	}{
		{"1790195467000", true, 1790195467000}, {"1790201442000", true, 1790201442000},
		{"0", true, 0}, {"-1", true, -1}, {"9223372036854775807", true, 9223372036854775807},
		{"-9223372036854775808", true, -9223372036854775808},
		{"1.790195467e12", true, 1790195467000}, {"1790195467000.0", true, 1790195467000},
		{"", false, 0}, {"null", false, 0}, {`"1790195467000"`, false, 0}, {"true", false, 0},
		{"{}", false, 0}, {"[]", false, 0}, {"1790195467000.5", false, 0},
		{"9223372036854775808", false, 0}, {"-9223372036854775809", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			m := fixture()
			extra := ""
			if tc.raw != "" {
				extra = `,"startedAt":` + tc.raw
			}
			put(m, registry+"a.json", `{"sessionId":"alpha","pid":42,"procStart":"150"`+extra+`}`)
			got := only(t, m)
			if got.HasStarted != tc.valid || tc.valid && got.Started.Compare(time.UnixMilli(tc.millis)) != 0 {
				t.Fatalf("start: %+v", got)
			}
		})
	}
}

func TestNewRegistrationStartIndependence(t *testing.T) {
	// R-NRO9-0VGK
	for _, raw := range []string{"1790195467000", "null"} {
		for _, variant := range []string{"missing", "content", "unreadable", "changed process and fields"} {
			t.Run(raw+variant, func(t *testing.T) {
				m := fixture()
				fields := `"sessionId":"alpha","pid":42,"procStart":"150","startedAt":` + raw
				if variant == "changed process and fields" {
					fields = `"sessionId":"alpha","pid":42,"procStart":"900","startedAt":` + raw + `,"name":"other","cwd":"/other","status":"busy"`
					put(m, "proc/stat", "btime 2000\n")
					put(m, "proc/42/stat", "42 (different) "+"0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 800")
				}
				put(m, registry+"a.json", "{"+fields+"}")
				if variant != "missing" {
					put(m, projects+"p/alpha.jsonl", `{"timestamp":"2030-01-01T00:00:00Z","startedAt":0}`+"\n")
				}
				var root fs.FS = m
				if variant == "unreadable" {
					root = &observedFS{MapFS: m, failFile: projects + "p/alpha.jsonl", failErr: fs.ErrPermission}
				}
				got := only(t, root)
				if got.HasStarted != (raw != "null") || got.HasStarted && got.Started.Compare(time.UnixMilli(1790195467000)) != 0 {
					t.Fatalf("start depends on other facts: %+v", got)
				}
			})
		}
	}
}
