package agentkit

import (
	"encoding/json"
	"fmt"
)

type responsesWire struct{ wireCodec }

// ResponsesWire returns the built-in generic Responses wire codec (used by
// xai and openrouter). Its request body grammar and decoded events are
// identical to OpenAIResponsesWire's; only the credential header logic
// differs, and that lives outside the wire.
func ResponsesWire() WireFormat { return newResponsesWire(nil) }

func newResponsesWire(classifier errorClassifier) wireFormat {
	wire := &responsesWire{}
	wire.wireCodec = wireCodec{
		encode:     wire.encodeRequest,
		decoder:    newOpenAIResponsesDecoder,
		reserved:   []string{"openai", "text"},
		classifier: classifier,
		capabilities: wireCapabilities{
			name:       "Responses",
			reasoning:  reasoningShapeOff | reasoningShapeEffort,
			toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
		},
	}
	return wire
}

func (w *responsesWire) encodeRequest(state requestState) ([]byte, error) {
	input, err := buildOpenAIResponsesInput(state.History)
	if err != nil {
		return nil, err
	}
	request := buildOpenAIResponsesRequest(input, state.Settings)
	request.Model = state.Model
	if state.Output != nil {
		schema, renderErr := w.renderOutputSchema(state.Output.Schema)
		if renderErr != nil {
			return nil, fmt.Errorf("agentkit: render OpenAI Responses output schema: %w", renderErr)
		}
		request.Text = &openAIResponsesText{
			Format: openAIResponsesJSONSchemaFormat{
				Type: "json_schema", Name: "agentkit_output", Strict: true, Schema: schema,
			},
		}
	}
	if len(state.Tools) > 0 {
		request.Tools, err = w.RenderTools(state.Tools)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(request)
	return append(encoded, '\n'), err
}

func (w *responsesWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderOpenAIResponsesTools(tools)
}
