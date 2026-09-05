# D7-credentials

A credential is a secret that can be presented on a request now and, for some
kinds, replaced with a fresh one when the vendor rejects it. agentkit names
that behavior a **rotator**: something that hands over the current token and
knows how to rotate it. Two rotators ship. An API key never rotates. An OAuth
token rotates through a `TokenStore` (D22), the place the token lives. The
catalog offering (D21) turns a rotator into the `Authenticator` (D6) that pairs
it with the offering's wire format, and knows which of its endpoints that
rotator's mode is served from. There are no per-vendor packages: the whole
construction path is root symbols driven by catalog data.

```go
package agentkit

// AuthMode names how a credential is presented; its values are also what a
// conversation reports as Identity.AuthMode (D1).
type AuthMode string

const (
	AuthModeAPIKey AuthMode = "api_key"
	AuthModeOAuth  AuthMode = "oauth"
)

// Token is one presentable secret. AccountID is optional; only the OpenAI
// wires read it.
type Token struct {
	Bearer    string
	AccountID string
}

// Rotator presents the current secret and can rotate it. AuthMode says which
// kind of credential this is, so an offering can pick the endpoint spec that
// serves it. Rotate receives the offering's Rotation (D21): where to send the
// refresh and which client id to present.
type Rotator interface {
	AuthMode() AuthMode
	Token(ctx context.Context) (Token, error)
	Rotate(ctx context.Context, r Rotation) (Token, error)
}

// APIKeyRotator is the non-rotating rotator: Token always returns key, and
// Rotate fails because an API key cannot be refreshed.
func APIKeyRotator(key string) Rotator

// OAuthRotator rotates through store: Token reads the stored access token,
// Rotate posts the refresh grant and writes the result back (D22).
func OAuthRotator(store TokenStore) Rotator

// Authenticator turns a rotator into the authenticator for this offering: it
// holds the rotator, remembers which endpoint spec serves its mode, and hands
// each token to o.WireFormat, which places it on the request (D5).
func (o Offering) Authenticator(r Rotator) (Authenticator, error)
```

The consumer's flow is the same shape for both kinds of credential: resolve an
offering, build a rotator, ask the offering for an authenticator, build the
endpoint from the authenticator, construct:

```go
offering, _ := agentkit.Lookup("gpt-5.6-sol", "openai", "responses")
auth, _     := offering.Authenticator(agentkit.APIKeyRotator(key))
ep, _       := agentkit.NewEndpoint(auth)
conv, _     := agentkit.New(offering.WireFormat, ep, offering.WireModel, cfg)
```

```go
offering, _ := agentkit.Lookup("gpt-5.6-sol", "openai", "responses")
auth, _     := offering.Authenticator(agentkit.OAuthRotator(agentkit.FileTokenStore(path)))
ep, _       := agentkit.NewEndpoint(auth)
conv, _     := agentkit.New(offering.WireFormat, ep, offering.WireModel, cfg)
```

The two flows differ in one expression. `NewEndpoint(auth)` needs no URL
because the authenticator came from an offering and knows the endpoint spec
for its mode (D6, D21); the OAuth flow above therefore posts to the Codex
backend, the only host that honors a ChatGPT token, while the API-key flow
posts to the platform API. `Offering.Endpoints` is the discovery surface: an
application that holds several kinds of credential reads it to decide which
to offer. `Authenticator` is the guard: a rotator whose mode no endpoint spec
of the offering lists is `ErrInvalidConfig` at construction, before any
request. Choosing between credentials is the application's business; where an
OAuth token lives is the application's `TokenStore`, and how it is rotated is
D22.

Where a token lands is fixed per wire format (D5), because a consumer who
overrides the URL to point at a proxy still relies on the headers being right.
The authenticator resolves the token and the wire format places it:

| Wire format | API key | OAuth |
|---|---|---|
| `AnthropicMessagesWire()` | `x-api-key` header | not accepted (Anthropic's terms do not permit it) |
| `GeminiGenerateContentWire()` | `key` query parameter | not accepted |
| `ChatWire()`, `ResponsesWire()` | `Authorization: Bearer` | `Authorization: Bearer` |
| `OpenAIChatWire()` | `Authorization: Bearer` | not accepted (no chat endpoint honors a ChatGPT token) |
| `OpenAIResponsesWire()` | `Authorization: Bearer` | `Authorization: Bearer` + `ChatGPT-Account-Id` |

Whether a credential mode is accepted at all is the offering's `Endpoints`,
not the wire's: OpenRouter speaks the generic chat wire and lists only an
`api_key` spec. The OAuth column was proven live before being written: a
ChatGPT token is rejected by `api.openai.com` on both protocols and by every
Codex chat path, and accepted only by the Codex responses endpoint; an xAI
token is accepted on both xAI protocols at `api.x.ai` with the bearer alone.

The `Authenticator` an offering returns also carries the provenance that
`New` reads into `Identity` (D1): `Identity.Endpoint` is the offering's `ID`
string and `Identity.AuthMode` the rotator's mode. That is the link cost (D3)
prices on, and it holds even when the consumer overrides the base URL: a proxy
in front of Anthropic still prices as Anthropic.

## REQUIREMENTS

- R-P5PA-T4A1: `agentkit` MUST export `type AuthMode string` with the constants `AuthModeAPIKey = "api_key"` and `AuthModeOAuth = "oauth"`.
- R-K1WX-1GLC: `agentkit` MUST export `type Token struct { Bearer string; AccountID string }` with exactly those fields and `type Rotator interface { AuthMode() AuthMode; Token(ctx context.Context) (Token, error); Rotate(ctx context.Context, r Rotation) (Token, error) }` with exactly that method set, and MUST NOT export `Credential`, `APIKey`, `OAuth`, or `TokenSource`.
- R-K34T-F8C1: `agentkit` MUST export `func APIKeyRotator(key string) Rotator`, whose `AuthMode` returns `AuthModeAPIKey`, whose `Token` returns `Token{Bearer: key}` with no store or network access, and whose `Rotate` returns `ErrInvalidConfig` without any network access.
- R-K4CP-T02Q: `agentkit` MUST export `func OAuthRotator(store TokenStore) Rotator`, whose `AuthMode` returns `AuthModeOAuth`; its `Token` and `Rotate` behavior is fixed by D22.
- R-KE5C-F60Q: `NewEndpoint` MUST accept any `Authenticator` with no compile-time guard on its origin.
- R-KFD8-SXRF: A single `Authenticate(ctx context.Context, req *http.Request, body []byte) error` method MUST cover API-key, OAuth, and body-signing (SigV4) schemes; no separate auth-type hierarchy may be introduced.
- R-K5KM-6RTF: `agentkit` MUST export `func (o Offering) Authenticator(r Rotator) (Authenticator, error)`, which MUST return `ErrInvalidConfig` for a nil `r` or for a rotator whose `AuthMode()` matches no `EndpointSpec.AuthMode` in `o.Endpoints`, and MUST NOT export `Offering.Auth` or `Offering.TokenSource`.
- R-K6SI-KJK4: The authenticator from `o.Authenticator(r)` where `r.AuthMode()` is `AuthModeAPIKey` MUST call `r.Token(ctx)` on every request and transmit `Token.Bearer` as placed by `o.WireFormat`: as the `x-api-key` header when it is `AnthropicMessagesWire()`, as the `key` URL query parameter when it is `GeminiGenerateContentWire()`, and as the `Authorization: Bearer <Bearer>` header when it is `ChatWire()`, `ResponsesWire()`, `OpenAIChatWire()`, or `OpenAIResponsesWire()`; and `Authenticate` MUST return `r`'s error unchanged when `Token` fails.
- R-K98B-C31I: The authenticator from `o.Authenticator(r)` where `r.AuthMode()` is `AuthModeOAuth` MUST call `r.Token(ctx)` on every request and transmit `Token.Bearer` as the `Authorization: Bearer` header; when `o.WireFormat` is `OpenAIResponsesWire()` it MUST also transmit `Token.AccountID` as the `ChatGPT-Account-Id` header and MUST fail with `ErrInvalidConfig` when `AccountID` is empty; and `Authenticate` MUST return `r`'s error unchanged when `Token` fails.
- R-KAG7-PUS7: A `Conversation` built by `New(o.WireFormat, ep, model, cfg)` where `ep` was built by `NewEndpoint` from `o.Authenticator(r)` MUST report `Identity.Endpoint` equal to `string(o.ID)` whether or not `WithBaseURL` was given, and `Identity.AuthMode` equal to `string(r.AuthMode())`.
