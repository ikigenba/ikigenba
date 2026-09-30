package agentkit

import (
	"encoding/json"
	"net/http"
)

// inBandErrorRecognizer inspects one decoded payload frame of a 200 stream and
// returns the terminal *Error when the frame is the in-band error frame its
// provider documents (D4), or nil for any other frame.
type inBandErrorRecognizer func(frame []byte) *Error

// anthropicErrorStatus pairs each Anthropic error type with the HTTP status
// Anthropic's errors reference lists beside it.
var anthropicErrorStatus = map[string]int{
	"invalid_request_error": 400,
	"authentication_error":  401,
	"billing_error":         402,
	"permission_error":      403,
	"not_found_error":       404,
	"conflict_error":        409,
	"request_too_large":     413,
	"rate_limit_error":      429,
	"api_error":             500,
	"timeout_error":         504,
	"overloaded_error":      529,
}

// openAIErrorStatus pairs each OpenAI error code with the HTTP status
// OpenAI's error-codes guide documents for it; it serves both the chat and
// the responses families.
var openAIErrorStatus = map[string]int{
	"slow_down":                         429,
	"server_is_overloaded":              503,
	"credit_balance_exhausted":          429,
	"organization_spend_limit_exceeded": 429,
	"project_spend_limit_exceeded":      429,
	"organization_usage_limit_exceeded": 429,
}

// inBandError builds the terminal error for an in-band frame: status 200, the
// frame's code and message verbatim, and the category the shared status table
// assigns to the status the code is paired with (CategoryUnknown if none).
func inBandError(statusByCode map[string]int, code, message string) *Error {
	category := CategoryUnknown
	if status, ok := statusByCode[code]; ok {
		category = classifyStatus(status)
	}
	return &Error{Category: category, Status: http.StatusOK, Code: code, Message: message}
}

// frameObject decodes frame as a JSON object, reporting false for anything
// else.
func frameObject(frame []byte) (map[string]json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(frame, &object); err != nil || object == nil {
		return nil, false
	}
	return object, true
}

// rawString returns raw as a string when it is a JSON string, else "".
func rawString(raw json.RawMessage) string {
	var value string
	if raw == nil || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

// anthropicInBandError recognizes Anthropic's streaming error event:
// {"type":"error","error":{"type":...,"message":...}}.
func anthropicInBandError(frame []byte) *Error {
	object, ok := frameObject(frame)
	if !ok || rawString(object["type"]) != "error" {
		return nil
	}
	inner, ok := frameObject(object["error"])
	if !ok {
		return nil
	}
	return inBandError(anthropicErrorStatus, rawString(inner["type"]), rawString(inner["message"]))
}

// responsesInBandError recognizes the Responses family's two error-ending
// events: ResponseErrorEvent (type "error") and ResponseFailedEvent (type
// "response.failed", carrying response.error).
func responsesInBandError(frame []byte) *Error {
	object, ok := frameObject(frame)
	if !ok {
		return nil
	}
	switch rawString(object["type"]) {
	case "error":
		return inBandError(openAIErrorStatus, rawString(object["code"]), rawString(object["message"]))
	case "response.failed":
		response, _ := frameObject(object["response"])
		failure, _ := frameObject(response["error"])
		return inBandError(openAIErrorStatus, rawString(failure["code"]), rawString(failure["message"]))
	default:
		return nil
	}
}

// chatInBandError recognizes a Chat Completions data frame whose JSON object
// carries a top-level error field in place of a completion chunk.
func chatInBandError(frame []byte) *Error {
	object, ok := frameObject(frame)
	if !ok {
		return nil
	}
	raw, present := object["error"]
	if !present || string(raw) == "null" {
		return nil
	}
	failure, _ := frameObject(raw)
	return inBandError(openAIErrorStatus, rawString(failure["code"]), rawString(failure["message"]))
}
