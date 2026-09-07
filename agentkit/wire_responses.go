package agentkit

import (
	"encoding/json"
	"fmt"
)

type responsesWire struct{ wireCodec }

// ResponsesWire returns the built-in generic Responses wire codec (used by
// openrouter). Its request body grammar and decoded events are
// identical to OpenAIResponsesWire's; only the credential header logic
// differs, and that lives outside the wire.
func ResponsesWire() WireFormat { return newResponsesWire() }

func newResponsesWire() wireFormat {
	wire := &responsesWire{}
	wire.wireCodec = newResponsesWireCodec(wire.encodeRequest)
	return wire
}

func newResponsesWireCodec(encode func(requestState) ([]byte, error)) wireCodec {
	return wireCodec{
		encode:      encode,
		decoder:     newOpenAIResponsesDecoder,
		optionSpecs: wireOptionSpecsWithoutStop,
		capabilities: wireCapabilities{
			name:       "Responses",
			reasoning:  reasoningShapeOff | reasoningShapeEffort | reasoningShapeOn | reasoningShapeBudget,
			toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
		},
	}
}

func (w *responsesWire) encodeRequest(state requestState) ([]byte, error) {
	return encodeResponsesRequest(&w.wireCodec, state)
}

func encodeResponsesRequest(codec *wireCodec, state requestState) ([]byte, error) {
	input, err := buildOpenAIResponsesInput(state.History)
	if err != nil {
		return nil, err
	}
	request := buildOpenAIResponsesRequest(input, state.Settings)
	request.Model = state.Model
	if state.Output != nil {
		schema, renderErr := codec.renderOutputSchema(state.Output.Schema)
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
		if state.Settings.SerialToolCalls {
			parallel := false
			request.ParallelToolCalls = &parallel
		}
		request.Tools, err = renderResponsesTools(state.Tools)
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

func renderResponsesTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderOpenAIResponsesTools(tools)
}
