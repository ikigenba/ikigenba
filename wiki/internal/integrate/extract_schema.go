package integrate

import "encoding/json"

// ExtractSchema is the structured-output JSON schema the extract call constrains
// the model to (design §4.2). It pins the output envelope — a "subjects" array,
// each object carrying type/kind/name/aliases/claims and an optional occurred_at —
// so the backend's native structured-output mode rejects a malformed shape before
// it reaches the parser. ParseExtract still validates semantically (closed-set
// type, non-empty name, claim-bearing) on top of the schema's shape check.
var ExtractSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["subjects"],
  "properties": {
    "subjects": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["type", "kind", "name", "aliases", "claims"],
        "properties": {
          "type": {"type": "string", "enum": ["entity", "event", "concept"]},
          "kind": {"type": "string"},
          "name": {"type": "string"},
          "aliases": {"type": "array", "items": {"type": "string"}},
          "claims": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["text"],
              "properties": {"text": {"type": "string"}}
            }
          },
          "occurred_at": {"type": "string"}
        }
      }
    }
  }
}`)
