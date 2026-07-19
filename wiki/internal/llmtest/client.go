// Package llmtest adapts existing scripted providers to the prompts HTTP seam.
package llmtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"wiki/internal/llm"
)

// Role, Block, Message, and Request are the small provider-script vocabulary
// used by wiki tests. They intentionally model only the prompts HTTP fields.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Block interface{}

type TextBlock struct{ Text string }

type Message struct {
	Role   Role
	Blocks []Block
}

type GenSettings struct {
	Temperature *float64
	MaxTokens   int
	Reasoning   Reasoning
}

type Reasoning interface {
	Disabled() bool
	Level() (string, bool)
}

type reasoningSetting struct {
	level    string
	disabled bool
}

func (setting reasoningSetting) Disabled() bool        { return setting.disabled }
func (setting reasoningSetting) Level() (string, bool) { return setting.level, setting.level != "" }

func Level(value string) Reasoning { return reasoningSetting{level: value} }

func DisableReasoning() Reasoning { return reasoningSetting{disabled: true} }

type Tool struct{}

type Request struct {
	Model    string
	System   string
	Gen      GenSettings
	Messages []Message
	Tools    []Tool
}

type Usage struct {
	InputUncached   int64
	Output          int64
	ReasoningOutput int64
	Total           int64
}

type FinishReason string

const (
	FinishStop  FinishReason = "stop"
	FinishOther FinishReason = "other"
)

type RoundTrip struct {
	Message Message
	Usage   Usage
	Err     error
}

func NewRoundTrip(message Message, _ FinishReason, usage Usage, rest ...any) *RoundTrip {
	var err error
	for _, value := range rest {
		if candidate, ok := value.(error); ok {
			err = candidate
			break
		}
	}
	return &RoundTrip{Message: message, Usage: usage, Err: err}
}

type Pricing struct{ Tiers []RateTier }
type RateTier struct{ MinInputTokens int }

type Provider interface {
	RoundTrip(context.Context, *Request) *RoundTrip
}

type completeRequest struct {
	Model  string `json:"model"`
	System string `json:"system"`
	Config struct {
		Temperature *float64 `json:"temperature"`
		MaxTokens   int      `json:"max_tokens"`
		Effort      string   `json:"effort"`
		Thinking    *bool    `json:"thinking"`
	} `json:"config"`
	Messages []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"messages"`
}

// EmbedRequest is the prompts /embed request captured by a test client.
type EmbedRequest struct {
	Origin     string   `json:"origin"`
	Name       string   `json:"name"`
	GroupID    string   `json:"group_id"`
	Model      string   `json:"model"`
	Dimensions int      `json:"dimensions"`
	Role       string   `json:"role"`
	Inputs     []string `json:"inputs"`
}

// EmbedCapture records prompts /embed requests and serves scripted vectors.
type EmbedCapture struct {
	mu       sync.Mutex
	vectors  [][]float32
	requests []EmbedRequest
}

func (c *EmbedCapture) Requests() []EmbedRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]EmbedRequest(nil), c.requests...)
}

// NewClient serves /complete through provider for tests and closes with t.
func NewClient(t testing.TB, provider Provider) *llm.Client {
	t.Helper()
	client, closeServer := ServeProvider(provider)
	t.Cleanup(closeServer)
	return client
}

// NewClientWithEmbeddings serves both /complete and /embed and closes with t.
func NewClientWithEmbeddings(t testing.TB, provider Provider, vectors [][]float32) (*llm.Client, *EmbedCapture) {
	t.Helper()
	capture := &EmbedCapture{vectors: cloneVectors(vectors)}
	client, closeServer := serve(provider, capture)
	t.Cleanup(closeServer)
	return client, capture
}

// ServeProvider returns a prompts-compatible loopback around a provider.
func ServeProvider(provider Provider) (*llm.Client, func()) {
	return serve(provider, nil)
}

func serve(provider Provider, embeds *EmbedCapture) (*llm.Client, func()) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/embed" {
			if embeds == nil {
				http.NotFound(w, r)
				return
			}
			var req EmbedRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			embeds.mu.Lock()
			embeds.requests = append(embeds.requests, req)
			vectors := cloneVectors(embeds.vectors)
			embeds.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"vectors": vectors})
			return
		}
		var req completeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		providerReq := &Request{
			Model:  req.Model,
			System: req.System,
			Gen: GenSettings{
				Temperature: req.Config.Temperature,
				MaxTokens:   req.Config.MaxTokens,
			},
		}
		if req.Config.Thinking != nil && !*req.Config.Thinking {
			providerReq.Gen.Reasoning = DisableReasoning()
		} else if req.Config.Effort != "" {
			providerReq.Gen.Reasoning = Level(req.Config.Effort)
		}
		for _, message := range req.Messages {
			providerReq.Messages = append(providerReq.Messages, Message{Role: Role(message.Role), Blocks: []Block{TextBlock{Text: message.Text}}})
		}
		result := provider.RoundTrip(r.Context(), providerReq)
		if result == nil {
			http.Error(w, "nil scripted provider response", http.StatusBadGateway)
			return
		}
		if result.Err != nil {
			http.Error(w, result.Err.Error(), http.StatusBadGateway)
			return
		}
		text := ""
		for _, block := range result.Message.Blocks {
			if value, ok := block.(TextBlock); ok {
				text += value.Text
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text":  text,
			"usage": map[string]any{"output": result.Usage.Output + result.Usage.ReasoningOutput},
		})
	}))
	return llm.New(server.URL), server.Close
}

func cloneVectors(in [][]float32) [][]float32 {
	out := make([][]float32, len(in))
	for i := range in {
		out[i] = append([]float32(nil), in[i]...)
	}
	return out
}
