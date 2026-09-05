# D6-endpoint-and-dialect

An **`Endpoint`** is *where* a wire's bytes are sent and *how* they are
authenticated. It is the second axis: a wire codec (D5) owns the body grammar
and the error envelope, an `Endpoint` owns the base URL and the authenticator,
and nothing else. The two are paired by the root constructor `New` (D18) for
the orchestrator to drive.

The authenticator is the default source of the URL. It came from an offering
(D7), so it knows which of the offering's endpoint specs (D21) serves its
credential mode, and that spec's `BaseURL` is where the bytes go. That is how
the same four lines reach the OpenAI platform under an API key and the Codex
backend under a ChatGPT OAuth token: the offering lists both specs and the
authenticator picks by mode. A consumer who wants a different URL, a proxy
say, overrides it with the one option `NewEndpoint` accepts:

```go
// Endpoint is an opaque transport description: base URL plus authenticator.
// Construct it with NewEndpoint; its fields are unexported.
type Endpoint struct { /* unexported */ }

// EndpointOption adjusts NewEndpoint. WithBaseURL is the only one.
type EndpointOption func(*endpointConfig)

// WithBaseURL replaces the URL the authenticator's offering would supply.
func WithBaseURL(url string) EndpointOption

// NewEndpoint builds an Endpoint from an authenticator obtained from
// Offering.Authenticator. Without WithBaseURL the URL is the BaseURL of the
// offering's EndpointSpec for the authenticator's AuthMode.
func NewEndpoint(auth Authenticator, opts ...EndpointOption) (Endpoint, error)
```

```go
ep, _ := agentkit.NewEndpoint(auth)
ep, _ := agentkit.NewEndpoint(auth, agentkit.WithBaseURL("https://proxy.example.com/v1/responses"))
```

Everything that earlier revisions hung off the endpoint as consumer-installable
hooks, extra headers, a replaceable `Framer`, an error classifier, a
request-mutation hook, an HTTP client, stays gone. `WithBaseURL` is the one
option because a URL is the one thing a consumer legitimately owns. Framing is
SSE for every built-in wire (D5), error classification belongs to the wire
(D4, D5), and requests execute with `http.DefaultClient`. Proxy and transport
configuration beyond the URL is a deliberately open question for a later
design.

The authenticator sees the *fully assembled* request, including the final body,
because a body-signing scheme (SigV4) must sign the exact bytes that ship and an
OAuth rotation needs the context and can fail. It holds and resolves the
credential; the one from `Offering.Authenticator` hands the resolved token to
the offering's wire format, which knows where it goes (D5, D7).
`Authenticator` is exported only because `Offering.Authenticator` returns one
and `NewEndpoint` must accept it across the consumer's call (D7); a consumer
never defines an auth mechanism.

```go
// Authenticator authenticates a fully assembled request. It sees the final
// body so a body-signing scheme can sign it; it takes a context so an OAuth
// rotation can run and fail. Obtained from Offering.Authenticator.
type Authenticator interface {
	Authenticate(ctx context.Context, req *http.Request, body []byte) error
}
```

The seam the orchestrator drives for one round-trip, build the request from
the wire body and the endpoint, decode the framed response into events,
classify an error response, is unexported and implemented only by the built-in
wires paired with an `Endpoint`. There is no consumer-implementable provider.

## REQUIREMENTS

- R-JVTF-4LVV: An `Endpoint` MUST expose no assignable fields to consumers and MUST be configurable only through its constructor's positional authenticator and the `WithBaseURL` option.
- R-PWJ3-82LB: An `Endpoint` MUST own the base URL (including any model-in-path placement the catalog offering bakes into `BaseURL`) and the authenticator, and nothing else.
- R-JX1B-IDMK: `agentkit` MUST export `func NewEndpoint(auth Authenticator, opts ...EndpointOption) (Endpoint, error)` and MUST return `ErrInvalidConfig` for a nil `auth`.
- R-JY97-W5D9: `agentkit` MUST export `type EndpointOption` and `func WithBaseURL(url string) EndpointOption`, and MUST NOT export any other `EndpointOption` constructor.
- R-JZH4-9X3Y: `NewEndpoint(auth)` with no `WithBaseURL`, where `auth` came from `o.Authenticator(r)`, MUST send every request to the `BaseURL` of the `EndpointSpec` in `o.Endpoints` whose `AuthMode` equals `r.AuthMode()`.
- R-K0P0-NOUN: `NewEndpoint(auth, WithBaseURL(u))` MUST send every request to `u` in place of the offering's URL, MUST return `ErrInvalidConfig` when `u` is not an absolute HTTP(S) URL, and when `WithBaseURL` is given more than once the last MUST win.
- R-OFIQ-BSPA: A `Conversation` MUST execute every request with `http.DefaultClient`, and construction MUST accept no consumer-supplied HTTP client, header, framer, classifier, or request mutation.
- R-YEPA-QILV: `agentkit` MUST export `Endpoint` as an opaque struct type with no exported fields, constructed only through `NewEndpoint`.
- R-KBPJ-NMJC: `agentkit` MUST export the `Authenticator` interface whose method set is exactly `Authenticate(ctx context.Context, req *http.Request, body []byte) error`, and MUST NOT export `AuthApplier`.
- R-U1DK-UGQI: When a `Conversation` builds a request, the `body` argument passed to `Authenticator.Authenticate` MUST be byte-equal to the `WireFormat.EncodeRequest` output used as that request's body, so a body-signing authenticator signs the exact bytes transmitted.
