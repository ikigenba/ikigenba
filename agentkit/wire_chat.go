package agentkit

import (
	"encoding/json"
	"fmt"
)

type chatWire struct{ wireCodec }

// ChatWire returns the built-in generic Chat Completions wire codec (used by
// openrouter). Its request body grammar and decoded events are
// identical to OpenAIChatWire's; only the credential header logic differs,
// and that lives outside the wire.
func ChatWire() WireFormat { return newChatWire() }

func newChatWire() wireFormat {
	wire := &chatWire{}
	wire.wireCodec = newChatWireCodec(wire.encodeRequest)
	return wire
}

func newChatWireCodec(encode func(requestState) ([]byte, error)) wireCodec {
	return wireCodec{
		encode:      encode,
		decoder:     newOpenAIChatDecoder,
		optionSpecs: wireOptionSpecsWithStop,
		capabilities: wireCapabilities{
			name:       "Chat Completions",
			reasoning:  reasoningShapeOff | reasoningShapeEffort | reasoningShapeOn | reasoningShapeBudget,
			toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
		},
	}
}

func (w *chatWire) encodeRequest(state requestState) ([]byte, error) {
	return encodeChatRequest(&w.wireCodec, state)
}

func encodeChatRequest(codec *wireCodec, state requestState) ([]byte, error) {
	messages, err := buildOpenAIChatMessages(state.History)
	if err != nil {
		return nil, err
	}
	request := openAIChatRequest{
		Model:         state.Model,
		Messages:      messages,
		Stream:        true,
		StreamOptions: openAIChatStreamOptions{IncludeUsage: true},
	}
	if state.Output != nil {
		schema, renderErr := codec.renderOutputSchema(state.Output.Schema)
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
		if state.Settings.SerialToolCalls {
			parallel := false
			request.ParallelToolCalls = &parallel
		}
		request.Tools, err = renderChatTools(state.Tools)
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

func renderChatTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderOpenAIChatTools(tools)
}
