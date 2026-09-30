package agentkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
)

func TestRootPackageIsImportableByConsumers(t *testing.T) {
	// R-01PL-YNFH
	if agentkit.AnthropicMessagesWire() == nil {
		t.Fatal("AnthropicMessagesWire() = nil")
	}
	if !errors.Is(agentkit.ErrInvalidConfig, agentkit.ErrInvalidConfig) {
		t.Fatal("ErrInvalidConfig is not comparable")
	}
}

func TestConsumerConstructsRotationFromExactlyItsFields(t *testing.T) {
	// R-O6XD-20K3
	fields := struct {
		RefreshURL string
		ClientID   string
	}{RefreshURL: "https://auth.test/token", ClientID: "client"}
	rotation := agentkit.Rotation(fields)
	if rotation.RefreshURL != "https://auth.test/token" || rotation.ClientID != "client" {
		t.Fatalf("Rotation = %#v", rotation)
	}
}

type consumerRotator struct{ rotated agentkit.Rotation }

func (*consumerRotator) AuthMode() agentkit.AuthMode { return agentkit.AuthModeOAuth }

func (*consumerRotator) Token(context.Context) (agentkit.Token, error) {
	return agentkit.Token{Bearer: "current"}, nil
}

func (r *consumerRotator) Rotate(_ context.Context, rotation agentkit.Rotation) (agentkit.Token, error) {
	r.rotated = rotation
	return agentkit.Token{Bearer: "rotated"}, nil
}

func TestConsumerUsesTokenAndImplementsRotator(t *testing.T) {
	// R-O859-FSAS
	expires := time.Unix(1700000000, 0)
	token := agentkit.Token(struct {
		Bearer    string
		AccountID string
		ExpiresAt time.Time
	}{Bearer: "bearer", AccountID: "account", ExpiresAt: expires})
	if token.Bearer != "bearer" || token.AccountID != "account" || !token.ExpiresAt.Equal(expires) {
		t.Fatalf("Token = %#v", token)
	}

	implementation := &consumerRotator{}
	var rotator agentkit.Rotator = implementation
	if rotator.AuthMode() != agentkit.AuthModeOAuth {
		t.Fatalf("AuthMode() = %q", rotator.AuthMode())
	}
	current, err := rotator.Token(context.Background())
	if err != nil || current.Bearer != "current" {
		t.Fatalf("Token() = %#v, %v", current, err)
	}
	rotation := agentkit.Rotation{RefreshURL: "https://auth.test/token", ClientID: "client"}
	rotated, err := rotator.Rotate(context.Background(), rotation)
	if err != nil || rotated.Bearer != "rotated" || implementation.rotated != rotation {
		t.Fatalf("Rotate() = %#v, %v; received %#v", rotated, err, implementation.rotated)
	}
}

type consumerAuthenticator struct{ bodies []string }

func (a *consumerAuthenticator) Authenticate(_ context.Context, request *http.Request, body []byte) error {
	a.bodies = append(a.bodies, string(body))
	request.Header.Set("Authorization", "Bearer consumer")
	return nil
}

func TestConsumerImplementsAuthenticatorWithItsSingleMethod(t *testing.T) {
	// R-OAL2-7BS6
	authorizations := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		authorizations <- request.Header.Get("Authorization")
	}))
	defer server.Close()

	implementation := &consumerAuthenticator{}
	var authenticator agentkit.Authenticator = implementation
	endpoint, err := agentkit.NewEndpoint(authenticator, agentkit.WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := agentkit.New(agentkit.OpenAIChatWire(), endpoint, "model", agentkit.Config{})
	if err != nil {
		t.Fatal(err)
	}
	consumerDrain(conversation.Send(context.Background(), agentkit.Text{Text: "hello"}))
	if got := <-authorizations; got != "Bearer consumer" {
		t.Fatalf("Authorization = %q, want the consumer authenticator's header", got)
	}
	if len(implementation.bodies) != 1 || implementation.bodies[0] == "" {
		t.Fatalf("Authenticate bodies = %q, want one request body", implementation.bodies)
	}
}

func TestConsumerSelectsEveryBuiltInWireAndReadsItsOptionSpecs(t *testing.T) {
	// R-OGOK-46HN
	// R-OD0U-YV9K
	constructors := []func() agentkit.WireFormat{
		agentkit.AnthropicMessagesWire,
		agentkit.GeminiGenerateContentWire,
		agentkit.ChatWire,
		agentkit.ResponsesWire,
		agentkit.OpenAIChatWire,
		agentkit.OpenAIResponsesWire,
		agentkit.XAIChatWire,
		agentkit.XAIResponsesWire,
	}
	for index, constructor := range constructors {
		wire := constructor()
		if wire == nil {
			t.Fatalf("constructor %d returned nil", index)
		}
		if specs := consumerOptionSpecs(wire); len(specs) == 0 {
			t.Fatalf("constructor %d OptionSpecs() is empty", index)
		}
	}
}

type consumerToolInput struct {
	Query string `json:"query" jsonschema:"required"`
}

func TestConsumerCallsEveryToolMethod(t *testing.T) {
	// R-OHWG-HY8C
	tool, err := agentkit.NewTool("lookup", "look something up", func(_ context.Context, in consumerToolInput) (string, error) {
		return "found " + in.Query, nil
	}, func(consumerToolInput) agentkit.Access { return agentkit.BlocksAll() })
	if err != nil {
		t.Fatal(err)
	}
	if tool.Name() != "lookup" || tool.Description() != "look something up" || !strings.Contains(string(tool.Schema()), `"query"`) {
		t.Fatalf("tool = %q/%q/%s", tool.Name(), tool.Description(), tool.Schema())
	}
	result, err := tool.Call(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err != nil || result != "found x" {
		t.Fatalf("Call = %q, %v", result, err)
	}
	_ = tool.Access(json.RawMessage(`{"query":"x"}`))
}

func TestConsumerSendsAndDrainsStream(t *testing.T) {
	// R-OJ4C-VPZ1
	// R-OKC9-9HPQ
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	endpoint, err := agentkit.NewEndpoint(&consumerAuthenticator{}, agentkit.WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := agentkit.New(agentkit.OpenAIChatWire(), endpoint, "model", agentkit.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := conversation.Send(context.Background(), agentkit.Text{Text: "hello"})
	if events := consumerDrain(stream); len(events) != 0 {
		t.Fatalf("events = %#v, want none for a rejected request", events)
	}
	var providerError *agentkit.Error
	if err := stream.Err(); !errors.As(err, &providerError) || providerError.Status != http.StatusBadRequest {
		t.Fatalf("Err() = %v, want the 400 *Error", err)
	}
}

// Each method and constructor is assigned to its declared signature, so a
// drift in any signature fails to compile.
var (
	_ func(agentkit.Tool) string                                                        = agentkit.Tool.Name
	_ func(agentkit.Tool) string                                                        = agentkit.Tool.Description
	_ func(agentkit.Tool) json.RawMessage                                               = agentkit.Tool.Schema
	_ func(agentkit.Tool, context.Context, json.RawMessage) (string, error)             = agentkit.Tool.Call
	_ func(agentkit.Tool, json.RawMessage) agentkit.Access                              = agentkit.Tool.Access
	_ func(*agentkit.Conversation, context.Context, ...agentkit.Block) *agentkit.Stream = (*agentkit.Conversation).Send
	_ func(*agentkit.Stream) iter.Seq[agentkit.Event]                                   = (*agentkit.Stream).Events
	_ func(*agentkit.Stream) error                                                      = (*agentkit.Stream).Err
	_ func(agentkit.WireFormat) []agentkit.OptionSpec                                   = agentkit.WireFormat.OptionSpecs
)

func consumerOptionSpecs(wire agentkit.WireFormat) []agentkit.OptionSpec { return wire.OptionSpecs() }

func consumerDrain(stream *agentkit.Stream) []agentkit.Event {
	var events []agentkit.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events
}
