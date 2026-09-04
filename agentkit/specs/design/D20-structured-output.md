# D20-structured-output

Structured output is a **result contract** on a conversation: the consumer
declares, at construction, the JSON Schema the model's final answer must satisfy,
and a turn ends only when the model has produced a document that satisfies it.
It is not a tool. A tool is optional, repeatable, and executed by agentkit; the
output contract is mandatory, terminal, and singular — "it looks like a tool to
the model, not to us." It joins the neutral-capability family of `ToolChoice` and
`ReasoningConfig` (D8): declared once in vendor-free terms, rendered by each wire
into its own grammar behind the codec seam (D5), never silently downgraded.

```go
package agentkit

// OutputContract declares the structured result a turn must end with. Schema is
// JSON Schema in the output subset (ValidateOutputSchema); MaxAttempts caps how
// many assistant messages the orchestrator will accept before giving up, the
// first attempt included — 0 means DefaultOutputAttempts, 1 means no retry.
type OutputContract struct {
	Schema      json.RawMessage
	MaxAttempts int
}

const DefaultOutputAttempts = 3

// OutputSchema derives an output-subset schema from T's `jsonschema` struct tags
// — the same documented tag vocabulary NewTool[In] reads (D9). Every field is
// required; a pointer field is rendered nullable.
func OutputSchema[T any]() (json.RawMessage, error)

// ValidateOutputSchema reports whether schema lies within the output subset.
func ValidateOutputSchema(schema json.RawMessage) error

// OutputDone reports that the turn produced a document satisfying the contract.
// It is the fourth Event variant (D13) and the last event of a successful turn.
type OutputDone struct {
	Value json.RawMessage
}

// Output drives s to completion if it has not been driven, then returns the
// turn's structured result decoded into T. Output[json.RawMessage] returns the
// bytes as they were accepted.
func Output[T any](s *Stream) (T, error)

// ErrInvalidOutput is the terminal error of a turn whose attempts were
// exhausted without a document satisfying the contract.
var ErrInvalidOutput = errors.New("agentkit: structured output rejected")
```

The contract rides on `Config.Output` (D18), sits in `RequestState.Output`
(D12) for every round-trip, and is gated at `Send` like every other piece of
config: a schema outside the subset is `ErrInvalidConfig` before any provider
call.

**Two enforcement tiers, and why both exist.** Vendors enforce a schema by
constraining the token grammar, and the four wires agree on a *structural*
intersection only: types, properties, `required`, `items`, `enum`, `const`,
`description`, nullable `anyOf`, closed objects, and internal `$ref`/`$defs`
without recursion. That is the **grammar tier** — the model cannot violate it.
Everything else a schema can say — `minimum`, `maxLength`, `pattern`, `format`,
`uniqueItems`, and the rest — is the **constraint tier**: at least one wire
cannot enforce it, and Anthropic (the strictest) rejects the keyword outright.
agentkit does not silently drop these. A model can learn a requirement through
the grammar channel or the prose channel; a constraint that is stripped *and*
unstated is one the model never sees and can never satisfy, so a validate-and-
retry loop against it would fail forever. Each wire therefore removes the
constraint keyword from the rendered schema and folds it into that property's
`description` ("must be >= 0"), and the orchestrator validates the returned
document against the *full* schema locally.

The subset diverges from the tool subset (D9) in three places, which is why it
has its own validator: `additionalProperties` is forbidden in a tool schema but
must be `false` in an output schema (every wire's strict mode requires closed
objects, and the wire injects it where absent); internal `$ref`/`$defs` are
forbidden in tools but allowed here; and constraint keywords are grammar-enforced
in tools but prose-tier here. Two further rules make behavior identical across
wires: the root must be an object, and every property must be listed in
`required` — optionality is expressed as a nullable type, which is the one form
all four strict modes accept. `OutputSchema[T]` produces exactly that shape.

**Terminate versus continue.** Real tools execute and continue (D12); a valid
document terminates. The two compose: an agentic turn can run any number of tool
round-trips and end in a structured verdict. Under a contract, a round-trip that
yields tool calls is dispatched exactly as before. A round-trip that yields no
tool call is the model's attempt at the answer: its text is parsed as JSON and
validated against the full schema. Valid → `OutputDone` is emitted and the turn
ends. Invalid → the orchestrator appends a `RoleUser` corrective message naming
each violation concretely ("`$.items[2].line` is -3, must be >= 0") and
re-drives; the description already states the general rule, the corrective
states the specific miss, and both are needed. The corrective message is a
completed protocol message, so it is emitted as a `MessageDone` with `RoleUser`
and lands in the log. Attempts count assistant messages, `MaxAttempts` in total;
exhaustion is a terminal error wrapping `ErrInvalidOutput` — not retryable,
`History` unchanged (D12). On success the whole exchange, rejected attempts and
corrections included, is spliced into `History` in order, so the transcript
replays faithfully.

This corrective retry is a **semantic** re-prompt and is unrelated to
`agentkit/retry` (D14), which handles transport failures.

**Model floor, not model gate.** Native structured output exists from the 4.5
generation onward on every day-one endpoint. agentkit keeps no per-model table
(D8): a model that lacks the feature returns the vendor's 400 through the
classifier. There is no forced-tool fallback for older models.

## REQUIREMENTS

- R-TIR8-W0WU: `agentkit` MUST export `type OutputContract struct { Schema json.RawMessage; MaxAttempts int }` with exactly those two fields.
- R-TJZ5-9SNJ: `agentkit` MUST export the constant `DefaultOutputAttempts = 3`.
- R-TL71-NKE8: `agentkit` MUST export `func OutputSchema[T any]() (json.RawMessage, error)`.
- R-TMEY-1C4X: `agentkit` MUST export `func ValidateOutputSchema(schema json.RawMessage) error`.
- R-TOUQ-SVMB: `agentkit` MUST export `type OutputDone struct { Value json.RawMessage }`, and `OutputDone` MUST implement `Event`.
- R-TQ2N-6ND0: `agentkit` MUST export `func Output[T any](s *Stream) (T, error)`.
- R-TRAJ-KF3P: `agentkit` MUST export the sentinel `ErrInvalidOutput`, an `error` created with `errors.New`, comparable via `errors.Is` including when wrapped in `*Error`.
- R-TSIF-Y6UE: `ValidateOutputSchema` MUST accept the grammar-tier keywords `type`, `properties`, `required`, `items`, `enum`, `const`, `description`, nullable `anyOf`, `additionalProperties: false`, and internal `$ref`/`$defs`, and the constraint-tier keywords `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf`, `minLength`, `maxLength`, `pattern`, `format`, `minItems`, `maxItems`, and `uniqueItems`.
- R-TTQC-BYL3: `ValidateOutputSchema` MUST reject a non-object root, `additionalProperties` with any value other than `false`, an external or recursive `$ref`, `allOf`, `oneOf`, a property not listed in its object's `required`, and any keyword outside the accepted set, naming the offending construct in the error.
- R-TUY8-PQBS: `OutputSchema[T]` MUST derive the schema from the D9 `jsonschema` tag vocabulary, list every field in `required`, render a pointer field as nullable, and produce a schema that passes `ValidateOutputSchema`.
- R-UKK4-QWWD: `ValidateOutputSchema` MUST accept `format` only with the values `date-time`, `date`, `time`, `email`, `uri`, `uuid`, `ipv4`, `ipv6`, and `hostname`, and the orchestrator MUST validate a returned document's `format`-tagged strings locally against those meanings.
- R-TW65-3I2H: Each shipped wire MUST render the grammar-tier keywords natively, MUST inject `additionalProperties: false` on every object schema that lacks it, and MUST preserve internal `$ref`/`$defs`.
- R-TXE1-H9T6: Each shipped wire MUST remove every constraint-tier keyword from the rendered schema and MUST append a prose statement of each removed constraint to that property's `description`, so no declared constraint is absent from what the model sees; pinned by a golden fixture per wire.
- R-TYLX-V1JV: The Anthropic Messages wire MUST render the contract under the top-level `output_config` request field in the vendor's JSON-schema format, with no beta header, pinned by a golden fixture.
- R-TZTU-8TAK: The OpenAI Chat wire MUST render the contract under the top-level `response_format` request field as a strict JSON-schema format, pinned by a golden fixture.
- R-U11Q-ML19: The OpenAI Responses wire MUST render the contract under the top-level `text` request field as a strict JSON-schema format, pinned by a golden fixture.
- R-U29N-0CRY: The Gemini wire MUST render the contract under `generationConfig` as a JSON response MIME type plus response schema, pinned by a golden fixture.
- R-U4PF-RW9C: When `Config.Output` is nil, no shipped wire's request body MAY contain any structured-output field, and the body MUST be byte-identical to the body produced before this design.
- R-U5XC-5O01: `Send` MUST fail with `ErrInvalidConfig`, making no provider call and leaving `History` unchanged, when `Config.Output.Schema` fails `ValidateOutputSchema` or `Config.Output.MaxAttempts` is negative.
- R-U758-JFQQ: Under an output contract, a round-trip yielding no tool call MUST have its assistant text parsed as JSON and validated against the full schema (grammar and constraint tiers), and on success the orchestrator MUST emit `OutputDone` and end the turn.
- R-UJC8-D55O: A returned document that satisfies the grammar tier but violates a constraint-tier keyword MUST be rejected by the orchestrator's local validation, even though no wire could have enforced it.
- R-U8D4-X7HF: Under an output contract, a round-trip yielding tool calls MUST be dispatched and continued exactly as D12 specifies, with no validation applied to that message's text.
- R-UASX-OQYT: On a rejected document the orchestrator MUST append a `RoleUser` corrective `Text` message naming each violation by JSON path, the rule violated, and the offending value, MUST emit it as a `MessageDone`, and MUST re-drive the round-trip.
- R-UC0U-2IPI: The orchestrator MUST accept at most `MaxAttempts` assistant attempts (`DefaultOutputAttempts` when `MaxAttempts` is 0); after the last rejected attempt the turn MUST end with a terminal `*Error` wrapping `ErrInvalidOutput` with `CategoryUnknown`, `Retryable` MUST report false for it, and `History` MUST be unchanged.
- R-UD8Q-GAG7: On a successful turn the spliced `History` MUST contain every rejected assistant message and every corrective user message, in order, before the accepted assistant message.
- R-UEGM-U26W: `OutputDone` MUST be emitted exactly once per successful turn under a contract, after the accepted `MessageDone`, never on a turn without a contract, and its `Value` MUST be byte-identical to the accepted message's `Text` and satisfy the schema.
- R-UFOJ-7TXL: `Output[T]` MUST drive a stream that has not been driven to completion, MUST NOT re-drive one that has, and MUST return `Stream.Err()` when it is non-nil.
- R-UGWF-LLOA: `Output[T]` MUST return an error when the conversation declares no output contract, and otherwise MUST return the `OutputDone.Value` decoded into `T` with `encoding/json`.
- R-UI4B-ZDEZ: When a log is attached, every `OutputDone` MUST be written as one `output` record carrying the same `Value`, in stream order, so the log mirrors the stream one for one (D13, D15).
