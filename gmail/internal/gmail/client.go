package gmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Client calls Gmail on behalf of the caller of each method.
type Client struct{ cfg Config }

// NewClient creates a client with the supplied request dependencies.
func NewClient(cfg Config) *Client {
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return &Client{cfg: cfg}
}

func statusError(status int, endpoint string, page bool) error {
	switch {
	case status == http.StatusUnauthorized:
		return ErrUnauthorized
	case status == http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	case status == http.StatusBadRequest && endpoint == EndpointList && page:
		return ErrBadPage
	case (status == 400 || status == 404) && (endpoint == EndpointGet || endpoint == EndpointAttachment || endpoint == EndpointThread || endpoint == EndpointDraft || endpoint == EndpointDelete):
		return ErrNotFound
	case status == 404 && (endpoint == EndpointSend || endpoint == EndpointUpdate):
		return ErrNotFound
	default:
		return ErrProvider
	}
}

func providerError(ctx context.Context) error {
	if ctx.Err() != nil {
		return errors.Join(ErrProvider, ctx.Err())
	}
	return ErrProvider
}

func (c *Client) exchange(ctx context.Context, token, method, target, endpoint string, header http.Header, body io.Reader) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, providerError(ctx)
	}
	r.Header = header.Clone()
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	if endpoint != EndpointChunk {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	var start time.Time
	if c.cfg.Telemetry != nil {
		start = c.cfg.Telemetry.Now()
	}
	resp, err := c.cfg.HTTP.Do(r)
	if c.cfg.Telemetry != nil {
		end := c.cfg.Telemetry.Now()
		d := int64(end.Sub(start) / time.Microsecond)
		if d < 0 {
			d = 0
		}
		var status int64
		if resp != nil {
			status = int64(resp.StatusCode)
		}
		c.cfg.Telemetry.Emit(ctx, Called, telemetry.Attrs{"endpoint": endpoint, "status": status, "duration_us": d})
	}
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, providerError(ctx)
	}
	return resp, nil
}

func (c *Client) target(path string, q url.Values) string {
	s := strings.TrimRight(c.cfg.BaseURL, "/") + path
	if len(q) > 0 {
		s += "?" + q.Encode()
	}
	return s
}

func (c *Client) request(ctx context.Context, token, method, path, endpoint string, q url.Values, data any, page bool, limit int64) ([]byte, error) {
	var body io.Reader
	h := make(http.Header)
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, ErrProvider
		}
		body = bytes.NewReader(b)
		h.Set("Content-Type", "application/json")
	}
	r, err := c.exchange(ctx, token, method, c.target(path, q), endpoint, h, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Body.Close() }()
	if page && endpoint == EndpointGet && r.StatusCode == 404 {
		return nil, nil
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return nil, statusError(r.StatusCode, endpoint, page)
	}
	var reader io.Reader = r.Body
	if limit >= 0 {
		reader = io.LimitReader(r.Body, limit+1)
	}
	b, err := io.ReadAll(reader)
	if err != nil {
		return nil, providerError(ctx)
	}
	if limit >= 0 && int64(len(b)) > limit {
		return nil, ErrTooLarge
	}
	return b, nil
}

const api = "/gmail/v1/users/me/"

// object rejects null and arrays; JSON's unknown fields are deliberately ignored.
func object(b []byte, v any) error {
	trim := bytes.TrimSpace(b)
	if len(trim) == 0 || trim[0] != '{' || json.Unmarshal(trim, v) != nil {
		return ErrProvider
	}
	return nil
}

func segment(id string) string { return url.PathEscape(id) }

// Labels calls the provider and reduces its answer to connector types.
func (c *Client) Labels(ctx context.Context, token string) ([]Label, error) {
	b, err := c.request(ctx, token, http.MethodGet, api+"labels", EndpointLabels, nil, nil, false, -1)
	if err != nil {
		return nil, err
	}
	var response struct {
		Labels []struct {
			ID   *string `json:"id"`
			Name *string `json:"name"`
			Type string  `json:"type"`
		} `json:"labels"`
	}
	if object(b, &response) != nil || arrayInvalid(b, "labels") {
		return nil, ErrProvider
	}
	labels := make([]Label, 0, len(response.Labels))
	for _, l := range response.Labels {
		if l.ID == nil || l.Name == nil {
			return nil, ErrProvider
		}
		labels = append(labels, Label{*l.ID, *l.Name, l.Type == "system"})
	}
	return labels, nil
}

func arrayInvalid(b []byte, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil {
		return true
	}
	v, ok := fields[key]
	return ok && (len(bytes.TrimSpace(v)) == 0 || bytes.TrimSpace(v)[0] != '[')
}
