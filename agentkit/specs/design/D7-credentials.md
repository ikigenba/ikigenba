# D7-credentials

Credentials are the input to an `AuthApplier` (D6), and agentkit deliberately
keeps **two credential worlds that are never unified**. A vendor package defines
its own credential set — its own constructors, its own closed set of accepted
shapes — and typed safety lives *inside* that package rather than in a shared root
type. The point is compile-time credential safety: passing one vendor's credential
to another vendor's constructor must not compile. `anthropic.New(openai.Subscription(ts))`
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
func OAuth(ts TokenSource) Credential        // subscription world; bakes transport (L2)

func New(cred Credential, opts ...Option) (*agentkit.Conversation, error)
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
func Subscription(ts TokenSource) Credential  // bakes transport: host/path/headers move (L2)
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
credential** (a subscription/OAuth credential that moves host, path, and headers —
D6) and `WithBaseURL` are **mutually exclusive**. Supplying both is
`ErrInvalidConfig` at construction time, before any request. `WithBaseURL` is for
the API-key and generic paths only, where the transport is not already fixed by the
credential; letting both win would leave two conflicting sources of truth for the
host.

## REQUIREMENTS

- R-3IB6-03OE: Each vendor package MUST define its own sealed credential interface carrying a package-private marker method, so a credential value from one vendor package MUST NOT satisfy another vendor package's constructor (a compile error).
- R-3JJ2-DVF3: There MUST NOT be a single shared credential interface spanning the two credential worlds; the only shared abstraction is the `AuthApplier` runtime shape.
- R-3KQY-RN5S: A single `Apply(ctx context.Context, req *http.Request, body []byte) error` method MUST cover API-key, OAuth, and body-signing (SigV4) schemes; no separate auth-type hierarchy may be introduced.
- R-3LYV-5EWH: The generic wire constructor MUST accept a bare `AuthApplier` with no compile-time vendor guard, and this absence MUST be treated as correct given an unknowable custom-base-URL vendor.
- R-3N6R-J6N6: Supplying both a transport-baking credential and `WithBaseURL` MUST fail with `ErrInvalidConfig` at construction, before any request is issued (L2).
- R-3OEN-WYDV: A per-vendor `TokenSource` MAY expose a vendor-specific shape (e.g. bearer-only vs. bearer-plus-account-id); the design MUST NOT force one common `TokenSource` signature across vendors.
