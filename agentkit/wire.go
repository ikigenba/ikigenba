package agentkit

import (
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
)

// Framer splits a response body into payload frames without interpreting the
// vendor grammar carried by those frames.
type Framer func(io.Reader) iter.Seq2[[]byte, error]

// WireFormat is the internal codec contract selected by provider constructors.
type WireFormat interface {
	EncodeRequest(state RequestState) ([]byte, error)
	DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error]
	RenderTools(tools []Tool) (json.RawMessage, error)
	ReservedKeys() []string
}

type wireClassifier = ErrorClassifier

type frameDecoder func(frame []byte) (message *Message, usage usageFragment, hasUsage bool, err error)

type wireCodec struct {
	encode       func(RequestState) ([]byte, error)
	decoder      func() frameDecoder
	reserved     []string
	classifier   wireClassifier
	lastUsage    Usage
	capabilities wireCapabilities
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
				// WireFormat preserves the Framer's error verbatim; the provider
				// boundary does not declare a second, transport-specific error seam.
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
				// Decoder errors use the same declared error channel and likewise
				// propagate unchanged so callers retain the original cause.
				yield(nil, err)
				return
			}
			if hasUsage {
				fragments = append(fragments, fragment)
				w.lastUsage = mergeUsage(fragments...)
			}
			if message != nil && !yield(MessageDone{Message: *message}, nil) {
				return
			}
		}
	}
}

func validateCanonicalTools(tools []Tool) error {
	for _, tool := range tools {
		if err := ValidateToolSchema(tool.Schema()); err != nil {
			return fmt.Errorf("agentkit: tool %q: %w", tool.Name(), err)
		}
	}
	return nil
}

func (w *wireCodec) ReservedKeys() []string {
	return append([]string(nil), w.reserved...)
}

func (w *wireCodec) cloneWithClassifier(classifier wireClassifier) wireCodec {
	clone := *w
	clone.classifier = classifier
	clone.lastUsage = Usage{}
	return clone
}

func (w *wireCodec) validateSettings(settings Settings) error {
	return w.capabilities.validate(settings)
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
