package mcp_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
)

func resultBytes(t *testing.T, r mcp.Result) string {
	t.Helper()
	b, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestResultConstructors(t *testing.T) {
	// R-KDG0-VPKW: construct the opaque struct and call all public methods.
	r := mcp.Result{}
	if err := r.UnmarshalJSON([]byte(`{"custom":1}`)); err != nil {
		t.Fatal(err)
	}
	if r.IsError() || resultBytes(t, r) != `{"custom":1}` {
		t.Fatal("opaque result methods failed")
	}
	// R-KENX-9HBL R-LRFW-KAZD
	if got := resultBytes(t, mcp.ErrorResult("bad\n\"value")); got != `{"content":[{"type":"text","text":"bad\n\"value"}],"isError":true}` {
		t.Fatal(got)
	}
	// R-KFVT-N92A R-LSNS-Y2Q2
	if got := resultBytes(t, mcp.TextResult("ok\n\"value")); got != `{"content":[{"type":"text","text":"ok\n\"value"}]}` {
		t.Fatal(got)
	}
	// R-LV3L-PM7G
	var zero mcp.Result
	if got := resultBytes(t, zero); got != `{"content":[]}` || zero.IsError() {
		t.Fatal(got)
	}
}

func TestResultRejectsAtomically(t *testing.T) {
	// R-LWBI-3DY5
	for _, input := range []string{`null`, `[]`, `1`, `true`, `"text"`, `{`, `{} {}`, `{"a":1,"a":2}`, `{"resultType":"a","resultType":"b"}`} {
		r := mcp.TextResult("retained")
		before := resultBytes(t, r)
		if err := r.UnmarshalJSON([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
		if got := resultBytes(t, r); got != before {
			t.Fatalf("changed after %s: %s", input, got)
		}
	}
}

func TestResultPreservesOrderedValues(t *testing.T) {
	// R-LXJE-H5OU
	var r mcp.Result
	if err := json.Unmarshal([]byte(`{"z": { "inner" : [ 1, true ] }, "resultType":"complete", "content" : [], "extra":"a b", "isError": false}`), &r); err != nil {
		t.Fatal(err)
	}
	if got := resultBytes(t, r); got != `{"z":{"inner":[1,true]},"content":[],"extra":"a b","isError":false}` {
		t.Fatal(got)
	}
	if err := r.UnmarshalJSON([]byte(`{"resultType":"complete"}`)); err != nil {
		t.Fatal(err)
	}
	if got := resultBytes(t, r); got != `{}` {
		t.Fatal(got)
	}
}

func TestResultErrorFlag(t *testing.T) {
	// R-LYRA-UXFJ
	for _, input := range []string{`{}`, `{"isError":false}`, `{"isError":null}`, `{"isError":"true"}`, `{"isError":1}`, `{"isError":{}}`, `{"isError":true}`} {
		var r mcp.Result
		if err := r.UnmarshalJSON([]byte(input)); err != nil {
			t.Fatal(err)
		}
		if got := r.IsError(); got != (input == `{"isError":true}`) {
			t.Fatalf("%s: %t", input, got)
		}
	}
}

func TestResultConcurrentReadersAndCopy(t *testing.T) {
	// R-LZZ7-8P68
	var r mcp.Result
	if err := r.UnmarshalJSON([]byte(`{"isError":true,"content":[],"extra":{"a":1}}`)); err != nil {
		t.Fatal(err)
	}
	copyOf := r
	want := resultBytes(t, r)
	if resultBytes(t, copyOf) != want {
		t.Fatal("copy differs")
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				b, err := r.MarshalJSON()
				if err != nil || string(b) != want || !r.IsError() {
					t.Error("concurrent reader changed result")
				}
			}
		})
	}
	wg.Wait()
}
