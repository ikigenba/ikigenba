package boxmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Local MCP methods and JSON-RPC errors.
const (
	MethodInitialize = "initialize"
	MethodPing       = "ping"

	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeRefused        = -32000

	MsgParseError     = "Parse error"
	MsgInvalidRequest = "Invalid Request"
	MsgMethodNotFound = "Method not found"
	MsgTooLarge       = "request too large"

	protocolVersion = "2.0"
	serverVersion   = "1"
)

var errConfig = errors.New("accounts MCP requires a socket path and a positive timeout")

// ProtocolVersions reports the supported MCP versions, latest first.
func ProtocolVersions() []string {
	return []string{"2025-11-25", "2025-06-18"}
}

// Config specifies the daemon socket and the total time allowed per forward.
type Config struct {
	Socket  string
	Timeout time.Duration
}

// Run serves stdio using the socket chosen by the process environment.
func Run(ctx context.Context, in io.Reader, out io.Writer, getenv func(string) string) error {
	socket := getenv(EnvSocket)
	if socket == "" {
		socket = SocketPath
	}
	return Serve(ctx, in, out, Config{Socket: socket, Timeout: AnswerTimeout})
}

// Serve handles one newline-delimited request at a time until EOF or cancellation.
func Serve(ctx context.Context, in io.Reader, out io.Writer, cfg Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.Socket == "" || cfg.Timeout <= 0 {
		return errConfig
	}

	input := bufio.NewReader(in)
	skipping := false
	for {
		line := nextInput(ctx, input, skipping)
		if err := ctx.Err(); err != nil {
			return err
		}
		if line.err != nil && !errors.Is(line.err, io.EOF) {
			return line.err
		}
		if !skipping {
			response := handle(ctx, cfg, line)
			if len(response) != 0 {
				// Write the frame including its LF in one operation. A writer
				// that cancels the context cannot leave a second write pending.
				response = append(response, '\n')
				if err := ctx.Err(); err != nil {
					return err
				}
				writeErr := writeParts(out, response)
				if err := ctx.Err(); err != nil {
					return err
				}
				if writeErr != nil {
					return writeErr
				}
			}
		}
		if errors.Is(line.err, io.EOF) {
			return ctx.Err()
		}
		skipping = line.more
	}
}

type inputLine struct {
	data    []byte
	tooLong bool
	more    bool
	err     error
}

// nextInput lets Serve return promptly if the caller-owned reader is blocked.
// In that case the pending read finishes when the caller unblocks its reader.
func nextInput(ctx context.Context, input *bufio.Reader, skipping bool) inputLine {
	read := func() inputLine {
		if skipping {
			return inputLine{err: discardLine(ctx, input)}
		}
		return readLine(ctx, input, protocol.MaxBoxRequest)
	}
	if ctx.Done() == nil {
		return read()
	}
	done := make(chan inputLine, 1)
	go func() { done <- read() }()
	select {
	case line := <-done:
		return line
	case <-ctx.Done():
		return inputLine{err: ctx.Err()}
	}
}

func readLine(ctx context.Context, r *bufio.Reader, limit int) inputLine {
	var line inputLine
	for {
		if err := ctx.Err(); err != nil {
			return inputLine{err: err}
		}
		part, err := r.ReadSlice('\n')
		continued := errors.Is(err, bufio.ErrBufferFull)
		if err == nil {
			part = part[:len(part)-1]
		}
		needed := len(line.data) + len(part)
		if needed > limit {
			if continued {
				return inputLine{tooLong: true, more: true}
			}
			return inputLine{tooLong: true, err: err}
		}
		// The backing buffer, as well as the bytes used, stays within the limit.
		if needed > cap(line.data) {
			next := make([]byte, len(line.data), min(limit, max(needed, cap(line.data)*2)))
			copy(next, line.data)
			line.data = next
		}
		line.data = append(line.data, part...)
		if !continued {
			line.err = err
			return line
		}
		if needed == limit {
			// Decide at the first excess byte, without waiting for another fragment.
			next, err := r.Peek(1)
			if err != nil {
				line.err = err
				return line
			}
			if next[0] != '\n' {
				return inputLine{tooLong: true, more: true}
			}
		}
	}
}

func discardLine(ctx context.Context, r *bufio.Reader) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := r.ReadSlice('\n'); !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
}

func handle(ctx context.Context, cfg Config, line inputLine) []byte {
	if line.tooLong {
		return rpcError(nil, CodeInvalidRequest, MsgTooLarge)
	}
	data := bytes.Trim(line.data, " \t\r")
	if len(data) == 0 {
		return nil
	}
	if !json.Valid(data) {
		return rpcError(nil, CodeParseError, MsgParseError)
	}
	if data[0] != '{' {
		return rpcError(nil, CodeInvalidRequest, MsgInvalidRequest)
	}

	fields := exactFields(data)
	methodRaw, hasMethod := fields["method"]
	if !hasMethod && (fields["result"] != nil || fields["error"] != nil) {
		return nil
	}

	id := fields["id"]
	method, isString := jsonString(methodRaw)
	version, _ := jsonString(fields["jsonrpc"])
	if id != nil && !validID(id) {
		return rpcError(nil, CodeInvalidRequest, MsgInvalidRequest)
	}
	if version != protocolVersion || !isString {
		return rpcError(id, CodeInvalidRequest, MsgInvalidRequest)
	}
	if id == nil {
		return nil
	}

	switch method {
	case MethodInitialize:
		return initialize(id, fields["params"])
	case MethodPing:
		return rpcResult(id, struct{}{})
	case protocol.MethodToolsList, protocol.MethodToolsCall:
		answer := forward(ctx, cfg, line.data)
		if hasMessage(answer.Message) {
			return answer.Message
		}
		if method == protocol.MethodToolsCall {
			return rpcResult(id, struct {
				Content []textContent `json:"content"`
				IsError bool          `json:"isError"`
			}{Content: []textContent{{Type: "text", Text: answer.Refusal}}, IsError: true})
		}
		return rpcError(id, CodeRefused, answer.Refusal)
	default:
		return rpcError(id, CodeMethodNotFound, MsgMethodNotFound)
	}
}

// exactFields observes the member spelling before the decoder unescapes it.
// The input is already known to be a valid JSON object.
func exactFields(data []byte) map[string]json.RawMessage {
	d := json.NewDecoder(bytes.NewReader(data))
	_, _ = d.Token()
	fields := make(map[string]json.RawMessage)
	for d.More() {
		start := d.InputOffset()
		if _, err := d.Token(); err != nil {
			break
		}
		raw := bytes.TrimLeft(data[start:d.InputOffset()], " \t\r\n,")
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			break
		}
		switch string(raw) {
		case `"jsonrpc"`, `"id"`, `"method"`, `"params"`, `"result"`, `"error"`:
			fields[string(raw[1:len(raw)-1])] = value
		}
	}
	return fields
}

func jsonString(raw json.RawMessage) (string, bool) {
	var value string
	ok := len(raw) != 0 && raw[0] == '"' && json.Unmarshal(raw, &value) == nil
	return value, ok
}

// The caller has already validated the JSON syntax. An integer has no fraction
// or exponent, and no imposed machine-sized range.
func validID(id json.RawMessage) bool {
	first := id[0]
	return first == '"' || (first == '-' || first >= '0' && first <= '9') && !bytes.ContainsAny(id, ".eE")
}

func initialize(id, params json.RawMessage) []byte {
	versions := ProtocolVersions()
	version := versions[0]
	var options map[string]json.RawMessage
	if json.Unmarshal(params, &options) == nil {
		if wanted, ok := jsonString(options["protocolVersion"]); ok && slices.Contains(versions, wanted) {
			version = wanted
		}
	}
	return rpcResult(id, map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": struct{}{}},
		"serverInfo":      map[string]string{"name": ServerName, "version": serverVersion},
	})
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type rpcFailure struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcError(id json.RawMessage, code int, message string) []byte {
	return rpcResponse(id, "error", rpcFailure{Code: code, Message: message})
}

func rpcResult(id json.RawMessage, result any) []byte {
	return rpcResponse(id, "result", result)
}

func rpcResponse(id json.RawMessage, member string, value any) []byte {
	if id == nil {
		id = json.RawMessage("null")
	}
	// All locally constructed result and error payloads are JSON-encodable.
	payload, _ := json.Marshal(value)
	prefix := `{"jsonrpc":"` + protocolVersion + `","id":`
	key := `,"` + member + `":`
	response := make([]byte, 0, len(prefix)+len(id)+len(key)+len(payload)+2)
	response = append(response, prefix...)
	response = append(response, id...)
	response = append(response, key...)
	response = append(response, payload...)
	return append(response, '}')
}

func forward(ctx context.Context, cfg Config, line []byte) Answer {
	wait, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(wait, "unix", cfg.Socket)
	if err != nil {
		return Answer{Refusal: NotAnswering}
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(wait, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := wait.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return Answer{Refusal: NotAnswering}
	}

	return exchange(wait, conn, line)
}

func exchange(ctx context.Context, socket io.ReadWriter, line []byte) Answer {
	// A busy daemon answers without reading. Read its refusal even if writing failed.
	_ = writeParts(socket, line, []byte("\n"))
	r := bufio.NewReader(io.LimitReader(socket, MaxAnswerLine+1))
	reply := readLine(ctx, r, MaxAnswerLine)
	refused := Answer{Refusal: NotAnswering}
	if reply.err != nil || reply.tooLong || ctx.Err() != nil {
		return refused
	}
	data := bytes.Trim(reply.data, " \t\r")
	if len(data) == 0 || data[0] != '{' {
		return refused
	}
	var answer Answer
	if json.Unmarshal(data, &answer) != nil {
		return refused
	}
	if hasMessage(answer.Message) {
		if answer.Refusal == "" && isObject(answer.Message) {
			return answer
		}
	} else if answer.Refusal != "" {
		answer.Message = nil
		return answer
	}
	return refused
}
