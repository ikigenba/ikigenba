package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

// Result describes the consumer's answer.
type Result struct {
	Status     int
	Outcome    Outcome
	RetryAfter time.Duration
}

func deliveryBody(d Delivery) ([]byte, error) {
	if !validDelivered(d.Event) || d.Attempt < 1 {
		return nil, fmt.Errorf("%w: invalid delivery", ErrRejected)
	}
	b, err := d.Event.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRejected, err)
	}
	b = append(b[:len(b)-1], []byte(`,"attempt":`+strconv.Itoa(d.Attempt)+`}`)...)
	if len(b) > MaxDeliveryBytes {
		return nil, fmt.Errorf("%w: delivery too large", ErrRejected)
	}
	return b, nil
}
func decodeOutcome(body []byte, status int) (Outcome, error) {
	if len(body) > MaxEventBytes {
		return Outcome{}, errors.New("events: outcome too large")
	}
	m, err := decodeObject(body)
	if err != nil {
		return Outcome{}, err
	}
	var kind string
	if err := json.Unmarshal(m["outcome"], &kind); err != nil {
		return Outcome{}, err
	}
	if status == http.StatusOK && len(m) == 1 {
		switch kind {
		case OutcomeOK:
			return OK(), nil
		case OutcomeSkip:
			return Skip(), nil
		}
	}
	if status == http.StatusInternalServerError && len(m) == 2 && kind == OutcomeError {
		raw, ok := m["error"]
		var msg string
		if ok && len(raw) > 0 && raw[0] == '"' {
			if err := json.Unmarshal(raw, &msg); err == nil {
				return Fail(msg), nil
			}
		}
	}
	return Outcome{}, errors.New("events: invalid outcome")
}
func retryAfter(r *http.Response) time.Duration {
	if r.StatusCode != http.StatusTooManyRequests && r.StatusCode != http.StatusServiceUnavailable {
		return 0
	}
	s := r.Header.Get("Retry-After")
	if s == "" {
		return 0
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return 0
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n > uint64(math.MaxInt64/int64(time.Second)) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(n) * time.Second
}

// Send sends one broker delivery with the supplied HTTP client.
func Send(ctx context.Context, client *http.Client, target string, d Delivery) (Result, error) {
	if client == nil {
		panic("events: client is nil")
	}
	b, err := deliveryBody(d)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+target+EventsPath, bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return Result{}, err
	}
	result := Result{Status: resp.StatusCode}
	if resp.StatusCode == http.StatusNotFound {
		result.Outcome = Skip()
		return result, nil
	}
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusInternalServerError {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxEventBytes+1))
		if readErr == nil {
			o, decodeErr := decodeOutcome(body, resp.StatusCode)
			if decodeErr == nil {
				result.Outcome = o
				return result, nil
			}
		}
	}
	result.RetryAfter = retryAfter(resp)
	return result, fmt.Errorf("events: invalid delivery response (HTTP %d)", resp.StatusCode)
}
