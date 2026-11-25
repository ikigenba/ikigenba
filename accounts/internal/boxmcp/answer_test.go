package boxmcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestDeclarations(t *testing.T) {
	// R-6X2V-27BF, R-6YAR-FZ24, R-6ZIN-TQST
	type command string
	const (
		socket command = boxmcp.SocketPath
		env    command = boxmcp.EnvSocket
		server command = boxmcp.ServerName
		init   command = boxmcp.MethodInitialize
		ping   command = boxmcp.MethodPing
		limit  int64   = boxmcp.MaxAnswerLine
		parse  int64   = boxmcp.CodeParseError
		bad    int64   = boxmcp.CodeInvalidRequest
		method int64   = boxmcp.CodeMethodNotFound
		refuse int64   = boxmcp.CodeRefused
	)
	if socket != "/run/accountsd.sock" || env != "ACCOUNTSD_SOCKET" || server != "accounts" ||
		init != "initialize" || ping != "ping" {
		t.Fatal("socket or protocol declaration differs")
	}
	if limit != protocol.MaxBoxAnswer+1024 || boxmcp.AnswerTimeout != 30*time.Second ||
		reflect.TypeOf(boxmcp.AnswerTimeout) != reflect.TypeFor[time.Duration]() {
		t.Fatal("answer bound differs")
	}
	if parse != -32700 || bad != -32600 || method != -32601 || refuse != -32000 {
		t.Fatal("JSON-RPC code differs")
	}

	// R-70QK-7IJI
	versions := boxmcp.ProtocolVersions()
	want := []string{"2025-11-25", "2025-06-18"}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("versions: %v", versions)
	}
	versions[0] = "other"
	if got := boxmcp.ProtocolVersions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("versions changed through caller: %v", got)
	}

	// R-71YG-LAA7, R-76U2-4D8Z
	assertFields(t, reflect.TypeFor[boxmcp.Answer](), []reflect.StructField{
		{Name: "Message", Type: reflect.TypeFor[json.RawMessage](), Tag: `json:"message,omitempty"`},
		{Name: "Refusal", Type: reflect.TypeFor[string](), Tag: `json:"refusal,omitempty"`},
	})
	assertFields(t, reflect.TypeFor[boxmcp.Config](), []reflect.StructField{
		{Name: "Socket", Type: reflect.TypeFor[string]()},
		{Name: "Timeout", Type: reflect.TypeFor[time.Duration]()},
	})
	if reflect.TypeOf(boxmcp.Serve) != reflect.TypeFor[func(context.Context, io.Reader, io.Writer, boxmcp.Config) error]() ||
		reflect.TypeOf(boxmcp.Run) != reflect.TypeFor[func(context.Context, io.Reader, io.Writer, func(string) string) error]() {
		t.Fatal("stdio signatures differ")
	}
	// R-75M5-QLIA
	if boxmcp.ErrAnswer == nil ||
		reflect.TypeOf(boxmcp.WriteAnswer) != reflect.TypeFor[func(io.Writer, boxmcp.Answer) error]() {
		t.Fatal("answer writer declaration differs")
	}
	// R-736C-Z20W
	copyTexts := []string{boxmcp.NotAnswering, boxmcp.MsgParseError, boxmcp.MsgInvalidRequest, boxmcp.MsgMethodNotFound, boxmcp.MsgTooLarge}
	seen := make(map[string]bool)
	for _, copyText := range copyTexts {
		if copyText == "" || seen[copyText] {
			t.Fatalf("empty or duplicate message %q", copyText)
		}
		seen[copyText] = true
	}
}

func assertFields(t *testing.T, structure reflect.Type, want []reflect.StructField) {
	t.Helper()
	if structure.Kind() != reflect.Struct || structure.NumField() != len(want) {
		t.Fatalf("unexpected shape for %v", structure)
	}
	for i, field := range want {
		got := structure.Field(i)
		if got.Name != field.Name || got.Type != field.Type || got.Tag != field.Tag {
			t.Errorf("%v field %d: %+v", structure, i, got)
		}
	}
}

// R-781Y-I4ZO
func TestAnswerEnvelopeJSON(t *testing.T) {
	for _, c := range []struct {
		answer boxmcp.Answer
		json   string
	}{
		{boxmcp.Answer{Refusal: "x"}, `{"refusal":"x"}`},
		{boxmcp.Answer{Message: json.RawMessage(`{"sample":1}`)}, `{"message":{"sample":1}}`},
		{boxmcp.Answer{}, `{}`},
	} {
		encoded, err := json.Marshal(c.answer)
		if err != nil || string(encoded) != c.json {
			t.Fatalf("Marshal = %s, %v", encoded, err)
		}
	}
	var answer boxmcp.Answer
	if err := json.Unmarshal([]byte(`{"refusal":"x","later":1}`), &answer); err != nil ||
		answer.Refusal != "x" || len(answer.Message) != 0 {
		t.Fatalf("Unmarshal = %+v, %v", answer, err)
	}
}

// R-799U-VWQD
func TestWriteAnswerVerbatim(t *testing.T) {
	message := json.RawMessage(`{ "jsonrpc":"2.0", "id": 1, "result":{"a":"<b>&"} }`)
	var encoded bytes.Buffer
	if err := boxmcp.WriteAnswer(&encoded, boxmcp.Answer{Message: message}); err != nil {
		t.Fatal(err)
	}
	if want := `{"message":` + string(message) + "}\n"; encoded.String() != want {
		t.Fatalf("message line: %q, want %q", encoded.String(), want)
	}
	for _, refusal := range []string{"x < y & y > z", "\\" + `"` + "\n\r\t\f\b\x00\u2028\u2029\uFFFD\xff", "international 世界"} {
		var value bytes.Buffer
		e := json.NewEncoder(&value)
		e.SetEscapeHTML(false)
		if err := e.Encode(refusal); err != nil {
			t.Fatal(err)
		}
		encoded.Reset()
		if err := boxmcp.WriteAnswer(&encoded, boxmcp.Answer{Message: json.RawMessage("null"), Refusal: refusal}); err != nil {
			t.Fatal(err)
		}
		if want := `{"refusal":` + strings.TrimSuffix(value.String(), "\n") + "}\n"; encoded.String() != want {
			t.Errorf("refusal line: %q, want %q", encoded.String(), want)
		}
	}
}

// R-7AHR-9OH2
func TestWriteAnswerValidation(t *testing.T) {
	message := func(n int) json.RawMessage {
		return json.RawMessage(`{"x":"` + strings.Repeat("z", n-len(`{"x":""}`)) + `"}`)
	}
	refusalLimit := boxmcp.MaxAnswerLine - len(`{"refusal":""}`)
	for index, answer := range []boxmcp.Answer{
		{},
		{Message: json.RawMessage("null")},
		{Message: json.RawMessage("{}"), Refusal: "both"},
		{Message: json.RawMessage(" {}")},
		{Message: json.RawMessage("{} ")},
		{Message: json.RawMessage("[]")},
		{Message: json.RawMessage("{x}")},
		{Message: json.RawMessage("{}{}")},
		{Message: json.RawMessage("{\n}")},
		{Message: json.RawMessage("{\r}")},
		{Message: message(protocol.MaxBoxAnswer + 1)},
		{Refusal: strings.Repeat("x", refusalLimit+1)},
		{Refusal: strings.Repeat("\x00", refusalLimit/6+1)},
	} {
		var out bytes.Buffer
		err := boxmcp.WriteAnswer(&out, answer)
		if !errors.Is(err, boxmcp.ErrAnswer) || out.Len() != 0 {
			t.Errorf("case %d: wrote %d bytes, returned %v", index, out.Len(), err)
		}
	}
	for _, test := range []struct {
		answer boxmcp.Answer
		size   int
	}{
		{boxmcp.Answer{Message: message(protocol.MaxBoxAnswer)}, protocol.MaxBoxAnswer + len("{\"message\":}\n")},
		{boxmcp.Answer{Refusal: strings.Repeat("x", refusalLimit)}, boxmcp.MaxAnswerLine + 1},
	} {
		var out bytes.Buffer
		if err := boxmcp.WriteAnswer(&out, test.answer); err != nil || out.Len() != test.size {
			t.Fatalf("at limit: wrote %d bytes, returned %v", out.Len(), err)
		}
	}
	writeErr := errors.New("test write failure")
	if err := boxmcp.WriteAnswer(failedWriter{writeErr}, boxmcp.Answer{Refusal: "x"}); !errors.Is(err, writeErr) {
		t.Fatalf("write error: %v", err)
	}
}

type failedWriter struct{ err error }

func (w failedWriter) Write([]byte) (int, error) { return 0, w.err }
