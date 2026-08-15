// Package telemetry defines the bounded, allowlisted records emitted by appkit.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

const (
	maxParamValueBytes = 1024
	maxParamsBytes     = 8192
)

// Kind identifies the role of a telemetry record in an operation chain.
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

// Actor identifies the owner or client responsible for an operation.
type Actor struct {
	OwnerEmail string `json:"owner_email,omitempty"`
	ClientID   string `json:"client_id,omitempty"`
}

// Outcome describes the bounded result metadata for an operation.
type Outcome struct {
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Bytes      int    `json:"bytes,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
}

// Record is the caller-facing, allowlisted telemetry representation of one
// operation. Identity and observation time are added by Recorder.
type Record struct {
	CorrelationID string         `json:"correlation_id,omitempty"`
	Service       string         `json:"service"`
	Kind          Kind           `json:"kind"`
	Actor         *Actor         `json:"actor,omitempty"`
	Op            string         `json:"op"`
	Params        map[string]any `json:"params,omitempty"`
	Outcome       *Outcome       `json:"outcome,omitempty"`
	Detail        map[string]any `json:"detail,omitempty"`
}

// Digest returns the byte length and lowercase hexadecimal SHA-256 of b.
func Digest(b []byte) (byteCount int, digest string) {
	sum := sha256.Sum256(b)
	return len(b), hex.EncodeToString(sum[:])
}

// EncodeParams renders raw tool or request arguments under the capture policy.
func EncodeParams(raw json.RawMessage, sensitive []string) map[string]any {
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil || params == nil {
		return map[string]any{"params": elisionMarker(compactOrRaw(raw))}
	}

	sensitiveSet := make(map[string]struct{}, len(sensitive))
	for _, name := range sensitive {
		sensitiveSet[name] = struct{}{}
	}

	type encodedValue struct {
		key     string
		encoded []byte
		elided  bool
	}

	values := make([]encodedValue, 0, len(params))
	for key, value := range params {
		encoded, _ := json.Marshal(value)
		_, isSensitive := sensitiveSet[key]
		shouldElide := isSensitive || len(encoded) > maxParamValueBytes
		if shouldElide {
			params[key] = elisionMarker(encoded)
		}
		values = append(values, encodedValue{key: key, encoded: encoded, elided: shouldElide})
	}

	sort.Slice(values, func(i, j int) bool {
		if len(values[i].encoded) == len(values[j].encoded) {
			return values[i].key < values[j].key
		}
		return len(values[i].encoded) > len(values[j].encoded)
	})

	for _, value := range values {
		encodedParams, _ := json.Marshal(params)
		if len(encodedParams) <= maxParamsBytes {
			break
		}
		if value.elided {
			continue
		}
		params[value.key] = elisionMarker(value.encoded)
	}

	return params
}

func elisionMarker(encoded []byte) map[string]any {
	bytes, digest := Digest(encoded)
	return map[string]any{
		"$elided": map[string]any{
			"bytes":  bytes,
			"sha256": digest,
		},
	}
}

func compactOrRaw(raw []byte) []byte {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	compact, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return compact
}
