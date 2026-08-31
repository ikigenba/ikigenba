package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
)

// ReplayEncoding identifies the opaque body encoding a wire uses when it
// replays provider reasoning. Endpoint-specific overrides arrive in D6.
type ReplayEncoding uint8

const (
	replayEncodingProviderBlock ReplayEncoding = iota
	replayEncodingMessageItem
)

// Tool is the sealed canonical tool shape consumed by wire renderers.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Call(ctx context.Context, args json.RawMessage) (string, error)
	isTool()
}

// Framer splits a response body into payload frames without interpreting the
// vendor grammar carried by those frames.
type Framer func(io.Reader) iter.Seq2[[]byte, error]

// WireFormat is the internal codec contract selected by provider constructors.
type WireFormat interface {
	EncodeRequest(state RequestState) ([]byte, error)
	DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error]
	RenderTools(tools []Tool) (json.RawMessage, error)
	DefaultReplayEncoding() ReplayEncoding
	ReservedKeys() []string
}

type wireClassifier func(status int, header http.Header, body []byte) error

type frameDecoder func(frame []byte) (message *Message, usage usageFragment, hasUsage bool, err error)

type wireCodec struct {
	encode     func(RequestState) ([]byte, error)
	decoder    func() frameDecoder
	render     func([]Tool) (json.RawMessage, error)
	replay     ReplayEncoding
	reserved   []string
	classifier wireClassifier
	lastUsage  Usage
}

func (w *wireCodec) EncodeRequest(state RequestState) ([]byte, error) {
	return w.encode(state)
}

func (w *wireCodec) DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		decode := w.decoder()
		var fragments []usageFragment
		for frame, frameErr := range frames {
			if frameErr != nil {
				yield(nil, frameErr)
				return
			}
			if w.classifier != nil {
				err := w.classifier(http.StatusOK, nil, append([]byte(nil), frame...))
				if err != nil {
					yield(nil, err)
					return
				}
			}

			message, fragment, hasUsage, err := decode(frame)
			if err != nil {
				yield(nil, err)
				return
			}
			if hasUsage {
				fragments = append(fragments, fragment)
				w.lastUsage = mergeUsage(fragments...)
			}
			if message != nil && !yield(*message, nil) {
				return
			}
		}
	}
}

func (w *wireCodec) RenderTools(tools []Tool) (json.RawMessage, error) {
	for _, tool := range tools {
		if err := validateCanonicalToolSchema(tool.Schema()); err != nil {
			return nil, fmt.Errorf("agentkit: tool %q: %w", tool.Name(), err)
		}
	}
	return w.render(tools)
}

func (w *wireCodec) DefaultReplayEncoding() ReplayEncoding { return w.replay }

func (w *wireCodec) ReservedKeys() []string {
	return append([]string(nil), w.reserved...)
}

func validateCanonicalToolSchema(schema json.RawMessage) error {
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return fmt.Errorf("invalid JSON schema: %w", err)
	}
	object, ok := root.(map[string]any)
	if !ok || object["type"] != "object" {
		return errors.New("schema root must have type object")
	}
	return rejectUnsupportedSchemaKeywords(object)
}

func rejectUnsupportedSchemaKeywords(value any) error {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			switch key {
			case "$ref", "$defs", "definitions", "allOf", "anyOf", "oneOf", "not", "if", "then", "else":
				return fmt.Errorf("unsupported schema keyword %q", key)
			}
			if err := rejectUnsupportedSchemaKeywords(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := rejectUnsupportedSchemaKeywords(child); err != nil {
				return err
			}
		}
	}
	return nil
}

type usageNormalizer struct {
	input     *int64
	cached    *int64
	output    *int64
	reasoning *int64
}

func (n *usageNormalizer) update(input, cached, output, reasoning *int64) usageFragment {
	n.input = lastPresent(n.input, input)
	n.cached = lastPresent(n.cached, cached)
	n.output = lastPresent(n.output, output)
	n.reasoning = lastPresent(n.reasoning, reasoning)

	fragment := usageFragment{CachedTokens: n.cached, ReasoningTokens: n.reasoning}
	if n.input != nil {
		value := *n.input
		if n.cached != nil {
			value -= *n.cached
		}
		fragment.InputTokens = &value
	}
	if n.output != nil {
		value := *n.output
		if n.reasoning != nil {
			value -= *n.reasoning
		}
		fragment.OutputTokens = &value
	}
	return fragment
}

func lastPresent(previous, current *int64) *int64 {
	if current != nil {
		return current
	}
	return previous
}
