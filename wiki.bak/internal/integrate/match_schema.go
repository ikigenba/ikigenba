package integrate

import "encoding/json"

// MatchSchema is the structured-output JSON schema the match call constrains the
// model to (design §4.3). It pins the binary verdict — either {"same": "<id>"} or
// {"no_match": true} — plus the dup_pairs side channel, so the backend's native
// structured-output mode rejects a malformed shape before it reaches the parser.
// ParseMatch still validates semantically (exactly one verdict arm set, the
// matched id is one of the offered candidates) on top of the schema's shape check.
var MatchSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict", "dup_pairs"],
  "properties": {
    "verdict": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "same": {"type": "string"},
        "no_match": {"type": "boolean"}
      }
    },
    "dup_pairs": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["a", "b"],
        "properties": {
          "a": {"type": "string"},
          "b": {"type": "string"}
        }
      }
    }
  }
}`)
