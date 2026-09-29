package codex

import (
	"encoding/json"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"
)

func newStartSession(t *testing.T, f *observedFS) (stamp time.Time, known bool) {
	t.Helper()
	got, err := List(f, home)
	if err != nil || len(got) != 1 {
		t.Fatalf("List: %+v %v", got, err)
	}
	return got[0].Started, got[0].HasStarted
}

func TestNewMetaStarts(t *testing.T) {
	// R-EODJ-505D R-KBNU-W724 R-MYFK-S6HX
	for _, raw := range []string{`"2026-09-14T05:13:21Z"`, `"2026-09-14T05:13:21.000Z"`, `"2026-09-14T00:13:21-05:00"`, `"2026-09-14T05:13:21.123456789Z"`, "", "null", "123", "{}", "[]", "true", `"2026-09-14 05:13:21"`, `"1790195467000"`, `""`} {
		t.Run(raw, func(t *testing.T) {
			f := fixture()
			f.lock("root", 1, 42)
			extra := ""
			if raw != "" {
				extra = `,"timestamp":` + raw
			}
			f.rollout("root", "2026/09/14", "rollout-a", `{"type":"session_meta"`+extra+`,"payload":{}}`+"\n")
			got, known := newStartSession(t, f)
			var value string
			_ = json.Unmarshal([]byte(raw), &value)
			want, err := time.Parse(time.RFC3339Nano, value)
			if known != (err == nil) || known && got.Compare(want) != 0 {
				t.Fatalf("start %v %v, want %v (%v)", got, known, want, err)
			}
		})
	}
	for _, content := range []string{"missing", "unreadable", "", "{}\n", "{broken\n", "{\"type\":\"session_meta\",\"timestamp\":\"2026-09-14T05:13:21Z\"}"} {
		t.Run(content, func(t *testing.T) {
			f := fixture()
			f.lock("root", 1, 42)
			if content != "missing" {
				f.rollout("root", "2026/09/14", "rollout-a", content)
			}
			if content == "unreadable" {
				f.fail["read:"+sessions+"2026/09/14/rollout-a-root.jsonl"] = fs.ErrPermission
			}
			_, known := newStartSession(t, f)
			if known {
				t.Fatal("unusable or no-meta rollout has start")
			}
		})
	}
}

func TestNewMetaStartIndependence(t *testing.T) {
	// R-P7VE-LZBA
	for _, tc := range []struct {
		first string
		known bool
	}{
		{`{"type":"session_meta","timestamp":"2026-09-14T05:13:21.123456789Z","payload":{"timestamp":"2040-01-01T00:00:00Z"}}`, true},
		{`{"type":"session_meta","payload":{"timestamp":"2040-01-01T00:00:00Z"}}`, false},
		{`{"type":"event_msg","timestamp":"2026-09-14T05:13:21Z"}`, false},
	} {
		for _, variant := range []string{"alone", "appended", "different names and index"} {
			t.Run(tc.first+variant, func(t *testing.T) {
				f := fixture()
				f.lock("root", 1, 42)
				date, filename := "2026/09/14", "rollout-a"
				content := "not JSON\n[]\n" + tc.first + "\n"
				if variant == "appended" {
					content += `{"type":"session_meta","timestamp":"2000-01-01T00:00:00Z","payload":{"timestamp":"2001-01-01T00:00:00Z"}}` + "\n" + `{"type":"event_msg","timestamp":"2040-01-01T00:00:00Z"}` + "\n"
				}
				if variant == "different names and index" {
					date, filename = "2000/01/01", "rollout-2000-01-01T00-00-00"
					f.MapFS[index] = &fstest.MapFile{Data: []byte(`{"id":"root","timestamp":"2040-01-01T00:00:00Z","thread_name":"renamed"}` + "\n")}
				}
				f.rollout("root", date, filename, content)
				got, known := newStartSession(t, f)
				want, err := time.Parse(time.RFC3339Nano, "2026-09-14T05:13:21.123456789Z")
				if err != nil {
					t.Fatal(err)
				}
				if known != tc.known || known && got.Compare(want) != 0 {
					t.Fatalf("start depends on another source: %v %v", got, known)
				}
			})
		}
	}
}
