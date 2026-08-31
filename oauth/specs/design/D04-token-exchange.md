# D04-token-exchange

The exchange trades the authorization code the callback delivered (D06) for the
token endpoint's response. It is the last protocol step and the one that
produces the program's entire output.

```go
package oauth

// MaxErrorBody bounds how much of a non-2xx token response body appears in the
// returned error.
const MaxErrorBody = 4096

// Exchange POSTs the authorization code to the token endpoint and returns the
// response body verbatim. The HTTP client is a parameter rather than a Client
// field: it is injected at the composition root (D01), and a nil-defaulting
// field would leave two code paths where one will do.
func (c Client) Exchange(ctx context.Context, hc *http.Client, s Session,
    code string, extra, headers []Param) ([]byte, error)

// ReservedTokenParam reports whether key is one the token request writes
// itself. It is the single authority on that set; options asks it rather than
// carrying a second copy (D09).
func ReservedTokenParam(key string) bool
```

**The request.** A `POST` to `Client.TokenURL` with a
`application/x-www-form-urlencoded` body carrying
`grant_type=authorization_code`, `code`, `code_verifier` (the session's, D02),
`redirect_uri`, and `client_id`. The `redirect_uri` is sent again at exchange
because RFC 6749 requires the authorization server to verify it matches the one
from the authorize request; D11 owns the guarantee that the two are byte-equal.

**Client authentication has two mutually exclusive forms.** `client_secret` is
included in the body **only when non-empty**, and omitted entirely otherwise —
a public client sends no secret at all, not an empty one. The alternative is an
`Authorization` header supplied through `--token-header` (D08). RFC 6749 §2.3.1
requires an authorization server to support HTTP Basic for clients issued a
password, making Basic the one method a conforming server is guaranteed to
accept; the same section says a server "MAY support including the client
credentials in the request-body" but that doing so is "NOT RECOMMENDED and
SHOULD be limited to clients unable to directly utilize the HTTP Basic
authentication scheme." §2.3 then states plainly that "the client MUST NOT use
more than one authentication method in each request." That last sentence is why
a body secret and an `Authorization` header are refused together — the
rejection itself is a flag-boundary concern (D09); the protocol fact is
established here. A header escape hatch has to exist at all precisely because
body-form credentials are the optional method and Basic is the mandatory one.

**Headers.** The request always sets `Content-Type:
application/x-www-form-urlencoded`, since the body is that format regardless of
what else the caller sends. Caller-supplied headers are added alongside it —
that is the mechanism by which Basic authentication, and any other header a
provider demands, reaches the request without this tool knowing what a provider
is.

**Extra parameters** behave exactly as they do on the authorize request (D03):
appended after the required set in caller order, with a repeated key sent
repeatedly rather than collapsed. Reserved keys — the ones this request writes
itself — are owned here and exported as `ReservedTokenParam`, so the flag
boundary asks rather than restating the list.

**Verbatim passthrough is the product's core promise.** On a 2xx, `Exchange`
returns the response body as the bytes that arrived, and D11 writes exactly
those bytes to stdout. Nothing is parsed, re-encoded, trimmed, reordered, or
enriched. This is deliberate and load-bearing: the consumer that motivated this
tool needs a value that lives inside a JWT claim in the response, and extracting
it is provider-specific knowledge. A tool that parsed the response would have to
know whose response it was, and that is the one thing this design refuses to
know. Returning bytes keeps the tool provider-neutral and makes the redirected
file a faithful record of what the service actually said.

**Failure carries the provider's own words.** A non-2xx response is the most
common real failure, and the reason is almost always in the body — an
`error_description` the provider wrote for exactly this moment. So the error
names the HTTP status and quotes the body rather than discarding it. The quote
is bounded by `MaxErrorBody` (4096 bytes) because a misconfigured endpoint can
return a full HTML error page, and a diagnostic that dumps a megabyte of markup
into a terminal buries the line the user needed. The bound is a named contract
constant rather than a literal at the truncation site, so the value is visible
to a reader of this package's surface.

**Cancellation is honored.** The context governs the request, so the callback
deadline (D11) reaches in-flight network I/O rather than only guarding the wait
that preceded it. A cancelled exchange returns an error wrapping the context's,
so `errors.Is` reaches `context.Canceled` and `context.DeadlineExceeded`.

## REQUIREMENTS

- R-JIJM-L0PC: `Exchange` MUST issue an HTTP `POST` to `Client.TokenURL` whose form-encoded body carries `grant_type=authorization_code`, `code` equal to the supplied code, `code_verifier` equal to `Session.CodeVerifier`, `redirect_uri` equal to `Client.RedirectURI`, and `client_id` equal to `Client.ClientID`.
- R-JJRI-YSG1: `Exchange` MUST include `client_secret` in the request body when `Client.ClientSecret` is non-empty, and MUST omit the `client_secret` field entirely when it is empty.
- R-JKZF-CK6Q: `Exchange` MUST set the request's `Content-Type` header to `application/x-www-form-urlencoded`.
- R-JM7B-QBXF: `Exchange` MUST add every caller-supplied header to the request, verified with a header whose key is `Authorization`, alongside the `Content-Type` the request sets itself.
- R-JNF8-43O4: `Exchange` MUST append the supplied extra parameters after the required body fields in caller order, and MUST emit a key supplied twice by the caller as two separate body parameters rather than collapsing it to one.
- R-JON4-HVET: `ReservedTokenParam` MUST return true for exactly `grant_type`, `code`, `code_verifier`, `redirect_uri`, `client_id`, and `client_secret`, and false for every other key.
- R-JPV0-VN5I: On a 2xx response, `Exchange` MUST return the response body byte-for-byte as received, verified with a body that is not valid JSON and one whose bytes would change under re-encoding.
- R-JR2X-9EW7: On a non-2xx response, `Exchange` MUST return an error naming the HTTP status and quoting the response body, and MUST NOT return the body as a success value.
- R-JSAT-N6MW: On a non-2xx response whose body exceeds `MaxErrorBody`, `Exchange` MUST include exactly the first `MaxErrorBody` bytes of that body in the returned error.
- R-AHPF-BMQU: `Exchange` MUST honor the supplied context: when the context is cancelled while the token request is in flight, it MUST return an error for which `errors.Is` reports the context's own error, and MUST NOT return a success value.
