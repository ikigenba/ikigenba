// Package trace provides optional debug tracing for R-92NN-7DNI. When
// active, a Tracer writes RFC 3339 timestamped plain-text lines to an
// io.Writer covering every boundary the agent loop touches: stdin from
// ralph-loops, stdout to ralph-loops, LLM HTTP, and tool dispatches.
// Each entry is prefixed with a directional bracket tag
// ([stdin>], [<stdout], [llm>], [<llm], [tool>], [<tool]) followed by
// a timestamp. A nil *Tracer is a valid no-op — all methods return
// immediately if the receiver is nil.
package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Tracer writes trace lines for HTTP requests, HTTP responses, SSE
// event/data pairs, and tool dispatches. All writes are serialized
// by an internal mutex.
type Tracer struct {
	out      io.Writer
	apiKeys  []string
	mu       sync.Mutex
	lastByte byte // last byte written to out; 0 = unknown/start
}

// New returns a Tracer that writes to out. Each value in apiKeys is a
// secret that must be redacted from every trace line before writing.
func New(out io.Writer, apiKeys ...string) *Tracer {
	return &Tracer{out: out, apiKeys: apiKeys}
}

func (t *Tracer) now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// redact replaces every occurrence of an API key value in s with [REDACTED].
func (t *Tracer) redact(s string) string {
	for _, k := range t.apiKeys {
		if k != "" {
			s = strings.ReplaceAll(s, k, "[REDACTED]")
		}
	}
	return s
}

func (t *Tracer) redactHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for name, vals := range h {
		rv := make([]string, len(vals))
		for i, v := range vals {
			rv[i] = t.redact(v)
		}
		out[name] = rv
	}
	return out
}

// writeLine writes s to out. If the previous byte written was not '\n',
// a leading '\n' is prepended to keep entries on their own visual lines
// (R-92NN-7DNI formatting rule).
func (t *Tracer) writeLine(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lastByte != 0 && t.lastByte != '\n' {
		fmt.Fprint(t.out, "\n")
	}
	fmt.Fprint(t.out, s)
	if len(s) > 0 {
		t.lastByte = s[len(s)-1]
	}
}

// LogRequest logs an outbound HTTP request to the LLM endpoint. Header
// values and body content matching any configured API key are replaced
// with [REDACTED]. R-92NN-7DNI boundary 2: [llm>].
func (t *Tracer) LogRequest(method, rawURL string, headers http.Header, body []byte) {
	if t == nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[llm>] %s %s %s\n", t.now(), method, t.redact(rawURL))
	for name, vals := range t.redactHeaders(headers) {
		for _, v := range vals {
			fmt.Fprintf(&b, "  %s: %s\n", name, v)
		}
	}
	if len(body) > 0 {
		fmt.Fprintf(&b, "  [body]\n%s\n  [/body]\n", t.redact(string(body)))
	}
	t.writeLine(b.String())
}

// LogResponse logs an inbound HTTP response from the LLM. body is the
// full response body for non-SSE responses; pass nil for SSE streams
// (pairs are logged individually via LogSSEPair). R-92NN-7DNI boundary
// 3: [<llm].
func (t *Tracer) LogResponse(statusCode int, headers http.Header, body []byte) {
	if t == nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[<llm] %s %d\n", t.now(), statusCode)
	for name, vals := range t.redactHeaders(headers) {
		for _, v := range vals {
			fmt.Fprintf(&b, "  %s: %s\n", name, v)
		}
	}
	if len(body) > 0 {
		fmt.Fprintf(&b, "  [body]\n%s\n  [/body]\n", t.redact(string(body)))
	}
	t.writeLine(b.String())
}

// LogSSEPair logs one SSE event+data pair as it arrives from the LLM.
// R-92NN-7DNI boundary 3: [<llm].
func (t *Tracer) LogSSEPair(event, data string) {
	if t == nil {
		return
	}
	t.writeLine(fmt.Sprintf("[<llm] %s SSE event:%s data:%s\n", t.now(), event, t.redact(data)))
}

// LogToolDispatch logs a tool invocation before it executes.
func (t *Tracer) LogToolDispatch(name string, input json.RawMessage) {
	if t == nil {
		return
	}
	t.writeLine(fmt.Sprintf("[tool>] %s %s %s\n", t.now(), name, t.redact(string(input))))
}

// LogToolResult logs a tool result after it returns.
func (t *Tracer) LogToolResult(name string, isError bool, content string) {
	if t == nil {
		return
	}
	errTag := "false"
	if isError {
		errTag = "true"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[<tool] %s %s is_error=%s\n", t.now(), name, errTag)
	if content != "" {
		fmt.Fprintf(&b, "  [output]\n%s\n  [/output]\n", t.redact(content))
	}
	t.writeLine(b.String())
}

// LogStdinEvent logs a raw stdin line received from ralph-loops.
func (t *Tracer) LogStdinEvent(raw []byte) {
	if t == nil {
		return
	}
	t.writeLine(fmt.Sprintf("[stdin>] %s %s\n", t.now(), t.redact(string(raw))))
}

// LogStdoutEvent logs a stream-json line about to be emitted on stdout.
// line is expected to be the complete newline-terminated NDJSON event.
func (t *Tracer) LogStdoutEvent(line string) {
	if t == nil {
		return
	}
	// line already carries a trailing '\n' from wire.Encode; prefix the tag.
	t.writeLine(fmt.Sprintf("[<stdout] %s %s", t.now(), t.redact(line)))
}
