# D6-endpoint-and-dialect

An **`Endpoint`** is *where* a wire's bytes are sent and *how* they are
authenticated. It is the second axis: a wire codec (D5) owns the body grammar
and the error envelope, an `Endpoint` owns the base URL and the auth applier —
and nothing else. The two are paired by the root constructor `New` (D18) for
the orchestrator to drive. A vendor package builds the `Endpoint` from its
default base URL (or the consumer's `WithBaseURL` override) and its typed
credential; that is the whole customization surface.

**What the endpoint owns:** the base URL, including any model-in-path placement
the vendor package has already baked into it (Gemini names the model in the
path; the `gemini` package builds that URL), and the auth applier. Everything
that earlier revisions hung off the endpoint as consumer-installable hooks —
extra headers, a replaceable `Framer`, an error classifier, a request-mutation
hook, an HTTP client — is gone. Framing is SSE for every built-in wire (D5),
error classification belongs to the wire (D4, D5), and requests execute with
`http.DefaultClient`. Proxy and transport configuration is a deliberately open
question for a later design; it is not rejected, it is simply not designed yet,
and it will not return as a consumer-implemented hook.

```go
// Endpoint is an opaque transport description: base URL plus auth applier.
// Construct it with NewEndpoint; its fields are unexported. A vendor package
// builds one; the root constructor New accepts one.
type Endpoint struct { /* unexported */ }

// NewEndpoint builds an Endpoint from its two required parts. There are no
// options.
func NewEndpoint(baseURL string, auth AuthApplier) (Endpoint, error)
```

The auth applier sees the *fully assembled* request, including the final body,
because a body-signing scheme (SigV4) must sign the exact bytes that ship and an
OAuth refresh needs the context and can fail. It is not a header-string producer;
it mutates the request in place. `AuthApplier` is exported only because the
vendor packages, which are separate Go packages, implement it with their sealed
credentials (D7); a consumer never defines an auth mechanism.

```go
// AuthApplier carries a credential onto a fully assembled request. It sees the
// final body so a body-signing scheme can sign it; it takes a context so an
// OAuth refresh can run and fail. Implemented by vendor packages only.
type AuthApplier interface {
	Apply(ctx context.Context, req *http.Request, body []byte) error
}
```

The seam the orchestrator drives for one round-trip — build the request from
the wire body and the endpoint, decode the framed response into events, classify
an error response — is unexported and implemented only by the built-in wires
paired with an `Endpoint`. There is no consumer-implementable provider.

## REQUIREMENTS

- R-OBV1-6HH7: An `Endpoint` MUST expose no assignable fields to consumers and MUST be configurable only through its constructor's positional base URL and auth applier.
- R-OD2X-K97W: An `Endpoint` MUST own the base URL (including any model-in-path placement a vendor package bakes into it) and the auth applier, and nothing else.
- R-OEAT-Y0YL: `agentkit` MUST export `func NewEndpoint(baseURL string, auth AuthApplier) (Endpoint, error)`, taking the base URL and auth applier as its only parameters, and MUST return `ErrInvalidConfig` for a base URL that is not an absolute HTTP(S) URL or for a nil auth applier.
- R-OFIQ-BSPA: A `Conversation` MUST execute every request with `http.DefaultClient`, and construction MUST accept no consumer-supplied HTTP client, header, framer, classifier, or request mutation.
- R-YEPA-QILV: `agentkit` MUST export `Endpoint` as an opaque struct type with no exported fields, constructed only through `NewEndpoint`.
- R-ZMT9-CK07: `agentkit` MUST export the `AuthApplier` interface whose method set is exactly `Apply(ctx context.Context, req *http.Request, body []byte) error`.
- R-0VXJ-I2FW: The `AuthApplier` MUST receive the fully assembled request together with the final request body, so a body-signing scheme signs the exact bytes that are sent.
