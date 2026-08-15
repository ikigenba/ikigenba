package completion

import (
	"bytes"
	"encoding/json"
	"errors"
	"eventplane/correlation"
	"fmt"
	"io"
	"net/http"
	"prompts/internal/calls"
	"prompts/internal/prompt"
	"regexp"
	"strings"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"
)

const (
	maxEnsureBody = 10 << 20
	maxKeyBytes   = 1024
	maxContext    = 64 << 10
)

var consumerPattern = regexp.MustCompile(`^service:[a-z0-9._-]+$`)

// HTTP owns the queue's loopback HTTP verbs. Execution is deliberately not a
// dependency: Ensure can only persist work, never call a provider.
type HTTP struct {
	store            *Store
	getenv           func(string) string
	subAuthAvailable func() bool
}

func NewHTTP(store *Store, getenv func(string) string, subAuthAvailable func() bool) *HTTP {
	return &HTTP{store: store, getenv: getenv, subAuthAvailable: subAuthAvailable}
}

type ensureRequest struct {
	Consumer string          `json:"consumer"`
	Origin   string          `json:"origin"`
	Key      string          `json:"key"`
	Context  json.RawMessage `json:"context,omitempty"`
	Name     string          `json:"name"`
	GroupID  string          `json:"group_id,omitempty"`
	Attempt  int             `json:"attempt,omitempty"`
	Model    string          `json:"model"`
	Provider string          `json:"provider,omitempty"`
	Config   prompt.Config   `json:"config,omitempty"`
	System   string          `json:"system,omitempty"`
	Messages []Message       `json:"messages"`
}

type ensureResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type readResponse struct {
	ID       string           `json:"id"`
	Consumer string           `json:"consumer"`
	Origin   string           `json:"origin"`
	Key      string           `json:"key"`
	Context  json.RawMessage  `json:"context"`
	Status   string           `json:"status"`
	Result   *json.RawMessage `json:"result,omitempty"`
	Error    string           `json:"error,omitempty"`
	Code     string           `json:"code,omitempty"`
	Usage    *json.RawMessage `json:"usage,omitempty"`
	CostUSD  *float64         `json:"cost_usd,omitempty"`
}

func (h *HTTP) EnsureHandler() http.Handler { return http.HandlerFunc(h.ensure) }
func (h *HTTP) GetHandler() http.Handler    { return http.HandlerFunc(h.get) }
func (h *HTTP) InboxHandler() http.Handler  { return http.HandlerFunc(h.inbox) }
func (h *HTTP) AckHandler() http.Handler    { return http.HandlerFunc(h.ack) }

func (h *HTTP) ensure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "bad_request", "method must be POST")
		return
	}
	var in ensureRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEnsureBody))
	if err := decoder.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", decodeError(err))
		return
	}
	if err := requireEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	request, code, err := h.validate(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, code, err.Error())
		return
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "unavailable", "marshal completion request: "+err.Error())
		return
	}
	item, inserted, err := h.store.Ensure(r.Context(), Item{
		Consumer: in.Consumer, Origin: in.Origin, Key: in.Key, Context: string(in.Context),
		Name: in.Name, GroupID: in.GroupID, CorrelationID: correlation.FromContext(r.Context()),
		Attempt: in.Attempt, Request: string(encoded),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "unavailable", err.Error())
		return
	}
	if inserted {
		writeJSON(w, http.StatusAccepted, ensureResponse{ID: item.ID, Status: item.Status})
		return
	}
	writeJSON(w, http.StatusOK, itemReadResponse(item))
}

func (h *HTTP) validate(in ensureRequest) (Request, string, error) {
	if !consumerPattern.MatchString(in.Consumer) {
		return Request{}, "bad_consumer", fmt.Errorf("invalid consumer %q: must match ^service:[a-z0-9._-]+$", in.Consumer)
	}
	if err := calls.ValidateOrigin(in.Origin); err != nil {
		return Request{}, "bad_origin", err
	}
	if err := calls.ValidateName(in.Name); err != nil {
		return Request{}, "bad_name", err
	}
	if in.Key == "" {
		return Request{}, "bad_key", errors.New("key must be non-empty")
	}
	if len(in.Key) > maxKeyBytes {
		return Request{}, "bad_key", errors.New("key exceeds 1024 bytes")
	}
	if len(in.Context) > maxContext {
		return Request{}, "bad_context", errors.New("context exceeds 64 KiB")
	}
	if len(in.Messages) == 0 {
		return Request{}, "bad_config", errors.New("messages must be non-empty")
	}
	for i, message := range in.Messages {
		if message.Role != string(agentkit.RoleUser) && message.Role != string(agentkit.RoleAssistant) {
			return Request{}, "bad_config", fmt.Errorf("messages[%d].role must be user or assistant", i)
		}
	}
	if in.Messages[len(in.Messages)-1].Role != string(agentkit.RoleUser) {
		return Request{}, "bad_config", errors.New("final message role must be user")
	}
	cfg := in.Config
	cfg.Provider, cfg.Model = in.Provider, in.Model
	validated, err := prompt.ValidateConfig(cfg, h.getenv, h.subAuthAvailable)
	if err != nil {
		code := "bad_config"
		if message := err.Error(); strings.Contains(message, "unknown prompt model") || strings.Contains(message, "does not route model") || strings.Contains(message, "provider") && strings.Contains(message, "not supported") {
			code = "bad_model"
		}
		return Request{}, code, err
	}
	resolution := catalog.Resolve(prompt.CatalogProviderID(validated.Provider), validated.Model)
	if resolution.Coverage == catalog.Unrouted {
		return Request{}, "bad_model", fmt.Errorf("provider %q does not route model %q", validated.Provider, validated.Model)
	}
	return Request{Model: validated.Model, Provider: validated.Provider, Config: in.Config, System: in.System, Messages: in.Messages}, "", nil
}

func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "bad_request", "method must be GET")
		return
	}
	consumer := r.URL.Query().Get("consumer")
	if !consumerPattern.MatchString(consumer) {
		writeError(w, http.StatusBadRequest, "bad_consumer", fmt.Sprintf("invalid consumer %q: must match ^service:[a-z0-9._-]+$", consumer))
		return
	}
	item, err := h.store.Get(r.Context(), r.PathValue("id"), consumer)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, itemReadResponse(item))
}

func (h *HTTP) inbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "bad_request", "method must be GET")
		return
	}
	consumer := r.URL.Query().Get("consumer")
	if !consumerPattern.MatchString(consumer) {
		writeError(w, http.StatusBadRequest, "bad_consumer", fmt.Sprintf("invalid consumer %q: must match ^service:[a-z0-9._-]+$", consumer))
		return
	}
	items, err := h.store.Inbox(r.Context(), consumer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "unavailable", err.Error())
		return
	}
	response := make([]readResponse, 0, len(items))
	for _, item := range items {
		response = append(response, itemReadResponse(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *HTTP) ack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "bad_request", "method must be DELETE")
		return
	}
	consumer := r.URL.Query().Get("consumer")
	if !consumerPattern.MatchString(consumer) {
		writeError(w, http.StatusBadRequest, "bad_consumer", fmt.Sprintf("invalid consumer %q: must match ^service:[a-z0-9._-]+$", consumer))
		return
	}
	err := h.store.Ack(r.Context(), r.PathValue("id"), consumer)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "unavailable", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func itemReadResponse(item Item) readResponse {
	var contextValue json.RawMessage
	if item.Context != "" {
		contextValue = json.RawMessage(item.Context)
	}
	response := readResponse{ID: item.ID, Consumer: item.Consumer, Origin: item.Origin, Key: item.Key, Context: contextValue, Status: item.Status}
	if item.Status == StatusDone {
		result := json.RawMessage(item.Result)
		response.Result = &result
	}
	if item.Status == StatusFailed {
		response.Error = item.Error
		response.Code = item.ErrorCode
	}
	if item.Status == StatusDone || item.Status == StatusFailed {
		cost := item.CostUSD
		response.CostUSD = &cost
		if item.UsageJSON != "" {
			usage := json.RawMessage(item.UsageJSON)
			response.Usage = &usage
		}
	}
	return response
}

func decodeError(err error) string {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return "request body exceeds 10 MiB"
	}
	return "invalid JSON body: " + err.Error()
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid JSON body: multiple values")
		}
		return errors.New(decodeError(err))
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	encoded, err := marshalHTTPJSON(value)
	if err != nil {
		http.Error(w, "encode JSON response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(encoded, '\n'))
}

func marshalHTTPJSON(value any) ([]byte, error) {
	switch typed := value.(type) {
	case readResponse:
		return marshalReadResponse(typed)
	case []readResponse:
		var encoded bytes.Buffer
		encoded.WriteByte('[')
		for i, response := range typed {
			if i > 0 {
				encoded.WriteByte(',')
			}
			item, err := marshalReadResponse(response)
			if err != nil {
				return nil, err
			}
			encoded.Write(item)
		}
		encoded.WriteByte(']')
		return encoded.Bytes(), nil
	default:
		return json.Marshal(value)
	}
}

func marshalReadResponse(response readResponse) ([]byte, error) {
	contextValue := response.Context
	response.Context = nil
	encoded, err := json.Marshal(response)
	if err != nil || len(contextValue) == 0 {
		return encoded, err
	}
	return bytes.Replace(encoded, []byte(`"context":null`), append([]byte(`"context":`), contextValue...), 1), nil
}
