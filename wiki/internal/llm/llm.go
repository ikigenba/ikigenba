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
	"strings"
)

// ErrTruncated reports a completion that reached its configured output ceiling.
var ErrTruncated = errors.New("llm: response truncated")

// Client posts completions to the prompts service.
type Client struct {
	baseURL string
	http    *http.Client
}

// New constructs a concurrency-safe prompts client. Calls carry their own
// context deadlines, so the shared HTTP client deliberately has no timeout.
func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

// Config is the prompts /complete generation configuration vocabulary.
type Config struct {
	Model       string   `json:"model"`
	Provider    string   `json:"provider,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Effort      string   `json:"effort,omitempty"`
	Thinking    *bool    `json:"thinking,omitempty"`
}

// CallSite configures one wiki generation stage.
type CallSite struct {
	Stage           string
	System          string
	Config          Config
	MaxParseRetries int

	// Deprecated compatibility fields for package-external evaluation residue.
	Model       string
	Temperature *float64
	Reasoning   any
	MaxTokens   int
}

type disabledReasoning struct{}

// DisableReasoning is retained for package-external evaluation configuration.
func DisableReasoning() any { return disabledReasoning{} }

// Attribution identifies the origin and correlation group recorded by prompts.
type Attribution struct {
	Origin  string
	GroupID string
}

type message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type completeRequest struct {
	Origin   string         `json:"origin"`
	Name     string         `json:"name"`
	GroupID  string         `json:"group_id,omitempty"`
	Attempt  int            `json:"attempt"`
	Model    string         `json:"model"`
	Provider string         `json:"provider,omitempty"`
	Config   completeConfig `json:"config"`
	System   string         `json:"system,omitempty"`
	Messages []message      `json:"messages"`
}

type completeConfig struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Effort      string   `json:"effort,omitempty"`
	Thinking    *bool    `json:"thinking,omitempty"`
}

type completeResponse struct {
	Text     string          `json:"text"`
	Response string          `json:"response"`
	Content  string          `json:"content"`
	Usage    json.RawMessage `json:"usage"`
}

// JSON runs one structured generation and validates the decoded value.
func JSON[T any](ctx context.Context, c *Client, site CallSite, attr Attribution, userText string, validate func(*T) error) (T, error) {
	var zero T
	if c == nil || c.http == nil || c.baseURL == "" {
		return zero, fmt.Errorf("llm JSON: invalid prompts client")
	}

	messages := []message{{Role: "user", Text: userText}}
	attempts := site.MaxParseRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		text, err := c.complete(ctx, site, attr, attempt, messages)
		if err != nil {
			return zero, err
		}

		var out T
		if err := json.Unmarshal([]byte(ExtractJSON(text)), &out); err != nil {
			lastErr = err
		} else if validate != nil {
			lastErr = validate(&out)
		} else {
			lastErr = nil
		}
		if lastErr == nil {
			return out, nil
		}
		if attempt < attempts {
			messages = []message{
				{Role: "user", Text: userText},
				{Role: "assistant", Text: text},
				{Role: "user", Text: correctivePrompt(userText, lastErr)},
			}
		}
	}
	return zero, fmt.Errorf("llm JSON: parse or validation failed after %d attempt(s): %w", attempts, lastErr)
}

func (c *Client) complete(ctx context.Context, site CallSite, attr Attribution, attempt int, messages []message) (string, error) {
	config := effectiveConfig(site)
	reqBody := completeRequest{
		Origin:   attr.Origin,
		Name:     "wiki." + site.Stage,
		GroupID:  attr.GroupID,
		Attempt:  attempt,
		Model:    config.Model,
		Provider: config.Provider,
		Config: completeConfig{
			Temperature: config.Temperature,
			MaxTokens:   config.MaxTokens,
			Effort:      config.Effort,
			Thinking:    config.Thinking,
		},
		System:   site.System,
		Messages: messages,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("llm: encode /complete request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/complete", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("llm: build /complete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: prompts /complete: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("llm: read prompts /complete response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", promptsStatusError(resp.StatusCode, body)
	}
	var out completeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("llm: decode prompts /complete response: %w", err)
	}
	text := out.Text
	if text == "" {
		text = out.Response
	}
	if text == "" {
		text = out.Content
	}
	if output, ok := outputUsage(out.Usage); ok && config.MaxTokens > 0 && output >= int64(config.MaxTokens) {
		return "", fmt.Errorf("%w: stage %s output usage %d reached max_tokens %d", ErrTruncated, site.Stage, output, config.MaxTokens)
	}
	return text, nil
}

func effectiveConfig(site CallSite) Config {
	config := site.Config
	if site.Model != "" {
		config.Model = site.Model
	}
	if site.Temperature != nil {
		config.Temperature = site.Temperature
	}
	if site.MaxTokens != 0 {
		config.MaxTokens = site.MaxTokens
	}
	if site.Reasoning != nil {
		if _, disabled := site.Reasoning.(disabledReasoning); disabled {
			value := false
			config.Thinking = &value
		} else if value, ok := site.Reasoning.(interface{ Disabled() bool }); ok && value.Disabled() {
			thinking := false
			config.Thinking = &thinking
		} else if value, ok := site.Reasoning.(interface{ Level() (string, bool) }); ok {
			if level, valid := value.Level(); valid {
				config.Effort = level
			}
		} else {
			config.Effort = fmt.Sprint(site.Reasoning)
		}
	}
	return config
}

func promptsStatusError(status int, body []byte) error {
	return promptsEndpointStatusError("/complete", status, body)
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
	for _, key := range []string{"output", "output_tokens"} {
		if value, ok := usage[key]; ok {
			var n int64
			if json.Unmarshal(value, &n) == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// ExtractJSON carves the first JSON object or array from a decorated reply.
func ExtractJSON(text string) string {
	s := strings.TrimSpace(text)
	firstObject := strings.IndexByte(s, '{')
	firstArray := strings.IndexByte(s, '[')
	start := firstObject
	close := byte('}')
	if firstArray >= 0 && (start < 0 || firstArray < start) {
		start = firstArray
		close = ']'
	}
	if start < 0 {
		return s
	}
	end := strings.LastIndexByte(s, close)
	if end < start {
		return s
	}
	return strings.TrimSpace(s[start : end+1])
}

func correctivePrompt(original string, err error) string {
	return original + "\n\nThe previous response could not be parsed and validated as the requested JSON: " +
		err.Error() + "\nReturn only valid JSON for the original request."
}
