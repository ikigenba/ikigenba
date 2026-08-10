// Package mcpclient provides the minimal MCP-over-HTTP client used to talk to
// other services in the suite.
package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const protocolVersion = "2025-11-25"

// Doer is the transport seam used for every request to a peer.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client speaks MCP to one peer.
type Client struct {
	doer    Doer
	baseURL string
	headers map[string]string
}

// Tool is an MCP tool descriptor. InputSchema is deliberately left opaque.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Result is the portion of a tools/call result needed by callers.
type Result struct {
	Text    string
	IsError bool
}

// New constructs a stateless client for a peer's /mcp endpoint.
func New(doer Doer, baseURL string, headers map[string]string) *Client {
	copiedHeaders := make(map[string]string, len(headers))
	for name, value := range headers {
		copiedHeaders[name] = value
	}
	return &Client{doer: doer, baseURL: baseURL, headers: copiedHeaders}
}

// Instructions initializes the peer and returns its server-level instructions.
func (c *Client) Instructions(ctx context.Context) (string, error) {
	initialized, err := c.initialize(ctx)
	if err != nil {
		return "", err
	}
	return initialized.Instructions, nil
}

// ListTools initializes the peer and obtains its tool descriptors.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	if _, err := c.initialize(ctx); err != nil {
		return nil, err
	}
	var listed struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := c.call(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`), &listed); err != nil {
		return nil, err
	}
	tools := make([]Tool, len(listed.Tools))
	for i, tool := range listed.Tools {
		tools[i] = Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}
	}
	return tools, nil
}

// CallTool initializes the peer and invokes name with args as its arguments
// object. A peer-reported tool error remains a successful JSON-RPC exchange.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	if _, err := c.initialize(ctx); err != nil {
		return Result{}, err
	}
	encodedName, err := json.Marshal(name)
	if err != nil {
		return Result{}, c.wrap(err)
	}
	body := make([]byte, 0, len(args)+len(encodedName)+96)
	body = append(body, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":`...)
	body = append(body, encodedName...)
	body = append(body, `,"arguments":`...)
	body = append(body, args...)
	body = append(body, `}}`...)

	var called struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call(ctx, body, &called); err != nil {
		return Result{}, err
	}
	var text string
	for _, content := range called.Content {
		if content.Type == "text" {
			text += content.Text
		}
	}
	return Result{Text: text, IsError: called.IsError}, nil
}

type initializeResult struct {
	Instructions string `json:"instructions"`
}

func (c *Client) initialize(ctx context.Context) (initializeResult, error) {
	var initialized initializeResult
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + protocolVersion + `","capabilities":{},"clientInfo":{"name":"prompts","version":"1"}}}`)
	if err := c.call(ctx, body, &initialized); err != nil {
		return initializeResult{}, err
	}
	if err := c.notify(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)); err != nil {
		return initializeResult{}, err
	}
	return initialized, nil
}

func (c *Client) call(ctx context.Context, body []byte, result any) error {
	response, err := c.post(ctx, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(response.Body)
		return c.wrap(fmt.Errorf("HTTP status %s: %s", response.Status, bytes.TrimSpace(message)))
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return c.wrap(fmt.Errorf("decode JSON-RPC response: %w", err))
	}
	if envelope.JSONRPC != "2.0" {
		return c.wrap(fmt.Errorf("malformed JSON-RPC envelope: version %q", envelope.JSONRPC))
	}
	if envelope.Error != nil {
		return c.wrap(fmt.Errorf("JSON-RPC error %d: %s", envelope.Error.Code, envelope.Error.Message))
	}
	if len(envelope.Result) == 0 || bytes.Equal(envelope.Result, []byte("null")) {
		return c.wrap(fmt.Errorf("malformed JSON-RPC envelope: missing result"))
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return c.wrap(fmt.Errorf("decode JSON-RPC result: %w", err))
	}
	return nil
}

func (c *Client) notify(ctx context.Context, body []byte) error {
	response, err := c.post(ctx, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(response.Body)
		return c.wrap(fmt.Errorf("HTTP status %s: %s", response.Status, bytes.TrimSpace(message)))
	}
	return nil
}

func (c *Client) post(ctx context.Context, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, c.wrap(fmt.Errorf("create request: %w", err))
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range c.headers {
		request.Header.Set(name, value)
	}
	response, err := c.doer.Do(request)
	if err != nil {
		return nil, c.wrap(err)
	}
	return response, nil
}

func (c *Client) wrap(err error) error {
	return fmt.Errorf("MCP peer %s: %w", c.baseURL, err)
}
