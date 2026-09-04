# D6-endpoint-and-dialect

An **`Endpoint`** is *where* a wire's bytes are sent and *how* they are
authenticated. It is the second axis: a wire codec (D5) owns the body grammar
and the error envelope, an `Endpoint` owns the base URL and the authenticator —
and nothing else. The two are paired by the root constructor `New` (D18) for
the orchestrator to drive. The consumer builds the `Endpoint` from the
catalog offering's `BaseURL` (or any URL of their own) and the authenticator
`Offering.Authenticator` returns for their credential (D7, D21); that is the whole
customization surface.

**What the endpoint owns:** the base URL, including any model-in-path placement
the catalog offering has already baked into it (Gemini names the model in the
path; the offering's `BaseURL` carries it), and the authenticator. Everything
that earlier revisions hung off the endpoint as consumer-installable hooks —
extra headers, a replaceable `Framer`, an error classifier, a request-mutation
hook, an HTTP client — is gone. Framing is SSE for every built-in wire (D5),
error classification belongs to the wire (D4, D5), and requests execute with
`http.DefaultClient`. Proxy and transport configuration is a deliberately open
question for a later design; it is not rejected, it is simply not designed yet,
and it will not return as a consumer-implemented hook.

```go
// Endpoint is an opaque transport description: base URL plus authenticator.
// Construct it with NewEndpoint; its fields are unexported. A consumer
// builds one; the root constructor New accepts one.
type Endpoint struct { /* unexported */ }

// NewEndpoint builds an Endpoint from its two required parts. There are no
// options.
func NewEndpoint(baseURL string, auth Authenticator) (Endpoint, error)
```

The authenticator sees the *fully assembled* request, including the final body,
because a body-signing scheme (SigV4) must sign the exact bytes that ship and an
OAuth refresh needs the context and can fail. It holds and resolves the
credential; the one from `Offering.Authenticator` hands the resolved credential
to the offering's wire format, which knows where it goes (D5, D7).
`Authenticator` is exported only because `Offering.Authenticator` returns one
and `NewEndpoint` must accept it across the consumer's call (D7); a consumer
never defines an auth mechanism.

```go
// Authenticator authenticates a fully assembled request. It sees the final
// body so a body-signing scheme can sign it; it takes a context so an OAuth
// refresh can run and fail. Obtained from Offering.Authenticator.
type Authenticator interface {
	Authenticate(ctx context.Context, req *http.Request, body []byte) error
}
```

The seam the orchestrator drives for one round-trip — build the request from
the wire body and the endpoint, decode the framed response into events, classify
an error response — is unexported and implemented only by the built-in wires
paired with an `Endpoint`. There is no consumer-implementable provider.

## REQUIREMENTS

- R-OBV1-6HH7: An `Endpoint` MUST expose no assignable fields to consumers and MUST be configurable only through its constructor's positional base URL and authenticator.
- R-PWJ3-82LB: An `Endpoint` MUST own the base URL (including any model-in-path placement the catalog offering bakes into `BaseURL`) and the authenticator, and nothing else.
- R-KAHN-9USN: `agentkit` MUST export `func NewEndpoint(baseURL string, auth Authenticator) (Endpoint, error)`, taking the base URL and authenticator as its only parameters, and MUST return `ErrInvalidConfig` for a base URL that is not an absolute HTTP(S) URL or for a nil authenticator.
- R-OFIQ-BSPA: A `Conversation` MUST execute every request with `http.DefaultClient`, and construction MUST accept no consumer-supplied HTTP client, header, framer, classifier, or request mutation.
- R-YEPA-QILV: `agentkit` MUST export `Endpoint` as an opaque struct type with no exported fields, constructed only through `NewEndpoint`.
- R-KBPJ-NMJC: `agentkit` MUST export the `Authenticator` interface whose method set is exactly `Authenticate(ctx context.Context, req *http.Request, body []byte) error`, and MUST NOT export `AuthApplier`.
- R-U1DK-UGQI: When a `Conversation` builds a request, the `body` argument passed to `Authenticator.Authenticate` MUST be byte-equal to the `WireFormat.EncodeRequest` output used as that request's body, so a body-signing authenticator signs the exact bytes transmitted.
