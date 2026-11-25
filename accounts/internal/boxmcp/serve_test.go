package boxmcp_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
	listRequest = `{ "jsonrpc":"2.0", "id":7, "method":"` + protocol.MethodToolsList + `" }`
	callRequest = `{"jsonrpc":"2.0","id":"a\u0062","method":"` + protocol.MethodToolsCall + `"}`
	pingRequest = `{"jsonrpc":"2.0","id":42,"method":"` + boxmcp.MethodPing + `"}`
)

type response struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func responses(t *testing.T, out string) []response {
	t.Helper()
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	if lines[len(lines)-1] != "" {
		t.Fatalf("output has no final LF: %q", out)
	}
	result := make([]response, 0, len(lines)-1)
	for _, line := range lines[:len(lines)-1] {
		var got response
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("invalid response %q: %v", line, err)
		}
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != 3 || got.Version != "2.0" || len(got.ID) == 0 ||
			(got.Error == nil) == (got.Result == nil) {
			t.Fatalf("malformed JSON-RPC response %q", line)
		}
		if got.Error != nil {
			var detail map[string]json.RawMessage
			if err := json.Unmarshal(fields["error"], &detail); err != nil || len(detail) != 2 ||
				!json.Valid(detail["code"]) || len(detail["message"]) == 0 || detail["message"][0] != '"' {
				t.Fatalf("malformed JSON-RPC error %q", line)
			}
		}
		result = append(result, got)
	}
	return result
}

func expectError(t *testing.T, got response, id string, code int, message string) {
	t.Helper()
	if string(got.ID) != id || got.Error == nil || got.Error.Code != code || got.Error.Message != message {
		t.Fatalf("response = %+v, error = %+v, want id %s, error %d %q", got, got.Error, id, code, message)
	}
}

func expectResult(t *testing.T, got response, id, result string) {
	t.Helper()
	var actual, want any
	if err := json.Unmarshal(got.Result, &actual); err != nil {
		t.Fatalf("invalid result %s: %v", got.Result, err)
	}
	if err := json.Unmarshal([]byte(result), &want); err != nil {
		t.Fatal(err)
	}
	if string(got.ID) != id || !reflect.DeepEqual(actual, want) {
		t.Fatalf("response ID %s result %s, want ID %s result %s", got.ID, got.Result, id, result)
	}
}

func expectCallRefusal(t *testing.T, got response, message string) {
	t.Helper()
	text, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	expectResult(t, got, `"a\u0062"`, `{"content":[{"type":"text","text":`+string(text)+`}],"isError":true}`)
}

func unixListener(t *testing.T) *net.UnixListener {
	t.Helper()
	dir, err := os.MkdirTemp("", "boxmcp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "s"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := listener.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return listener
}

func readLine(c net.Conn) (string, error) {
	if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}
	return bufio.NewReader(c).ReadString('\n')
}

func serve(t *testing.T, input io.Reader, cfg boxmcp.Config) []response {
	t.Helper()
	var out bytes.Buffer
	if err := boxmcp.Serve(t.Context(), input, &out, cfg); err != nil {
		t.Fatal(err)
	}
	return responses(t, out.String())
}

// localConfig actively detects accidental dials. The listener answers any
// request that arrives before the probe, which marks the end of the test.
func localConfig(t *testing.T) (boxmcp.Config, func()) {
	t.Helper()
	l := unixListener(t)
	probe := "done\n"
	done := make(chan error, 1)
	accepted := make(chan string, 1)
	go func() {
		for {
			c, err := l.AcceptUnix()
			if err != nil {
				done <- err
				return
			}
			line, err := readLine(c)
			if err == nil && line != probe {
				select {
				case accepted <- line:
				default:
				}
				err = boxmcp.WriteAnswer(c, boxmcp.Answer{Refusal: "unexpected forward"})
			}
			_ = c.Close()
			if err != nil || line == probe {
				done <- err
				return
			}
		}
	}()
	end := func() {
		t.Helper()
		c, err := (&net.Dialer{}).DialContext(t.Context(), "unix", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.WriteString(c, probe)
		_ = c.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := await(t, done); err != nil {
			t.Fatal(err)
		}
		select {
		case line := <-accepted:
			t.Fatalf("locally handled line dialed daemon: %q", line)
		default:
		}
	}
	return boxmcp.Config{Socket: l.Addr().String(), Timeout: 3 * time.Second}, end
}

func await[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case value := <-c:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

// R-7E5G-EZP5, R-7GL9-6J6J, R-7HT5-KAX8, R-PUFI-9ZM2, R-7K8Y-BUEM, R-7MOR-3DW0, R-7P4J-UXDE
func TestLocalRequests(t *testing.T) {
	const pr = `"jsonrpc":"2.0"`
	cases := []struct {
		line    string
		id      string
		code    int
		message string
	}{
		{line: ""},
		{line: " \t\r"},
		{line: `{"jsonrpc":`, id: "null", code: boxmcp.CodeParseError, message: boxmcp.MsgParseError},
		{line: "null", id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: "true", id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: "[]", id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: "[" + pingRequest + "," + listRequest + "]", id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{"id":1,"result":null}`},
		{line: `{"id":1,"error":{"code":1}}`},
		{line: `{"id":1,"METHOD":"ping","result":null}`},
		{line: `{"id":"a","RESULT":null}`, id: `"a"`, code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":"<\u0061>","method":7,"result":null}`, id: `"<\u0061>"`, code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{"jsonrpc":"1.0","id":null,"method":"ping"}`, id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":1.5,"method":"tools/list"}`, id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":1e3,"method":"tools/call"}`, id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":true,"method":"ping"}`, id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":4,"METHOD":"tools/list"}`, id: "4", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":4,"m\u0065thod":"tools/list"}`, id: "4", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":5,"method":"tools/call","method":"tools/list"}`, id: "5", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":6,"method":"tools/call","metho\u0064":"tools/list"}`, id: "6", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":7,"method":"ping","jsonrp\u0063":"2.0"}`, id: "7", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"id":"\u0061","method":"ping","\u0069d":9}`, id: `"\u0061"`, code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"method":"ping","\u0069d":9}`, id: "null", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{"JSONRPC":"2.0","id":3,"method":"ping"}`, id: "3", code: boxmcp.CodeInvalidRequest, message: boxmcp.MsgInvalidRequest},
		{line: `{` + pr + `,"ID":4,"method":"ping"}`},
		{line: `{` + pr + `,"method":"notifications/initialized"}`},
		{line: `{` + pr + `,"method":"notifications/cancelled"}`},
		{line: `{` + pr + `,"method":"tools/call"}`},
		{line: `{` + pr + `,"method":"unknown"}`},
		{line: `{` + pr + `,"id":-0,"method":"server/discover"}`, id: "-0", code: boxmcp.CodeMethodNotFound, message: boxmcp.MsgMethodNotFound},
		{line: `{` + pr + `,"id":"","method":""}`, id: `""`, code: boxmcp.CodeMethodNotFound, message: boxmcp.MsgMethodNotFound},
		{line: `{` + pr + `, "id": 1000000000000000000000000000000000000000000000, "method" : "ping"}`, id: "1000000000000000000000000000000000000000000000"},
		{line: " \t" + pingRequest + "\r", id: "42"},
	}
	var input strings.Builder
	for _, test := range cases {
		if input.Len() > 0 {
			input.WriteByte('\n')
		}
		input.WriteString(test.line)
	}
	cfg, end := localConfig(t)
	defer end()
	got := serve(t, strings.NewReader("\n"+input.String()), cfg)
	responseIndex := 0
	for _, test := range cases {
		if test.id == "" {
			continue
		}
		if responseIndex == len(got) {
			t.Fatalf("no response to %s", test.line)
		}
		if test.code != 0 {
			expectError(t, got[responseIndex], test.id, test.code, test.message)
		} else {
			expectResult(t, got[responseIndex], test.id, "{}")
		}
		responseIndex++
	}
	if responseIndex != len(got) {
		t.Errorf("got %d responses, expected %d", len(got), responseIndex)
	}
}

// R-ISYS-K21D
func TestInitialize(t *testing.T) {
	versions := boxmcp.ProtocolVersions()
	init := `{"jsonrpc":"2.0","method":"` + boxmcp.MethodInitialize + `","id":"<\u0078>"`
	cases := []struct{ input, version string }{
		{init + `,"params":{"protocolVersion":"` + versions[1] + `"}}`, versions[1]},
		{init + `,"params":{"protocolVersion":"1900-01-01"}}`, versions[0]},
		{init + "}", versions[0]},
		{init + `,"params":[]}`, versions[0]},
		{init + `,"PARAMS":{"protocolVersion":"` + versions[1] + `"}}`, versions[0]},
		{init + `,"params":{"protocolVersion":7}}`, versions[0]},
		{init + `,"params":{"protocolVersion":"` + versions[1] + `"}}`, versions[1]},
	}
	var lines []string
	for _, c := range cases {
		lines = append(lines, c.input)
	}
	cfg, end := localConfig(t)
	defer end()
	got := serve(t, strings.NewReader(strings.Join(lines, "\n")), cfg)
	if len(got) != len(cases) {
		t.Fatalf("got %d responses, want %d", len(got), len(cases))
	}
	for i, got := range got {
		var result struct {
			Version      string          `json:"protocolVersion"`
			Capabilities json.RawMessage `json:"capabilities"`
			Info         struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		}
		if err := json.Unmarshal(got.Result, &result); err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(got.Result, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != 3 || string(got.ID) != `"<\u0078>"` || result.Version != cases[i].version ||
			string(result.Capabilities) != `{"tools":{}}` || result.Info.Name != boxmcp.ServerName || result.Info.Version == "" {
			t.Fatalf("initialize: ID %s, result %s", got.ID, got.Result)
		}
	}
}

// R-7CXK-17YG, R-7XNU-JBK9
func TestInvalidConfigurationAndIOErrors(t *testing.T) {
	for _, cfg := range []boxmcp.Config{
		{Timeout: time.Second},
		{Socket: "x"},
		{Socket: "x", Timeout: -time.Second},
	} {
		if err := boxmcp.Serve(t.Context(), nil, nil, cfg); err == nil {
			t.Fatal("invalid configuration succeeded")
		}
	}
	cfg := boxmcp.Config{Socket: "unused", Timeout: time.Second}
	errRead := errors.New("failed test read")
	var output bytes.Buffer
	if err := boxmcp.Serve(t.Context(), failingRead{errRead}, &output, cfg); !errors.Is(err, errRead) || output.Len() != 0 {
		t.Fatalf("read error: output %q, error %v", output.String(), err)
	}
	errWrite := errors.New("failed test write")
	in := io.MultiReader(strings.NewReader(pingRequest+"\n"), failOnRead{})
	if err := boxmcp.Serve(t.Context(), in, failedWriter{errWrite}, cfg); !errors.Is(err, errWrite) {
		t.Fatalf("write error: %v", err)
	}
}

type failingRead struct{ err error }

func (r failingRead) Read([]byte) (int, error) { return 0, r.err }

type failOnRead struct{}

func (failOnRead) Read([]byte) (int, error) { panic("unwanted additional read") }

type spaces struct{}

const spaceChunkSize = 4096

var spaceChunk = bytes.Repeat([]byte{' '}, spaceChunkSize)

func (spaces) Read(p []byte) (int, error) {
	size := len(p)
	for len(p) != 0 {
		p = p[copy(p, spaceChunk):]
	}
	return size, nil
}

// R-7FDC-SRFU
func TestLongInputIsDrainedWithoutAllocation(t *testing.T) {
	cfg, end := localConfig(t)
	defer end()
	long := io.LimitReader(spaces{}, 256<<20)
	atLimit := strings.Repeat(" ", protocol.MaxBoxRequest-len(pingRequest)) + pingRequest
	input := io.MultiReader(long, strings.NewReader("\n"+atLimit+"\n"+pingRequest))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := serve(t, input, cfg)
	runtime.ReadMemStats(&after)
	if used := after.TotalAlloc - before.TotalAlloc; used >= 16<<20 {
		t.Errorf("allocated %d bytes over an oversized line", used)
	}
	if len(got) != 3 {
		t.Fatalf("got %d responses, want 3", len(got))
	}
	expectError(t, got[0], "null", boxmcp.CodeInvalidRequest, boxmcp.MsgTooLarge)
	expectResult(t, got[1], "42", "{}")
	expectResult(t, got[2], "42", "{}")
}
