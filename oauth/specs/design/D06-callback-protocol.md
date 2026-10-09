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
`200` on success, `400` on each failure. They are the
only thing the user sees in the browser, and the terminal is where the real
detail goes, so they stay minimal.

The pages are templates, not code. `assets/` holds them as two human-authored
`html/template` files, embedded by the root package `oauth` and handed out as
`oauth.Assets()` (D01). **oauth's template set** is what this package draws
every page from: the set the standard library's `html/template.ParseFS` makes
from `oauth.Assets()` with the pattern `*.html`, with no function added. The
files define two templates, `success` and `failure`. `success` receives no
data. `failure` receives a value whose one field, `Description`, a string, is
the explanation the page carries; when it is empty, the template shows its own
fallback. The success page is `success` executed with nil. A state mismatch,
and a callback carrying neither code nor error, execute `failure` with an
empty `Description`, so the fallback shows; a state mismatch does so even when
the request also carries `error_description`, for the reason above. A provider
error executes `failure` with the provider's `error_description`, which may be
empty, and then the fallback shows too. Executing the named template is the
only markup the code writes: every page body is that template's output, byte
for byte. What a template writes for its data — its words, its markup, its
classes, ids and attributes — is the asset's; no requirement and no test names
any of it. A test proves a page by executing the same template of oauth's
template set with the data a requirement states and comparing the bytes with
the response, or by checking that a description it supplied appears in the
body. A template that cannot show a state this design names is an issue for a
human, never a change the build makes to the asset.

Two properties are contract beyond the template's output. First, the pages are
**self-contained** — no stylesheet, script, image, font, or any other external
reference. The page is
served from a loopback port that stops listening moments later, often to a
browser with no working network context for whatever the page might cite; a
page that renders only if a CDN answers is a page that renders as a blank
error at the exact moment the user needs to be told to go back to their
terminal. Second, every interpolated value is **HTML-escaped**. The
`error_description` a page displays is provider-supplied text arriving over the
network from outside this program, and it lands in a document the user's
browser executes. `html/template`'s contextual autoescaping is the only
escaping there is, and the set carries no function that could bypass it.

Self-containment is a property of the page, not copy, so a test proves it by
parsing the bytes of each page the handler answered, success and failure with
and without a description, as the HTML Standard reads an HTML document, and
examining every reference the parsed markup carries: every attribute of every
element that the HTML Standard, its obsolete features included, SVG or MathML
defines to hold a URL, every style sheet and style attribute, and any refresh
the page declares. A URL anywhere among them, other than an empty value or a
bare fragment pointing within the page, is a defect, whatever element carries it. Script and
event handlers are forbidden outright rather than examined, because what they
would fetch is not something a parse can decide. Such a test looks for no word
and no hook of the page, and chooses no element or attribute because this page
uses it. The parse is written in the test itself, adding no dependency. A
wrong word or hook in a template is the asset's to fix; an external reference
is a defect the test reports.

Both pages declare `Content-Type: text/html; charset=utf-8` and are flushed
before `Wait` returns, so the browser has the page in hand while the token
exchange (D04) is still in flight — the user is told to go back to the terminal
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
- R-GR97-DMAT: `Wait` MUST end with an error satisfying `errors.Is(err, context.DeadlineExceeded)` when its context's deadline expires before any callback arrives, and with an error satisfying `errors.Is(err, context.Canceled)` but not `errors.Is(err, context.DeadlineExceeded)` when its context is canceled instead.
- R-VDGK-41EM: Package `internal/callback` MUST export a `Result` struct with the fields `Code string`.
- R-LRLS-P4X1: Package `internal/callback` MUST export the sentinel errors `ErrStateMismatch` and `ErrNoCode`.
- R-VEOG-HT5B: Package `internal/callback` MUST export an `AuthorizeError` struct with the fields `Code string` and `Description string`, with a method `Error() string`.
- R-LV9H-UG54: Package `internal/callback` MUST export the method `Wait(ctx context.Context, path, state string) (Result, error)` on `*Server`.
- R-GT3R-0PA9: oauth's design defines **oauth's template set** as the `*template.Template`, where `template` is the standard library's `html/template`, that `template.ParseFS` returns when called with the file system the root package's `Assets()` returns (R-GQNY-95SV) and the single pattern `*.html`, no function having been added to it by `Funcs`; and every requirement of oauth's design that names oauth's template set MUST denote a set so made.
- R-GUBN-EH0Y: Making oauth's template set MUST succeed, its `template.ParseFS` call returning a nil error, and the set made MUST define a template named `success` and a template named `failure`.
- R-GVJJ-S8RN: The body of the response to a callback that ends `Wait` with a nil error (R-GGA3-XOMK) MUST be exactly the text that executing the template `success` of oauth's template set writes with nil data.
- R-GWRG-60IC: The body of the response to a callback that ends `Wait` with an error satisfying `errors.Is(err, ErrStateMismatch)` (R-GIPW-P83Y), whatever `error` and `error_description` parameters it also carries, or with an error satisfying `errors.Is(err, ErrNoCode)` (R-GL5P-GRLC), MUST be exactly the text that executing the template `failure` of oauth's template set writes with a value of a struct type whose only field is `Description string`, that field holding the empty string.
- R-GZ78-XJZQ: The body of the response to a callback that ends `Wait` with an error that `errors.As` unwraps to `*AuthorizeError` (R-GJXT-2ZUN) MUST be exactly the text that executing the template `failure` of oauth's template set writes with a value of a struct type whose only field is `Description string`, that field holding the request's `error_description` value, the empty string when that parameter is absent or empty.
- R-6C1K-3M98: Executing the template `success` of oauth's template set with nil data, and executing its template `failure` with a value of a struct type whose only field is `Description string`, that field holding the empty string and, separately, a non-empty string, MUST each return a nil error.
- R-TPZN-04BM: Every body with which a callback on the configured path is answered (R-GVJJ-S8RN, R-GWRG-60IC, R-GZ78-XJZQ), the failure page drawn with an empty and with a non-empty `Description` included, MUST be self-contained: its bytes, read as the HTML Standard parses an HTML document, MUST give no non-empty value, other than one consisting only of a fragment that begins with `#`, to any attribute that the HTML Standard, its obsolete features included, SVG or MathML defines to hold a URL or a list of URLs, on whatever element it sits; MUST declare no refresh; MUST hold no `url(`, `image-set(` or `@import`, compared ASCII case-insensitively, in any style sheet or style attribute; and MUST carry no script and no event-handler attribute.
