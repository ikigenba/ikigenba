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
per-wire envelope parsing on top of the table. Until then `Error.Code` and
`Error.Message` may be empty and the status table is the whole classification
of the *category*. One header is read today, because it is wire-agnostic: the
standard `Retry-After` header (RFC 9110, delta-seconds or HTTP-date) is lifted
into `Error.RetryAfter`. Vendor-specific reset headers (an OpenAI
`x-ratelimit-reset-*`, a Gemini `retryInfo` in the body) wait for the envelope
design. Nothing in the root drives `agentkit/retry` yet: the leaf exists (D14),
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

**A mid-stream error can arrive after a 200.** Some wires open with HTTP 200 and
then emit an error *event* partway through the byte stream, after usable content
has already been decoded. The classifier must therefore be reachable from inside
the stream decode (D5/D13), not only at the moment the response headers land: the
adapter, on seeing an error frame, runs the same classification path (status is
the already-seen 200, but the body is the error frame and any trailing headers
are absent) and surfaces the resulting `*Error` as the stream's terminal error.
There is one classification path, invoked from two points. Recognizing a
vendor's error *frame* is envelope parsing, so the built-in wires do not yet
detect one; the path is reachable and exercised, and the per-wire frame
recognizers arrive with the envelope design.

**Config failures and lifecycle failures are fail-loud sentinels.** Two
conditions are the consumer's mistake, not the provider's, and are reported as
sentinel errors comparable with `errors.Is`:

```go
// ErrInvalidConfig is returned (wrapped in *Error with CategoryInvalidRequest)
// when a Conversation is constructed or a Send is issued with a configuration
// that cannot be honored: a reserved-key collision in ProviderOptions, a
// base-URL set against a transport-baking credential, or a generation setting a
// wire cannot express (e.g. a reasoning form with no representation). Such a
// Send makes no provider call and leaves History unchanged.
var ErrInvalidConfig = errors.New("agentkit: invalid configuration")

// ErrClosed is returned when Send is called on a Conversation whose event log
// has been Closed, or on an otherwise finalized Conversation.
var ErrClosed = errors.New("agentkit: conversation closed")
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
- R-2P1K-TLVQ: The same classification path MUST be reachable from inside the stream decode so that an error frame arriving after an HTTP 200 is surfaced as the stream's terminal `*Error`.
- R-2RHD-L5D4: `Retryable(err)` MUST be the single authority on retryability, returning true for rate-limit, overloaded, timeout, and transport categories and false for auth, invalid-request, insufficient-quota, and unknown, unwrapping to find an agentkit `*Error`.
- R-2SP9-YX3T: `ErrInvalidConfig` and `ErrClosed` MUST be sentinel errors comparable via `errors.Is`, including when wrapped in `*Error`.
- R-2TX6-COUI: A `Send` that fails configuration validation MUST make no provider call and MUST leave History unchanged.
- R-2V52-QGL7: A forced tool-choice a wire cannot express MUST fail at `Send` with `ErrInvalidConfig` rather than degrade silently; agentkit MUST expose no `Warning` type or warning channel.
- R-ZAM9-IUL9: `agentkit` MUST export `type Category int` with the constants `CategoryUnknown`, `CategoryAuth`, `CategoryInvalidRequest`, `CategoryRateLimit`, `CategoryOverloaded`, `CategoryInsufficientQuota`, `CategoryTimeout`, `CategoryTransport` declared in that `iota` order starting at 0.
- R-ZBU5-WMBY: `agentkit` MUST export `type Error struct { Category Category; Status int; Code string; Message string; RetryAfter time.Duration; Endpoint Identity }` with those exported fields plus an unexported wrapped cause, and `*Error` MUST implement `Error() string` and `Unwrap() error`.
- R-ZD22-AE2N: `agentkit` MUST export `func Retryable(err error) bool`.
- R-ZE9Y-O5TC: `agentkit` MUST export the sentinel errors `ErrInvalidConfig` and `ErrClosed`, each an `error` created with `errors.New`.
- R-UBUS-5J8U: Built-in classification MUST set `Error.RetryAfter` from a `Retry-After` response header, accepting both RFC 9110 forms — a non-negative delta-seconds integer as that many seconds, and an HTTP-date as the duration from now until that date — and MUST leave `RetryAfter` zero when the header is absent, unparseable, or names a time already past.
- R-OHYJ-3C6O: Built-in classification MUST map HTTP status to `Category` as: 401 and 403 → `CategoryAuth`; 400, 404, 409, 413, 415, and 422 → `CategoryInvalidRequest`; 402 → `CategoryInsufficientQuota`; 429 → `CategoryRateLimit`; 408 and 504 → `CategoryTimeout`; 500, 502, 503, and 529 → `CategoryOverloaded`; every other non-2xx status → `CategoryUnknown`.
