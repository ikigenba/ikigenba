package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func parseDraft(b []byte) (Draft, error) {
	var response struct {
		ID      string `json:"id"`
		Message *struct {
			ID     string `json:"id"`
			Thread string `json:"threadId"`
		} `json:"message"`
	}
	if object(b, &response) != nil || response.ID == "" || response.Message == nil || response.Message.ID == "" {
		return Draft{}, ErrProvider
	}
	return Draft{response.ID, response.Message.ID, response.Message.Thread}, nil
}

// CreateDraft calls the provider and reduces its answer to connector types.
func (c *Client) CreateDraft(ctx context.Context, token, thread string, o *Outgoing) (Draft, error) {
	if o.Size() > UploadCap {
		return Draft{}, ErrTooLarge
	}
	defer func() { _ = o.Close() }()
	raw, err := io.ReadAll(o)
	if err != nil {
		return Draft{}, err
	}
	message := map[string]string{"raw": base64.URLEncoding.EncodeToString(raw)}
	if thread != "" {
		message["threadId"] = thread
	}
	b, err := c.request(ctx, token, http.MethodPost, api+"drafts", EndpointCreate, nil, map[string]any{"message": message}, false, -1)
	if err != nil {
		return Draft{}, err
	}
	return parseDraft(b)
}

// GetDraft calls the provider and reduces its answer to connector types.
func (c *Client) GetDraft(ctx context.Context, token, draft string) (Draft, error) {
	if draft == "" {
		return Draft{}, ErrNotFound
	}
	b, err := c.request(ctx, token, http.MethodGet, api+"drafts/"+segment(draft), EndpointDraft, url.Values{"format": {"minimal"}}, nil, false, -1)
	if err != nil {
		return Draft{}, err
	}
	return parseDraft(b)
}

// SendDraft calls the provider and reduces its answer to connector types.
func (c *Client) SendDraft(ctx context.Context, token, draft string) (Draft, error) {
	if draft == "" {
		return Draft{}, ErrNotFound
	}
	b, err := c.request(ctx, token, http.MethodPost, api+"drafts/send", EndpointSend, nil, map[string]string{"id": draft}, false, -1)
	if err != nil {
		return Draft{}, err
	}
	var response struct {
		ID     string `json:"id"`
		Thread string `json:"threadId"`
	}
	if object(b, &response) != nil || response.ID == "" {
		return Draft{}, ErrProvider
	}
	return Draft{draft, response.ID, response.Thread}, nil
}

// DeleteDraft calls the provider and reduces its answer to connector types.
func (c *Client) DeleteDraft(ctx context.Context, token, draft string) error {
	if draft == "" {
		return ErrNotFound
	}
	_, err := c.request(ctx, token, http.MethodDelete, api+"drafts/"+segment(draft), EndpointDelete, nil, nil, false, -1)
	return err
}

// UpdateDraft calls the provider and reduces its answer to connector types.
func (c *Client) UpdateDraft(ctx context.Context, token, draft, thread string, o *Outgoing) (Draft, error) {
	if draft == "" {
		return Draft{}, ErrNotFound
	}
	if o.Size() > UploadCap {
		return Draft{}, ErrTooLarge
	}
	defer func() { _ = o.Close() }()
	metadata := map[string]any{"id": draft}
	if thread != "" {
		metadata["message"] = map[string]string{"threadId": thread}
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return Draft{}, ErrProvider
	}
	h := http.Header{"Content-Type": {"application/json; charset=UTF-8"}, "X-Upload-Content-Type": {"message/rfc822"}, "X-Upload-Content-Length": {strconv.FormatInt(o.Size(), 10)}}
	r, err := c.exchange(ctx, token, http.MethodPut, c.target("/upload/gmail/v1/users/me/drafts/"+segment(draft), url.Values{"uploadType": {"resumable"}}), EndpointUpdate, h, bytes.NewReader(b))
	if err != nil {
		return Draft{}, err
	}
	location := r.Header.Get("Location")
	status := r.StatusCode
	_, readErr := io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()
	if status < 200 || status >= 300 {
		return Draft{}, statusError(status, EndpointUpdate, false)
	}
	if readErr != nil {
		return Draft{}, providerError(ctx)
	}
	u, err := url.Parse(location)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return Draft{}, ErrProvider
	}
	for offset := int64(0); offset < o.Size(); {
		n := min(int64(PushChunk), o.Size()-offset)
		piece := make([]byte, n)
		_, err = io.ReadFull(o, piece)
		if err != nil {
			return Draft{}, err
		}
		last := offset+n == o.Size()
		if last {
			var probe [1]byte
			_, err = o.Read(probe[:])
			if !errors.Is(err, io.EOF) {
				if err == nil {
					err = ErrPartSize
				}
				return Draft{}, err
			}
		}
		end := offset + n - 1
		h = http.Header{"Content-Range": {fmt.Sprintf("bytes %d-%d/%d", offset, end, o.Size())}}
		r, err = c.exchange(ctx, "", http.MethodPut, location, EndpointChunk, h, bytes.NewReader(piece))
		if err != nil {
			return Draft{}, err
		}
		if !last {
			status = r.StatusCode
			rangeHeader := r.Header.Get("Range")
			_, readErr = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			if readErr != nil {
				return Draft{}, providerError(ctx)
			}
			if status != 308 || rangeHeader != fmt.Sprintf("bytes=0-%d", end) {
				return Draft{}, statusError(status, EndpointChunk, false)
			}
		} else {
			if r.StatusCode != 200 && r.StatusCode != 201 {
				_ = r.Body.Close()
				return Draft{}, statusError(r.StatusCode, EndpointChunk, false)
			}
			b, err = io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err != nil {
				return Draft{}, providerError(ctx)
			}
			return parseDraft(b)
		}
		offset += n
	}
	return Draft{}, ErrProvider
}
