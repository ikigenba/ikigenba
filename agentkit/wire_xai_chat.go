package agentkit

import "encoding/json"

type xaiChatWire struct{ wireCodec }

// XAIChatWire returns the built-in xAI Chat wire codec. Its request body
// grammar and decoded events are identical to ChatWire's and
// OpenAIChatWire's; only the credential header logic differs, and that
// lives outside the wire.
func XAIChatWire() WireFormat {
	wire := &xaiChatWire{}
	wire.wireCodec = newChatWireCodec(wire.encodeRequest)
	wire.rejectsCredential = xaiRejectsCredential
	return wire
}

func (w *xaiChatWire) encodeRequest(state requestState) ([]byte, error) {
	return encodeChatRequest(&w.wireCodec, state)
}

func (w *xaiChatWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderOpenAIChatTools(tools)
}
