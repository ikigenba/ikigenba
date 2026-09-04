package agentkit

import (
	"encoding/json"
	"fmt"
)

type chatWire struct{ wireCodec }

// ChatWire returns the built-in generic Chat Completions wire codec (used by
// xai and openrouter). Its request body grammar and decoded events are
// identical to OpenAIChatWire's; only the credential header logic differs,
// and that lives outside the wire.
func ChatWire() WireFormat { return newChatWire(nil) }

func newChatWire(classifier errorClassifier) wireFormat {
	wire := &chatWire{}
	wire.wireCodec = wireCodec{
		encode:     wire.encodeRequest,
		decoder:    newOpenAIChatDecoder,
		reserved:   []string{"openai", "response_format"},
		classifier: classifier,
		capabilities: wireCapabilities{
			name:       "Chat Completions",
			reasoning:  reasoningShapeOff | reasoningShapeEffort,
			toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
		},
	}
	return wire
}

func (w *chatWire) encodeRequest(state requestState) ([]byte, error) {
	messages, err := buildOpenAIChatMessages(state.History)
	if err != nil {
		return nil, err
	}
	request := openAIChatRequest{Model: state.Model, Messages: messages}
	if state.Output != nil {
		schema, renderErr := w.renderOutputSchema(state.Output.Schema)
		if renderErr != nil {
			return nil, fmt.Errorf("agentkit: render OpenAI Chat output schema: %w", renderErr)
		}
		request.ResponseFormat = &openAIChatResponseFormat{
			Type: "json_schema",
			JSONSchema: openAIChatJSONSchema{
				Name: "agentkit_output", Strict: true, Schema: schema,
			},
		}
	}
	configureOpenAIChatRequest(&request, state.Settings)
	if len(state.Tools) > 0 {
		request.Tools, err = w.RenderTools(state.Tools)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(request)
	return append(encoded, '\n'), err
}

func (w *chatWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderOpenAIChatTools(tools)
}
