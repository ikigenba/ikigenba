# D06-callback-protocol

What the bound loopback endpoint (D05) does with an incoming request, what the
browser is shown, and how the wait ends.

```go
package callback

// Result is the successful information carried by a callback.
type Result struct{ Code string }

var (
    ErrStateMismatch = errors.New("callback state did not match")
    ErrNoCode        = errors.New("callback carried neither code nor error")
)

// AuthorizeError reports a provider-sent error=/error_description= redirect.
type AuthorizeError struct{ Code, Description string }

func (e *AuthorizeError) Error() string

// Wait serves the bound listeners until one callback completes the flow or ctx
// expires. Precondition: called at most once.
func (s *Server) Wait(ctx context.Context, path, state string) (Result, error)
```

`path` and `state` are parameters rather than `Server` fields: they are facts
about **one login attempt**, not about the socket, and passing them makes it
impossible to start waiting without having decided what a valid callback looks
like. `Wait` is single-use by precondition — the flow it serves happens once —
and that is documented here rather than defended with a runtime guard the only
caller cannot reach.

**Outcomes are typed.** Every way the wait can end is an `errors.Is`/
`errors.As` target rather than a formatted sentence to substring-match:
`ErrStateMismatch`, `ErrNoCode`, `*AuthorizeError` for a provider-reported
failure, and the context's own `context.DeadlineExceeded` or
`context.Canceled` for the two ways time runs out. `cli` (D11) owns turning
these into the text a user reads.

**A stray request must not kill a login.** A request whose path is not `path`
gets `404` and the wait continues. Browsers speculatively fetch `/favicon.ico`
against any origin they render a page from, so treating "some request arrived"
as "the flow concluded" would make a login fail for a reason the user could
neither see nor act on. Only a request to the configured callback path is a
callback at all.

**`state` is checked first, before `error=`.** The order is load-bearing and
security-motivated, not incidental. `state` is the only thing tying an inbound
request to the session this process started; until it matches, the request is
from an unauthenticated stranger who reached a loopback port. Reading
`error`/`error_description` out of such a request and reporting them to the
user would let that stranger choose the sentence the user reads — a diagnostic
the operator trusts, written by whoever got there first. So a callback whose
`state` does not match is rejected as a state mismatch **even when it also
carries `error=`**, and the provider's error is not consulted. The cost is
real and worth naming: a genuine provider error that arrives with a mangled
`state` reports the less specific cause. That trade favors not surfacing
attacker-controlled prose, and the requirement below pins the interaction so
the ordering cannot be quietly inverted later.

Given a matching `state`, a callback carrying `error=` yields an
`*AuthorizeError` carrying the provider's `error` code and its
`error_description`; one carrying neither `code` nor `error` — a shape the
protocol does not define — yields `ErrNoCode`, distinct from both so a
misbehaving provider is not misreported as a rejection.

**The pages the browser is shown.** Every terminal outcome writes an HTML page:
`200` with "Login complete" on success, `400` on each failure. They are the
only thing the user sees in the browser, and the terminal is where the real
detail goes, so they stay minimal.

Two properties are contract. First, the pages are **self-contained** — no
stylesheet, script, image, font, or any other external reference. The page is
served from a loopback port that stops listening moments later, often to a
browser with no working network context for whatever the page might cite; a
page that renders only if a CDN answers is a page that renders as a blank
error at the exact moment the user needs to be told to go back to their
terminal. Second, every interpolated value is **HTML-escaped**. The
`error_description` a page displays is provider-supplied text arriving over the
network from outside this program, and it lands in a document the user's
browser executes.

Both pages declare `Content-Type: text/html; charset=utf-8` and are flushed
before `Wait` returns, so the browser has the page in hand while the token
exchange (D04) is still in flight — the user sees "go back to your terminal"
at the moment the terminal starts doing the work, rather than after it
finishes.

## REQUIREMENTS

- R-GGA3-XOMK: A request to the configured callback path whose `state` equals the expected value and which carries a non-empty `code` MUST end `Wait` with a nil error and a `Result` whose `Code` is that value, and MUST be answered with HTTP status 200.
- R-GHI0-BGD9: A request to any path other than the configured callback path MUST be answered with HTTP status 404 and MUST NOT end the wait; a subsequent valid callback on the configured path MUST still yield its code.
- R-GIPW-P83Y: A callback whose `state` parameter is absent, empty, or unequal to the expected value MUST end `Wait` with an error satisfying `errors.Is(err, ErrStateMismatch)` and MUST be answered with HTTP status 400.
- R-GJXT-2ZUN: A callback with a matching `state` that carries a non-empty `error` parameter MUST end `Wait` with an error that `errors.As` unwraps to `*AuthorizeError` whose `Code` and `Description` are the request's `error` and `error_description` values, and MUST be answered with HTTP status 400.
- R-GL5P-GRLC: A callback with a matching `state` carrying neither a non-empty `code` nor a non-empty `error` MUST end `Wait` with an error satisfying `errors.Is(err, ErrNoCode)` and MUST be answered with HTTP status 400.
- R-GMDL-UJC1: A callback carrying both a mismatched `state` and a non-empty `error` parameter MUST end `Wait` with an error satisfying `errors.Is(err, ErrStateMismatch)`, and that error MUST NOT unwrap to `*AuthorizeError` via `errors.As`.
- R-GNLI-8B2Q: Both the success and the failure page MUST be served with the `Content-Type` header exactly `text/html; charset=utf-8`, and a provider-supplied `error_description` containing HTML metacharacters MUST appear in the failure page only in escaped form, never as raw markup.
- R-GOTE-M2TF: Both the success and the failure page MUST be self-contained, containing no reference that would cause the browser to fetch a further resource.
- R-GR97-DMAT: `Wait` MUST end with an error satisfying `errors.Is(err, context.DeadlineExceeded)` when its context's deadline expires before any callback arrives, and with an error satisfying `errors.Is(err, context.Canceled)` but not `errors.Is(err, context.DeadlineExceeded)` when its context is canceled instead.
