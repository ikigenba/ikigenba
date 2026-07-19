package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"

	"prompts/internal/admit"
	"prompts/internal/calls"
	"prompts/internal/ids"
)

// EmbedRequest is the envelope accepted by POST /embed.
type EmbedRequest struct {
	Origin     string   `json:"origin"`
	Name       string   `json:"name"`
	GroupID    string   `json:"group_id,omitempty"`
	Attempt    int      `json:"attempt,omitempty"`
	Model      string   `json:"model"`
	Provider   string   `json:"provider,omitempty"`
	Dimensions int      `json:"dimensions,omitempty"`
	Role       string   `json:"role"`
	Inputs     []string `json:"inputs"`
}

type embedResponse struct {
	CallID  string                  `json:"call_id"`
	Vectors [][]float32             `json:"vectors"`
	Usage   agentkit.EmbeddingUsage `json:"usage"`
	CostUSD float64                 `json:"cost_usd"`
}

// EmbedderFactory constructs an embedding provider for a resolved provider.
type EmbedderFactory func(string, func(string) string) (agentkit.EmbeddingProvider, error)

// EmbedExecutor runs and records stateless embedding calls.
type EmbedExecutor struct {
	store         CallStore
	gate          *admit.Gate
	buildEmbedder EmbedderFactory
	getenv        func(string) string
}

func NewEmbedExecutor(store CallStore, gate *admit.Gate, build EmbedderFactory, getenv func(string) string) *EmbedExecutor {
	return &EmbedExecutor{store: store, gate: gate, buildEmbedder: build, getenv: getenv}
}

// EmbedHandler returns the synchronous batch embedding endpoint.
func (e *EmbedExecutor) EmbedHandler() http.Handler { return http.HandlerFunc(e.embed) }

func (e *EmbedExecutor) embed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method must be POST")
		return
	}

	var req EmbedRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCompleteBody))
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, decodeError(err))
		return
	}
	if err := requireEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	role, err := validateEmbedRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entry, ok := catalog.Lookup(req.Model)
	if !ok || entry.Embedding == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("model %q is not a catalog embedding model", req.Model))
		return
	}
	providerName := req.Provider
	if providerName == "" {
		providerName = entry.Provider
	}
	if providerName != "openai" && providerName != "google" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("provider %q does not support embeddings", providerName))
		return
	}
	if req.Dimensions != 0 && (req.Dimensions < entry.Embedding.MinDimension || req.Dimensions > entry.Embedding.MaxDimension) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("dimensions must be between %d and %d for model %q", entry.Embedding.MinDimension, entry.Embedding.MaxDimension, req.Model))
		return
	}
	resolvedProvider, wireModel, _, ok := catalog.Resolve(providerName, req.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("provider %q does not route embedding model %q", providerName, req.Model))
		return
	}
	embedProvider, err := e.buildEmbedder(resolvedProvider, e.getenv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create embedding provider: "+err.Error())
		return
	}
	release, err := e.gate.AcquireCall(r.Context(), resolvedProvider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "acquire provider call: "+err.Error())
		return
	}
	defer release()

	embedder := &agentkit.Embedder{
		Provider: embedProvider, Model: wireModel,
		Pricing: &entry.Embedding.Pricing, Dimensions: req.Dimensions,
	}
	result, embedErr := embedder.Embed(r.Context(), req.Inputs, role)
	usage := embedder.TotalUsage()
	cost := embedder.TotalCost()
	if embedErr == nil && len(result.Vectors) != len(req.Inputs) {
		embedErr = fmt.Errorf("provider returned %d vectors for %d inputs", len(result.Vectors), len(req.Inputs))
	}

	requestJSON, err := json.Marshal(req.Inputs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal embedding inputs: "+err.Error())
		return
	}
	usageJSON, err := json.Marshal(usage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal usage: "+err.Error())
		return
	}
	callID := ids.NewULID()
	attempt := req.Attempt
	if attempt == 0 {
		attempt = 1
	}
	row := calls.Row{
		ID: callID, Class: calls.ClassEmbedding, Origin: req.Origin, Name: req.Name,
		GroupID: req.GroupID, Attempt: attempt, Provider: resolvedProvider, Model: req.Model,
		InputTokens: usage.InputTokens, TotalTokens: usage.Total,
		UsageJSON: string(usageJSON), CostUSD: cost.USD(), RequestBody: stringPointer(string(requestJSON)),
	}
	if embedErr != nil {
		row.Error = embedErr.Error()
	}
	if err := e.store.Insert(context.WithoutCancel(r.Context()), row); err != nil {
		writeError(w, http.StatusInternalServerError, "record embedding: "+err.Error())
		return
	}
	if embedErr != nil {
		writeError(w, http.StatusBadGateway, embedErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, embedResponse{
		CallID: callID, Vectors: result.Vectors, Usage: result.Usage(), CostUSD: result.Cost().USD(),
	})
}

func validateEmbedRequest(req EmbedRequest) (agentkit.InputType, error) {
	if err := calls.ValidateOrigin(req.Origin); err != nil {
		return agentkit.InputUnspecified, err
	}
	if err := calls.ValidateName(req.Name); err != nil {
		return agentkit.InputUnspecified, err
	}
	if len(req.Inputs) == 0 {
		return agentkit.InputUnspecified, errors.New("inputs must be non-empty")
	}
	for i, input := range req.Inputs {
		if strings.TrimSpace(input) == "" {
			return agentkit.InputUnspecified, fmt.Errorf("inputs[%d] must be non-empty", i)
		}
	}
	switch req.Role {
	case "document":
		return agentkit.InputDocument, nil
	case "query":
		return agentkit.InputQuery, nil
	default:
		return agentkit.InputUnspecified, errors.New("role must be document or query")
	}
}
