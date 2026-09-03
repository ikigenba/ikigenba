package agentkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/agentkit/retry"
)

type siblingTypedInput struct {
	Query string `json:"query" jsonschema:"required,minLength=1"`
}

func discoverSiblingTools(schemas []json.RawMessage, calls *int) ([]agentkit.Tool, error) {
	tools := make([]agentkit.Tool, 0, len(schemas))
	for _, schema := range schemas {
		tool, err := agentkit.NewToolFromSchema("remote", "discovered tool", schema, func(context.Context, json.RawMessage) (string, error) {
			*calls++
			return "remote result", nil
		})
		if err != nil {
			return nil, fmt.Errorf("discover remote tool: %w", err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func TestExternalSiblingCanUseTheCompleteSharedVocabulary(t *testing.T) {
	// R-647F-GFRV
	// R-67V4-LQZY
	t.Run("tool construction", assertExternalToolConstruction)
	t.Run("SSE decoding", assertExternalSSEDecoding)
	t.Run("discovery failure", assertExternalDiscoveryFailure)
	t.Run("error and value vocabulary", assertExternalErrorAndValueVocabulary)
	t.Run("retry leaf", assertExternalRetryLeaf)
}

func assertExternalToolConstruction(t *testing.T) {
	t.Helper()
	typed, err := agentkit.NewTool("typed", "typed tool", func(context.Context, siblingTypedInput) (string, error) {
		return "typed result", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	must := agentkit.MustTool("must", "static tool", func(context.Context, siblingTypedInput) (string, error) {
		return "must result", nil
	})
	callbackCalls := 0
	discovered, err := discoverSiblingTools([]json.RawMessage{json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)}, &callbackCalls)
	if err != nil {
		t.Fatal(err)
	}
	tools := []agentkit.Tool{typed, must, discovered[0]}
	if len(tools) != 3 || tools[2].Name() != "remote" {
		t.Fatalf("external []Tool = %#v", tools)
	}
	if err := agentkit.ValidateToolSchema(tools[2].Schema()); err != nil {
		t.Fatalf("external schema validation: %v", err)
	}
}

func assertExternalSSEDecoding(t *testing.T) {
	t.Helper()
	var frames []map[string]any
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":\ndata: {\"tools\":[]}}\n\n"
	for payload, frameErr := range agentkit.SSEFrames(strings.NewReader(stream)) {
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		var frame map[string]any
		if err := json.Unmarshal(payload, &frame); err != nil {
			t.Fatalf("SSE JSON-RPC-like frame %q: %v", payload, err)
		}
		frames = append(frames, frame)
	}
	if len(frames) != 1 || frames[0]["jsonrpc"] != "2.0" {
		t.Fatalf("decoded frames = %#v", frames)
	}
}

func assertExternalDiscoveryFailure(t *testing.T) {
	t.Helper()
	callbackCalls := 0
	failed, discoveryErr := discoverSiblingTools([]json.RawMessage{json.RawMessage(`{"type":"array"}`)}, &callbackCalls)
	if discoveryErr == nil || failed != nil || callbackCalls != 0 {
		t.Fatalf("discovery failure = tools %#v, error %v, callback calls %d", failed, discoveryErr, callbackCalls)
	}
}

func assertExternalErrorAndValueVocabulary(t *testing.T) {
	t.Helper()
	identity := agentkit.Identity{Endpoint: "mcp", AuthMode: "remote", Model: "server"}
	usage := agentkit.Usage{InputTokens: 1, CachedTokens: 2, OutputTokens: 3, ReasoningTokens: 4}
	_ = agentkit.Pricing{}
	cost := agentkit.Cost(70)
	providerErr := &agentkit.Error{Category: agentkit.CategoryTransport, Status: 503, Code: "dead", Message: "server unavailable", RetryAfter: time.Second, Endpoint: identity}
	if !agentkit.Retryable(providerErr) || !errors.Is(fmt.Errorf("closed: %w", agentkit.ErrClosed), agentkit.ErrClosed) || agentkit.ErrInvalidConfig == nil ||
		usage.InputTokens != 1 || cost != 70 || providerErr.Endpoint != identity {
		t.Fatal("external error/value vocabulary is not usable with its documented fields and policy")
	}
}

func assertExternalRetryLeaf(t *testing.T) {
	t.Helper()
	value, err := retry.Do(context.Background(), retry.Policy{MaxAttempts: 1}, func(context.Context) (string, error) {
		return "leaf result", nil
	}, nil)
	if err != nil || value != "leaf result" {
		t.Fatalf("retry leaf = %q, %v", value, err)
	}
	var _ retry.Clock
}
