# D03-authorize-url

The authorize URL is the one artifact the user's browser visits. Package
`internal/oauth` builds it from the client configuration and a session (D02):

```go
package oauth

// Client holds the provider-independent OAuth client configuration. AuthURL
// and TokenURL arrive already parsed, so no URL syntax error can surface this
// far down; options rejects a malformed endpoint at the flag boundary (D09).
type Client struct {
    AuthURL, TokenURL      *url.URL
    ClientID, ClientSecret string
    RedirectURI, Scope     string
}

// Param is a key/value parameter or header supplied by the caller.
type Param struct{ Key, Value string }

// Challenge returns BASE64URL-ENCODE(SHA256(ASCII(verifier))), unpadded,
// per RFC 7636 §4.2. It is exported so the RFC's published test vector can be
// asserted directly, rather than by re-parsing a constructed URL.
func Challenge(verifier string) string

// AuthorizeURL constructs the browser authorization URL. It is infallible:
// AuthURL is already parsed (see Client), the verifier's grammar is a
// postcondition of NewSession (D02), and the caller guarantees extra contains
// no reserved key (see ReservedAuthorizeParam).
func (c Client) AuthorizeURL(s Session, extra []Param) string

// ReservedAuthorizeParam reports whether key is one the authorize request
// writes itself. It is the single authority on that set; options asks it
// rather than carrying a second copy (D09).
func ReservedAuthorizeParam(key string) bool
```

**The required parameters.** Every authorize URL carries
`response_type=code`, `client_id`, `redirect_uri`, `state`,
`code_challenge`, and `code_challenge_method=S256`. `scope` is carried **only
when non-empty** — an empty `--scope` omits the parameter entirely rather than
sending `scope=`, since a present-but-empty scope is a different request from
no scope at all, and providers may treat it as one.

**S256 only.** RFC 7636 defines two `code_challenge_method` values, `plain` and
`S256`, and makes S256 Mandatory To Implement on servers. `plain` exists in the
RFC for clients that cannot compute SHA-256 — not a constraint any Go program
has — so this design offers no way to select it. A flag to weaken the challenge
would be a footgun with no legitimate caller.

**The challenge derivation is pinned to the RFC's own vector.** RFC 7636
Appendix B publishes a worked example, reproduced here and verified
independently of any implementation in this repository:

| field | value |
|---|---|
| `code_verifier` | `dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk` |
| `code_challenge` | `E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM` |

That verifier is 43 characters — exactly the grammar's minimum (D02) — so the
vector pins the lower bound of the accepted range at the same time as it pins
the digest, the base64url alphabet, and the absence of padding.

**Space encodes as `+`, not `%20`.** This is protocol, not preference. RFC 6749
§4.1.1 states that the client constructs the request URI "by adding the
following parameters to the query component of the authorization endpoint URI
using the `application/x-www-form-urlencoded` format, per Appendix B" — so the
*query component* of the authorize URL is form-urlencoded by mandate, not
merely percent-encoded. Appendix B's own worked example encodes U+0020 SPACE as
`+` and U+002B PLUS SIGN as `%2B`. A space-delimited `scope` therefore reaches
the provider with `+` between its values. `%20` would be the deviation, and the
reference first-party clients for the providers in this tool's help text send
`+` for the same reason.

**A query already on the endpoint is retained.** RFC 6749 §3.1: the
authorization endpoint URI "MAY include an `application/x-www-form-urlencoded`
formatted query component, which **MUST be retained** when adding additional
query parameters." So a provider whose documented authorize endpoint already
carries a parameter keeps it, and this design's parameters are appended
alongside. The predecessor implementation violated this — it assigned the query
outright and silently discarded whatever the endpoint URI arrived with,
producing a request missing a parameter the provider's own documentation told
the user to include, with no diagnostic.

**Extra parameters are the vendor escape hatch.** Real providers demand
non-protocol parameters on the authorize request; this tool holds no
provider-specific knowledge, so `--auth-param` (D08) supplies them generically.
They are appended **after** the required set, in the order the caller gave them,
and a key repeated by the caller is sent repeatedly rather than collapsing to
its last value — a query string is a multimap, and silently discarding a
caller's second value would be a surprise the caller cannot detect.

**One owner for the reserved set.** The parameters this request writes itself
cannot also be supplied by the caller without the two disagreeing. This package
writes them, so this package owns the list and exports it as
`ReservedAuthorizeParam`; `options` decides *when* to check a caller's key and
*what* to report (D09), and carries no copy of the set. One encoding of the
rule, in the package that owns the format — adding a parameter to the request
is then a single edit that the flag boundary follows automatically.

## REQUIREMENTS

- R-J54Q-DJJP: `AuthorizeURL` MUST produce a URL whose query carries `response_type=code`, `client_id` equal to `Client.ClientID`, `redirect_uri` equal to `Client.RedirectURI`, `state` equal to `Session.State`, `code_challenge_method=S256`, and a `code_challenge` equal to `Challenge(Session.CodeVerifier)`.
- R-J6CM-RBAE: `Challenge` MUST return `E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM` for RFC 7636 Appendix B's published verifier `dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk`.
- R-J7KJ-5313: For a PRNG-seeded sample of verifiers, `Challenge` MUST return a value containing no `=` padding and composed only of characters in `[A-Za-z0-9\-_]`.
- R-J8SF-IURS: `AuthorizeURL` MUST include a `scope` parameter equal to `Client.Scope` when that field is non-empty, and MUST omit the `scope` parameter entirely when it is empty.
- R-JA0B-WMIH: `AuthorizeURL` MUST render a space inside any parameter value as `+` and a literal plus sign as `%2B`, per RFC 6749 §4.1.1 and Appendix B, verified with a multi-valued `Client.Scope`.
- R-JB88-AE96: `AuthorizeURL` MUST retain every parameter already present in `Client.AuthURL`'s query component and append its own parameters alongside them, per RFC 6749 §3.1.
- R-JCG4-O5ZV: `AuthorizeURL` MUST append the supplied extra parameters after the required set in caller order, and MUST emit a key supplied twice by the caller as two separate query parameters rather than collapsing it to one.
- R-JDO1-1XQK: `ReservedAuthorizeParam` MUST return true for exactly `response_type`, `client_id`, `redirect_uri`, `state`, `code_challenge`, `code_challenge_method`, and `scope`, and false for every other key.
