# D6-endpoint-and-dialect

An **`Endpoint`** is the public, opaque, option-built description of *where* and
*how* a wire's bytes are sent. It is the second axis: a `WireFormat` (D5) owns the
body grammar, an `Endpoint` owns the transport around it. The two compose into a
`Provider` — the SPI the orchestrator drives for one HTTP round-trip and the last
rung of the escape-hatch ladder (D1). A vendor constructor bakes a fixed
`(WireFormat, Endpoint)` pair; the generic wire constructor lets a consumer supply
an `Endpoint` for a custom deployment of a known grammar.

**What the endpoint owns:** the base URL and path; whether the model id goes in
the path or the body; the auth applier; extra headers; the `Framer` (D5) handed to
the wire's `DecodeStream`; the error classifier; a request-mutation hook; and the
*encoding* of reasoning replay (D-K) — the wire supplies a default replay
encoding, the endpoint may override it. This last point is load-bearing and is the
reason replay encoding is not a wire constant: two endpoints on the *same* wire can
demand opposite replay encodings, so the wire cannot own the value. The endpoint is
also where "does changing the credential move the transport?" is answered: at one
day-one endpoint a credential swap moves host, path, and headers; at another the
host, path, and headers are byte-identical and only the bearer source differs. That
divergence is a property of the endpoint, stated here as a shape, not baked into any
wire.

```go
// Endpoint is a public, opaque, option-built transport description. Construct it
// with EndpointOptions; its fields are unexported. A vendor constructor bakes
// one; the generic wire constructor accepts one.
type Endpoint struct { /* unexported */ }

// EndpointOption configures an Endpoint at construction. Options may fail
// (e.g. an invalid base URL), so each returns an error.
type EndpointOption func(*endpointConfig) error

func WithBaseURL(raw string) EndpointOption          // custom deployment of a known wire (L2: excludes a transport-baking credential)
func WithHeader(name, value string) EndpointOption   // extra static headers (attribution, beta flags)
func WithFramer(f Framer) EndpointOption             // override the default SSE Framer (D5)
func WithClassifier(c ErrorClassifier) EndpointOption
func WithMutator(m RequestMutator) EndpointOption
func WithReplayEncoding(e ReplayEncoding) EndpointOption // override the wire default (D-K)
```

The auth applier sees the *fully assembled* request, including the final body,
because a body-signing scheme (SigV4) must sign the exact bytes that ship and an
OAuth refresh needs the context and can fail. It is not a header-string producer;
it mutates the request in place.

```go
// AuthApplier carries a credential onto a fully assembled request. It sees the
// final body so a body-signing scheme can sign it; it takes a context so an
// OAuth refresh can run and fail.
type AuthApplier interface {
	Apply(ctx context.Context, req *http.Request, body []byte) error
}
```

Request assembly is one step with one endpoint-supplied mutation hook, and the
ordering of the two mutations is a requirement, not an implementation detail: the
mutation hook runs **first**, the auth applier runs **second** (mutate-then-auth),
so that when the auth applier signs the body it signs the *final* bytes the
mutation hook produced. The mutation hook covers day-one needs that are pure
transport reshaping — moving the model id into the path, or redirecting one
credential's host/path/headers — subsuming path templating as its own concept.

```go
// RequestMutator rewrites the assembled request and its body before auth. It
// may replace the body (hence *[]byte). It runs FIRST; the AuthApplier runs
// SECOND, so a body-signing AuthApplier signs the mutated bytes.
type RequestMutator func(req *http.Request, body *[]byte) error

// ErrorClassifier maps a response (non-2xx, or an in-band error frame) to a
// typed error. It gets status, headers, and body because retry-after and
// rate-limit-reset live in headers and some endpoints are separable only by
// message text in the body (D4).
type ErrorClassifier func(status int, header http.Header, body []byte) error

// Provider is the composed (WireFormat, Endpoint) SPI the orchestrator drives
// for one round-trip, and the last escape-hatch rung: a consumer may implement
// it directly. A vendor constructor returns one.
type Provider interface {
	// BuildRequest assembles the wire body (D5) for the turn, wraps it against
	// the endpoint (base URL, path, model placement, headers), then applies the
	// RequestMutator and the AuthApplier in that order.
	BuildRequest(ctx context.Context, state RequestState) (*http.Request, error)
	// Decode frames the response (endpoint Framer) and decodes it (wire
	// DecodeStream) into message-granular events.
	Decode(ctx context.Context, resp *http.Response) iter.Seq2[Event, error]
	// Classify applies the endpoint's ErrorClassifier.
	Classify(status int, header http.Header, body []byte) error
	// Identity reports endpoint identity, auth mode, and model (D15).
	Identity() Identity
}
```

## REQUIREMENTS

- R-37C2-K605: An `Endpoint` MUST be constructed only from `EndpointOption` values and MUST expose no assignable fields to consumers.
- R-38JY-XXQU: An `Endpoint` MUST own the base URL and path, model-in-path-vs-body placement, the auth applier, extra headers, the `Framer`, the error classifier, the request-mutation hook, and the reasoning-replay encoding.
- R-3AZR-PH88: The reasoning-replay *encoding* MUST be endpoint-owned with a wire-supplied default, so two endpoints on one `WireFormat` can carry opposite replay encodings (D-K); the wire MUST NOT hardcode the value.
- R-3DFK-H0PM: Request assembly MUST run the endpoint's `RequestMutator` before the `AuthApplier`, so that a body-signing applier signs the mutated body.
- R-3ENG-USGB: The request-mutation hook MUST be able to rewrite both the request and its body before send (subsuming model-in-path rewriting and per-credential host/path/header redirection); path templating MUST NOT be a separate concept.
- R-3FVD-8K70: The error classifier MUST receive status, headers, and body and return a typed error (D4), so that header-carried retry timing and body-text-only disambiguation are both reachable.
- R-3H39-MBXP: The composed `Provider` SPI MUST be public and implementable by a consumer as the final escape-hatch rung, with the same standing as a vendor constructor.
- R-ZJ5K-78S4: `agentkit` MUST export `Endpoint` as an opaque struct type with no exported fields, constructed only from `EndpointOption` values.
- R-ZKDG-L0IT: `agentkit` MUST export `EndpointOption` as a functional-option type that configures an `Endpoint` at construction and returns an `error`.
- R-ZLLC-YS9I: `agentkit` MUST export the endpoint options `WithBaseURL(raw string) EndpointOption`, `WithHeader(name, value string) EndpointOption`, `WithFramer(f Framer) EndpointOption`, `WithClassifier(c ErrorClassifier) EndpointOption`, `WithMutator(m RequestMutator) EndpointOption`, and `WithReplayEncoding(e ReplayEncoding) EndpointOption`.
- R-ZMT9-CK07: `agentkit` MUST export the `AuthApplier` interface whose method set is exactly `Apply(ctx context.Context, req *http.Request, body []byte) error`.
- R-ZO15-QBQW: `agentkit` MUST export `type RequestMutator func(req *http.Request, body *[]byte) error`.
- R-ZP92-43HL: `agentkit` MUST export `type ErrorClassifier func(status int, header http.Header, body []byte) error`.
- R-ZQGY-HV8A: `agentkit` MUST export the `Provider` interface whose method set is exactly `BuildRequest(ctx context.Context, state RequestState) (*http.Request, error)`, `Decode(ctx context.Context, resp *http.Response) iter.Seq2[Event, error]`, `Classify(status int, header http.Header, body []byte) error`, and `Identity() Identity`.
- R-0VXJ-I2FW: The `AuthApplier` MUST receive the fully assembled request together with the final request body, so a body-signing scheme signs the exact bytes that are sent.
