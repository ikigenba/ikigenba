package agentkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Framer splits a response body into payload frames without interpreting the
// vendor grammar carried by those frames.
type Framer func(io.Reader) iter.Seq2[[]byte, error]

// wireFormat is the internal codec contract selected by provider constructors.
type wireFormat interface {
	EncodeRequest(state requestState) ([]byte, error)
	DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error]
	RenderTools(tools []Tool) (json.RawMessage, error)
	ReservedKeys() []string
}

type frameDecoder func(frame []byte) (message *Message, usage usageFragment, hasUsage bool, err error)

type wireCodec struct {
	encode       func(requestState) ([]byte, error)
	decoder      func() frameDecoder
	reserved     []string
	classifier   errorClassifier
	lastUsage    Usage
	capabilities wireCapabilities
}

func (w *wireCodec) EncodeRequest(state requestState) ([]byte, error) {
	return w.encode(state)
}

func (w *wireCodec) DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		decode := w.decoder()
		var fragments []usageFragment
		for frame, frameErr := range frames {
			if frameErr != nil {
				// wireFormat preserves the Framer's error verbatim; the provider
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

func (w *wireCodec) classifyResponse(status int, header http.Header, body []byte) error {
	if w.classifier != nil {
		if err := w.classifier(status, header, body); err != nil {
			return err
		}
	}
	return &Error{
		Category:   classifyStatus(status),
		Status:     status,
		Message:    string(body),
		RetryAfter: parseRetryAfter(header),
	}
}

// parseRetryAfter reads the RFC 9110 delta-seconds form of a Retry-After
// header (a non-negative integer count of seconds) and returns it as a
// Duration. It deliberately does not parse the HTTP-date form: that value
// depends on the wall clock, which classification has no injected source
// for. A missing header, a negative number, a non-integer, or an HTTP-date
// all yield zero.
func parseRetryAfter(header http.Header) time.Duration {
	if header == nil {
		return 0
	}
	value := header.Get("Retry-After")
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (w *wireCodec) validateSettings(settings Settings) error {
	return w.capabilities.validate(settings)
}

type outputConstraint struct {
	keyword string
	prose   func(any) string
}

var outputConstraints = []outputConstraint{
	{"minimum", func(value any) string { return "Value must be >= " + outputConstraintValue(value) + "." }},
	{"maximum", func(value any) string { return "Value must be <= " + outputConstraintValue(value) + "." }},
	{"exclusiveMinimum", func(value any) string { return "Value must be > " + outputConstraintValue(value) + "." }},
	{"exclusiveMaximum", func(value any) string { return "Value must be < " + outputConstraintValue(value) + "." }},
	{"multipleOf", func(value any) string { return "Value must be a multiple of " + outputConstraintValue(value) + "." }},
	{"minLength", func(value any) string { return "Length must be >= " + outputConstraintValue(value) + "." }},
	{"maxLength", func(value any) string { return "Length must be <= " + outputConstraintValue(value) + "." }},
	{"pattern", func(value any) string { return "Value must match pattern " + outputConstraintValue(value) + "." }},
	{"format", func(value any) string { return "Value must use format " + outputConstraintValue(value) + "." }},
	{"minItems", func(value any) string { return "Item count must be >= " + outputConstraintValue(value) + "." }},
	{"maxItems", func(value any) string { return "Item count must be <= " + outputConstraintValue(value) + "." }},
	{"uniqueItems", outputUniqueItemsConstraint},
}

// renderOutputSchema produces the common strict schema shown to every model.
// Vendor request envelopes deliberately remain the responsibility of each wire.
func (w *wireCodec) renderOutputSchema(schema json.RawMessage) (json.RawMessage, error) {
	if err := ValidateOutputSchema(schema); err != nil {
		return nil, fmt.Errorf("agentkit: render output schema: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(schema))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("agentkit: render output schema: %w", err)
	}
	renderOutputSchemaNode(root)
	rendered, err := marshalPortableOutputSchema(root)
	if err != nil {
		return nil, fmt.Errorf("agentkit: render output schema: %w", err)
	}
	return rendered, nil
}

func marshalPortableOutputSchema(root map[string]any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}), nil
}

func renderOutputSchemaNode(schema map[string]any) {
	if schema["type"] == "object" {
		if _, present := schema["additionalProperties"]; !present {
			schema["additionalProperties"] = false
		}
	}

	statements := make([]string, 0, len(outputConstraints))
	for _, constraint := range outputConstraints {
		if value, present := schema[constraint.keyword]; present {
			statements = append(statements, constraint.prose(value))
			delete(schema, constraint.keyword)
		}
	}
	if len(statements) != 0 {
		description, _ := schema["description"].(string)
		if description != "" {
			description += " "
		}
		schema["description"] = description + strings.Join(statements, " ")
	}

	for _, keyword := range []string{"properties", "$defs"} {
		if children, ok := schema[keyword].(map[string]any); ok {
			for _, child := range children {
				renderOutputSchemaNode(child.(map[string]any))
			}
		}
	}
	if item, ok := schema["items"].(map[string]any); ok {
		renderOutputSchemaNode(item)
	}
	if branches, ok := schema["anyOf"].([]any); ok {
		for _, branch := range branches {
			renderOutputSchemaNode(branch.(map[string]any))
		}
	}
}

func outputConstraintValue(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("validated output constraint cannot fail to marshal: " + err.Error())
	}
	return string(encoded)
}

func outputUniqueItemsConstraint(value any) string {
	if value.(bool) {
		return "Items must be unique."
	}
	return "Items may repeat."
}

type usageNormalizer struct {
	input        *int64
	cached       *int64
	cacheWrite5m *int64
	cacheWrite1h *int64
	output       *int64
	reasoning    *int64
}

func (n *usageNormalizer) update(input, cached, cacheWrite5m, cacheWrite1h, output, reasoning *int64) usageFragment {
	n.input = lastPresent(n.input, input)
	n.cached = lastPresent(n.cached, cached)
	n.cacheWrite5m = lastPresent(n.cacheWrite5m, cacheWrite5m)
	n.cacheWrite1h = lastPresent(n.cacheWrite1h, cacheWrite1h)
	n.output = lastPresent(n.output, output)
	n.reasoning = lastPresent(n.reasoning, reasoning)

	fragment := usageFragment{
		CachedTokens:       n.cached,
		CacheWrite5mTokens: n.cacheWrite5m,
		CacheWrite1hTokens: n.cacheWrite1h,
		ReasoningTokens:    n.reasoning,
	}
	if n.input != nil {
		value := *n.input
		if n.cached != nil {
			value -= *n.cached
		}
		if n.cacheWrite5m != nil {
			value -= *n.cacheWrite5m
		}
		if n.cacheWrite1h != nil {
			value -= *n.cacheWrite1h
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
