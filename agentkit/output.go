package agentkit

import (
	"encoding/json"
	"errors"
)

// OutputDone reports that the turn produced a document satisfying the contract.
// It is the fourth Event variant and the last event of a successful turn.
type OutputDone struct {
	Value json.RawMessage
}

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

// Output drives s to completion if it has not been driven, then returns the
// turn's structured result decoded into T. Output[json.RawMessage] returns the
// bytes as they were accepted.
func Output[T any](s *Stream) (T, error) {
	var zero T
	if s == nil {
		return zero, errors.New("agentkit: output called with nil stream")
	}
	for event := range s.Events() {
		_ = event
	}
	if err := s.Err(); err != nil {
		return zero, err
	}
	if !s.outputDeclared {
		return zero, errors.New("agentkit: stream has no output contract")
	}
	if !s.outputDone {
		return zero, errors.New("agentkit: stream ended without completed output")
	}
	var result T
	if raw, ok := any(&result).(*json.RawMessage); ok {
		*raw = append(*raw, s.outputValue...)
		return result, nil
	}
	if err := json.Unmarshal(s.outputValue, &result); err != nil {
		return zero, err
	}
	return result, nil
}
