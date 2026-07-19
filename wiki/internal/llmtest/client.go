// Package llmtest adapts existing scripted providers to the prompts HTTP seam.
package llmtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	agentkit "github.com/ikigenba/agentkit"

	"wiki/internal/llm"
)

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
		Role    string `json:"role"`
		Content string `json:"content"`
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
func NewClient(t testing.TB, provider agentkit.Provider, _ ...llm.Recorder) *llm.Client {
	t.Helper()
	client, closeServer := ServeProvider(provider)
	t.Cleanup(closeServer)
	return client
}

// NewClientWithEmbeddings serves both /complete and /embed and closes with t.
func NewClientWithEmbeddings(t testing.TB, provider agentkit.Provider, vectors [][]float32) (*llm.Client, *EmbedCapture) {
	t.Helper()
	capture := &EmbedCapture{vectors: cloneVectors(vectors)}
	client, closeServer := serve(provider, capture)
	t.Cleanup(closeServer)
	return client, capture
}

// ServeProvider returns a prompts-compatible loopback around a provider.
func ServeProvider(provider agentkit.Provider) (*llm.Client, func()) {
	return serve(provider, nil)
}

func serve(provider agentkit.Provider, embeds *EmbedCapture) (*llm.Client, func()) {
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
		conv := &agentkit.Conversation{
			Provider: provider,
			Model:    req.Model,
			System:   req.System,
			Gen: agentkit.GenSettings{
				Temperature: req.Config.Temperature,
				MaxTokens:   req.Config.MaxTokens,
			},
		}
		if req.Config.Thinking != nil && !*req.Config.Thinking {
			conv.Gen.Reasoning = agentkit.DisableReasoning()
		} else if req.Config.Effort != "" {
			conv.Gen.Reasoning = agentkit.Level(req.Config.Effort)
		}
		last := ""
		for i, message := range req.Messages {
			if i == len(req.Messages)-1 {
				last = message.Content
				break
			}
			conv.History = append(conv.History, agentkit.Message{Role: agentkit.Role(message.Role), Blocks: []agentkit.Block{agentkit.TextBlock{Text: message.Content}}})
		}
		stream := conv.Send(r.Context(), last)
		text := ""
		for event := range stream.Events() {
			if done, ok := event.(agentkit.MessageDone); ok {
				for _, block := range done.Message.Blocks {
					if value, ok := block.(agentkit.TextBlock); ok {
						text += value.Text
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		usage := stream.Usage()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text":  text,
			"usage": map[string]any{"output": usage.Output + usage.ReasoningOutput},
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
