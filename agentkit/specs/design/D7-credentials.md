# D7-credentials

A credential is what a consumer holds — an API key, or a source of OAuth
tokens. The wire format (D5) is what knows where a credential goes on the
request. agentkit keeps those two apart. The root package exports one sealed
`Credential` type with two constructors, and every `Offering` says which
credential modes it accepts and turns a credential into the `Authenticator`
(D6) that pairs it with the offering's wire format. There are no per-vendor
packages: the whole construction path is root symbols driven by catalog data.

```go
package agentkit

// AuthMode names how a credential is presented; its values are also what a
// conversation reports as Identity.AuthMode (D1).
type AuthMode string

const (
	AuthModeAPIKey AuthMode = "api_key"
	AuthModeOAuth  AuthMode = "oauth"
)

// Credential is the sealed set of consumer credentials. Its methods are
// unexported, so a value comes only from APIKey or OAuth.
type Credential interface {
	mode() AuthMode
	isCredential()
}

func APIKey(key string) Credential
func OAuth(source TokenSource) Credential

// Token is one OAuth grant. AccountID is optional; only providers that need
// it (OpenAI) read it.
type Token struct {
	Bearer    string
	AccountID string
}

// TokenSource yields the current token and can mint a new one. Token is
// called before every request; Refresh is called by the conversation when a
// request comes back 401 (D22). The root ships one concrete source,
// Offering.TokenSource (D22); the interface stays public so tests can fake it.
type TokenSource interface {
	Token(ctx context.Context) (Token, error)
	Refresh(ctx context.Context) (Token, error)
}

// Authenticator turns a credential into the authenticator for this offering:
// it holds the credential and hands each resolved value to o.WireFormat,
// which places it on the request (D5).
func (o Offering) Authenticator(cred Credential) (Authenticator, error)
```

The consumer's flow is: resolve an offering, pick a credential whose mode the
offering lists, build the endpoint, construct:

```go
offering, _ := agentkit.Lookup("claude-sonnet-5", "", "")
auth, _     := offering.Authenticator(agentkit.APIKey(key))
ep, _       := agentkit.NewEndpoint(offering.BaseURL, auth)   // or a proxy URL
conv, _     := agentkit.New(offering.WireFormat, ep, offering.WireModel, cfg)
```

`AuthModes` is the discovery surface: an application that holds several kinds
of credential reads it to decide which to offer. `Authenticator` is the guard: a
credential whose mode the offering does not list is `ErrInvalidConfig` at
construction, before any request. Choosing between credentials is the
application's business; where an OAuth token lives is the application's
`TokenStore`, and how it is refreshed is D22.

Where a credential lands is fixed per wire format (D5), because a consumer who
overrides `BaseURL` to point at a proxy still relies on the headers being
right. The authenticator resolves the credential — the key, or the current
token from the source — and the wire format places it:

| Wire format | API key | OAuth |
|---|---|---|
| `AnthropicMessagesWire()` | `x-api-key` header | not accepted (Anthropic's terms do not permit it) |
| `GeminiGenerateContentWire()` | `key` query parameter | not accepted |
| `ChatWire()`, `ResponsesWire()` | `Authorization: Bearer` | `Authorization: Bearer` |
| `OpenAIChatWire()`, `OpenAIResponsesWire()` | `Authorization: Bearer` | `Authorization: Bearer` + `ChatGPT-Account-Id` |

Whether a credential mode is accepted at all is the offering's `AuthModes`,
not the wire's: OpenRouter speaks the generic chat wire and lists only
`api_key`.

The `Authenticator` an offering returns also carries the provenance that
`New` reads into `Identity` (D1): `Identity.Endpoint` is the offering's
`ID` string and `Identity.AuthMode` the credential's mode. That is the
link cost (D3) prices on, and it holds even when the consumer overrides the
base URL — a proxy in front of Anthropic still prices as Anthropic.

A base-URL override and OAuth are no longer mutually exclusive: the consumer
owns the URL they pass to `NewEndpoint`, so there is one source of truth.

## REQUIREMENTS

- R-P5PA-T4A1: `agentkit` MUST export `type AuthMode string` with the constants `AuthModeAPIKey = "api_key"` and `AuthModeOAuth = "oauth"`.
- R-P6X7-6W0Q: `agentkit` MUST export a sealed `Credential` interface, not implementable outside the root package, together with the constructors `APIKey(key string) Credential` and `OAuth(source TokenSource) Credential`.
- R-V7UO-KE8S: `agentkit` MUST export `type Token struct { Bearer string; AccountID string }` with exactly those fields and `type TokenSource interface { Token(ctx context.Context) (Token, error); Refresh(ctx context.Context) (Token, error) }`.
- R-KE5C-F60Q: `NewEndpoint` MUST accept any `Authenticator` with no compile-time guard on its origin.
- R-KFD8-SXRF: A single `Authenticate(ctx context.Context, req *http.Request, body []byte) error` method MUST cover API-key, OAuth, and body-signing (SigV4) schemes; no separate auth-type hierarchy may be introduced.
- R-KGL5-6PI4: `agentkit` MUST export `func (o Offering) Authenticator(cred Credential) (Authenticator, error)`, which MUST return `ErrInvalidConfig` for a nil `cred` or for a credential whose mode is not in `o.AuthModes`, and MUST NOT export `Offering.Auth`.
- R-KHT1-KH8T: The authenticator from `o.Authenticator(APIKey(k))` MUST transmit `k` as placed by `o.WireFormat`: as the `x-api-key` header when it is `AnthropicMessagesWire()`, as the `key` URL query parameter when it is `GeminiGenerateContentWire()`, and as the `Authorization: Bearer k` header when it is `ChatWire()`, `ResponsesWire()`, `OpenAIChatWire()`, or `OpenAIResponsesWire()`.
- R-KJ0X-Y8ZI: The authenticator from `o.Authenticator(OAuth(src))` MUST call `src.Token(ctx)` on every request and transmit `Token.Bearer` as the `Authorization: Bearer` header; when `o.WireFormat` is `OpenAIResponsesWire()` or `OpenAIChatWire()` it MUST also transmit `Token.AccountID` as the `ChatGPT-Account-Id` header and MUST fail with `ErrInvalidConfig` when `AccountID` is empty; and `Authenticate` MUST return `ErrInvalidConfig` for a nil `src` and MUST return `src`'s error unchanged when `Token` fails.
- R-KK8U-C0Q7: A `Conversation` built by `New(o.WireFormat, NewEndpoint(url, auth), model, cfg)` where `auth` came from `o.Authenticator(cred)` MUST report `Identity.Endpoint` equal to `string(o.ID)` for any `url`, and `Identity.AuthMode` equal to `"api_key"` for an `APIKey` credential and `"oauth"` for an `OAuth` credential.
