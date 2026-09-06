package agentkit

import (
	"encoding/json"
	"net/http"
)

type xaiResponsesWire struct{ wireCodec }

// XAIResponsesWire returns the built-in xAI Responses wire codec. Its
// request body grammar and decoded events are identical to ResponsesWire's
// and OpenAIResponsesWire's; only the credential header logic differs, and
// that lives outside the wire.
func XAIResponsesWire() WireFormat {
	wire := &xaiResponsesWire{}
	wire.wireCodec = newResponsesWireCodec(wire.encodeRequest)
	wire.rejectsCredential = xaiRejectsCredential
	return wire
}

func (w *xaiResponsesWire) encodeRequest(state requestState) ([]byte, error) {
	return encodeResponsesRequest(&w.wireCodec, state)
}

func (w *xaiResponsesWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderOpenAIResponsesTools(tools)
}

// xaiRejectsCredential is xAI's OAuth-rejection shape (D5), shared by
// XAIResponsesWire and XAIChatWire: a 403 whose JSON body's "code" field is
// exactly "unauthenticated:bad-credentials". xAI's 401 means no credential
// was sent at all, which no rotation can fix, so it is deliberately excluded.
func xaiRejectsCredential(status int, body []byte) bool {
	if status != http.StatusForbidden {
		return false
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.Code == "unauthenticated:bad-credentials"
}
