# D7-credentials

Credentials are the input to an `AuthApplier` (D6), and agentkit deliberately
keeps **two credential worlds that are never unified**. A vendor package defines
its own credential set — its own constructors, its own closed set of accepted
shapes — and typed safety lives *inside* that package rather than in a shared root
type. The point is compile-time credential safety: passing one vendor's credential
to another vendor's constructor must not compile. `anthropic.New(openai.OAuth(ts), model)`
is a compile error, and that is the whole design, not an accident of naming.

The mechanism is a per-package **unexported marker method**. Each vendor package's
credential interface carries a method only that package can implement, so a value
from another world does not satisfy it:

```go
// package anthropic
//
// Credential is the sealed set of Anthropic credentials. The unexported marker
// means only this package can produce a value satisfying it, so a credential
// from any other vendor package fails to type-check at anthropic.New.
type Credential interface {
	apply(ctx context.Context, req *http.Request, body []byte) error
	isAnthropicCredential()
}

func APIKey(key string) Credential          // x-api-key world
func OAuth(ts TokenSource) Credential        // OAuth world; bakes transport (L2)

func New(cred Credential, model string, opts ...Option) (*agentkit.Conversation, error)
```

```go
// package openai
//
// Credential is the sealed set of OpenAI credentials, sealed by its own
// unexported marker — structurally distinct from anthropic.Credential even
// though both wrap an AuthApplier.
type Credential interface {
	apply(ctx context.Context, req *http.Request, body []byte) error
	isOpenAICredential()
}

func APIKey(key string) Credential           // Bearer world
func OAuth(ts TokenSource) Credential        // bakes transport: host/path/headers move (L2)
func New(cred Credential, model string, opts ...Option) (*agentkit.Conversation, error)
```

The two worlds are structurally distinct types; there is intentionally no single
root `Credential` interface spanning them, because a unifying interface would
re-open exactly the cross-world substitution the marker forbids. What the two
worlds share is only the runtime shape they both feed — the `AuthApplier` — and a
single `Apply` method covers every scheme: a static API key, an OAuth token
source, or SigV4 body signing. A full auth-type taxonomy is cut; there is one
method, not a hierarchy. `TokenSource` shapes legitimately differ per vendor (one
returns only a bearer, another a bearer plus an account id), and forcing them into
a common signature would be a false unification of its own.

Compile-time safety exists on the vendor path and **cannot** exist on the generic
path, because a custom base URL's vendor is unknowable — and that is correct, not a
gap. The generic wire constructor takes a bare `AuthApplier` directly; there is no
sealed credential there to mis-pair, so the compile-time guard has nothing to guard
and is rightly absent. The generic path trades the guard for reach.

One transport interaction is enforced at construction (L2): a **transport-baking
credential** (an OAuth credential that moves host, path, and headers —
D6) and `WithBaseURL` are **mutually exclusive**. Supplying both is
`ErrInvalidConfig` at construction time, before any request. `WithBaseURL` is for
the API-key and generic paths only, where the transport is not already fixed by the
credential; letting both win would leave two conflicting sources of truth for the
host.

## REQUIREMENTS

- R-3IB6-03OE: Each vendor package MUST define its own sealed credential interface carrying a package-private marker method, so a credential value from one vendor package MUST NOT satisfy another vendor package's constructor (a compile error).
- R-3JJ2-DVF3: There MUST NOT be a single shared credential interface spanning the two credential worlds; the only shared abstraction is the `AuthApplier` runtime shape.
- R-3KQY-RN5S: A single `Apply(ctx context.Context, req *http.Request, body []byte) error` method MUST cover API-key, OAuth, and body-signing (SigV4) schemes; no separate auth-type hierarchy may be introduced.
- R-3N6R-J6N6: Supplying both a transport-baking credential and `WithBaseURL` MUST fail with `ErrInvalidConfig` at construction, before any request is issued (L2).
- R-3OEN-WYDV: A per-vendor `TokenSource` MAY expose a vendor-specific shape (e.g. bearer-only vs. bearer-plus-account-id); the design MUST NOT force one common `TokenSource` signature across vendors.
- R-YM0P-1521: The `anthropic` package MUST export a sealed `Credential` interface whose method set is exactly the package-private `apply(ctx context.Context, req *http.Request, body []byte) error` and the package-private marker `isAnthropicCredential()`, plus the constructors `APIKey(key string) Credential`, `OAuth(ts TokenSource) Credential`, and `New(cred Credential, model string, opts ...Option) (*agentkit.Conversation, error)`.
- R-YN8L-EWSQ: The `openai` package MUST export a sealed `Credential` interface whose method set is exactly the package-private `apply(ctx context.Context, req *http.Request, body []byte) error` and the package-private marker `isOpenAICredential()`, plus the constructors `APIKey(key string) Credential`, `OAuth(ts TokenSource) Credential`, and `New(cred Credential, model string, opts ...Option) (*agentkit.Conversation, error)`.
- R-YOGH-SOJF: `NewEndpoint` MUST accept a bare `AuthApplier` with no compile-time vendor guard, and this absence MUST be treated as correct given an unknowable custom-base-URL vendor on the generic path.
- R-YPOE-6GA4: Every vendor constructor package MUST export a sealed `Credential` interface carrying its own package-private marker, an `APIKey(key string) Credential` constructor, and a `New(cred Credential, model string, opts ...Option) (*agentkit.Conversation, error)` constructor that bakes a selectable `(WireFormat, Endpoint)` pair, with `model` a required positional parameter.
- R-YQWA-K80T: The `anthropic` package MUST export `type TokenSource interface { Token(ctx context.Context) (string, error) }`.
- R-YS46-XZRI: The `openai` package MUST export `type TokenSource interface { Token(ctx context.Context) (bearer, accountID string, err error) }`.
- R-YTC3-BRI7: The `xai` package MUST export `type TokenSource interface { Token(ctx context.Context) (string, error) }`.
- R-YUJZ-PJ8W: The `anthropic` package MUST export `type API int` with constants `Messages` (0) and `TextCompletions` declared in that `iota` order, and `func WithAPI(api API) Option`; the zero value selects Messages.
- R-YVRW-3AZL: The `openai` package MUST export `type API int` with constants `Responses` (0) and `ChatCompletions` declared in that `iota` order, and `func WithAPI(api API) Option`; the zero value selects Responses.
- R-YWZS-H2QA: The `xai` package MUST export `type API int` with constants `Responses` (0) and `ChatCompletions` declared in that `iota` order, and `func WithAPI(api API) Option`; the zero value selects Responses.
- R-YY7O-UUGZ: The `openrouter` package MUST export `type API int` with constants `ChatCompletions` (0) and `Responses` declared in that `iota` order, and `func WithAPI(api API) Option`; the zero value selects ChatCompletions.
- R-YZFL-8M7O: The `xai` package MUST export a sealed `Credential` interface whose method set is exactly the package-private `apply(ctx context.Context, req *http.Request, body []byte) error` and the package-private marker `isXAICredential()`, plus `APIKey(key string) Credential`, `OAuth(ts TokenSource) Credential`, and `New(cred Credential, model string, opts ...Option) (*agentkit.Conversation, error)`.
- R-Z0NH-MDYD: The `openrouter` package MUST export a sealed `Credential` interface whose method set is exactly the package-private `apply(ctx context.Context, req *http.Request, body []byte) error` and the package-private marker `isOpenRouterCredential()`, plus `APIKey(key string) Credential` and `New(cred Credential, model string, opts ...Option) (*agentkit.Conversation, error)`; it MUST NOT export an `OAuth` constructor or a `TokenSource`.
- R-Z1VE-05P2: The `gemini` package MUST export a sealed `Credential` interface whose method set is exactly the package-private `apply(ctx context.Context, req *http.Request, body []byte) error` and the package-private marker `isGeminiCredential()`, plus `APIKey(key string) Credential` and `New(cred Credential, model string, opts ...Option) (*agentkit.Conversation, error)`; it MUST NOT export an `OAuth` constructor, `TokenSource`, or `API` enum.
