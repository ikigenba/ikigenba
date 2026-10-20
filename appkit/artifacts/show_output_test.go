package artifacts_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

// R-66BT-9QFZ R-653W-VYPA
func TestShowSuccess(t *testing.T) {
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, expiry := range []string{"", `,"expires":"2028-02-03T06:05:06+02:00"`} {
			for _, isError := range []string{"false", "null", "1", `"true"`} {
				body := `{"id":null,` + strings.TrimPrefix(strings.TrimSuffix(metadataJSON(), "}"), "{") + expiry + `,"ID":null,"Bytes":false}`
				result := `{"isError":` + isError + `,"structuredContent":` + body + `,"content":[{"type":"text","text":"ignored"}],"future":null}`
				m, err := showCall(context.Background(), t, showAnswer(t, media, result), "input")
				want := artifacts.Metadata{ID: "artifact-id", Path: "saved/path", Bytes: 7, ContentType: "binary/test", SHA256: "sum", Owner: "owner", Created: fixedTime, Modified: fixedTime, URL: "https://file.invalid"}
				if expiry != "" {
					want.Expires = fixedTime
				}
				if err != nil || m != want || m.Created.Location() != time.UTC || m.Modified.Location() != time.UTC || (!m.Expires.IsZero() && m.Expires.Location() != time.UTC) {
					t.Fatalf("metadata %+v, want %+v: %v", m, want, err)
				}
			}
		}
	}
}

func showMutate(t *testing.T, key, value string, remove bool) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(metadataJSON()), &fields); err != nil {
		t.Fatal(err)
	}
	if remove {
		delete(fields, key)
	} else {
		fields[key] = json.RawMessage(value)
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// R-69ZI-F1O2 R-653W-VYPA
func TestShowMalformedOutputs(t *testing.T) {
	results := []string{`{}`, `{"structuredContent":null}`, `{"structuredContent":[]}`, `{"structuredContent":1}`, `{"structuredContent":true}`, `{"structuredContent":"text"}`, `{"structuredContent":{},"content":[{"type":"text","text":` + fmt.Sprintf("%q", metadataJSON()) + `}]}`, `{"isError":true,"isError":true}`, `{"structuredContent":{},"structuredContent":{}}`}
	for _, key := range []string{"id", "path", "bytes", "content_type", "sha256", "owner", "created", "modified", "url"} {
		results = append(results, `{"structuredContent":`+showMutate(t, key, "", true)+`}`)
	}
	for _, key := range []string{"id", "path", "bytes", "content_type", "sha256", "owner", "created", "modified", "expires", "url"} {
		for _, bad := range []string{"null", "true", "[]", "{}"} {
			results = append(results, `{"structuredContent":`+showMutate(t, key, bad, false)+`}`)
		}
	}
	for key, bad := range map[string]string{"bytes": "9223372036854775808", "created": `"yesterday"`, "modified": `"invalid"`, "expires": `"invalid"`} {
		results = append(results, `{"structuredContent":`+showMutate(t, key, bad, false)+`}`)
	}
	results = append(results, `{"structuredContent":`+strings.TrimSuffix(metadataJSON(), "}")+`,"id":null}}`)
	for _, media := range []string{"application/json", "text/event-stream"} {
		for i, result := range results {
			t.Run(fmt.Sprintf("%s/%d", media, i), func(t *testing.T) {
				_, err := showCall(context.Background(), t, showAnswer(t, media, result), "path")
				showMalformed(t, err)
			})
		}
	}
}

// R-608B-CVQI R-67JP-NI6O R-68RM-19XD
func TestShowRefusals(t *testing.T) {
	fixtures := []struct{ result, text string }{
		{`{"isError":true}`, ""}, {`{"isError":true,"content":null}`, ""}, {`{"isError":true,"content":{}}`, ""},
		{`{"isError":true,"content":[{"type":"text","text":"first"},{"type":"image","text":"ignore"},{"type":"text","text":null},null,1,[],{"Type":"text","text":"ignore"},{"type":"text","Text":"ignore"},{"type":"text","text":""},{"type":"text","text":"last\nline"}],"structuredContent":{}}`, "first\n\nlast\nline"},
		{`{"isError":true,"content":[{"type":"text","text":"old","text":"new"},{"type":"image","type":"text","text":"second"}]}`, "new\nsecond"},
	}
	for _, text := range []string{"not_found: missing", "invalid: bad", "unavailable: retry", "日本\nline"} {
		raw, _ := json.Marshal(text)
		fixtures = append(fixtures, struct{ result, text string }{`{"isError":true,"content":[{"type":"text","text":` + string(raw) + `}],"structuredContent":null}`, text})
	}
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, f := range fixtures {
			_, err := showCall(context.Background(), t, showAnswer(t, media, f.result), "bad path")
			var ref *artifacts.Refusal
			var he *artifacts.HTTPError
			var rpc *mcp.RPCError
			if !errors.As(err, &ref) || errors.As(err, &he) || errors.As(err, &rpc) || showSentinel(err) || ref.Text != f.text || ref.Error() != "artifacts: "+f.text {
				t.Fatalf("refusal %q: %v", f.text, err)
			}
		}
	}
	ref := artifacts.Refusal{"sample"}
	var shape struct{ Text string } = ref
	method := (*artifacts.Refusal).Error
	if shape.Text != "sample" || method(&ref) != "artifacts: sample" {
		t.Fatal(ref)
	}
}

// R-66BT-9QFZ
func TestShowPreservesMetadataValues(t *testing.T) {
	body := `{"id":"","path":"a b?日本","bytes":-7,"content_type":"","sha256":"arbitrary","owner":"","created":"2028-02-03T06:05:06+02:00","modified":"2028-02-03T06:05:06+02:00","url":"unaltered?url"}`
	m, err := showCall(context.Background(), t, showAnswer(t, "application/json", `{"structuredContent":`+body+`}`), "path")
	want := artifacts.Metadata{Path: "a b?日本", Bytes: -7, SHA256: "arbitrary", Created: fixedTime, Modified: fixedTime, URL: "unaltered?url"}
	if err != nil || m != want {
		t.Fatal(m, err)
	}
}
