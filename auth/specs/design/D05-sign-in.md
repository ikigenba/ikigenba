# D05-sign-in

The browser sign-in flow, its Google/OIDC client, and the terms every page
auth draws is described in. A visitor lands on `/`, which is the sign-in page when
there is no live session and the profile when there is. `/login/google` starts
a Google sign-in; `/login/google/callback` finishes it; `POST /logout` ends a
session. All of this attaches its observable HTTP behavior to
`internal/server`'s router (D03 owns the router itself), and it speaks to
Google through `internal/google`, whose exported surface this design declares.

`internal/google` is a small client this sub-project constructs once from configuration. The
client does two things: it builds the authorization redirect URL that sends the
browser to Google, and it exchanges an authorization code plus its PKCE verifier
for a verified set of ID-token claims. Construction touches no network: it takes
the configuration, records the issuer, and returns — it cannot fail, so it
surfaces no error. The client learns Google's OAuth 2.0 / OIDC endpoints and JWKS
by discovering them from the issuer's OpenID configuration on demand, the first
time a sign-in needs them, not at startup; a discovery that fails is not
remembered, so the next sign-in tries again. Because discovery is deferred, auth
starts and serves without reaching Google, and only a sign-in depends on Google
being reachable — so both `AuthCodeURL` (starting a sign-in) and `Exchange`
(finishing one) can fail when Google cannot be reached, and each surfaces that as
an error. The `redirect_uri` is not fixed at construction — auth has no startup
config for its space host, and OAuth needs the same `redirect_uri` at
authorization and exchange — so the server works it out per request and
threads it into both `AuthCodeURL` and `Exchange`. On a host it comes from the
`Host`. In a sandbox it comes from `IKIGENBA_CALLBACK_URL` (D03), carried in
`server.Config.CallbackURL`: Google accepts a sign-in redirect to
`http://localhost:<port>` but never to a name under `localhost`, so the
sandbox asks Google to send the browser to bare `localhost`, and the sandbox's
nginx bounces that request, path and query unchanged, to auth's own name. The
callback that reaches auth therefore carries auth's own `Host`, yet the
exchange must name the address the code was issued for, so with `CallbackURL`
set the `redirect_uri` is that origin plus `/login/google/callback` at both
ends, whatever the `Host`. Because the client is
constructed with an issuer taken from `Process.OIDCIssuer`, a loopback fake
Google can stand in for tests while production points at Google itself. Every
Google fact stated as a requirement below is proven by the evidence gathered for
this design (a live fetch of Google's discovery document plus Google's published
OpenID-Connect docs); no requirement asserts Google bytes beyond what that
evidence documents. The authorization URL fixes `prompt=select_account`, the
value Google's OpenID Connect documentation ("Authentication URI parameters",
`prompt`) defines as asking the user to select an account, so every sign-in
shows Google's account chooser rather than silently reusing the browser's
account; no other `prompt` value is sent. Deferring discovery is auth's own
timing decision, not a claim about Google's bytes, so it needs no new
observation.

The session is carried by a cookie named `ikigenba_session`. auth learns its own
place in the world from each request's `Host`: the *space* is that host with a
leading `auth.` label removed, and from the space auth derives the callback
`redirect_uri`, the cookie `Domain`, its own origin (for the token routes'
`Origin` check, D07), which origins are *on this space* (for the logout
`Origin` check), and which return URLs count as being under the space. Every
request is on a space: auth is reached only through nginx, on a host's space
or in a sandbox, never by a browser directly, so there is no other form of
request to tell apart. In a sandbox the space still comes from the `Host`
(`auth.wip.localhost:7400` gives the space `wip.localhost:7400`, its cookie
domain and apex `wip.localhost`), and two of those derivations take the
sandbox's scheme and port from configuration instead of assuming `https`
with no port: with `server.Config.PublicURL` set (from `IKIGENBA_PUBLIC_URL`,
D03), auth's own origin is exactly that value, and the origins on this space
are those with its scheme and port. The callback `redirect_uri` follows
`CallbackURL` as above. With both fields empty, as on a host, every
derivation is the `Host`-based one, unchanged. Users
are keyed by the verified ID
token's `(issuer, subject)`; the email is a copy refreshed on every login; auth's
own `X-User-Id` is a fresh opaque id minted by `idcodec.NewID`, never Google's
`sub`.

A return URL is under the space when it is an absolute `http` or `https` URL
whose host is the space or a subdomain of it, written so plainly that every
URL parser reads the same host from it; anything else is outside the space.
The test is made on the URL's text, not on what some parser makes of it,
because the host auth checks must be the host the browser goes to when it
follows the redirect. The browser reads the URL by the WHATWG URL Standard's
basic URL parser, which first removes leading and trailing C0 controls and
spaces and every tab and newline ("Remove any leading and trailing C0 control
or space from input", "Remove all ASCII tab or newline from input"), reads a
`\` as a `/` in an `http` or `https` URL, skips any run of `/` and `\` after
the scheme (its "special authority ignore slashes state", so `https:host` and
`https:///host` both name `host`), and takes everything before an `@` in the
authority as userinfo; its host parser then percent-decodes the host and
maps it through UTS46 IDNA processing (§3.3) before using it. Go's `net/url`,
by contrast, "generally follows RFC 3986" (its package documentation), in
which `\` delimits nothing and the authority is what follows `//` up to the
next `/`, `?`, or `#` (RFC 3986 §3.2), so `https:///host` has an empty one. So a return URL is outside the space when it holds a `\`, a control
character, or a space anywhere, when its scheme is not followed by exactly
`//`, or when its authority holds anything but a host of ASCII letters,
digits, `-`, and `.` and an optional port of digits — which rules out `@`
userinfo, `%` escapes, bracketed literals, and non-ASCII hosts at once. Both
sides are compared ASCII case-insensitively and without a port. The one rule serves
the sign-in page, which names an in-space destination, and the callback,
which honors one. The login start records whatever return URL it is given,
unvalidated; only the callback decides whether to honor it.

Sign-out is the one state-changing route any app on the space may drive: an
app renders a form that POSTs to auth's `/logout`, so its `Origin` is the
app's, not auth's. On a host, where `PublicURL` is empty, the logout check accepts `https://<space>` and
`https://<prefix>.<space>` for any non-empty prefix, with no port. These are
the forms a sign-out is accepted from, and they are where the session cookie goes: it carries
`Domain=` the *cookie domain*, the space with any `:port` removed (a
cookie `Domain` is a host name and never carries a port, RFC 6265 §4.1.1
and §5.3), and `Secure`, and a `Domain` cookie matches a host only when
the host equals the domain or ends in `.` plus the domain (RFC 6265 §5.1.3),
which is why `evil<space>` and `<space>.evil.com` are refused; a host with a
leading or trailing `.` (such as `<space>.`) or a `..` is refused too. The accepted set is narrower than every origin the cookie
reaches: the cookie also reaches a space host on a non-default `https` port,
which is refused. A browser serializes an origin as
scheme, `://`, lowercase host, and a port only when it is not the scheme's
default (RFC 6454 §6.2; the WHATWG URL standard's origin serialization), so a
value carrying `/`, `?`, `#`, or `@` after the host is no origin at all, and
it sends `Origin` on every `POST` (Fetch standard, "append a request `Origin`
header"); a request with no `Origin`, the opaque origin `null`, or more than
one `Origin` field cannot show that it came from this space and is refused.
The token routes keep the narrower own-origin check.

In a sandbox the same rule is kept, with the scheme and port taken from
`PublicURL` instead of `https` and none: a sandbox serves every app at
`http://<app>.<name>.localhost:<port>`, so a sign-out from another app's
banner carries that app's origin, with `http` and the sandbox's port, and its
host is under the cookie domain. An origin with another scheme (the `https`
a host would use), another port (another sandbox's), or no port is refused,
as is one whose host is not under the cookie domain, such as the bare
`http://localhost:<port>` the callback bounces through. The port is compared
as written, with no default-port folding: a browser omits a default port when
it serializes an origin, and D03 refuses a `PublicURL` that writes one (`:80`
for `http`, `:443` for `https`) at start, so the port `PublicURL` writes is
the one a browser's `Origin` carries. A host with a leading or trailing `.` or
a `..` is refused here as on a host.

The session cookie stays `Secure` in a sandbox too, though the sandbox speaks
plain HTTP. The W3C Secure Contexts specification (§3.1, "Is origin
potentially trustworthy?") counts a host of `localhost` or one ending in
`.localhost` as potentially trustworthy, and Chrome and Firefox treat such an
`http` origin as a secure context, keeping a `Secure` cookie it sets and
sending it back to names under it. So the cookie requirements need no
sandbox variant: the attributes are the same, and the cookie domain already
drops the port (`Domain=wip.localhost`). What a browser does with the cookie
is the browser's, and no requirement here asserts it.

## Pages

auth's pages adopt the platform's visual design, defined in the repository's
`design/`. A page is designed by the template it executes and the data it
receives; its words, its markup, its link targets and form targets live in that
template, never in this design. Every page auth draws is a template of auth's
template set (D01), all of them human-authored files under `assets/`, and every
page but D09's approve page is drawn by the template `page`, which draws the
sign-in card alone for a visitor who is not signed in and otherwise the banner,
one main and the footer through `chrome`. Its data, the **auth page data**,
carries the banner and exactly one of four parts: the sign-in data, the profile
data, D07's create data for a rejected submission, or D07's created data. A
test proves a page by executing `page` with the data this design states and
comparing the bytes with the answer, never by looking for a word or a tag.
Every page auth draws is an **auth page**; auth's plain-text failures and its
redirects are not pages.

The shared files a page links, the stylesheet, the fonts, the launcher's and the
button feedback scripts, and the favicon, are appkit's, served by auth under
`/_appkit/` through `page.Static()`; the asset-serving design (D08) owns that,
and the rule that no page refers to another origin. A browser that asks for
`/favicon.ico` gets auth's ordinary 404. What a script does in a browser is
appkit's and needs a JavaScript engine the gates do not have.

Every value auth writes into a page from outside itself, the `Host`, a return
URL, an email Google returned, `WORKSPACE_DOMAIN`, anything from the store, is
escaped, so it adds no tag. The **apex** is the last two labels of the
request's `Host` with its port dropped: `auth.sbx.ikigenba.dev` gives
`ikigenba.dev`.

The sign-in page at `/`, the cancelled page and the non-member page share one
card, the template `signIn`, and one shape of data, the **sign-in data**: the
apex; which refusal, if any, the page answers (`cancelled` when Google did not
grant access, `not_member` when the account is not a verified member of the
workspace, empty on the sign-in page); the return URL percent-encoded, with
every byte outside RFC 3986's unreserved set written as `%` and two uppercase
hex digits (RFC 3986 §2.1, §2.3); the destination's display host when the
return URL is in the space; the request's `Host` exactly as sent; the
workspace; and, on the non-member page, the email Google returned. These pages
have no banner. The template chooses every word from that data and writes the
login link itself, carrying the encoded return URL to the login start as its
`return` query value, so what the design fixes of the link is the data it is
built from and that the login start, given exactly that value, records the
return URL the sign-in page was asked for. An out-of-space return URL is
carried on all the same and discarded by the callback. Both the sign-in page
and the login start decode `return` as a form-encoded query value (WHATWG URL
Standard, `application/x-www-form-urlencoded` parsing), with three exceptions
that are auth's own choice, not WHATWG's: a pair holding a `;` is ignored, a
pair with a malformed `%` escape is ignored, and bytes that are not valid UTF-8
are kept as they are rather than replaced by U+FFFD, so they re-encode to the
same escapes. The percent-encoding holds no `;`, no `+` and no malformed
escape, so the login start decodes it back to the same bytes.

The login start's own answer is a redirect, not a page: auth answers it with
the standard library's `http.Redirect`, whose documentation says that when no
`Content-Type` has been set it sets `text/html; charset=utf-8` and writes a
small HTML body; that body is the standard library's, and auth writes no
markup of its own there.

A page for a signed-in user is drawn with the banner, which is not auth's
markup: it is the platform's, the same for every app, shipped by appkit as the
template named `banner` in the set `page.Templates()` returns. auth decides what
goes into it and where it goes; appkit decides how it is drawn. The data comes
from the `Banner` function in `server.Config` (D03): in the running binary the
`Banner` method of the kit `main` made with `page.New("auth", v)`, `v` being the
display string `main` reads (D01), which adds the service's name, the display
string `--version` prints, and the launcher's services and the service's own
icon, read afresh from the host's services file on every call; in a test, a
closure returning whatever the case needs. auth calls it once per page it draws
with the banner, with the **banner user**: the signed-in user's email, `/` as
the profile URL, and `/logout` as the sign-out target, both relative, because
auth is its own profile and its own sign-out, on its own origin. auth never
calls it for anything else, so a page for a visitor who is not signed in never
has a banner or a launcher and never causes a read of the services file; its
auth page data carries the zero banner.

appkit's design fixes what the banner and the footer show (its
D03-page-templates). appkit's `footer` template, given the same data, writes the
page's footer. So auth states only that the page carries the text the `banner`
template writes for the data the call returned, the **appkit banner**, and after
it the text the `footer` template writes for that same data, the **appkit
footer**. A test builds the server with its own `Banner` closure and compares
the page with `page` executed with the auth page data holding the banner its
closure returned. The profile, D07's token-created page, D07's rejected-create
page, and D09's approve page are drawn with the banner.

The profile at `/` is drawn from the **profile data**: the apex, the user's
email, the workspace, the user's personal tokens as D07's token rows, an empty
create form (D07's empty create data), and D07's MCP clients data.

The identity headers `X-User-Id`/`X-User-Email` and credential parsing are
D06's. Server construction and Google-config validation are D03's, and so is the
rule that a request auth handles, a 5xx included, writes nothing to stderr: the
request is in the trail instead. The two `502`s here, Google unreachable at the
start of a sign-in and a failed exchange at its end, are handled failures whose
`request.finished` event carries the `502`. auth's failures that are not pages,
the unknown-state `400`, the `502`s, and the `403` sign-out refusals, stay one
line of plain text with no banner, because the visitor may not be signed in. The
routing of *other* apps through `/check` or `/check/open`, the public `/check`
and `/check/open` 404s, and the nginx-side redirect that carries `?return` are
properties of the space produced by opsctl, a separate sub-project, and are out
of scope here; what this design owns is that auth answers its own host's `/`
with the sign-in page, and the data that page is drawn from.

## What the flow records

Every request auth serves is in the trail through the `request.started` and
`request.finished` events appkit's middleware records for it (D03). The
sign-in flow adds its own domain events, metadata only, each under the
request's id. A sign-in that completes records `user.signed_in` under the
signed-in user's id, and the first sign-in of an account records
`user.created` just before it, under the same new id. A sign-out auth accepts
records `user.signed_out` under the id of the user whose live session it
ended, or under no user when the cookie named no live session. A callback that
signs no one in records `sign_in.refused` under no user, with one attribute,
`reason`, taken from the answer auth gives: `unknown_state` for the `400` (the
`state` matched no login state), `cancelled` for the cancelled page (the
visitor declined at Google), `not_member` for the non-member `403`, and
`provider_failed` for the `502` (Google failed the exchange or could not be
reached). Nothing else on these routes records a domain event: not the pages
at `/`, not the start of a sign-in, whether it redirects or answers `502`, not
a refused sign-out, and not a request answered `500` because auth's database
failed, even one whose user had already been provisioned before the failure.

The middleware's context names the request id but no user, because auth's own
pages never pass through `/check` and nginx sends them no `X-User-Id`. So the
flow emits each event with the request's context carrying the acting user —
`identity.NewContext` with the request's caller and its user id set — which
keeps the request id. No event carries an email, a session id, a Google
`code`, a `state` value, or a return URL; the users are named only by auth's
opaque user id, in the envelope.

These requirements, and D06's and D07's, are observed the same way: a test
builds the server with a telemetry writer whose sink is a `telemetry.Capture`,
sends its request, flushes the writer, and reads the events in the order they
were recorded.

## REQUIREMENTS

- R-I82C-VOP7: `internal/google` MUST export `type Claims struct { Issuer string; Subject string; Email string; EmailVerified bool; HostedDomain string }`, carrying the verified ID token's `iss`, `sub`, `email`, `email_verified`, and `hd` claims (`HostedDomain` empty when the `hd` claim is absent).
- R-KUGP-2ZZD: `internal/google` MUST export `func NewClient(clientID, clientSecret, workspaceDomain, issuer string) *Client` returning a `*Client`, the exported type through which this project reaches Google; `NewClient` MUST NOT return an error.
- R-KVOL-GRQ2: `*Client` MUST export `func (*Client) AuthCodeURL(state, verifier, redirectURI string) (string, error)`, returning the Google authorization redirect URL for the given login state, PKCE code verifier, and per-request `redirectURI`, or a non-nil error when it cannot be built.
- R-FX2G-ZLVJ: `*Client` MUST export `func (*Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (Claims, error)`, exchanging an authorization code, its PKCE verifier, and the per-request `redirectURI`, and returning verified ID-token `Claims` or an error.
- R-ICXY-ERNZ: `internal/server` MUST export `const SessionCookieName = "ikigenba_session"`, the name of the session cookie; other designs reference this name rather than re-declaring it.
- R-KWWH-UJGR: `NewClient` MUST build the client from `GOOGLE_CLIENT_ID` (as `clientID`), `GOOGLE_CLIENT_SECRET` (as `clientSecret`), `WORKSPACE_DOMAIN` (as `workspaceDomain`), and `Process.OIDCIssuer` (as `issuer`) without performing any I/O, and MUST NOT contact the `issuer` at construction; the client MUST discover the Google OAuth 2.0 / OIDC endpoints and JWKS from that `issuer`'s OpenID configuration on demand when a sign-in needs them (in `AuthCodeURL` and `Exchange`), so that a loopback fake standing in as `Process.OIDCIssuer` fully replaces Google in tests; a discovery that fails MUST NOT be remembered — a later sign-in retries it; the `redirect_uri` MUST NOT be fixed at construction — it is derived per request and passed to `AuthCodeURL` and `Exchange`.
- R-KZCA-M2Y5: When the client cannot discover the `issuer`'s endpoints (the `issuer` is unreachable or its OpenID configuration cannot be fetched), `AuthCodeURL` MUST return a non-nil error and an empty URL string.
- R-HAV6-ITBJ: `AuthCodeURL` MUST return a URL addressed to the `authorization_endpoint` discovered from the client's issuer (for Google's issuer this is `https://accounts.google.com/o/oauth2/v2/auth`), whose query carries the OAuth client id (from `GOOGLE_CLIENT_ID`), `hd` set to `WORKSPACE_DOMAIN`, exactly one `prompt` parameter, whose value is `select_account`, the `redirect_uri`, the given `state`, a `code_challenge` that is the S256 hash of the given `verifier`, and `code_challenge_method=S256`.
- R-TXGZ-HJQ1: `Exchange` MUST POST the code, PKCE `verifier`, and the given `redirectURI` to the `token_endpoint` discovered from the client's issuer (for Google's issuer this is `https://oauth2.googleapis.com/token`), MUST send to that endpoint the same `redirect_uri` value it was given (matching the one used at authorization), MUST verify the returned ID token's RS256 signature against the discovered JWKS, MUST accept an issuer claim of either `accounts.google.com` or `https://accounts.google.com`, and MUST return `Claims` populated from the verified token; a failed exchange, unreachable endpoint, or failed verification MUST return a non-nil error.
- R-J1MC-TVZ3: On a successful sign-in the response MUST set the `SessionCookieName` cookie to the created session's opaque id with attributes `Domain=<cookie domain>` (R-9Y8U-AAQ2), `Path=/`, `Secure`, `HttpOnly`, and `SameSite=Lax`, so that a cookie jar following RFC 6265 given the response for the request's own `https` URL, port included, stores the cookie and returns it for `https` URLs whose host is the cookie domain or ends with `.` followed by it.
- R-J2U9-7NPS: On logout the response MUST clear the `SessionCookieName` cookie with an empty value, `Max-Age=0`, and the same domain, path, and security attributes as the login cookie (`Domain=<cookie domain>` (R-9Y8U-AAQ2), `Path=/`, `Secure`, `HttpOnly`, `SameSite=Lax`), so that a cookie jar following RFC 6265 that holds the login cookie, given the response for the request's own `https` URL, no longer returns it.
- R-ILH9-35UU: auth MUST derive the *space* for a request as the request's `Host` with a single leading `auth.` label removed.
- R-9Y8U-AAQ2: auth's design defines the **cookie domain** of a request as its space (R-ILH9-35UU) with a trailing `:` followed by one or more ASCII digits removed, and as the space itself when it has no such suffix, so that `Host: auth.green.example:8443`, `Host: auth.green.example:443`, and `Host: auth.green.example` all give `green.example`.
- R-T8R1-7SBK: When the `CallbackURL` field of the `server.Config` passed to `server.New` (D03) is empty, the callback `redirect_uri` auth sends to Google MUST be `https://auth.<space>/login/google/callback`.
- R-T9YX-LK29: When the `CallbackURL` field of the `server.Config` passed to `server.New` (D03) is non-empty, the callback `redirect_uri` auth sends to Google, both in the `Location` it answers `GET /login/google` with and in the code exchange it makes for `GET /login/google/callback`, MUST be that `CallbackURL` followed by `/login/google/callback`, whatever `Host` the request carries.
- R-TCEQ-D3JN: When the `PublicURL` field of the `server.Config` passed to `server.New` (D03) is empty, auth's own origin (used for the token routes' `Origin` check, D07) MUST be `https://auth.<space>`.
- R-TDMM-QVAC: When the `PublicURL` field of the `server.Config` passed to `server.New` (D03) is non-empty, auth's own origin (used for the token routes' `Origin` check, D07) MUST be exactly that `PublicURL`, whatever `Host` the request carries.
- R-V33R-60ZQ: `GET /` with a live session MUST respond `200` with `Content-Type: text/html; charset=utf-8`; it MUST resolve the identity via `LookupSessionIdentity` (no touch), MUST ignore any `?return`, and MUST NOT change any state.
- R-TB6T-ZBSY: `GET /login/google` MUST mint a PKCE verifier from `Process.Rand`, take the callback `redirect_uri` for the request (R-T8R1-7SBK, R-T9YX-LK29), record a login state via `CreateLoginState` carrying that verifier and any return URL carried from the sign-in page, and obtain the authorization redirect URL via `AuthCodeURL(state, verifier, redirectURI)` for the recorded login state's `State` and that `redirect_uri` as `redirectURI`; when `AuthCodeURL` returns a nil error it MUST respond `302` whose `Location` is that URL, so that the `state` value in `Location` names that login state.
- R-136Q-UF9F: When `AuthCodeURL` returns a non-nil error during `GET /login/google` (the `issuer`'s endpoints could not be discovered), auth MUST respond `502` with `Content-Type: text/plain; charset=utf-8` and a single line of body, MUST create no user, session, or cookie, and MUST leave no login state recorded — removing via `ConsumeLoginState` any login state it created for the request — except that, when that `ConsumeLoginState` call fails, the login state it was to remove MAY remain (D03, R-GDOY-JEGC).
- R-IV8G-5BSE: `GET /login/google/callback` whose `state` matches no recorded login state, including a request with no `state`, MUST respond `400` with `Content-Type: text/plain; charset=utf-8` and a single line of body, and MUST create no user, session, or cookie.
- R-V4BN-JSQF: `GET /login/google/callback` carrying `error=access_denied` MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body — the **cancelled page** —, MUST send no `Set-Cookie`, MUST consume the login state named by `state` if one exists, and MUST create no user or session, unless a store operation it makes to read or consume that login state fails, in which case it MUST answer `500` as D03 answers a store operation that fails; this `error=access_denied` response takes precedence over the unknown/missing-state `400` case (R-IV8G-5BSE), so when `error=access_denied` is present and no such store operation fails the response is `200` whether or not `state` matches a recorded login state.
- R-IXO8-WV9S: `GET /login/google/callback` with a matched login state and a member result MUST exchange the code and verifier via `Exchange`, call `UpsertUserOnLogin`, call `CreateSession`, set the session cookie, consume the login state via `ConsumeLoginState`, and respond `302`.
- R-IYW5-AN0H: On a member sign-in the `issuer`, `subject`, and `email` passed to `UpsertUserOnLogin` MUST be the verified ID token's `Claims.Issuer`, `Claims.Subject`, and `Claims.Email`, so that users are keyed by the verified `(issuer, subject)` and the stored email is refreshed to the token's value on every login.
- R-U14O-MUY4: A callback result MUST be treated as a member if and only if the verified `Claims.HostedDomain` equals `WORKSPACE_DOMAIN` and `Claims.EmailVerified` is true (an absent `hd` claim, i.e. empty `HostedDomain`, is not a member); a non-member result MUST respond `403` with `Content-Type: text/html; charset=utf-8`, send no `Set-Cookie`, consume the login state, and create or change no user, session, or cookie.
- R-TA5C-93RJ: When a matched-state callback's token exchange fails or Google is unreachable, auth MUST respond `502` with `Content-Type: text/plain; charset=utf-8` and a single line of body, and MUST create no user, session, or cookie.
- R-GQZR-C3MP: When the `PublicURL` field of the `server.Config` passed to `server.New` (D03) is empty, an `Origin` value MUST be treated as *on this space* if and only if it has the serialized shape `https://` followed by a host `H` and nothing else, where `H` contains none of `/`, `?`, `#`, `@`, or `:` (so the value carries no path, query, fragment, userinfo, or port), neither begins nor ends with `.`, and contains no `..`, and `H`, compared with the space ASCII case-insensitively, either equals the space or ends in `.` followed by the space with a non-empty prefix before that `.`; the scheme `https` MUST be matched ASCII case-insensitively, and every other value — including an `http://` origin naming the space's hosts, any value carrying a port (`:443` included), a host with a leading or trailing `.` or a `..`, a host that merely ends in the space without a separating `.` (`evil<space>`), a host that only contains the space (`<space>.evil.com`), and `null` — MUST be treated as not on this space; so that with `Host: auth.green.example.` neither `https://auth.green.example.` nor `https://green.example.` is on this space, and with `Host: auth.green.example` `https://.app.green.example` is not.
- R-GS7N-PVDE: When the `PublicURL` field of the `server.Config` passed to `server.New` (D03) is non-empty, an `Origin` value MUST be treated as *on this space* if and only if it is exactly the concatenation of `PublicURL`'s scheme, matched ASCII case-insensitively, then `://`, then a non-empty host `H` that contains none of `/`, `?`, `#`, `@`, or `:`, neither begins nor ends with `.`, and contains no `..`, then `PublicURL`'s port part — `:` followed by its port exactly as `PublicURL` writes it when it has a port, and nothing when it has none — where `H`, compared with the request's cookie domain (R-9Y8U-AAQ2) ASCII case-insensitively, either equals it or ends in `.` followed by it with a non-empty prefix before that `.`; every other value — including one with another scheme, another port, no port when `PublicURL` has one, a port when it has none, a path, query, fragment, or userinfo, a host with a leading or trailing `.` or a `..`, `evil<cookie domain>`, `<cookie domain>.evil.com`, and `null` — MUST be treated as not on this space; so that with `PublicURL` `http://auth.green.example:7400` and `Host: auth.green.example.:7400`, `http://auth.green.example.:7400` is not on this space, and with `PublicURL` `http://auth.wip.localhost:7400` and `Host: auth.wip.localhost:7400`, neither `http://.app.wip.localhost:7400` nor `http://app.wip.localhost:07400` is.
- R-GTFK-3N43: `POST /logout` carrying exactly one `Origin` header field whose value is on this space (R-GQZR-C3MP, R-GS7N-PVDE) MUST respond `302` with `Location: /`, clear the session cookie, delete the session server-side via `DeleteSession`, and leave the user row and the user's tokens untouched; the response MUST be the same whichever on-this-space origin the request carries, auth's own origin (R-TCEQ-D3JN, R-TDMM-QVAC) among them whenever it is on this space.
- R-GUNG-HEUS: `POST /logout` carrying no `Origin` header field, more than one `Origin` header field, or one whose value is not on this space (R-GQZR-C3MP, R-GS7N-PVDE) MUST respond `403` with `Content-Type: text/plain; charset=utf-8` and a single line of body, send no `Set-Cookie`, not call `DeleteSession`, and leave the session untouched; this `Origin` check is the second line of cross-site defense after the cookie's `SameSite=Lax`.
- R-T5YJ-3Y97: auth's design defines that auth **records** an event when a `Writer.Emit`, `Writer.Ready`, or `Writer.Shutdown` call forms that `Event` on auth's writer — `telemetry` being the package `github.com/ikigenba/ikigenba/appkit/telemetry` — whether the writer delivers it to its sink or writes it out as an undelivered-event line, where auth's writer is the `*telemetry.Writer` the `Telemetry` field of the `server.Config` passed to `server.New` (D03) carries, and, for the events `Run` forms itself (`service.started` and `service.stopping`), the one writer `Run` builds, which is the writer `Run` passes as that field (R-T2AT-YN14); a test at the level of `server.New` observes the events auth records by building that writer itself with a `*telemetry.Capture` as its sink and reading `Capture.Events` after `Writer.Flush` returns, in the order `Capture.Events` returns them, and a test at the level of `Run` observes them through the sink it passes as `Process.Sink` (D01) and, for an event not delivered, through the undelivered-event line on `Process.Stderr`; an event's **envelope request id** and **envelope user** are its `RequestID` and `User` fields, and its **attributes** are its `Attrs`; every requirement of auth's design that says auth records an event, or names an event's envelope request id, envelope user, or attributes, MUST denote that.
- R-T76F-HPZW: auth's design defines that auth records an event **for a request** when auth records it (R-T5YJ-3Y97) and the `Writer.Emit` call that forms it is made while that request is being served; every requirement of auth's design that says auth records an event for a request, or records an event of or for a named request, MUST denote that.
- R-T6HN-3SJG: auth's design defines the **request id** of a request auth serves as the request's first `X-Request-Id` value when the request carries one that is not empty, and otherwise as the envelope request id of the `request.started` event auth records for that request; every requirement of auth's design that names a request's request id MUST denote that.
- R-TBD8-MVI8: For each `GET /login/google/callback` auth answers `302` having set the session cookie (R-IXO8-WV9S) and for which `UpsertUserOnLogin` (D04) created the user, auth MUST record exactly one event named `user.created`, before that request's `user.signed_in` event (R-TDT1-EEZM), with the same envelope request id and envelope user as that `user.signed_in` event and no attributes; for such a request whose `(issuer, subject)` pair already had a user, auth MUST record no `user.created` event.
- R-TDT1-EEZM: For each `GET /login/google/callback` auth answers `302` having set the session cookie (R-IXO8-WV9S), auth MUST record exactly one event named `user.signed_in`, whose envelope request id is the request's request id, whose envelope user is the `ID` of the `User` that `UpsertUserOnLogin` (D04) returned for the request — the owner of the session the cookie names — and which has no attributes.
- R-TF0X-S6QB: For each `POST /logout` auth answers `302` (R-GTFK-3N43), auth MUST record exactly one event named `user.signed_out`, whose envelope request id is the request's request id, which has no attributes, and whose envelope user is the `UserID` of the `Identity` that `LookupSessionIdentity` (D04) returns, at the time the server's clock reads while handling the request, for the session the request's `SessionCookieName` cookie names when that session is live, and empty when the request carries no such cookie or the cookie names no live session.
- R-V5JJ-XKH4: For each `GET /login/google/callback` auth answers `400` (R-IV8G-5BSE), `200` with the cancelled page (R-V4BN-JSQF), `403` (R-U14O-MUY4), or `502` (R-TA5C-93RJ), auth MUST record exactly one event named `sign_in.refused`, whose envelope request id is the request's request id, whose envelope user is empty, and whose attributes are exactly the one key `reason` with the `string` value `unknown_state`, `cancelled`, `not_member`, or `provider_failed` respectively.
- R-V6RG-BC7T: For a request whose path is `/`, `/login/google`, `/login/google/callback`, or `/logout`, auth MUST record no event other than its `request.started` and `request.finished` events and the events R-TBD8-MVI8, R-TDT1-EEZM, R-TF0X-S6QB, and R-V5JJ-XKH4 require of it; so a request auth answers `500` (D03), a `GET /` or `GET /login/google` whatever its answer, and a `POST /logout` answered `403` record no other event.
- R-TIOM-XHYE: No event auth records — its envelope request id, envelope user, and every attribute value — MUST contain, other than where it coincides with a value a requirement of auth's design states for that field, the value of any cookie the request carries, a session id auth minted, a token secret, a user's email, an ID token's email, a token's `Name`, an authorization `code` or login `state`, a return URL, or the text after the first `?` of the request's URL or of its `X-Original-URI` header.
- R-V7ZC-P3YI: When auth serves its own host on a space, `GET https://auth.<space>/` with no live session MUST respond `200` with `Content-Type: text/html; charset=utf-8` and the sign-in page (R-VCUY-86XA) as its body.
- R-3GUK-F9PN: auth's design defines an **auth page** as a response body that a requirement of auth's design states is an auth page or is drawn with the banner; every requirement of auth's design that names an auth page MUST denote that.
- R-VAF5-GNFW: Every value auth writes into an auth page that it takes from the request, from Google, from `WORKSPACE_DOMAIN`, or from the store MUST contribute no `<` and no `>` character to the page's bytes.
- R-3I2G-T1GC: auth's design defines the **apex name** of a request as the result of taking the request's `Host` as sent, removing a trailing `:` followed by one or more ASCII digits, and keeping the last two of the remaining text's `.`-separated labels joined by `.`, or the whole remaining text when it has fewer than two labels, so that `auth.sbx.ikigenba.dev` gives `ikigenba.dev`; every requirement of auth's design that names the apex name of a request MUST denote that.
- R-4ZBA-OGOE: auth's design defines the **banner user** for a user as the `page.User` value, where `page` is the package `github.com/ikigenba/ikigenba/appkit/page`, whose `Email` is that user's email — the `Email` of the `store.Identity` (D04) the request's session resolves to — whose `ProfileURL` is `/`, and whose `LogoutURL` is `/logout`; every requirement of auth's design that names the banner user for a user MUST denote that.
- R-50J7-28F3: auth's design defines the **appkit banner** of a response as the text written by executing the template named `banner` in a set that `page.Templates()` returns, with its data the `page.Banner` value that the `Banner` field of the `server.Config` passed to `server.New` (D03) returned from the call the server made to it while answering that response's request; every requirement of auth's design that names the appkit banner MUST denote that.
- R-51R3-G05S: auth's design defines the **appkit footer** of a response as the text written by executing the template named `footer` in a set that `page.Templates()` returns, with its data the same `page.Banner` value the response's appkit banner is written from; every requirement of auth's design that names the appkit footer MUST denote that.
- R-VBN1-UF6L: auth's design defines an auth page **drawn with the banner** for a user as a response body answering a request during which the server called the `Banner` field of its `server.Config` with the banner user for that user as the argument, and whose bytes hold that call's appkit banner and, after it, that call's appkit footer; every page a requirement of auth's design states is drawn with the banner for a user MUST be so.
- R-PT80-EZ08: While answering a request whose response body a requirement of auth's design states is drawn with the banner for a user, the server MUST call the `Banner` field of its `server.Config` exactly once, with the banner user for that user as the argument; while answering any other request it MUST NOT call it, so that the sign-in page, the cancelled page, the non-member page, a plain-text failure, a redirect, `/check`, and a request under `page.StaticPrefix` never cause a read of the services file.
- R-ED79-D60K: auth's design defines the **percent-encoding** of a value as the value's bytes (a text's UTF-8 encoding, or the byte sequence itself when it is not valid UTF-8) with every byte other than an ASCII letter, an ASCII digit, `-`, `.`, `_`, or `~` written as `%` followed by that byte's two uppercase hexadecimal digits; every requirement of auth's design that names the percent-encoding of a value MUST denote that.
- R-VCUY-86XA: `GET /` with no live session MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body that is an auth page — the **sign-in page** — and MUST change no state, persisting neither the request's return URL nor anything else.
- R-PNB9-4VJK: The **return URL** of a request for the sign-in page MUST be the value of the first `&`-separated pair of the request's query whose name, decoded the same way as the value, is exactly `return`, the name and value being decoded as the WHATWG URL Standard's `application/x-www-form-urlencoded` parser decodes them (`+` as a space, and `%` followed by two hexadecimal digits as the byte they denote), except that a pair that holds a `;`, or whose name or value holds a `%` not followed by two hexadecimal digits, MUST be ignored as though it were absent, and except that the decoded value MUST be the byte sequence the value denotes, kept as it is when it is not valid UTF-8 (no byte is replaced by U+FFFD), so that `?return=%FF` yields the single byte `0xFF`, whose percent-encoding is `%FF`; a request with no such pair, or whose first such pair's value decodes to the empty string, MUST be treated as carrying no return URL.
- R-PPR1-WF0Y: `GET /login/google` MUST record, as the return URL of the login state it creates via `CreateLoginState`, the value of its own first `return` query parameter found and decoded by the rule R-PNB9-4VJK states for the sign-in page, unvalidated — including when that value is out-of-space or cannot be parsed as a URL — and MUST record no return URL when it has no such parameter or that parameter decodes to the empty string.
- R-QI40-VGBM: The **display host** of an in-space return URL MUST be the host subcomponent of its authority (RFC 3986 §3.2.2) exactly as the URL writes it, followed, when the authority has a non-empty port subcomponent (§3.2.3), by `:` and that port exactly as written; any userinfo subcomponent (§3.2.1) MUST NOT be part of it.
- R-N2LW-9AQJ: A return URL MUST be treated as **in-space** if and only if all of the following hold: (a) it contains no `\` and no byte from 0x00 through 0x20 or equal to 0x7F (no ASCII control character and no space); (b) it begins with `http://` or `https://`, the scheme matched ASCII case-insensitively; (c) its **authority** — the text after that `//` up to, but not including, the first `/`, `?`, or `#` after it, or to its end when there is none — consists of a non-empty **host** made only of ASCII letters, ASCII digits, `-`, and `.`, optionally followed by `:` and zero or more ASCII digits, so that an authority holding `@`, `%`, `[`, or any byte that is not ASCII is never in-space; and (d) the space's host name — the space (R-ILH9-35UU) with a trailing `:` followed by one or more ASCII digits removed — is non-empty, and the host, compared with it ASCII case-insensitively, either equals it or ends with `.` followed by it. Every other return URL, including one that is not a URL at all, MUST be treated as **out-of-space**.
- R-N3TS-N2H8: The `302` `Location` of a successful member sign-in MUST be the login state's carried return URL when that URL is in-space (R-N2LW-9AQJ), and MUST be `/` when there is no return URL or the carried return URL is out-of-space.
- R-VK6C-ITDG: The `200` body R-V33R-60ZQ requires of `GET /` with a live session — the **profile** — MUST be an auth page drawn with the banner for the user `LookupSessionIdentity` resolves for that session.
- R-6ZCB-90OL: The `internal/server` package MUST declare the unexported `type authPageData struct { Banner page.Banner; SignIn *signInPageData; Profile *profilePageData; Create *tokenCreateData; Created *tokenCreatedData }`, with exactly these fields in this order, where `page` is the package `github.com/ikigenba/ikigenba/appkit/page`; it is the **auth page data**.
- R-70K7-MSFA: The `internal/server` package MUST declare the unexported `type signInPageData struct { Apex, Refused, Return, Destination, Host, Workspace, Email string }`, with exactly these fields in this order.
- R-71S4-0K5Z: The `internal/server` package MUST declare the unexported `type profilePageData struct { Apex, Email, Workspace string; Rows []tokenRowData; Create tokenCreateData; Clients mcpClientsData }`, with exactly these fields in this order.
- R-3JAD-6T71: auth's design defines the **sign-in data** of a request, for a refusal `f`, which is the empty string, `cancelled` or `not_member`, and an email `m`, as the `signInPageData` whose `Apex` is the request's apex name (R-3I2G-T1GC); whose `Refused` is `f`; whose `Return` is, when `f` is empty and the request carries a return URL (R-PNB9-4VJK), the percent-encoding (R-ED79-D60K) of that return URL, and otherwise the empty string; whose `Destination` is, when `f` is empty and the request carries an in-space return URL (R-N2LW-9AQJ), that return URL's display host (R-QI40-VGBM), and otherwise the empty string; whose `Host` is the request's `Host` as sent, port included; whose `Workspace` is the value of `WORKSPACE_DOMAIN`; and whose `Email` is `m`; every requirement of auth's design that names the sign-in data of a request MUST denote that value.
- R-3MY2-C4F4: The sign-in page (R-VCUY-86XA) MUST be exactly the text that executing the template `page` of auth's template set (D01) writes with the `authPageData` whose `SignIn` points to the request's sign-in data (R-3JAD-6T71) for the empty refusal and the empty email, and whose other fields are their zero values, the zero `page.Banner` included.
- R-3O5Y-PW5T: The cancelled page (R-V4BN-JSQF) MUST be an auth page, and MUST be exactly the text that executing the template `page` of auth's template set (D01) writes with the `authPageData` whose `SignIn` points to the request's sign-in data (R-3JAD-6T71) for the refusal `cancelled` and the empty email, and whose other fields are their zero values, the zero `page.Banner` included.
- R-3PDV-3NWI: The `403` body R-U14O-MUY4 requires for a non-member result — the **non-member page** — MUST be an auth page, and MUST be exactly the text that executing the template `page` of auth's template set (D01) writes with the `authPageData` whose `SignIn` points to the request's sign-in data (R-3JAD-6T71) for the refusal `not_member` and the email that is the verified ID token's `Claims.Email`, and whose other fields are their zero values, the zero `page.Banner` included.
- R-3LQ5-YCOF: auth's design defines the **profile data** of a `GET /` answered with the profile (R-VK6C-ITDG) as the `profilePageData` whose `Apex` is the request's apex name (R-3I2G-T1GC); whose `Email` is the `Email` of the `store.Identity` that `LookupSessionIdentity` (D04) resolves for the request's session; whose `Workspace` is the value of `WORKSPACE_DOMAIN`; whose `Rows` holds exactly one element for each token of `Kind` `store.TokenPersonal` among the tokens `ListTokens` (D04) returns for that identity's user, the token row (D07) of that token for the profile's draw time (D07), the element of a token `a` lying before that of a token `b` whenever `a` precedes `b` in profile order (D07); whose `Create` is the empty create data (D07); and whose `Clients` is the profile's MCP clients data (D07); every requirement of auth's design that names the profile data MUST denote that value.
- R-3RTN-V7DW: The profile (R-VK6C-ITDG) MUST be exactly the text that executing the template `page` of auth's template set (D01) writes with the `authPageData` whose `Banner` is the `page.Banner` that the server's one call of the `Banner` field of its `server.Config` while answering the request (R-PT80-EZ08) returned, whose `Profile` points to the request's profile data (R-3LQ5-YCOF), and whose `SignIn`, `Create` and `Created` are nil.
- R-3QLR-HFN7: For every request for the sign-in page whose sign-in data (R-3JAD-6T71) for the empty refusal and the empty email has a non-empty `Return` `x`, a `GET /login/google` whose URL query is exactly `return=` followed by `x` MUST record, as the return URL of the login state it creates via `CreateLoginState` (R-PPR1-WF0Y), exactly the return URL that request for the sign-in page carries, byte for byte, whatever bytes it holds; and a `GET /login/google` whose URL has no query MUST record no return URL.
- R-7BJB-2Q3J: When `AuthCodeURL` returns a nil error during `GET /login/google` (R-TB6T-ZBSY), with the URL `u`, auth's answer MUST have the status, the `Location` and `Content-Type` header values, and the body that the standard library's `http.Redirect` writes when called with a `ResponseWriter` whose header map is empty, a request with the same method and URL, `u`, and `http.StatusFound`; so that the body is the standard library's and holds no markup of auth's own.
