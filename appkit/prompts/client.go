// Package prompts runs catalog prompts through the prompts service's MCP tools.
package prompts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// ClientConfig supplies the sibling service and event writer.
type ClientConfig struct {
	Service   services.Entry
	Telemetry *telemetry.Writer
}

// Client makes independent calls and is safe for concurrent use.
type Client struct{ mcp *mcp.Client }

// Prompt describes a catalog prompt and the tools it can use.
type Prompt struct {
	ID, Name, Model, Prompt, System string
	Tools                           []string
}

// Start identifies a newly submitted run.
type Start struct{ ID, Status, Reason string }

// Outcome describes a run's current state, output and usage.
type Outcome struct {
	ID, Prompt, Model, Status, Reason                                   string
	ExitCode                                                            *int
	Started, Finished                                                   time.Time
	Stdout                                                              string
	InputTokens, CachedTokens, OutputTokens, ReasoningTokens, CostNanos int64
}

// Refusal carries the tool's refusal text without interpreting it.
type Refusal struct{ Text string }

func (r *Refusal) Error() string { return "prompts: " + r.Text }

// HTTPError retains an unexpected response's status and bounded body prefix.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("prompts: unexpected HTTP response (status %d)", e.StatusCode)
}

// NewClient constructs a client without contacting the service.
func NewClient(cfg ClientConfig) *Client {
	client := telemetry.SiblingClient(cfg.Telemetry, cfg.Service.Name, telemetry.SocketTransport(cfg.Service.Socket))
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	endpoint := (&url.URL{Scheme: "http", Host: cfg.Service.Name, Path: "/mcp"}).String()
	return &Client{mcp: mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint, HTTPClient: client})}
}

// Show reads a prompt by its catalog name.
func (c *Client) Show(ctx context.Context, name string) (Prompt, error) {
	fields, err := c.call(ctx, "show", map[string]any{"name": name})
	if err != nil {
		return Prompt{}, err
	}
	var p Prompt
	if !stringsInto(fields, []stringField{{"id", &p.ID, false}, {"name", &p.Name, false}, {"model", &p.Model, false}, {"prompt", &p.Prompt, false}, {"system", &p.System, false}}) {
		return Prompt{}, outputError(ctx, errors.New("invalid show output"))
	}
	raw := bytes.TrimSpace(fields["tools"])
	var tools []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &tools) != nil {
		return Prompt{}, outputError(ctx, errors.New("invalid show tools"))
	}
	p.Tools = make([]string, len(tools))
	for i, tool := range tools {
		if !stringInto(tool, &p.Tools[i]) {
			return Prompt{}, outputError(ctx, errors.New("invalid show tool"))
		}
	}
	return p, nil
}

// Run starts a run, leaving scheduling and polling to the consumer.
func (c *Client) Run(ctx context.Context, name string, input json.RawMessage) (Start, error) {
	args := map[string]any{"name": name}
	if input != nil {
		if _, err := object(input); err != nil {
			return Start{}, fmt.Errorf("prompts: invalid input: %w", err)
		}
		args["input"] = input
	}
	fields, err := c.call(ctx, "run", args)
	if err != nil {
		return Start{}, err
	}
	var s Start
	if !stringsInto(fields, []stringField{{"id", &s.ID, false}, {"status", &s.Status, false}, {"reason", &s.Reason, true}}) {
		return Start{}, outputError(ctx, errors.New("invalid run output"))
	}
	return s, nil
}

// Result reads one run without waiting for it to finish.
func (c *Client) Result(ctx context.Context, run string) (Outcome, error) {
	fields, err := c.call(ctx, "result", map[string]any{"run": run})
	if err != nil {
		return Outcome{}, err
	}
	out, err := decodeOutcome(fields)
	if err != nil {
		return Outcome{}, outputError(ctx, err)
	}
	return out, nil
}

func (c *Client) call(ctx context.Context, tool string, args map[string]any) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("prompts: encode arguments: %w", err)
	}
	caller, _ := identity.FromContext(ctx)
	result, err := c.mcp.CallTool(ctx, caller, tool, encoded)
	if err != nil {
		var rpc *mcp.RPCError
		if errors.As(err, &rpc) {
			return nil, fmt.Errorf("prompts: call %s: %w", tool, err)
		}
		var he *mcp.HTTPError
		if errors.As(err, &he) {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("prompts: call %s: %w", tool, ctx.Err())
			}
			return nil, &HTTPError{StatusCode: he.StatusCode, Body: he.Body}
		}
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return nil, fmt.Errorf("prompts: call %s: %w", tool, err)
	}
	raw, err := result.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("prompts: decode result: %w", err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, fmt.Errorf("prompts: decode result: %w", err)
	}
	if result.IsError() {
		return nil, &Refusal{Text: refusalText(members["content"])}
	}
	fields, err := object(members["structuredContent"])
	if err != nil {
		return nil, outputError(ctx, err)
	}
	return fields, nil
}

func outputError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	return fmt.Errorf("prompts: invalid output: %w", err)
}

func refusalText(raw json.RawMessage) string {
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, block := range blocks {
		var fields map[string]json.RawMessage
		if json.Unmarshal(block, &fields) != nil {
			continue
		}
		var kind, text string
		if stringInto(fields["type"], &kind) && kind == "text" && stringInto(fields["text"], &text) {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func object(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("invalid JSON encoding")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("expected object")
	}
	return fields, nil
}

type stringField struct {
	name     string
	target   *string
	optional bool
}

func stringsInto(fields map[string]json.RawMessage, targets []stringField) bool {
	for _, target := range targets {
		raw, ok := fields[target.name]
		if !ok && target.optional {
			continue
		}
		if !stringInto(raw, target.target) {
			return false
		}
	}
	return true
}

func stringInto(raw []byte, target *string) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && raw[0] == '"' && json.Unmarshal(raw, target) == nil
}

func decodeOutcome(fields map[string]json.RawMessage) (Outcome, error) {
	var out Outcome
	var started, finished string
	if !stringsInto(fields, []stringField{{"id", &out.ID, false}, {"prompt", &out.Prompt, false}, {"model", &out.Model, false}, {"status", &out.Status, false}, {"reason", &out.Reason, true}, {"stdout", &out.Stdout, true}, {"started", &started, false}, {"finished", &finished, true}}) {
		return Outcome{}, errors.New("expected string member")
	}
	var err error
	out.Started, err = time.Parse(time.RFC3339, started)
	if err != nil {
		return Outcome{}, err
	}
	out.Started = out.Started.UTC()
	if _, ok := fields["finished"]; ok {
		out.Finished, err = time.Parse(time.RFC3339, finished)
		if err != nil {
			return Outcome{}, err
		}
		out.Finished = out.Finished.UTC()
	}
	if raw, ok := fields["exit_code"]; ok {
		var code int
		if !numberInto(raw, &code) {
			return Outcome{}, errors.New("invalid exit_code")
		}
		out.ExitCode = &code
	}
	for _, member := range []struct {
		name   string
		target *int64
	}{{"input_tokens", &out.InputTokens}, {"cached_tokens", &out.CachedTokens}, {"output_tokens", &out.OutputTokens}, {"reasoning_tokens", &out.ReasoningTokens}, {"cost_nanos", &out.CostNanos}} {
		if raw, ok := fields[member.name]; ok && !numberInto(raw, member.target) {
			return Outcome{}, fmt.Errorf("invalid %s", member.name)
		}
	}
	return out, nil
}

func numberInto(raw []byte, target any) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') && json.Unmarshal(raw, target) == nil
}
