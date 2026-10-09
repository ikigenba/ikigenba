// Package agent runs the binary's child role.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/toolkit"
	"golang.org/x/sys/unix"
)

// Command selects the child role.
const Command = "agent"

// Exit codes describe the child outcome.
const (
	ExitAnswered = iota
	ExitFailed
	ExitLimit
	ExitNoOutput
	ExitUnusable
)

// Tool groups and diagnostic formats are the child vocabulary.
const (
	GroupFiles   = "files"
	GroupBash    = "bash"
	GroupSuite   = "suite"
	Failed       = "run failed: %s"
	LimitReached = "tool limit reached: %s"
	NoOutput     = "no accepted output: %s"
	Unusable     = "run unusable: %s"
)

// Spec is the JSON document delivered on standard input.
type Spec struct {
	Model        string          `json:"model"`
	Key          string          `json:"key"`
	System       string          `json:"system"`
	Prompt       string          `json:"prompt"`
	Input        json.RawMessage `json:"input"`
	Tools        []string        `json:"tools"`
	Schema       json.RawMessage `json:"schema"`
	MaxToolCalls int             `json:"max_tool_calls"`
	UserID       string          `json:"user_id"`
	Email        string          `json:"email"`
	RequestID    string          `json:"request_id"`
	EventID      string          `json:"event_id"`
	EventDepth   int             `json:"event_depth"`
	BaseURL      string          `json:"base_url"`
}

// Process provides the child input, output and environment seams.
type Process struct {
	Stdin     io.ReadCloser
	Stdout    io.Writer
	Stderr    io.Writer
	LookupEnv func(string) (string, bool)
	Now       func() time.Time
}

// ValidGroups accepts each known group at most once.
func ValidGroups(groups []string) bool {
	seen := map[string]bool{}
	for _, g := range groups {
		if (g != GroupFiles && g != GroupBash && g != GroupSuite) || seen[g] {
			return false
		}
		seen[g] = true
	}
	return true
}

// Offering selects the catalog default host and wire.
func Offering(model string) (agentkit.Offering, error) { return agentkit.Lookup(model, "", "") }

// KeyVariable names the host credential variable.
func KeyVariable(h agentkit.Host) string {
	switch h {
	case agentkit.HostAnthropic:
		return "ANTHROPIC_API_KEY"
	case agentkit.HostOpenAI:
		return "OPENAI_API_KEY"
	case agentkit.HostGemini:
		return "GEMINI_API_KEY"
	case agentkit.HostXAI:
		return "XAI_API_KEY"
	case agentkit.HostOpenRouter:
		return "OPENROUTER_API_KEY"
	}
	return ""
}

// UserMessage appends compact input when it is not empty.
func UserMessage(prompt string, input []byte) (string, error) {
	if len(input) == 0 {
		return prompt, nil
	}
	var b bytes.Buffer
	if err := json.Compact(&b, input); err != nil {
		return "", err
	}
	if b.String() == "{}" {
		return prompt, nil
	}
	return prompt + "\n\n" + b.String(), nil
}
func (p Process) failure(code int, err error) int {
	formats := []string{"", Failed, LimitReached, NoOutput, Unusable}
	if p.Stderr != nil {
		_, _ = p.Stderr.Write([]byte("prompts: " + fmt.Sprintf(formats[code], strings.ReplaceAll(err.Error(), "\n", " ")) + "\n"))
	}
	return code
}
func decode(p Process) (Spec, error) {
	var s Spec
	if p.Stdin == nil {
		return s, errors.New("missing standard input")
	}
	data, err := io.ReadAll(p.Stdin)
	_ = p.Stdin.Close()
	if err != nil {
		return s, err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return s, errors.New("spec must be an object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&s); err != nil {
		return s, err
	}
	var extra any
	if err = dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return s, errors.New("spec must be exactly one object")
	}
	if s.Model == "" || s.Prompt == "" || !ValidGroups(s.Tools) || s.MaxToolCalls < 0 || s.EventDepth < 0 {
		return s, errors.New("invalid run spec")
	}
	s.Input = bytes.TrimSpace(s.Input)
	if len(s.Input) == 0 || bytes.Equal(s.Input, []byte("null")) {
		s.Input = json.RawMessage("{}")
	}
	if s.Input[0] != '{' {
		return s, errors.New("input must be an object")
	}
	if bytes.Equal(bytes.TrimSpace(s.Schema), []byte("null")) {
		s.Schema = nil
	}
	return s, nil
}

// Run executes one agent turn and returns its outcome.
func Run(ctx context.Context, p Process) int {
	_ = unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
	s, err := decode(p)
	if err != nil {
		return p.failure(ExitUnusable, err)
	}
	env := map[string]string{}
	for _, k := range []string{"IKIGENBA_RUN_DIR", "IKIGENBA_RUN_ID", "IKIGENBA_WORK_DIR"} {
		if p.LookupEnv == nil {
			return p.failure(ExitUnusable, errors.New("missing environment lookup"))
		}
		v, ok := p.LookupEnv(k)
		if !ok || v == "" {
			return p.failure(ExitUnusable, fmt.Errorf("missing %s", k))
		}
		env[k] = v
	}
	off, err := Offering(s.Model)
	if err != nil {
		return p.failure(ExitUnusable, err)
	}
	auth, err := off.Authenticator(agentkit.APIKeyRotator(s.Key))
	if err != nil {
		return p.failure(ExitUnusable, err)
	}
	s.Key = ""
	var opts []agentkit.EndpointOption
	if s.BaseURL != "" {
		opts = append(opts, agentkit.WithBaseURL(s.BaseURL))
	}
	endpoint, err := agentkit.NewEndpoint(auth, opts...)
	if err != nil {
		return p.failure(ExitUnusable, err)
	}
	tools, closeTools, err := buildTools(ctx, p, s, env["IKIGENBA_WORK_DIR"])
	if err != nil {
		return p.failure(ExitUnusable, err)
	}
	defer closeTools()
	cfg := agentkit.Config{Tools: tools, Limits: agentkit.Limits{MaxToolCalls: s.MaxToolCalls}}
	if len(s.Schema) > 0 {
		if err = agentkit.ValidateOutputSchema(s.Schema); err != nil {
			return p.failure(ExitUnusable, err)
		}
		cfg.Output = &agentkit.OutputContract{Schema: s.Schema}
	}
	f, err := os.Create(filepath.Join(env["IKIGENBA_RUN_DIR"], "transcript.jsonl"))
	if err != nil {
		return p.failure(ExitUnusable, err)
	}
	defer func() { _ = f.Close() }()
	now := p.Now
	if now == nil {
		now = time.Now
	}
	log := agentkit.NewLog(f, now, env["IKIGENBA_RUN_ID"])
	defer func() { _ = log.Close() }()
	cfg.Log = log
	c, err := agentkit.New(off.WireFormat, endpoint, off.WireModel, cfg)
	if err != nil {
		return p.failure(ExitUnusable, err)
	}
	if strings.TrimSpace(s.System) != "" {
		if err = c.AddSystem(s.System); err != nil {
			return p.failure(ExitUnusable, err)
		}
	}
	message, err := UserMessage(s.Prompt, s.Input)
	if err != nil {
		return p.failure(ExitUnusable, err)
	}
	stream := c.Send(ctx, agentkit.Text{Text: message})
	var answer []byte
	if cfg.Output != nil {
		answer, err = agentkit.Output[json.RawMessage](stream)
	} else {
		for event := range stream.Events() {
			if m, ok := event.(agentkit.MessageDone); ok && m.Message.Role == agentkit.RoleAssistant {
				answer = nil
				for _, b := range m.Message.Blocks {
					if text, ok := b.(agentkit.Text); ok {
						answer = append(answer, text.Text...)
					}
				}
			}
		}
		err = stream.Err()
	}
	if err != nil {
		code := ExitFailed
		switch {
		case errors.Is(err, agentkit.ErrLimitExceeded):
			code = ExitLimit
		case errors.Is(err, agentkit.ErrInvalidOutput):
			code = ExitNoOutput
		case errors.Is(err, agentkit.ErrInvalidConfig):
			code = ExitUnusable
		}
		return p.failure(code, err)
	}
	if p.Stdout != nil {
		_, _ = p.Stdout.Write(answer)
	}
	return ExitAnswered
}

type eventTransport struct{ base http.RoundTripper }

func (t eventTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	events.Forward(r.Context(), r)
	return t.base.RoundTrip(r)
}
func buildTools(ctx context.Context, p Process, s Spec, root string) ([]agentkit.Tool, func(), error) {
	var tools []agentkit.Tool
	groups := map[string]bool{}
	for _, g := range s.Tools {
		groups[g] = true
	}
	noop := func() {}
	if groups[GroupFiles] {
		constructors := []func(string) (agentkit.Tool, error){toolkit.Read, toolkit.Write, toolkit.Edit, func(r string) (agentkit.Tool, error) { return toolkit.Glob(r) }, func(r string) (agentkit.Tool, error) { return toolkit.Grep(r) }}
		for _, makeTool := range constructors {
			t, err := makeTool(root)
			if err != nil {
				return nil, noop, err
			}
			tools = append(tools, t)
		}
	}
	if groups[GroupBash] {
		t, err := toolkit.Bash(root)
		if err != nil {
			return nil, noop, err
		}
		tools = append(tools, t)
	}
	if !groups[GroupSuite] {
		return tools, noop, nil
	}
	path, ok := p.LookupEnv("IKIGENBA_SERVICES")
	if !ok || path == "" {
		return nil, noop, errors.New("missing services file")
	}
	list, err := services.Read(path)
	if err != nil {
		return nil, noop, err
	}
	gateway, ok := list.Find("mcp")
	if !ok || !gateway.Enabled || gateway.Socket == "" {
		return nil, noop, errors.New("gateway unavailable")
	}
	transport := telemetry.SocketTransport(gateway.Socket)
	closeTools := transport.CloseIdleConnections
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://mcp/mcp", HTTPClient: &http.Client{Transport: eventTransport{transport}}})
	caller := identity.Caller{UserID: s.UserID, Email: s.Email, RequestID: s.RequestID}
	if s.EventID != "" {
		ctx = events.NewContext(ctx, events.Cause{ID: s.EventID, Depth: s.EventDepth})
	}
	listed, err := client.ListTools(ctx, caller)
	if err != nil {
		closeTools()
		return nil, noop, err
	}
	for _, info := range listed {
		schema, err := bridgeSchema(info.InputSchema)
		if err != nil {
			closeTools()
			return nil, noop, err
		}
		tool, err := agentkit.NewToolFromSchema(info.Name, info.Description, schema, func(callCtx context.Context, args json.RawMessage) (string, error) {
			if s.EventID != "" {
				callCtx = events.NewContext(callCtx, events.Cause{ID: s.EventID, Depth: s.EventDepth})
			}
			result, err := client.CallTool(callCtx, caller, info.Name, args)
			if err != nil {
				return "", err
			}
			data, err := result.MarshalJSON()
			if err != nil {
				return "", err
			}
			if result.IsError() {
				return "", errors.New(string(data))
			}
			return string(data), nil
		}, nil)
		if err != nil {
			closeTools()
			return nil, noop, err
		}
		tools = append(tools, tool)
	}
	return tools, closeTools, nil
}
func bridgeSchema(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	stripSchema(value)
	return json.Marshal(value)
}
func stripSchema(value any) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	delete(object, "additionalProperties")
	if props, ok := object["properties"].(map[string]any); ok {
		for _, v := range props {
			stripSchema(v)
		}
	}
	stripSchema(object["items"])
	for _, key := range []string{"anyOf", "oneOf"} {
		if list, ok := object[key].([]any); ok {
			for _, v := range list {
				stripSchema(v)
			}
		}
	}
}
