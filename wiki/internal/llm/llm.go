// Package llm is wiki's stateless client for the prompts completion service.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	Consumer    = "service:wiki"
	ConsumerAsk = "service:wiki.ask"
)

var (
	ErrGone      = errors.New("llm: completion gone")
	ErrTruncated = errors.New("llm: response truncated")
)

type Client struct {
	baseURL      string
	http         *http.Client
	pollInterval time.Duration
}

// New constructs a concurrency-safe prompts client. The optional client is
// retained for callers which need a transport carrying request instrumentation.
func New(baseURL string, clients ...*http.Client) *Client {
	hc := http.DefaultClient
	if len(clients) != 0 && clients[0] != nil {
		hc = clients[0]
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc, pollInterval: 500 * time.Millisecond}
}

type Config struct {
	Model       string   `json:"model"`
	Provider    string   `json:"provider,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Effort      string   `json:"effort,omitempty"`
	Thinking    *bool    `json:"thinking,omitempty"`
}

type CallSite struct {
	Stage           string
	System          string
	Config          Config
	MaxParseRetries int
}

type Attribution struct {
	Origin  string
	GroupID string
}

type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type Request struct {
	Key      string
	Context  []byte
	Site     CallSite
	Attr     Attribution
	Attempt  int
	Messages []Message
}

type Item struct {
	ID      string
	Key     string
	Status  string
	Context []byte
	Result  json.RawMessage
	Error   string
	Usage   json.RawMessage
	CostUSD float64
}

type systemComposerKey struct{}

func WithSystemComposer(ctx context.Context, compose func(string) string) context.Context {
	return context.WithValue(ctx, systemComposerKey{}, compose)
}

func systemComposer(ctx context.Context) func(string) string {
	compose, _ := ctx.Value(systemComposerKey{}).(func(string) string)
	return compose
}

type ensureRequest struct {
	Consumer string          `json:"consumer"`
	Key      string          `json:"key"`
	Context  json.RawMessage `json:"context,omitempty"`
	Origin   string          `json:"origin"`
	GroupID  string          `json:"group_id,omitempty"`
	Attempt  int             `json:"attempt"`
	Name     string          `json:"name"`
	Model    string          `json:"model"`
	Provider string          `json:"provider,omitempty"`
	Config   ensureConfig    `json:"config"`
	System   string          `json:"system,omitempty"`
	Messages []Message       `json:"messages"`
}

type ensureConfig struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Effort      string   `json:"effort,omitempty"`
	Thinking    *bool    `json:"thinking,omitempty"`
}

type wireItem struct {
	ID      string          `json:"id"`
	Key     string          `json:"key"`
	Status  string          `json:"status"`
	Context json.RawMessage `json:"context"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
	Usage   json.RawMessage `json:"usage"`
	CostUSD float64         `json:"cost_usd"`
}

func (w wireItem) item() Item {
	contextRaw := append([]byte(nil), w.Context...)
	result := append(json.RawMessage(nil), w.Result...)
	usage := append(json.RawMessage(nil), w.Usage...)
	if string(contextRaw) == "null" {
		contextRaw = nil
	}
	if string(result) == "null" {
		result = nil
	}
	if string(usage) == "null" {
		usage = nil
	}
	return Item{ID: w.ID, Key: w.Key, Status: w.Status, Context: contextRaw, Result: result, Error: w.Error, Usage: usage, CostUSD: w.CostUSD}
}

func (c *Client) Ensure(ctx context.Context, r Request) (Item, error) {
	return c.ensure(ctx, r, Consumer)
}

func (c *Client) ensure(ctx context.Context, r Request, consumer string) (Item, error) {
	if err := c.valid("Ensure"); err != nil {
		return Item{}, err
	}
	site := r.Site
	if compose := systemComposer(ctx); compose != nil {
		site.System = compose(site.System)
	}
	body := ensureRequest{
		Consumer: consumer, Key: r.Key, Context: json.RawMessage(r.Context), Origin: r.Attr.Origin,
		GroupID: r.Attr.GroupID, Attempt: r.Attempt, Name: "wiki." + site.Stage,
		Model: site.Config.Model, Provider: site.Config.Provider,
		Config: ensureConfig{Temperature: site.Config.Temperature, MaxTokens: site.Config.MaxTokens, Effort: site.Config.Effort, Thinking: site.Config.Thinking},
		System: site.System, Messages: append([]Message(nil), r.Messages...),
	}
	var out wireItem
	if err := c.doJSON(ctx, http.MethodPost, "/completions", body, &out, http.StatusOK, http.StatusAccepted); err != nil {
		return Item{}, err
	}
	return out.item(), nil
}

func (c *Client) Get(ctx context.Context, id string) (Item, error) {
	if err := c.valid("Get"); err != nil {
		return Item{}, err
	}
	var out wireItem
	err := c.doJSON(ctx, http.MethodGet, "/completions/"+url.PathEscape(id), nil, &out, http.StatusOK)
	if errors.Is(err, errNotFound) {
		return Item{}, ErrGone
	}
	if err != nil {
		return Item{}, err
	}
	return out.item(), nil
}

func (c *Client) Inbox(ctx context.Context) ([]Item, error) {
	if err := c.valid("Inbox"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/completions?consumer="+url.QueryEscape(Consumer), nil)
	if err != nil {
		return nil, fmt.Errorf("llm Inbox: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: prompts inbox: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: read prompts inbox: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, promptsEndpointStatusError("/completions", resp.StatusCode, raw)
	}
	var wire []wireItem
	if err := json.Unmarshal(raw, &wire); err != nil {
		var envelope struct {
			Items []wireItem `json:"items"`
		}
		if envelopeErr := json.Unmarshal(raw, &envelope); envelopeErr != nil {
			return nil, fmt.Errorf("llm: decode prompts inbox: %w", err)
		}
		wire = envelope.Items
	}
	items := make([]Item, len(wire))
	for i := range wire {
		items[i] = wire[i].item()
	}
	return items, nil
}

func (c *Client) Ack(ctx context.Context, id string) error {
	if err := c.valid("Ack"); err != nil {
		return err
	}
	err := c.doJSON(ctx, http.MethodDelete, "/completions/"+url.PathEscape(id), nil, nil, http.StatusOK, http.StatusNoContent)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}

func (c *Client) Do(ctx context.Context, r Request) (Item, error) {
	item, err := c.ensure(ctx, r, ConsumerAsk)
	if err != nil {
		return Item{}, err
	}
	for item.Status != "done" && item.Status != "failed" {
		interval := c.pollInterval
		if interval <= 0 {
			interval = 500 * time.Millisecond
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return Item{}, ctx.Err()
		case <-timer.C:
		}
		id := item.ID
		item, err = c.Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrGone) {
				return Item{}, fmt.Errorf("llm Do: %w while polling %s", ErrGone, id)
			}
			return Item{}, err
		}
	}
	if item.Status == "failed" {
		_ = c.Ack(ctx, item.ID)
		return Item{}, fmt.Errorf("llm: completion %s failed: %s", item.ID, item.Error)
	}
	if err := c.Ack(ctx, item.ID); err != nil {
		return Item{}, fmt.Errorf("llm: ack completion %s: %w", item.ID, err)
	}
	return item, nil
}

func JSON[T any](ctx context.Context, c *Client, site CallSite, attr Attribution, baseKey, userText string, validate func(*T) error) (T, error) {
	var zero T
	messages := []Message{{Role: "user", Text: userText}}
	attempts := site.MaxParseRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		item, err := c.Do(ctx, Request{Key: fmt.Sprintf("%s/a%d", baseKey, attempt), Site: site, Attr: attr, Attempt: attempt, Messages: messages})
		if err != nil {
			return zero, err
		}
		if output, ok := outputUsage(item.Usage); ok && site.Config.MaxTokens > 0 && output >= int64(site.Config.MaxTokens) {
			return zero, fmt.Errorf("%w: stage %s output usage %d reached max_tokens %d", ErrTruncated, site.Stage, output, site.Config.MaxTokens)
		}
		var out T
		if err := json.Unmarshal(item.Result, &out); err != nil {
			lastErr = err
		} else if validate != nil {
			lastErr = validate(&out)
		} else {
			lastErr = nil
		}
		if lastErr == nil {
			return out, nil
		}
		messages = []Message{{Role: "user", Text: userText}, {Role: "assistant", Text: string(item.Result)}, {Role: "user", Text: correctivePrompt(userText, lastErr)}}
	}
	return zero, fmt.Errorf("llm JSON: parse or validation failed after %d attempt(s): %w", attempts, lastErr)
}

var errNotFound = errors.New("llm: HTTP 404")

func (c *Client) valid(method string) error {
	if c == nil || c.http == nil || c.baseURL == "" {
		return fmt.Errorf("llm %s: invalid prompts client", method)
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, input, output any, accepted ...int) error {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("llm: encode prompts %s: %w", path, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("llm: build prompts %s: %w", path, err)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("llm: prompts %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("llm: read prompts %s: %w", path, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	for _, status := range accepted {
		if resp.StatusCode == status {
			if output == nil || len(bytes.TrimSpace(raw)) == 0 {
				return nil
			}
			if err := json.Unmarshal(raw, output); err != nil {
				return fmt.Errorf("llm: decode prompts %s: %w", path, err)
			}
			return nil
		}
	}
	return promptsEndpointStatusError(path, resp.StatusCode, raw)
}

func promptsEndpointStatusError(endpoint string, status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var envelope struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		switch value := envelope.Error.(type) {
		case string:
			message = value
		default:
			if raw, err := json.Marshal(value); err == nil {
				message = string(raw)
			}
		}
	}
	return fmt.Errorf("llm: prompts %s returned %d: %s", endpoint, status, message)
}

func outputUsage(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var usage map[string]json.RawMessage
	if json.Unmarshal(raw, &usage) != nil {
		return 0, false
	}
	for _, key := range []string{"output", "output_tokens", "completion_tokens"} {
		if value, ok := usage[key]; ok {
			var n int64
			if json.Unmarshal(value, &n) == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func correctivePrompt(original string, err error) string {
	return original + "\n\nThe previous response could not be decoded and validated as the requested JSON: " + err.Error() + "\nReturn valid JSON for the original request."
}
