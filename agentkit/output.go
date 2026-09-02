package agentkit

import "encoding/json"

// OutputContract declares the structured result a turn must end with. Schema is
// JSON Schema in the output subset; MaxAttempts caps how many assistant messages
// are accepted, including the first attempt. Zero means DefaultOutputAttempts.
type OutputContract struct {
	Schema      json.RawMessage
	MaxAttempts int
}

// DefaultOutputAttempts is the number of structured-output attempts used when
// OutputContract.MaxAttempts is zero.
const DefaultOutputAttempts = 3
