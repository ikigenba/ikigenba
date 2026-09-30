# D4-errors

Every failure a consumer can see — a transport error, a rejected request, a
mid-stream fault, a bad config — arrives as one Go error type, `*Error`, whose
**category lives on a struct field**, not in the Go type. There is no error type
hierarchy to switch on: a caller reads `err.Category`, and cross-cutting checks
like retryability read the same field through one helper. A type tree would force
every new endpoint envelope to grow a new leaf type and every consumer `switch`
to grow a new case; a category enum on a shared struct absorbs a new endpoint by
mapping its envelope to an existing category.

**Classification is a seam that sees the whole response, and the library owns
it.** Each built-in wire classifies its vendor's error responses (D5); there is
no consumer-installable classifier. The classifier receives the HTTP status, the
response headers, and the body bytes, and returns a populated `*Error`. Headers are not optional input: a
retry-after or rate-limit-reset value lives only in the headers, and the
classifier lifts it into a typed `RetryAfter` on the error so a retry layer
(D14) never re-parses a header. Body is required because some endpoints do not
separate distinct failures at the envelope level — a bad credential and an
unknown model can both arrive as the same status and the same envelope code,
distinguishable only by the human-readable message text. The classifier is thus
allowed to inspect message text as a last resort, and the category it assigns is
authoritative regardless of how it decided.

**What is designed now is the status table.** Every built-in wire maps the HTTP
status to a `Category` with one shared table (the requirement below). Reading
the vendor's JSON error envelope — its `code` and `message`, and the cases where
one status covers two categories (an OpenAI 429 that is really exhausted quota,
a Gemini 400 that is really a bad key) — is deferred to a later design that adds
per-wire envelope parsing on top of the table. Until then, for a non-2xx
response, `Error.Code` and `Error.Message` may be empty and the status table is
the whole classification of the *category*. The one exception is an error that
arrives in-band after a 200 (below): there the frame *is* the error, so the
built-in wires read its code and message, and the category comes from the same
status table applied to the HTTP status the provider documents for that code.
One header is read today, because it is wire-agnostic: a
`Retry-After` header in RFC 9110 delta-seconds form is lifted into
`Error.RetryAfter`. The HTTP-date form is deliberately not read — its value
depends on the wall clock, which classification has no injected source for,
and a test of it could only assert a tolerance. Vendor-specific reset headers
(an OpenAI `x-ratelimit-reset-*`, a Gemini `retryInfo` in the body) wait for
the envelope design. One narrow envelope reading exists ahead of it: the xAI
wires read the `code` field of a `403` body to recognise a **rejected
credential** (D5), because xAI answers an expired or invalid OAuth token with
`403` and a body code, never with `401`, as captured live. That reading feeds
only the OAuth re-issue path (D22); it does not populate `Error.Code`, and it
is not consumer-visible on `*Error`. Exposing it there is the envelope
design's question, not this one's. The natural test drives a fake server that answers 429
with `Retry-After: 30` through `Send` and asserts `RetryAfter == 30 *
time.Second` as a literal; a missing or non-integer header is asserted to
leave the field zero. Nothing in the root drives `agentkit/retry` yet: the leaf exists (D14),
`RetryAfter` is the floor it would read, and the orchestrator-side wiring — when
a turn retries, what it logs (`RecordRetry`, D15) — is a later design.

```go
// Category is a closed enumeration of failure kinds, carried on Error. It is a
// field, not a type: adding an endpoint maps its envelope onto these values
// rather than introducing a new Go error type.
type Category int

const (
    CategoryUnknown        Category = iota // unclassifiable; default, never retried
    CategoryAuth                           // rejected credential / permission
    CategoryInvalidRequest                 // malformed request, unknown model, bad params
    CategoryRateLimit                      // throttled; RetryAfter is typically set
    CategoryOverloaded                     // transient upstream/server fault (5xx-ish)
    CategoryInsufficientQuota              // out of credits/balance; not retryable
    CategoryTimeout                        // deadline exceeded / connection reset
    CategoryTransport                      // network-level failure before a usable response
)

// Error is the single error type agentkit returns for a provider interaction.
// Category is the discriminator; the remaining fields are context. RetryAfter is
// the classifier's typed reading of a retry-after / rate-limit-reset header, zero
// when none was present. Status is the HTTP status (0 for transport failures).
type Error struct {
    Category   Category
    Status     int           // HTTP status, 0 if no response was received
    Code       string        // vendor envelope code, verbatim, when present
    Message    string        // vendor message text, verbatim
    RetryAfter time.Duration // classifier's reading of a retry hint, 0 if none
    Endpoint   Identity      // which endpoint/model produced this (D-identity)
    err        error         // wrapped cause, exposed via Unwrap
}

func (e *Error) Error() string { /* "<category>: <message> (status N)" */ }
func (e *Error) Unwrap() error { return e.err }
```

**Retryability is derived from the category, in one place.** Consumers and the
retry layer both call `Retryable`, never re-deciding per call site:

```go
// Retryable reports whether err (or anything it wraps) is an agentkit *Error
// whose category is transient. Rate-limit, overloaded, timeout, and transport
// are retryable; auth, invalid-request, insufficient-quota, and unknown are
// not. It is the single authority — the retry layer (D14) and consumer code ask
// it rather than switching on Category themselves.
func Retryable(err error) bool
```

**A mid-stream error can arrive after a 200.** Some providers open with HTTP 200
and then emit an error *frame* partway through the byte stream, possibly after
usable content has already been decoded. Each built-in wire recognizes the
in-band error frame its provider documents, and nothing else, and ends the
stream with a terminal `*Error` (D5's decode error channel; D12/D13 define what
a terminal error does to the stream and History). The error's `Status` is the
response's actual status, 200; `Code` and `Message` are the frame's own values,
verbatim; `RetryAfter` follows the ordinary `Retry-After` header rule applied
to the 200 response's headers — a frame carries no headers of its own, and a
200 in practice carries no `Retry-After`, so it is normally zero. The category is not guessed from the code's name: it is the same
status table a non-2xx response goes through, applied to the HTTP status the
provider's documentation pairs with that code. A code the documentation pairs
with no status is `CategoryUnknown`, which is never retried — the safe reading
of an error nobody has characterized. The recognizers are stated per family,
not per vendor, because the three wires of a family yield identical events for
the same frames (D5).

- **Anthropic** (`AnthropicMessagesWire()`). The streaming guide documents an
  SSE `event: error` whose data is `{"type": "error", "error": {"type":
  "overloaded_error", "message": "Overloaded"}}`
  (https://platform.claude.com/docs/en/build-with-claude/streaming#error-events).
  The errors reference says an error can occur after the API returns a 200 and
  points at that shape, and lists each error type beside its HTTP status —
  `invalid_request_error` 400 through `overloaded_error` 529 — noting the list
  may grow (https://platform.claude.com/docs/en/api/errors). `Code` is the
  inner `error.type`; the category is that type's listed status run through the
  table, and an unlisted type is `CategoryUnknown`.
- **Responses family** (`ResponsesWire()`, `OpenAIResponsesWire()`,
  `XAIResponsesWire()`). OpenAI's official OpenAPI description
  (https://github.com/openai/openai-openapi, `openapi.yaml`; rendered in the
  API reference at https://developers.openai.com) documents two stream events
  that end a response in error: `ResponseErrorEvent`, `{"type": "error",
  "code": <string or null>, "message": <string>, "param": ..., "sequence_number":
  ...}`, and `ResponseFailedEvent`, `type` `response.failed`, whose
  `response.error` is a `ResponseError` `{"code": ..., "message": ...}`. Either
  ends the stream; a null code becomes an empty `Code`.
- **Chat family** (`ChatWire()`, `OpenAIChatWire()`, `XAIChatWire()`). The same
  OpenAPI description, on the 200 response of `POST /chat/completions`, says
  that if a failure occurs after streaming has started, a data frame may
  contain a JSON object with an `error` field instead of a completion chunk. It
  gives that field no schema; the description's standard `Error` object is
  `{code, message, param, type}`, so the wire reads `error.code` and
  `error.message` when each is a string and leaves the field empty otherwise.
- **OpenAI category** (both families). The only code-to-status pairs OpenAI
  documents are in its error-codes guide
  (https://developers.openai.com/api/docs/guides/error-codes): `slow_down`,
  `credit_balance_exhausted`, `organization_spend_limit_exceeded`,
  `project_spend_limit_exceeded`, and `organization_usage_limit_exceeded` with
  429, and `server_is_overloaded` with 503. Every other code — an empty one, and
  every Responses `ResponseErrorCode` such as `server_error`, none of which has
  a documented status — is `CategoryUnknown`.
- **Gemini** (`GeminiGenerateContentWire()`) recognizes no in-band frame,
  because Google documents none for `streamGenerateContent`: the method's
  response "contains a stream of `GenerateContentResponse` instances"
  (https://ai.google.dev/api/generate-content), and the API errors guide
  documents errors only as non-2xx bodies
  (https://ai.google.dev/gemini-api/docs/generate-content/api-errors). The
  in-stream error shown at https://ai.google.dev/gemini-api/docs/api-errors
  belongs to the Interactions API, which no built-in wire speaks.

**Config failures and lifecycle failures are fail-loud sentinels.** Two
conditions are the consumer's mistake, not the provider's, and are reported as
sentinel errors comparable with `errors.Is`:

```go
// ErrInvalidConfig is returned (wrapped in *Error with CategoryInvalidRequest)
// when a Conversation is constructed or a Send is issued with a configuration
// that cannot be honored: an option key the wire does not know or a value it
// cannot parse (D8), a
// base-URL set against a transport-baking credential, or a generation setting a
// wire cannot express (e.g. a reasoning form with no representation). Such a
// Send makes no provider call and leaves History unchanged.
var ErrInvalidConfig = errors.New("agentkit: invalid configuration")

// ErrClosed is returned when Send or AddSystem is called on a Conversation
// whose event log has been Closed, or on an otherwise finalized Conversation.
var ErrClosed = errors.New("agentkit: conversation closed")

// ErrInvalidArgument is returned when a call's own argument cannot be honored
// regardless of configuration: AddSystem with empty or blank text (D24). The
// call has no effect and leaves History unchanged.
var ErrInvalidArgument = errors.New("agentkit: invalid argument")
```

Fail-loud is a deliberate stance that removed a whole type. There is **no
`Warning` channel**: the two things the old design warned about are gone as
warnings. A cost that could not be resolved is reported as zero (D3), not a
warning. A forced tool-choice on a wire that cannot express it does
not degrade silently — it fails at `Send` with `ErrInvalidConfig`, the same way
an unrepresentable reasoning form or an out-of-subset tool schema fails. Every
condition that would have been a warning is now either a typed field or a hard
`Send`-time error; nothing is whispered.

## REQUIREMENTS

- R-2K5Z-AIWY: agentkit MUST return provider failures as a single `*Error` type whose failure kind is a `Category` field, and MUST NOT distinguish failure kinds by distinct Go error types.
- R-OGQM-PKFZ: A non-2xx HTTP response MUST surface from `Send` as a populated `*Error` whose `Status` is the response status and whose `Category` is assigned by the library's built-in classification, with no consumer-installed classifier involved.
- R-ISHO-CITZ: When a 200 stream from `AnthropicMessagesWire()` carries an event whose data is a JSON object with `type` equal to `"error"` and an `error` object, the stream MUST end with a terminal `*Error` whose `Status` is 200, whose `Code` is that `error` object's `type`, and whose `Message` is that `error` object's `message`.
- R-ITPK-QAKO: A terminal `*Error` from an Anthropic in-band error event MUST carry the `Category` built-in classification assigns to the HTTP status the error type is paired with — `invalid_request_error` 400, `authentication_error` 401, `billing_error` 402, `permission_error` 403, `not_found_error` 404, `conflict_error` 409, `request_too_large` 413, `rate_limit_error` 429, `api_error` 500, `timeout_error` 504, `overloaded_error` 529 — and MUST carry `CategoryUnknown` for any other error type.
- R-IW5D-HU22: When a 200 stream from `ResponsesWire()`, `OpenAIResponsesWire()`, or `XAIResponsesWire()` carries an event with `type` equal to `"error"`, the stream MUST end with a terminal `*Error` whose `Status` is 200, whose `Code` is the event's `code` (empty when `code` is null), and whose `Message` is the event's `message`.
- R-IXD9-VLSR: When a 200 stream from `ResponsesWire()`, `OpenAIResponsesWire()`, or `XAIResponsesWire()` carries an event with `type` equal to `"response.failed"`, the stream MUST end with a terminal `*Error` whose `Status` is 200, whose `Code` is the event's `response.error.code` (empty when null), and whose `Message` is the event's `response.error.message`.
- R-IYL6-9DJG: When a 200 stream from `ChatWire()`, `OpenAIChatWire()`, or `XAIChatWire()` carries a data frame whose JSON object has a top-level `error` field, the stream MUST end with a terminal `*Error` whose `Status` is 200, whose `Code` is `error.code` when that is a string and empty otherwise, and whose `Message` is `error.message` when that is a string and empty otherwise.
- R-IZT2-N5A5: A terminal `*Error` from an in-band error frame of a chat-family or responses-family wire MUST carry the `Category` built-in classification assigns to the HTTP status its `Code` is paired with — `slow_down` 429, `server_is_overloaded` 503, `credit_balance_exhausted` 429, `organization_spend_limit_exceeded` 429, `project_spend_limit_exceeded` 429, `organization_usage_limit_exceeded` 429 — and MUST carry `CategoryUnknown` for any other `Code`, including an empty one.
- R-2RHD-L5D4: `Retryable(err)` MUST be the single authority on retryability, returning true for rate-limit, overloaded, timeout, and transport categories and false for auth, invalid-request, insufficient-quota, and unknown, unwrapping to find an agentkit `*Error`.
- R-CJTD-QX2U: `ErrInvalidConfig`, `ErrClosed`, and `ErrInvalidArgument` MUST be sentinel errors comparable via `errors.Is`, including when wrapped in `*Error`.
- R-2TX6-COUI: A `Send` that fails configuration validation MUST make no provider call and MUST leave History unchanged.
- R-2V52-QGL7: A forced tool-choice a wire cannot express MUST fail at `Send` with `ErrInvalidConfig` rather than degrade silently; agentkit MUST expose no `Warning` type or warning channel.
- R-ZAM9-IUL9: `agentkit` MUST export `type Category int` with the constants `CategoryUnknown`, `CategoryAuth`, `CategoryInvalidRequest`, `CategoryRateLimit`, `CategoryOverloaded`, `CategoryInsufficientQuota`, `CategoryTimeout`, `CategoryTransport` declared in that `iota` order starting at 0.
- R-B4LX-H3OC: `agentkit` MUST export `type Error struct { Category Category; Status int; Code string; Message string; RetryAfter time.Duration; Endpoint Identity }` with those exported fields, and `*Error` MUST implement `Error() string` and `Unwrap() error`.
- R-ZD22-AE2N: `agentkit` MUST export `func Retryable(err error) bool`.
- R-B5TT-UVF1: `agentkit` MUST export the sentinel errors `ErrInvalidConfig`, `ErrClosed`, and `ErrInvalidArgument`, each an `error`.
- R-1JWR-1RWS: Built-in classification MUST set `Error.RetryAfter` to N seconds when the response carries a `Retry-After` header whose value is a non-negative integer N (RFC 9110 delta-seconds), and MUST leave `RetryAfter` zero when the header is absent or its value is anything else, including an HTTP-date.
- R-OHYJ-3C6O: Built-in classification MUST map HTTP status to `Category` as: 401 and 403 → `CategoryAuth`; 400, 404, 409, 413, 415, and 422 → `CategoryInvalidRequest`; 402 → `CategoryInsufficientQuota`; 429 → `CategoryRateLimit`; 408 and 504 → `CategoryTimeout`; 500, 502, 503, and 529 → `CategoryOverloaded`; every other non-2xx status → `CategoryUnknown`.
