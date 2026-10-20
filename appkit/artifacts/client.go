// Package artifacts streams files through the artifacts service.
package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// ClientConfig supplies the sibling service and event writer.
type ClientConfig struct {
	Service   services.Entry
	Telemetry *telemetry.Writer
}

// Client is a concurrent, streaming files client.
type Client struct {
	service services.Entry
	http    *http.Client
	mcp     *mcp.Client
}

// Metadata describes a stored file.
type Metadata struct {
	ID, Path                   string
	Bytes                      int64
	ContentType, SHA256, Owner string
	Created, Modified, Expires time.Time
	URL                        string
}

// PutOptions controls the stored content type and expiry.
type PutOptions struct {
	ContentType string
	Expires     time.Time
}

// The service's typed refusals.
var (
	ErrBadPath     = errors.New("artifacts: bad path")
	ErrNotFound    = errors.New("artifacts: not found")
	ErrForbidden   = errors.New("artifacts: forbidden")
	ErrTooLarge    = errors.New("artifacts: too large")
	ErrUnavailable = errors.New("artifacts: unavailable")
)

// HTTPError describes an unexpected response, with a bounded body excerpt.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("artifacts: unexpected HTTP response (status %d)", e.StatusCode)
}

// NewClient builds a client without contacting the service.
func NewClient(cfg ClientConfig) *Client {
	client := telemetry.SiblingClient(cfg.Telemetry, cfg.Service.Name, telemetry.SocketTransport(cfg.Service.Socket))
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	endpoint := (&url.URL{Scheme: "http", Host: cfg.Service.Name, Path: "/mcp"}).String()
	return &Client{service: cfg.Service, http: client, mcp: mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint, HTTPClient: client})}
}

func sendable(path string) bool {
	for i := 0; i < len(path); i++ {
		b := path[i]
		if b == '%' {
			if i+2 >= len(path) || !hex(path[i+1]) || !hex(path[i+2]) {
				return false
			}
			i += 2
			continue
		}
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("/-._~!$&'()*+,;=:@", rune(b)) {
			continue
		}
		return false
	}
	return true
}
func hex(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }
func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if !sendable(path) {
		return nil, ErrBadPath
	}
	raw := "/files/" + path
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return nil, ErrBadPath
	}
	// Wrapping avoids transferring ownership when the reader is a closer.
	if body != nil {
		body = struct{ io.Reader }{body}
	}
	r, err := http.NewRequestWithContext(ctx, method, "http://"+c.service.Name, body)
	if err != nil {
		return nil, fmt.Errorf("artifacts: request: %w", err)
	}
	r.URL.Path, r.URL.RawPath = decoded, raw
	return r, nil
}
func (c *Client) send(r *http.Request) (*http.Response, error) {
	response, err := c.http.Do(r)
	if err != nil {
		return nil, fmt.Errorf("artifacts: request failed: %w", err)
	}
	return response, nil
}
func responseError(r *http.Response) error {
	switch r.StatusCode {
	case 400:
		return ErrBadPath
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 413:
		return ErrTooLarge
	case 503:
		return ErrUnavailable
	}
	data, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	return &HTTPError{r.StatusCode, string(data)}
}

// Put streams body without closing it and returns the service's metadata.
func (c *Client) Put(ctx context.Context, path string, opts PutOptions, body io.Reader) (Metadata, error) {
	r, err := c.request(ctx, http.MethodPut, path, body)
	if err != nil {
		return Metadata{}, err
	}
	if opts.ContentType != "" {
		r.Header.Set("Content-Type", opts.ContentType)
	}
	if !opts.Expires.IsZero() {
		r.Header.Set("X-Artifact-Expires", opts.Expires.UTC().Format(time.RFC3339))
	}
	response, err := c.send(r)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 && response.StatusCode != 201 {
		return Metadata{}, responseError(response)
	}
	m, err := decodeMetadata(response.Body)
	if err != nil {
		return Metadata{}, fmt.Errorf("artifacts: metadata: %w", err)
	}
	return m, nil
}
func decodeMetadata(body io.Reader) (Metadata, error) {
	var fields map[string]json.RawMessage
	d := json.NewDecoder(body)
	if err := d.Decode(&fields); err != nil {
		return Metadata{}, err
	}
	if fields == nil {
		return Metadata{}, errors.New("expected object")
	}
	var m Metadata
	for name, ptr := range map[string]*string{"id": &m.ID, "path": &m.Path, "content_type": &m.ContentType, "sha256": &m.SHA256, "owner": &m.Owner, "url": &m.URL} {
		raw := fields[name]
		if len(raw) == 0 || raw[0] != '"' {
			return Metadata{}, fmt.Errorf("invalid %s", name)
		}
		if err := json.Unmarshal(raw, ptr); err != nil {
			return Metadata{}, err
		}
	}
	if raw := fields["bytes"]; len(raw) == 0 || string(raw) == "null" {
		return Metadata{}, errors.New("invalid bytes")
	}
	if err := json.Unmarshal(fields["bytes"], &m.Bytes); err != nil {
		return Metadata{}, err
	}
	for name, ptr := range map[string]*time.Time{"created": &m.Created, "modified": &m.Modified, "expires": &m.Expires} {
		raw, present := fields[name]
		if name == "expires" && !present {
			continue
		}
		var text string
		if len(raw) == 0 || raw[0] != '"' {
			return Metadata{}, fmt.Errorf("invalid %s", name)
		}
		if err := json.Unmarshal(raw, &text); err != nil {
			return Metadata{}, err
		}
		instant, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return Metadata{}, err
		}
		*ptr = instant.UTC()
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return Metadata{}, errors.New("expected exactly one object")
	}
	return m, nil
}
func (c *Client) headers(response *http.Response, path string) (Metadata, error) {
	length := response.Header.Get("Content-Length")
	if length == "" || strings.Trim(length, "0123456789") != "" {
		return Metadata{}, errors.New("artifacts: invalid content length")
	}
	size, err := strconv.ParseInt(length, 10, 64)
	if err != nil {
		return Metadata{}, fmt.Errorf("artifacts: content length: %w", err)
	}
	tag := response.Header.Get("ETag")
	if len(tag) < 3 || tag[0] != '"' || tag[len(tag)-1] != '"' || strings.Contains(tag[1:len(tag)-1], "\"") {
		return Metadata{}, errors.New("artifacts: invalid ETag")
	}
	modified, err := http.ParseTime(response.Header.Get("Last-Modified"))
	if err != nil {
		return Metadata{}, fmt.Errorf("artifacts: last modified: %w", err)
	}
	return Metadata{Path: path, Bytes: size, ContentType: response.Header.Get("Content-Type"), SHA256: tag[1 : len(tag)-1], Modified: modified.UTC(), URL: strings.TrimRight(c.service.URL, "/") + "/files/" + path}, nil
}

// Get returns the headers and the response stream, which the caller closes.
func (c *Client) Get(ctx context.Context, path string) (Metadata, io.ReadCloser, error) {
	r, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Metadata{}, nil, err
	}
	response, err := c.send(r)
	if err != nil {
		return Metadata{}, nil, err
	}
	if response.StatusCode != 200 {
		err = responseError(response)
	} else {
		var m Metadata
		m, err = c.headers(response, path)
		if err == nil {
			return m, response.Body, nil
		}
	}
	_ = response.Body.Close()
	return Metadata{}, nil, err
}

// Head returns metadata carried in response headers.
func (c *Client) Head(ctx context.Context, path string) (Metadata, error) {
	r, err := c.request(ctx, http.MethodHead, path, nil)
	if err != nil {
		return Metadata{}, err
	}
	response, err := c.send(r)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return Metadata{}, responseError(response)
	}
	return c.headers(response, path)
}

// Refusal carries the tool's refusal text without interpreting it.
type Refusal struct{ Text string }

func (r *Refusal) Error() string { return "artifacts: " + r.Text }

// Show reads the full metadata through the service's show tool.
func (c *Client) Show(ctx context.Context, path string) (Metadata, error) {
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return Metadata{}, fmt.Errorf("artifacts: encode arguments: %w", err)
	}
	caller, _ := identity.FromContext(ctx)
	result, err := c.mcp.CallTool(ctx, caller, "show", args)
	if err != nil {
		var rpc *mcp.RPCError
		if errors.As(err, &rpc) {
			return Metadata{}, fmt.Errorf("artifacts: call show: %w", err)
		}
		if ctx.Err() != nil {
			return Metadata{}, fmt.Errorf("artifacts: call show: %w", errors.Join(err, ctx.Err()))
		}
		var he *mcp.HTTPError
		if errors.As(err, &he) {
			return Metadata{}, &HTTPError{StatusCode: he.StatusCode, Body: he.Body}
		}
		return Metadata{}, fmt.Errorf("artifacts: call show: %w", err)
	}
	raw, err := result.MarshalJSON()
	if err != nil {
		return Metadata{}, showOutputError(ctx, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Metadata{}, showOutputError(ctx, err)
	}
	if result.IsError() {
		return Metadata{}, &Refusal{Text: refusalText(fields["content"])}
	}
	metadata, err := decodeMetadata(bytes.NewReader(fields["structuredContent"]))
	if err != nil {
		return Metadata{}, showOutputError(ctx, err)
	}
	return metadata, nil
}

func showOutputError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	return fmt.Errorf("artifacts: invalid output: %w", err)
}

func refusalText(raw json.RawMessage) string {
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, block := range blocks {
		var fields map[string]json.RawMessage
		if json.Unmarshal(block, &fields) != nil {
			continue
		}
		var kind, text string
		typeRaw, textRaw := bytes.TrimSpace(fields["type"]), bytes.TrimSpace(fields["text"])
		if len(typeRaw) > 0 && typeRaw[0] == '"' && len(textRaw) > 0 && textRaw[0] == '"' && json.Unmarshal(typeRaw, &kind) == nil && kind == "text" && json.Unmarshal(textRaw, &text) == nil {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}
