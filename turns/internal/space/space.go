// Package space implements the stateless sessions gateway client.
package space

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/session"
)

// Gateway protocol and tool names.
const (
	ProtocolVersion = "2026-07-28"
	Service         = "sessions"
	PushTool        = "push_turns"
	LatestTool      = "get_latest_turns"
)

// Request batch limits.
const (
	MaxLatest       = 500
	MaxPushSessions = 500
	MaxPushTurns    = 1000
)

const detailLimit = 4096

// Endpoint returns the gateway URL for a space.
func Endpoint(space string) string { return "https://mcp." + space + "/mcp" }

// Client calls the sessions service through a space's gateway.
type Client struct {
	endpoint string
	token    string
	hc       *http.Client
}

// New constructs a client without performing I/O.
func New(space, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{endpoint: Endpoint(space), token: token, hc: hc}
}

// Held pairs a requested session with its latest stored moment.
type Held struct {
	Ref    session.Ref
	Latest time.Time
}

// Error describes a failed space request.
type Error struct {
	Reason string
	Detail string
	Quoted bool
}

func (e *Error) Error() string { return e.Reason }

type wireRef struct {
	Tool session.Tool `json:"tool"`
	ID   string       `json:"id"`
}

type wireCommand struct {
	Name     string `json:"name"`
	Argument string `json:"argument"`
}

type wireTurn struct {
	Moment    string        `json:"moment"`
	Whose     session.Whose `json:"whose"`
	Model     string        `json:"model"`
	Input     int64         `json:"input"`
	Cached    int64         `json:"cached"`
	Output    int64         `json:"output"`
	Reasoning int64         `json:"reasoning"`
}

type wireSession struct {
	Tool      session.Tool `json:"tool"`
	ID        string       `json:"id"`
	Host      string       `json:"host"`
	Directory string       `json:"directory"`
	Headless  bool         `json:"headless"`
	Turns     []wireTurn   `json:"turns"`
	Parent    *wireRef     `json:"parent,omitempty"`
	Command   *wireCommand `json:"command,omitempty"`
}

// Latest returns stored moments in the order of refs.
func (c *Client) Latest(ctx context.Context, refs []session.Ref) ([]Held, error) {
	entries := make([]wireRef, len(refs))
	for i, ref := range refs {
		entries[i] = wireRef{ref.Tool, ref.ID}
	}
	return c.call(ctx, "call", LatestTool, entries, refs)
}

// Push stores sessions and returns their stored moments in the same order.
func (c *Client) Push(ctx context.Context, host string, sessions []session.Session) ([]Held, error) {
	entries := make([]wireSession, len(sessions))
	refs := make([]session.Ref, len(sessions))
	for i, s := range sessions {
		refs[i] = s.Ref
		e := wireSession{Tool: s.Ref.Tool, ID: s.Ref.ID, Host: host, Directory: s.Directory, Headless: s.Headless, Turns: make([]wireTurn, len(s.Turns))}
		if s.Parent != nil {
			e.Parent = &wireRef{s.Parent.Tool, s.Parent.ID}
		}
		if s.Command != nil {
			e.Command = &wireCommand{s.Command.Name, s.Command.Argument}
		}
		for j, turn := range s.Turns {
			e.Turns[j] = wireTurn{turn.Moment.UTC().Format("2006-01-02T15:04:05.000Z07:00"), turn.Whose, turn.Model, turn.Tokens.Input, turn.Tokens.Cached, turn.Tokens.Output, turn.Tokens.Reasoning}
		}
		entries[i] = e
	}
	return c.call(ctx, "mutate", PushTool, entries, refs)
}

func (c *Client) network(err error) *Error {
	return &Error{Reason: "cannot reach " + c.endpoint, Detail: err.Error()}
}

func (c *Client) call(ctx context.Context, name, tool string, entries any, refs []session.Ref) ([]Held, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": map[string]any{"service": Service, "tool": tool, "args": map[string]any{"sessions": entries}},
			"_meta":     map[string]any{"io.modelcontextprotocol/protocolVersion": ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}},
		},
	})
	if err != nil {
		return nil, c.network(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, c.network(err)
	}
	req.Header = http.Header{
		"Authorization": {"Bearer " + c.token}, "Content-Type": {"application/json"}, "Accept": {"application/json"},
		"Mcp-Protocol-Version": {ProtocolVersion}, "Mcp-Method": {"tools/call"}, "Mcp-Name": {name},
	}
	hc := *c.hc
	hc.Jar = nil
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return nil, c.network(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var reader io.Reader = resp.Body
	if resp.StatusCode != http.StatusOK {
		reader = io.LimitReader(reader, detailLimit)
	}
	answer, err := io.ReadAll(reader)
	if err != nil {
		return nil, c.network(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, c.network(err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, quoted("the space refused the token", string(answer))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, quoted("the space answered HTTP "+strconv.Itoa(resp.StatusCode), string(answer))
	}
	return parseAnswer(resp.Header.Get("Content-Type"), answer, refs, name)
}

func quoted(reason, detail string) *Error {
	if len(detail) > detailLimit {
		detail = detail[:detailLimit]
	}
	return &Error{reason, detail, true}
}

func object(data json.RawMessage) (map[string]json.RawMessage, bool) {
	var value map[string]json.RawMessage
	err := json.Unmarshal(data, &value)
	return value, err == nil && value != nil
}

func text(data json.RawMessage) (string, bool) {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return "", false
	}
	s, ok := value.(string)
	return s, ok
}

func parseAnswer(contentType string, body []byte, refs []session.Ref, name string) ([]Held, error) {
	unreadable := func() ([]Held, error) { return nil, quoted("the space's answer could not be read", string(body)) }
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return unreadable()
	}
	envelope, ok := object(body)
	if !ok {
		return unreadable()
	}
	version, ok := text(envelope["jsonrpc"])
	if !ok || version != "2.0" {
		return unreadable()
	}
	id, ok := new(big.Rat).SetString(string(bytes.TrimSpace(envelope["id"])))
	if !ok || id.Cmp(big.NewRat(1, 1)) != 0 {
		return unreadable()
	}
	resultData, hasResult := envelope["result"]
	errorData, hasError := envelope["error"]
	if hasResult == hasError {
		return unreadable()
	}
	if hasError {
		rpcError, ok := object(errorData)
		if !ok {
			return unreadable()
		}
		message, ok := text(rpcError["message"])
		if !ok {
			return unreadable()
		}
		return nil, quoted("the gateway refused the request", message)
	}
	result, ok := object(resultData)
	if !ok {
		return unreadable()
	}
	if bytes.Equal(bytes.TrimSpace(result["isError"]), []byte("true")) {
		var content []json.RawMessage
		_ = json.Unmarshal(result["content"], &content)
		var messages []string
		for _, data := range content {
			item, ok := object(data)
			if !ok {
				continue
			}
			kind, _ := text(item["type"])
			message, ok := text(item["text"])
			if kind == "text" && ok {
				messages = append(messages, message)
			}
		}
		reason := "sessions refused the query"
		if name == "mutate" {
			reason = "sessions refused a batch"
		}
		return nil, quoted(reason, strings.Join(messages, "\n"))
	}
	structured, ok := object(result["structuredContent"])
	if !ok {
		return unreadable()
	}
	sessionsData := bytes.TrimSpace(structured["sessions"])
	if len(sessionsData) == 0 || sessionsData[0] != '[' {
		return unreadable()
	}
	var entries []json.RawMessage
	if json.Unmarshal(sessionsData, &entries) != nil || len(entries) != len(refs) {
		return unreadable()
	}
	held := make([]Held, len(refs))
	for i, data := range entries {
		entry, ok := object(data)
		if !ok {
			return unreadable()
		}
		tool, toolOK := text(entry["tool"])
		id, idOK := text(entry["id"])
		if !toolOK || !idOK || tool != string(refs[i].Tool) || id != refs[i].ID {
			return unreadable()
		}
		held[i].Ref = refs[i]
		if latest, exists := entry["latest"]; exists {
			value, ok := text(latest)
			if !ok {
				return unreadable()
			}
			moment, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return unreadable()
			}
			held[i].Latest = moment.UTC()
		}
	}
	return held, nil
}

// Verify Error implements the standard error interface.
var _ error = (*Error)(nil)
