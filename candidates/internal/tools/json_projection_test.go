package tools

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func encodedJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// R-RIUB-88I4 R-RK27-M08T
func TestDerivedAnswers(t *testing.T) {
	for _, tc := range []struct{ kind, source, found string }{
		{enrichers.KindLinkFacts, enrichers.SourceLinks, `{ "categories": [], "fleet_verdict": "unknown" }`},
		{enrichers.KindDriveMinutes, enrichers.SourceZipDrive, `{}`},
		{enrichers.KindDriveMinutes, enrichers.SourceZipDrive, ``},
	} {
		e := store.Enrichment{Kind: tc.kind, Source: tc.source, Inputs: "chosen-inputs", Found: json.RawMessage(tc.found), Cost: .25}
		got := enrichmentOf(e)
		compact := new(bytes.Buffer)
		if tc.found == "" {
			compact.WriteString(`{}`)
		} else if err := json.Compact(compact, []byte(tc.found)); err != nil {
			t.Fatal(err)
		}
		want := Enrichment{tc.kind, tc.source, "chosen-inputs", json.RawMessage(compact.Bytes()), "", "", "", 0, nil, "", "", .25, stamp(e.At)}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]json.RawMessage
		if err = json.Unmarshal(b, &obj); err != nil {
			t.Fatal(err)
		}
		if len(obj) != 6 || string(obj["found"]) != compact.String() {
			t.Fatal(string(b))
		}
	}
}

// R-RIUB-88I4 R-RK27-M08T
func TestFullEnrichmentEncodingAndLegacyWebsiteMembers(t *testing.T) {
	size := int64(3)
	v := Enrichment{"kind", "source", "inputs", json.RawMessage(`{}`), "state", "url", "title", 200, &size, "error", "domain", .25, "at"}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"kind","source":"source","inputs":"inputs","found":{},"state":"state","url":"url","title":"title","status_code":200,"size":3,"error":"error","domain":"domain","cost":0.25,"at":"at"}`
	if string(b) != want {
		t.Fatal(string(b))
	}
	e := store.Enrichment{Kind: sources.WebsiteCheck, Source: "fixture.test", Inputs: "fixture", Found: json.RawMessage(`{"state":"none","url":"","title":"","status_code":0,"size":0,"error":"","domain":"legacy.test","cost":9,"checked":"old"}`)}
	got := enrichmentOf(e)
	if got.State != sources.WebsiteNone || got.Domain != e.Source || got.Found != nil || got.Size == nil || *got.Size != 0 {
		t.Fatal(got)
	}
	b, err = json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(b, &obj); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"found", "url", "title", "status_code", "error", "checked"} {
		if _, ok := obj[k]; ok {
			t.Fatal(string(b))
		}
	}
}

// R-KAXF-Q4QL
func TestBriefFoundNormalizesEmptyArrays(t *testing.T) {
	e := store.Enrichment{Kind: enrichers.KindBrief, Source: enrichers.SourcePrompts, Inputs: "fixture-inputs", Found: encodedJSON(enrichers.BriefFound{Run: "fixture-run", Status: "failed"})}
	got := enrichmentOf(e)
	var found map[string]json.RawMessage
	if err := json.Unmarshal(got.Found, &found); err != nil {
		t.Fatal(err)
	}
	if string(found["categories"]) != "[]" || string(found["labels"]) != "[]" {
		t.Fatal(string(got.Found))
	}
	if _, ok := found["exit_code"]; ok {
		t.Fatal(string(got.Found))
	}
}
