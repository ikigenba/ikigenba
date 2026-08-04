// Package record defines the telemetry protocol record.
package record

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TimeLayout is the fixed-width UTC timestamp representation used by telemetry.
const TimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Kind identifies the operation represented by a record.
type Kind string

const (
	KindEdge      Kind = "edge"
	KindRequest   Kind = "request"
	KindOutbound  Kind = "outbound"
	KindPublish   Kind = "publish"
	KindConsume   Kind = "consume"
	KindRoot      Kind = "root"
	KindLifecycle Kind = "lifecycle"
)

// Record is the exact telemetry protocol record stored by the service.
type Record struct {
	ID            string          `json:"id"`
	Time          string          `json:"time"`
	CorrelationID string          `json:"correlation_id"`
	Service       string          `json:"service"`
	Kind          Kind            `json:"kind"`
	Actor         Actor           `json:"actor"`
	Op            string          `json:"op"`
	Params        json.RawMessage `json:"params,omitempty"`
	Outcome       Outcome         `json:"outcome"`
	Detail        json.RawMessage `json:"detail,omitempty"`
}

// Actor identifies the owner and client responsible for an operation.
type Actor struct {
	OwnerEmail string `json:"owner_email,omitempty"`
	ClientID   string `json:"client_id,omitempty"`
}

// Outcome contains the protocol's operation result fields.
type Outcome struct {
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
	Bytes      *int64 `json:"bytes,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
}

// Validate enforces the telemetry protocol shape without rewriting the record.
func (r Record) Validate() error {
	if !validID(r.ID) {
		return errors.New("id must be 26 Crockford base32 characters")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.Time); err != nil {
		return fmt.Errorf("time must be RFC3339: %w", err)
	}
	if !validKind(r.Kind) {
		return fmt.Errorf("unknown kind %q", r.Kind)
	}
	if strings.TrimSpace(r.Service) == "" {
		return errors.New("service is required")
	}
	if strings.TrimSpace(r.Op) == "" {
		return errors.New("op is required")
	}
	if r.Kind != KindLifecycle && strings.TrimSpace(r.CorrelationID) == "" {
		return errors.New("correlation_id is required except for lifecycle records")
	}
	if err := validateObject("params", r.Params); err != nil {
		return err
	}
	if err := validateObject("detail", r.Detail); err != nil {
		return err
	}
	return nil
}

// NormalizeTime parses an RFC3339 timestamp and renders it in fixed-width UTC form.
func NormalizeTime(s string) (string, error) {
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return "", fmt.Errorf("parse timestamp: %w", err)
	}
	return parsed.UTC().Format(TimeLayout), nil
}

func validID(id string) bool {
	if len(id) != 26 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", c) {
			return false
		}
	}
	return true
}

func validKind(kind Kind) bool {
	switch kind {
	case KindEdge, KindRequest, KindOutbound, KindPublish, KindConsume, KindRoot, KindLifecycle:
		return true
	default:
		return false
	}
}

func validateObject(name string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return fmt.Errorf("%s must be a JSON object", name)
	}
	return nil
}
