# D05-sign-in

The browser sign-in flow, its Google/OIDC client, and the page vocabulary every
HTML page auth draws. A visitor lands on `/`, which is the sign-in page when
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
evidence documents. Deferring discovery is auth's own timing decision, not a
claim about Google's bytes, so it needs no new observation.

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
the story's forms, and they are where the session cookie goes: it carries
`Domain=` the *cookie domain*, the space with any `:port` removed (a
cookie `Domain` is a host name and never carries a port, RFC 6265 §4.1.1
and §5.3), and `Secure`, and a `Domain` cookie matches a host only when
the host equals the domain or ends in `.` plus the domain (RFC 6265 §5.1.3),
which is why `evil<space>` and `<space>.evil.com` are refused; a host with a
leading or trailing `.` (such as `<space>.`) or a `..` is refused too. The accepted set is narrower than every origin the cookie
reaches: the cookie also reaches a space host on a non-default `https` port,
which is refused, following the story's forms. A browser serializes an origin as
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
`design/`: the stylesheet keys on a handful of elements and classes, and the
pages fix those hooks and their visible text, and nothing else of their markup.
This design owns the vocabulary every page auth draws is described in, and D07
uses it by name without restating it: what an **auth page** is and what every
one carries in its head; the procedures a requirement reads a page by; the two
frames a page is drawn in, the **sign-in card** and a page **drawn with the
banner**; the **banner user**, the **appkit banner**, the **appkit footer**,
and a page's **written markup**; the **card titled** a name, the **alert**,
and the **icon**; and the **apex name** a page shows.

Every auth page is titled `auth`, links the stylesheet at `/_appkit/theme.css`
and declares the phone-width viewport. The stylesheet, the fonts it loads, and
the launcher's script are appkit's shared files, served by auth under
`/_appkit/` through `page.Static()`; the asset-serving design (D08) owns
that, and this design refers to the stylesheet only by its URL. appkit's
`banner` template never links the stylesheet, so
each page links it in its own head.

The rules below govern auth's **written markup**: the page with the appkit
banner and the appkit footer taken out, or the whole page when it has neither,
as a sign-in card never does. The banner, the launcher it may carry, and the
footer are appkit's markup, fixed by
appkit's published design, not auth's; they hold what auth's own rules would
refuse — the launcher's links go to other hosts, its icons are the host's
SVG inserted verbatim, its script loads `/_appkit/launcher.js`, the profile
link holds an icon `svg` that is not a button's — so auth's
rules read around them rather than through them. auth's written markup loads
nothing from any other host: every resource reference it carries — a `src`,
`poster`, `data`, `background`, or `manifest`, or a `link`'s `href` —
is a path under `/_appkit/`, it carries no inline style that could name one, every
resource-naming attribute is written in the one form the reading rules read,
and its only `svg` elements are button icons, whose content is nothing but
paths. The attribute scan looks only at attribute-name positions: it skips
every double-quoted run, so a submitted value that happens to spell `src=`
is not mistaken for an attribute, and it skips the content of `script`
elements. A page carries no script unless a requirement places one there;
what a placed script does is left to the design that places it. Event-handler
attributes, `srcdoc`, and `http-equiv` are forbidden outright, the last so no
`meta` refresh can navigate or fetch. Link and form
targets are paths on auth's own host too, so no `javascript:` or other-host
URL can sit in one. An `href` sits only on an `a`, where it is a navigation,
and on a `link`, where it is a resource reference; no other element carries
one, and no element carries `xlink:href`, because on SVG's `image`, `use`,
`feImage`, and `script` either attribute is a fetch. `srcset`, `imagesrcset`,
and `ping` are forbidden outright as well: each holds a list of URLs, and a
rule on how a value begins would check only the first. A path on auth's own host is not enough for a resource
reference, since auth's own `/login/google` answers with a redirect to Google:
a page that loaded it as an image or stylesheet would make the browser fetch
from Google. Only the shared files auth answers through appkit (D08) may be
loaded, so a resource reference begins with `/_appkit/` and holds no `..` segment,
counting the percent-encoded forms the WHATWG URL parser treats as one
("A double-dot URL path segment is a URL path segment that is ".." or an
ASCII case-insensitive match for ".%2e", "%2e.", or "%2e%2e"") and splitting
at `\` as well as `/`, which that parser does in an `http` or `https` URL; a
navigation — a link, a form, a form button — is the visitor's own step and
keeps the looser rule. Attribute values are always double-quoted and no `'`
appears between attributes, so the simple reading rules and a browser's
tokenizer agree — for every page these rules admit, a browser reads the same
tags and attributes the reading rules find. That agreement needs the tokenizer
to stay in its data state wherever the rules scan. The elements that switch it
out (HTML Living Standard §13.2.6.2, "Parsing elements that contain only
text", and the "in head" and "in body" insertion modes, §13.2.6.4.4 and
§13.2.6.4.7) are `title` and `textarea` (RCDATA); `style`, `xmp`, `iframe`,
`noembed`, `noframes`, and `noscript` when scripting is on (RAWTEXT); `script`
(script data); and `plaintext` (PLAINTEXT). `style` and all of those but
`title` and `script` are forbidden outright, wherever they would sit. A
start tag for `title`, `script`, `style`, `noframes`, or `template` inside the
body is reprocessed with the "in head" rules (§13.2.6.4.7) and switches the
tokenizer just as it would in the head, so the pins hold across the whole
page, not just the head: there is exactly one `title`, before the `body`, and
it is exactly `auth`, so it holds no `<` at all; every `script` tag is
pinned; and `style`, `noframes`, and `template` are forbidden everywhere. A placed script closes at its first `</script`, which
must be exactly `</script>`, and holds no `<!--`, so its content never
reaches the escaped script states. `template` and `math` are forbidden too,
and the only foreign content is an icon's `svg`, which holds nothing but
`path` tags, so no HTML breakout or CDATA section can arise there. The rules
also exclude, outside
script content, every other use of `<`: no comment, bogus comment, processing
instruction, or end tag carrying anything but its name (html/template strips
comments from template text and escapes a `<` in any value it writes). The
tokenizer's before-attribute-name,
attribute-name, after-attribute-name, before-attribute-value, the three
attribute-value, after-attribute-value (quoted), and self-closing start tag
states (HTML Living Standard §13.2.5.32–§13.2.5.40) read an attribute written
that way, and separated by whitespace, `/`, or a closing `"`, as exactly that
attribute. An attribute counts only where it starts outside every quoted run,
every `=` follows a non-empty attribute name, and a `<` inside a quoted run
starts no tag,
so a submitted value that spells an attribute is never mistaken for one, and a
tag ends at its first `>` outside a quoted run, for start tags and tag spans
alike. An icon's `svg` and `path` carry only the attributes the icon
definition lists, and the `svg` holds nothing but its listed `path` tags and
whitespace, so no painted `url()` reference or embedded element can make a
request from inside it; `attributionsrc` is forbidden with the other attributes that
make a request.

Every page begins with `<!DOCTYPE html>`. HTML Living Standard §13.1.1
("The DOCTYPE") fixes that form, matched case-insensitively, and without it
the tree builder's initial insertion mode (§13.2.6.4.1) puts the document in
quirks mode, where class selectors match case-insensitively (§4.16.2,
"Case-sensitivity of selectors"). Standards mode keeps the
stylesheet's hooks exact. The doctype is not a tag span, since its `<` is
followed by `!`, not a letter.

auth's module may depend on only three external modules, none of them an HTML
parser, so a test reads a page with the standard library. The reading rules
are therefore stated as procedures: what a start tag and an end tag are, how an
attribute is read, what an element's content is, and how text is normalised
into the **visible text** a reader sees. A requirement over text says a
content **reads** a string when its normalisation is exactly that string, and
says it **contains** a string when a page has more around it. The element
content rule takes the first end tag of the same name, so it reads correctly
only elements that do not nest inside one of their own kind, which holds for
every element these requirements name. Every value auth writes into a page
from outside itself — the `Host`, a return URL, an email Google returned,
`WORKSPACE_DOMAIN`, anything from the store — is escaped, so it adds no tag
and reads back as itself.

A page for a visitor who is not signed in is drawn as a sign-in card: no
banner, one `main` of class `auth-page` holding one card, which begins
with the bare `ikigenba` mark and is headed `Sign in to <apex>`, and whose one
way forward is a link styled as the Google button. The sign-in page at `/`, the
page a cancelled Google sign-in returns to, and the non-member page are all
sign-in cards. The apex is the last two labels of the request's `Host` with
its port dropped: `auth.sbx.ikigenba.dev` gives `ikigenba.dev`.

The sign-in page tells the visitor where they are signing in. With no return
URL, or one outside the space, it names the workspace in the card and auth's
own host, exactly as the request's `Host` carries it, in the card's footer.
With an in-space return URL it names the destination's host (and port, as the
URL writes them) instead, and the workspace sentence moves to the footer. An
out-of-space return URL is never named, but the link still carries it to the
login start, where the callback discards it. The link carries a return URL
percent-encoded with every byte outside RFC 3986's unreserved set written as
`%` and two uppercase hex digits (RFC 3986 §2.1, §2.3). Both the sign-in page
and the login start decode `return` as a form-encoded query value (WHATWG URL
Standard, `application/x-www-form-urlencoded` parsing), with three exceptions
that are auth's own choice, not WHATWG's: a pair holding a `;` is ignored, a
pair with a malformed `%` escape is ignored, and bytes that are not valid
UTF-8 are kept as they are rather than replaced by U+FFFD, so they re-encode
to the same escapes in the link. The sign-in cards fix their whole visible
text, in the order S3 lists it, with one space between the pieces.

A page for a signed-in user is drawn with the banner, which is no longer
auth's markup: it is the platform's, the same for every app, shipped by appkit
as the template named `banner` in the set `page.Templates()` returns. auth
decides what goes into it and where it goes; appkit decides how it is drawn.
The data comes from the `Banner` function in `server.Config` (D03): in the
running binary the `Banner` method of the kit `main` made with
`page.New("auth", version.Version)` (D01), which adds the service's name,
auth's release version, and the launcher's services, read afresh from the
host's services file on every call; in a test,
a closure returning whatever the case needs. auth calls it once per page it
draws with the banner, with the **banner user**: the signed-in user's email,
`/` as the profile URL, and `/logout` as the sign-out target — both relative,
because auth is its own profile and its own sign-out, on its own origin. auth
never calls it for anything else, so a sign-in card never has a banner or a
launcher and never causes a read of the services file.

appkit's design fixes what the banner then shows (its D03-page-templates):
the `ikigenba` mark naming the service, which is not a link; the profile icon,
a link to the profile URL with no text, labelled `Profile` and titled with the
email, escaped; a sign-out form POSTing to the sign-out target, whose button
reads `Sign out` after its `logout` icon; and, exactly when the services are
not empty, the launcher button labelled `Services` (it opens the banner row, as
the mark's immediately preceding sibling, which auth states again as its own
requirement, since the stories fix that order and an appkit whose banner
put the button elsewhere must not satisfy auth's design), the list of services
with its `Find a service` field, one entry per service in order, the hidden
no-match line, and a deferred `script` loading `/_appkit/launcher.js`. With no
services none of those appear. appkit's `footer` template, given the same
data, writes the page's footer: the service's name and the version, separated
by one space. So auth states only that the page carries, at the top of its body,
exactly the text the `banner` template writes for the data the call returned
— the **appkit banner** — and, at the end of its body, exactly the text the
`footer` template writes for that same data — the **appkit footer** — and
that everything between them is one `main`. A test builds the server with its
own `Banner` closure, renders the expected banner and footer itself with
`page.Templates()`, and compares; it finds the email a page was drawn for
from the page's own data (the store, or the `Account` card), never from the
banner's markup. The profile,
D07's token-created page, and D07's rejected-create page are drawn with the
banner. An icon auth draws itself is inline and `aria-hidden` (WAI-ARIA 1.2,
`aria-hidden`: the element is excluded from the accessibility tree), so a
button's accessible name is its word alone.

The profile at `/` is headed `Your account` over the subtitle
`You're signed in to <apex>.`, and holds three cards: `Account`, then
`API tokens`, then `Create a token`. This design fixes the frame, the heading,
the `Account` card, and where the other two cards sit; D07 fixes the
`API tokens` and `Create a token` cards themselves, the per-token rows and
their forms, and D07's own pages. The cancelled and non-member sign-in pages
show their message in an alert — a `strong` title over a `p`, which is how the
stylesheet lays an alert out — marked `warn` with `role="status"` for the
cancelled sign-in and `err` with `role="alert"` for the refused one.

The identity headers `X-User-Id`/`X-User-Email` and bearer parsing are
D06's. Server construction and Google-config validation are D03's, and so is
the one line auth writes to its diagnostic stream for a request it answers
with a 5xx: `auth: request <id>: <reason>`, naming the request by its
`X-Request-Id`. The two `502`s here — Google unreachable at the start of a
sign-in, and a failed exchange at its end — are the sign-in flow's own trouble,
and each writes that line with Google's error as the reason. auth's failures
that are not pages — the unknown-state `400`, the `502`s, and the `403` sign-out
refusals — stay one line of plain text, neither a sign-in card nor a page
drawn with the banner, because the visitor may not be signed in. S7's routing
of *other* apps through `/check`, the public `/check` 404, and the nginx-side
redirect that carries `?return` are properties of the space produced by
opsctl, a separate sub-project, and are out
of scope here; the S7 facts this design owns are that auth answers its own
host's `/` with the sign-in page, and what that page carries.

## REQUIREMENTS

- R-I82C-VOP7: `internal/google` MUST export `type Claims struct { Issuer string; Subject string; Email string; EmailVerified bool; HostedDomain string }`, carrying the verified ID token's `iss`, `sub`, `email`, `email_verified`, and `hd` claims (`HostedDomain` empty when the `hd` claim is absent).
- R-KUGP-2ZZD: `internal/google` MUST export `func NewClient(clientID, clientSecret, workspaceDomain, issuer string) *Client` returning a `*Client`, the exported type through which this project reaches Google; `NewClient` MUST NOT return an error.
- R-KVOL-GRQ2: `*Client` MUST export `func (*Client) AuthCodeURL(state, verifier, redirectURI string) (string, error)`, returning the Google authorization redirect URL for the given login state, PKCE code verifier, and per-request `redirectURI`, or a non-nil error when it cannot be built.
- R-FX2G-ZLVJ: `*Client` MUST export `func (*Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (Claims, error)`, exchanging an authorization code, its PKCE verifier, and the per-request `redirectURI`, and returning verified ID-token `Claims` or an error.
- R-ICXY-ERNZ: `internal/server` MUST export `const SessionCookieName = "ikigenba_session"`, the name of the session cookie; other designs reference this name rather than re-declaring it.
- R-KWWH-UJGR: `NewClient` MUST build the client from `GOOGLE_CLIENT_ID` (as `clientID`), `GOOGLE_CLIENT_SECRET` (as `clientSecret`), `WORKSPACE_DOMAIN` (as `workspaceDomain`), and `Process.OIDCIssuer` (as `issuer`) without performing any I/O, and MUST NOT contact the `issuer` at construction; the client MUST discover the Google OAuth 2.0 / OIDC endpoints and JWKS from that `issuer`'s OpenID configuration on demand when a sign-in needs them (in `AuthCodeURL` and `Exchange`), so that a loopback fake standing in as `Process.OIDCIssuer` fully replaces Google in tests; a discovery that fails MUST NOT be remembered — a later sign-in retries it; the `redirect_uri` MUST NOT be fixed at construction — it is derived per request and passed to `AuthCodeURL` and `Exchange`.
- R-KZCA-M2Y5: When the client cannot discover the `issuer`'s endpoints (the `issuer` is unreachable or its OpenID configuration cannot be fetched), `AuthCodeURL` MUST return a non-nil error and an empty URL string.
- R-TTTA-C8HY: `AuthCodeURL` MUST return a URL addressed to the `authorization_endpoint` discovered from the client's issuer (for Google's issuer this is `https://accounts.google.com/o/oauth2/v2/auth`), whose query carries the OAuth client id (from `GOOGLE_CLIENT_ID`), `hd` set to `WORKSPACE_DOMAIN`, the `redirect_uri`, the given `state`, a `code_challenge` that is the S256 hash of the given `verifier`, and `code_challenge_method=S256`.
- R-TXGZ-HJQ1: `Exchange` MUST POST the code, PKCE `verifier`, and the given `redirectURI` to the `token_endpoint` discovered from the client's issuer (for Google's issuer this is `https://oauth2.googleapis.com/token`), MUST send to that endpoint the same `redirect_uri` value it was given (matching the one used at authorization), MUST verify the returned ID token's RS256 signature against the discovered JWKS, MUST accept an issuer claim of either `accounts.google.com` or `https://accounts.google.com`, and MUST return `Claims` populated from the verified token; a failed exchange, unreachable endpoint, or failed verification MUST return a non-nil error.
- R-J1MC-TVZ3: On a successful sign-in the response MUST set the `SessionCookieName` cookie to the created session's opaque id with attributes `Domain=<cookie domain>` (R-9Y8U-AAQ2), `Path=/`, `Secure`, `HttpOnly`, and `SameSite=Lax`, so that a cookie jar following RFC 6265 given the response for the request's own `https` URL, port included, stores the cookie and returns it for `https` URLs whose host is the cookie domain or ends with `.` followed by it.
- R-J2U9-7NPS: On logout the response MUST clear the `SessionCookieName` cookie with an empty value, `Max-Age=0`, and the same domain, path, and security attributes as the login cookie (`Domain=<cookie domain>` (R-9Y8U-AAQ2), `Path=/`, `Secure`, `HttpOnly`, `SameSite=Lax`), so that a cookie jar following RFC 6265 that holds the login cookie, given the response for the request's own `https` URL, no longer returns it.
- R-ILH9-35UU: auth MUST derive the *space* for a request as the request's `Host` with a single leading `auth.` label removed.
- R-9Y8U-AAQ2: auth's design defines the **cookie domain** of a request as its space (R-ILH9-35UU) with a trailing `:` followed by one or more ASCII digits removed, and as the space itself when it has no such suffix, so that `Host: auth.green.example:8443`, `Host: auth.green.example:443`, and `Host: auth.green.example` all give `green.example`.
- R-T8R1-7SBK: When the `CallbackURL` field of the `server.Config` passed to `server.New` (D03) is empty, the callback `redirect_uri` auth sends to Google MUST be `https://auth.<space>/login/google/callback`.
- R-T9YX-LK29: When the `CallbackURL` field of the `server.Config` passed to `server.New` (D03) is non-empty, the callback `redirect_uri` auth sends to Google, both in the `Location` it answers `GET /login/google` with and in the code exchange it makes for `GET /login/google/callback`, MUST be that `CallbackURL` followed by `/login/google/callback`, whatever `Host` the request carries.
- R-TCEQ-D3JN: When the `PublicURL` field of the `server.Config` passed to `server.New` (D03) is empty, auth's own origin (used for the token routes' `Origin` check, D07) MUST be `https://auth.<space>`.
- R-TDMM-QVAC: When the `PublicURL` field of the `server.Config` passed to `server.New` (D03) is non-empty, auth's own origin (used for the token routes' `Origin` check, D07) MUST be exactly that `PublicURL`, whatever `Host` the request carries.
- R-LAND-R94N: `GET /` with a live session MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body containing a form that POSTs to `/logout` and a form that POSTs to `/tokens` with fields `name` and `expires`; it MUST resolve the identity via `LookupSessionIdentity` (no touch), MUST ignore any `?return`, and MUST NOT change any state.
- R-TB6T-ZBSY: `GET /login/google` MUST mint a PKCE verifier from `Process.Rand`, take the callback `redirect_uri` for the request (R-T8R1-7SBK, R-T9YX-LK29), record a login state via `CreateLoginState` carrying that verifier and any return URL carried from the sign-in page, and obtain the authorization redirect URL via `AuthCodeURL(state, verifier, redirectURI)` for the recorded login state's `State` and that `redirect_uri` as `redirectURI`; when `AuthCodeURL` returns a nil error it MUST respond `302` whose `Location` is that URL, so that the `state` value in `Location` names that login state.
- R-XXPJ-ZJU1: When `AuthCodeURL` returns a non-nil error during `GET /login/google` (the `issuer`'s endpoints could not be discovered), auth MUST respond `502` with `Content-Type: text/plain; charset=utf-8` and a single line of body, MUST write the line R-XV9R-80CN states with that error as its reason, MUST create no user, session, or cookie, and MUST leave no login state recorded — removing via `ConsumeLoginState` any login state it created for the request.
- R-IV8G-5BSE: `GET /login/google/callback` whose `state` matches no recorded login state, including a request with no `state`, MUST respond `400` with `Content-Type: text/plain; charset=utf-8` and a single line of body, and MUST create no user, session, or cookie.
- R-Y05C-R3BF: `GET /login/google/callback` carrying `error=access_denied` MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body containing a link whose target is `/login/google`, MUST send no `Set-Cookie`, MUST consume the login state named by `state` if one exists, and MUST create no user or session, unless a store operation it makes to read or consume that login state fails, in which case it MUST answer as R-CCQE-EHNR states and write the line R-XV9R-80CN states; this `error=access_denied` response takes precedence over the unknown/missing-state `400` case (R-IV8G-5BSE), so when `error=access_denied` is present and no such store operation fails the response is `200` whether or not `state` matches a recorded login state.
- R-IXO8-WV9S: `GET /login/google/callback` with a matched login state and a member result MUST exchange the code and verifier via `Exchange`, call `UpsertUserOnLogin`, call `CreateSession`, set the session cookie, consume the login state via `ConsumeLoginState`, and respond `302`.
- R-IYW5-AN0H: On a member sign-in the `issuer`, `subject`, and `email` passed to `UpsertUserOnLogin` MUST be the verified ID token's `Claims.Issuer`, `Claims.Subject`, and `Claims.Email`, so that users are keyed by the verified `(issuer, subject)` and the stored email is refreshed to the token's value on every login.
- R-U14O-MUY4: A callback result MUST be treated as a member if and only if the verified `Claims.HostedDomain` equals `WORKSPACE_DOMAIN` and `Claims.EmailVerified` is true (an absent `hd` claim, i.e. empty `HostedDomain`, is not a member); a non-member result MUST respond `403` with `Content-Type: text/html; charset=utf-8`, send no `Set-Cookie`, consume the login state, and create or change no user, session, or cookie.
- R-XYXG-DBKQ: When a matched-state callback's token exchange fails or Google is unreachable, auth MUST respond `502` with `Content-Type: text/plain; charset=utf-8` and a single line of body, MUST write the line R-XV9R-80CN states with the `Exchange` error as its reason, and MUST create no user, session, or cookie.
- R-GQZR-C3MP: When the `PublicURL` field of the `server.Config` passed to `server.New` (D03) is empty, an `Origin` value MUST be treated as *on this space* if and only if it has the serialized shape `https://` followed by a host `H` and nothing else, where `H` contains none of `/`, `?`, `#`, `@`, or `:` (so the value carries no path, query, fragment, userinfo, or port), neither begins nor ends with `.`, and contains no `..`, and `H`, compared with the space ASCII case-insensitively, either equals the space or ends in `.` followed by the space with a non-empty prefix before that `.`; the scheme `https` MUST be matched ASCII case-insensitively, and every other value — including an `http://` origin naming the space's hosts, any value carrying a port (`:443` included), a host with a leading or trailing `.` or a `..`, a host that merely ends in the space without a separating `.` (`evil<space>`), a host that only contains the space (`<space>.evil.com`), and `null` — MUST be treated as not on this space; so that with `Host: auth.green.example.` neither `https://auth.green.example.` nor `https://green.example.` is on this space, and with `Host: auth.green.example` `https://.app.green.example` is not.
- R-GS7N-PVDE: When the `PublicURL` field of the `server.Config` passed to `server.New` (D03) is non-empty, an `Origin` value MUST be treated as *on this space* if and only if it is exactly the concatenation of `PublicURL`'s scheme, matched ASCII case-insensitively, then `://`, then a non-empty host `H` that contains none of `/`, `?`, `#`, `@`, or `:`, neither begins nor ends with `.`, and contains no `..`, then `PublicURL`'s port part — `:` followed by its port exactly as `PublicURL` writes it when it has a port, and nothing when it has none — where `H`, compared with the request's cookie domain (R-9Y8U-AAQ2) ASCII case-insensitively, either equals it or ends in `.` followed by it with a non-empty prefix before that `.`; every other value — including one with another scheme, another port, no port when `PublicURL` has one, a port when it has none, a path, query, fragment, or userinfo, a host with a leading or trailing `.` or a `..`, `evil<cookie domain>`, `<cookie domain>.evil.com`, and `null` — MUST be treated as not on this space; so that with `PublicURL` `http://auth.green.example:7400` and `Host: auth.green.example.:7400`, `http://auth.green.example.:7400` is not on this space, and with `PublicURL` `http://auth.wip.localhost:7400` and `Host: auth.wip.localhost:7400`, neither `http://.app.wip.localhost:7400` nor `http://app.wip.localhost:07400` is.
- R-GTFK-3N43: `POST /logout` carrying exactly one `Origin` header field whose value is on this space (R-GQZR-C3MP, R-GS7N-PVDE) MUST respond `302` with `Location: /`, clear the session cookie, delete the session server-side via `DeleteSession`, and leave the user row and the user's tokens untouched; the response MUST be the same whichever on-this-space origin the request carries, auth's own origin (R-TCEQ-D3JN, R-TDMM-QVAC) among them whenever it is on this space.
- R-GUNG-HEUS: `POST /logout` carrying no `Origin` header field, more than one `Origin` header field, or one whose value is not on this space (R-GQZR-C3MP, R-GS7N-PVDE) MUST respond `403` with `Content-Type: text/plain; charset=utf-8` and a single line of body, send no `Set-Cookie`, not call `DeleteSession`, and leave the session untouched; this `Origin` check is the second line of cross-site defense after the cookie's `SameSite=Lax`.
- R-TQ5L-6X9V: When auth serves its own host on a space, `GET https://auth.<space>/` with no live session MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body containing a link whose target is `/login/google`.
- R-VWSW-YH7W: auth's design defines, scanning an auth page from left to right, a **tag span** as a `<` that lies outside the content of every `script` element and outside every quoted run of an earlier tag span and is immediately followed by an ASCII letter, running through the first `>` after it that lies outside every **quoted run** of the span (a `"` and the text through the next `"`, the runs taken left to right); a tag span's **name** as the run of ASCII letters, ASCII digits, and `-` after its `<`; a **start tag** for a lowercase element name `N` as a tag span whose name is exactly `N`; and an **end tag** for `N` as the characters `</`, `N`, `>`; every requirement of auth's design that names these MUST denote that.
- R-BSTK-NA80: auth's design defines an **occurrence** of an attribute `A` in a tag span (a start tag included) as `A` starting at a place outside the span's quoted runs, preceded by ASCII whitespace, and immediately followed by `="`; the **read value** of that occurrence as the text of the quoted run that this `"` opens, from after it up to the next `"`, with every character reference decoded as the WHATWG HTML tokenizer decodes one inside an attribute value (HTML Living Standard §13.2.5.77 onward: numeric references, and named references with or without their trailing `;` exactly where the tokenizer decodes them); and a start tag that **carries** `A` **reading** `v` as one holding exactly one occurrence of `A` whose read value is exactly `v`; every requirement of auth's design that names these MUST denote that.
- R-PHJ1-EC2S: auth's design defines the **content** of an element as the text from the `>` that ends its start tag up to the `<` that begins the first end tag of the same name after that start tag; an element **holds** whatever lies in its content; a text **begins with** an element when, after any leading ASCII whitespace, it starts with that element's start tag; and a text **consists of** a sequence of elements when it is exactly those elements, each from its start tag through the end tag that ends its content, in that order, with nothing before, between, or after them but ASCII whitespace; every requirement of auth's design that names these MUST denote that.
- R-PIQX-S3TH: auth's design defines the **normalisation** of a text as the result of removing every `script` element and every `style` element whole (from its start tag through the first end tag of the same name after it), then removing every remaining span from a `<` through the next `>`, then replacing every HTML character reference with the character or characters it denotes, then replacing every run of ASCII whitespace with a single space and removing any leading or trailing space; a text **reads** `s` when its normalisation is exactly `s`; and the **visible text** of an auth page is the normalisation of its `body` element's content; every requirement of auth's design that names these MUST denote that.
- R-08U8-JUAH: auth's design defines an **auth page** as a response body that a requirement of auth's design states is an auth page, is drawn as a sign-in card, or is drawn with the banner; the written markup (R-056J-EJ2E) of every auth page MUST hold exactly one `body` start tag and, after it, exactly one `</body>` end tag.
- R-0A24-XM16: The written markup of every auth page MUST hold exactly one tag span whose name is `title` matched ASCII case-insensitively, anywhere in it, and that tag span MUST lie before the `body` start tag, MUST be a start tag for `title`, and MUST be followed immediately by exactly `auth</title>`.
- R-0BA1-BDRV: Every auth page MUST hold, before its `body` start tag, exactly one `link` start tag carrying `rel` reading `stylesheet`, and that start tag MUST carry `href` reading `/_appkit/theme.css`.
- R-0YG4-L0V2: Every auth page MUST hold, before its `body` start tag, exactly one `meta` start tag carrying `name` reading `viewport`, and that start tag MUST carry `content` reading `width=device-width, initial-scale=1`.
- R-0DPU-2X99: In the written markup of every auth page, the read value of every occurrence (R-BSTK-NA80) of `action` or `formaction` in any tag span, and of every occurrence of `href` in a tag span whose name is `a` matched ASCII case-insensitively, MUST contain no ASCII tab, line feed, or carriage return and MUST either be exactly `/` or begin with `/` followed by a character that is neither `/` nor `\`.
- R-0EXQ-GOZY: In the written markup of every auth page, every tag span whose name, matched ASCII case-insensitively, is neither `a` nor `link` MUST hold no occurrence (R-BSTK-NA80) of `href`, and every tag span MUST hold no occurrence of `xlink:href`, `srcset`, `imagesrcset`, or `ping`.
- R-0G5M-UGQN: In the written markup of every auth page, the read value of every occurrence (R-BSTK-NA80) of `src`, `poster`, `data`, `background`, or `manifest` in any tag span, and of every occurrence of `href` in a tag span whose name is `link` matched ASCII case-insensitively, MUST contain no ASCII tab, line feed, or carriage return, MUST begin with `/_appkit/`, and MUST hold no double-dot segment: when the part of the value before its first `?` or `#` (the whole value when it holds neither) is split at every `/` and every `\`, no piece is `..` or an ASCII case-insensitive match for `.%2e`, `%2e.`, or `%2e%2e`.
- R-0HDJ-88HC: The written markup of every auth page MUST hold no tag span whose name is `style` matched ASCII case-insensitively, and every tag span of it MUST hold no occurrence of the attribute `style`.
- R-0ZO0-YSLR: Every value auth writes into an auth page that it takes from the request, from Google, from `WORKSPACE_DOMAIN`, or from the store MUST contribute no `<` and no `>` character to the page's bytes, and every normalisation or read value that a requirement states in terms of such a value MUST contain that value unchanged apart from the whitespace collapse normalisation performs.
- R-3Q46-Q9HS: auth's design defines the **apex name** of a request as the result of taking the request's `Host` as sent, removing a trailing `:` followed by one or more ASCII digits, and keeping the last two of the remaining text's `.`-separated labels joined by `.`, or the whole remaining text when it has fewer than two labels, so that `auth.sbx.ikigenba.dev` gives `ikigenba.dev`; every apex a page shows MUST be the request's apex name.
- R-FQZJ-J8UK: auth's design defines a `button` element **drawn with** an icon and a word as one whose content begins with an `svg` element whose start tag carries `class` reading `ico`, `aria-hidden` reading `true`, `viewBox` reading `0 0 24 24`, `fill` reading `none`, `stroke` reading `currentColor`, `stroke-width` reading `2`, `stroke-linecap` reading `round`, and `stroke-linejoin` reading `round` and has no attribute name other than those eight; whose `svg` element's content is made of nothing but ASCII whitespace, `</path>` end tags, and the `path` start tags that icon's definition lists, each exactly once and in that order, each carrying `d` reading the listed value and having no attribute name other than `d`, so that it holds no other tag span and no other text; and whose whole content reads that word alone; every requirement of auth's design that says a button is drawn with an icon MUST denote that.
- R-PXDQ-DCPT: auth's design defines an auth page **drawn as a sign-in card** as one that holds no `header` start tag, whose `body` element's content consists of exactly one `main` element whose start tag carries `class` reading `auth-page`, and whose `main` element's content consists of exactly one `section` element — the page's **card** — whose start tag carries `class` reading `card`; every page a requirement of auth's design states is drawn as a sign-in card MUST be so.
- R-PYLM-R4GI: The card of a page drawn as a sign-in card MUST begin with a `span` element whose start tag carries `class` reading `mark` and holds no occurrence of `data-service`, and whose content is exactly `ikigenba`.
- R-PZTJ-4W77: A page drawn as a sign-in card MUST hold exactly one `h1` start tag, inside its card, and that `h1` element's content MUST read `Sign in to <apex>`, where `<apex>` is the request's apex name.
- R-Q11F-INXW: The card of a page drawn as a sign-in card MUST hold exactly one `a` start tag, and that start tag MUST carry `class` reading `button secondary large google`; that `a` element is the page's **card link**.
- R-Q29B-WFOL: The card of a page drawn as a sign-in card MUST hold at most one `footer` start tag, and when it holds one, the card's content MUST end, apart from trailing ASCII whitespace, with that `footer` element's end tag; the card **holds a footer reading** `s` when it holds a `footer` element whose content reads `s`.
- R-4ZBA-OGOE: auth's design defines the **banner user** for a user as the `page.User` value, where `page` is the package `github.com/ikigenba/ikigenba/appkit/page`, whose `Email` is that user's email — the `Email` of the `store.Identity` (D04) the request's session resolves to — whose `ProfileURL` is `/`, and whose `LogoutURL` is `/logout`; every requirement of auth's design that names the banner user for a user MUST denote that.
- R-50J7-28F3: auth's design defines the **appkit banner** of a response as the text written by executing the template named `banner` in a set that `page.Templates()` returns, with its data the `page.Banner` value that the `Banner` field of the `server.Config` passed to `server.New` (D03) returned from the call the server made to it while answering that response's request; every requirement of auth's design that names the appkit banner MUST denote that.
- R-51R3-G05S: auth's design defines the **appkit footer** of a response as the text written by executing the template named `footer` in a set that `page.Templates()` returns, with its data the same `page.Banner` value the response's appkit banner is written from; every requirement of auth's design that names the appkit footer MUST denote that.
- R-056J-EJ2E: auth's design defines the **written markup** of an auth page as the page's bytes with its appkit banner removed when the page holds its appkit banner beginning immediately after the `>` of the page's first `body` start tag (that one occurrence removed), and with its appkit footer removed when the page holds its appkit footer ending immediately before the `<` of the page's last `</body>` end tag (that one occurrence removed), and nothing else removed, and as the page itself when it holds neither; every requirement of auth's design that names the written markup of a page MUST denote that, so that those requirements govern the markup auth writes and not the markup appkit's `banner` and `footer` templates write.
- R-06EF-SAT3: auth's design defines an auth page **drawn with the banner** for a user as a response body answering a request during which the server called the `Banner` field of its `server.Config` with the banner user for that user as the argument, whose bytes hold that call's appkit banner beginning immediately after the `>` of the page's first `body` start tag and that call's appkit footer ending immediately before the `<` of the page's last `</body>` end tag, and whose written markup's `body` element's content consists of exactly one `main` element, which holds everything else on the page; every page a requirement of auth's design states is drawn with the banner for a user MUST be so.
- R-52YZ-TRWH: While answering a request whose response body a requirement of auth's design states is drawn with the banner for a user, the server MUST call the `Banner` field of its `server.Config` exactly once, with the banner user for that user as the argument; while answering any other request it MUST NOT call it, so that a sign-in card, a plain-text failure, a redirect, `/check`, and a request under `page.StaticPrefix` never cause a read of the services file.
- R-2XJU-MVK4: On every auth page drawn with the banner whose appkit banner was written from a `page.Banner` whose `Services` is not empty, the appkit banner MUST hold exactly one `header` start tag, exactly one `button` start tag carrying `class` reading `launcher`, and exactly one `strong` start tag carrying `class` reading `mark`; that `header` element's content MUST begin with that `button` element, and the text between the `</button>` end tag that ends that `button` element's content and that `strong` start tag MUST be only ASCII whitespace.
- R-0USF-FPMZ: auth's design defines a **card titled** `T` as a `section` element whose content begins with a `header` element that holds an `h2` element whose content reads `T`, and a requirement that places a card titled `T` also states the `class` its `section` start tag carries or names the design that states it; every requirement of auth's design that names a card titled `T` MUST denote that.
- R-4TZY-0Z0K: auth's design defines an **alert titled** `T` **reading** `X` as a `div` element whose content consists of a `strong` element whose content reads `T` followed by a `p` element whose content reads `X`, and a requirement that places an alert also states the attributes its `div` start tag carries; every requirement of auth's design that names an alert titled `T` reading `X` MUST denote that.
- R-ED79-D60K: auth's design defines the **percent-encoding** of a value as the value's bytes (a text's UTF-8 encoding, or the byte sequence itself when it is not valid UTF-8) with every byte other than an ASCII letter, an ASCII digit, `-`, `.`, `_`, or `~` written as `%` followed by that byte's two uppercase hexadecimal digits; every requirement of auth's design that names the percent-encoding of a value MUST denote that.
- R-QC0I-YLM5: `GET /` with no live session MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body that is an auth page drawn as a sign-in card — the **sign-in page** — and MUST change no state, persisting neither the request's return URL nor anything else.
- R-PNB9-4VJK: The **return URL** of a request for the sign-in page MUST be the value of the first `&`-separated pair of the request's query whose name, decoded the same way as the value, is exactly `return`, the name and value being decoded as the WHATWG URL Standard's `application/x-www-form-urlencoded` parser decodes them (`+` as a space, and `%` followed by two hexadecimal digits as the byte they denote), except that a pair that holds a `;`, or whose name or value holds a `%` not followed by two hexadecimal digits, MUST be ignored as though it were absent, and except that the decoded value MUST be the byte sequence the value denotes, kept as it is when it is not valid UTF-8 (no byte is replaced by U+FFFD), so that `?return=%FF` yields the single byte `0xFF`, whose percent-encoding is `%FF`; a request with no such pair, or whose first such pair's value decodes to the empty string, MUST be treated as carrying no return URL.
- R-PPR1-WF0Y: `GET /login/google` MUST record, as the return URL of the login state it creates via `CreateLoginState`, the value of its own first `return` query parameter found and decoded by the rule R-PNB9-4VJK states for the sign-in page, unvalidated — including when that value is out-of-space or cannot be parsed as a URL — and MUST record no return URL when it has no such parameter or that parameter decodes to the empty string.
- R-QEGB-Q53J: The sign-in page's card link MUST have content reading `Continue with Google`, and its start tag MUST carry `href` reading `/login/google` when the request carries no return URL, and reading `/login/google?return=` followed by the percent-encoding of the return URL when it carries one, whether that return URL is in-space or out-of-space.
- R-EFN2-4PHY: When the request for the sign-in page carries no return URL or an out-of-space one, the page's visible text MUST be exactly these strings, in this order, each separated from the next by a single space: `ikigenba`, `Sign in to <apex>`, `Access is limited to Google accounts in the <workspace> workspace.`, `Continue with Google`, and `You're signing in at <host>. One sign-in covers every service in this space.`; and the card MUST hold a footer reading that last string; where `<apex>` is the request's apex name, `<workspace>` is the value of `WORKSPACE_DOMAIN`, and `<host>` is the request's `Host` as sent, port included.
- R-EGUY-IH8N: When the request for the sign-in page carries an in-space return URL, the page's visible text MUST be exactly these strings, in this order, each separated from the next by a single space: `ikigenba`, `Sign in to <apex>`, `Sign in to continue to <display>.`, `Continue with Google`, and `Access is limited to Google accounts in the <workspace> workspace.`; and the card MUST hold a footer reading that last string; where `<apex>` is the request's apex name, `<display>` is the return URL's display host, and `<workspace>` is the value of `WORKSPACE_DOMAIN`.
- R-QI40-VGBM: The **display host** of an in-space return URL MUST be the host subcomponent of its authority (RFC 3986 §3.2.2) exactly as the URL writes it, followed, when the authority has a non-empty port subcomponent (§3.2.3), by `:` and that port exactly as written; any userinfo subcomponent (§3.2.1) MUST NOT be part of it.
- R-N2LW-9AQJ: A return URL MUST be treated as **in-space** if and only if all of the following hold: (a) it contains no `\` and no byte from 0x00 through 0x20 or equal to 0x7F (no ASCII control character and no space); (b) it begins with `http://` or `https://`, the scheme matched ASCII case-insensitively; (c) its **authority** — the text after that `//` up to, but not including, the first `/`, `?`, or `#` after it, or to its end when there is none — consists of a non-empty **host** made only of ASCII letters, ASCII digits, `-`, and `.`, optionally followed by `:` and zero or more ASCII digits, so that an authority holding `@`, `%`, `[`, or any byte that is not ASCII is never in-space; and (d) the space's host name — the space (R-ILH9-35UU) with a trailing `:` followed by one or more ASCII digits removed — is non-empty, and the host, compared with it ASCII case-insensitively, either equals it or ends with `.` followed by it. Every other return URL, including one that is not a URL at all, MUST be treated as **out-of-space**.
- R-N3TS-N2H8: The `302` `Location` of a successful member sign-in MUST be the login state's carried return URL when that URL is in-space (R-N2LW-9AQJ), and MUST be `/` when there is no return URL or the carried return URL is out-of-space.
- R-QLRQ-0RJP: The `200` body R-Y05C-R3BF requires of `GET /login/google/callback` carrying `error=access_denied` — the **cancelled page** — MUST be an auth page drawn as a sign-in card whose card holds, after its `h1` element, a `div` element whose start tag carries `class` reading `alert`, `data-kind` reading `warn`, and `role` reading `status`, and which is an alert titled `Sign-in cancelled` reading `Google didn't grant access, so you weren't signed in. You can try again.`
- R-QMZM-EJAE: The cancelled page's card link MUST have content reading `Continue with Google` and its start tag MUST carry `href` reading `/login/google`, and the cancelled page's card MUST hold no `footer` start tag.
- R-QO7I-SB13: The `403` body R-U14O-MUY4 requires for a non-member result — the **non-member page** — MUST be an auth page drawn as a sign-in card whose card holds, after its `h1` element, a `div` element whose start tag carries `class` reading `alert`, `data-kind` reading `err`, and `role` reading `alert`, and which is an alert titled `Workspace membership required` reading `<email> isn't a verified account in the <workspace> workspace. Sign in with your @<workspace> account instead.`, where `<email>` is the verified ID token's `Claims.Email` and `<workspace>` is the value of `WORKSPACE_DOMAIN`.
- R-QPFF-62RS: The non-member page's card link MUST have content reading `Try another account` and its start tag MUST carry `href` reading `/login/google`, and the non-member page's card MUST hold a footer reading `Think you should have access? Ask your <workspace> workspace admin to add you.`, where `<workspace>` is the value of `WORKSPACE_DOMAIN`.
- R-ZZ31-HOCX: The `200` body R-LAND-R94N requires of `GET /` with a live session — the **profile** — MUST be an auth page drawn with the banner for the user `LookupSessionIdentity` resolves for that session.
- R-10VX-CKCG: The profile's `main` element's content MUST begin with an `h1` element whose content reads `Your account`, followed, with nothing between them but ASCII whitespace, by a `p` element whose content reads `You're signed in to <apex>.`, where `<apex>` is the request's apex name.
- R-123T-QC35: The profile's card titled `Account` MUST have a `section` start tag carrying `class` reading `card`, and MUST hold a `dl` element whose start tag carries `class` reading `kv` and whose content consists of exactly six elements, alternately `dt` and `dd`, whose contents read, in order, `Email`, the email of the user the profile is drawn for, `Workspace`, the value of `WORKSPACE_DOMAIN`, `Signed in via`, and `Google`.
- R-13BQ-43TU: The profile's `main` element's content MUST consist of its `h1` element, its `p` element, and exactly three cards, in this order: a card titled `Account`, a card titled `API tokens`, and a card titled `Create a token`; D07 states the `class` the last two cards' `section` start tags carry and everything those two cards hold.
- R-0ILF-M081: In every tag span of the written markup of an auth page, every place outside the span's quoted runs where one of `src`, `poster`, `data`, `background`, `manifest`, `ping`, `href`, `xlink:href`, `action`, `formaction`, `srcset`, `imagesrcset`, or `style`, matched ASCII case-insensitively, is preceded by ASCII whitespace, by `/`, or by the closing `"` of a quoted run, and is followed, after optional ASCII whitespace, by `=`, MUST be an occurrence of that attribute (R-BSTK-NA80): the name in lowercase, preceded by ASCII whitespace and immediately followed by `="`.
- R-0JTB-ZRYQ: In the written markup of every auth page, every `<` followed by `svg`, matched ASCII case-insensitively, and then by a character that is neither an ASCII letter, an ASCII digit, nor `-` MUST begin the `svg` element of a `button` element drawn with an icon and a word.
- R-EI2U-W8ZC: The cancelled page's visible text MUST be exactly these strings, in this order, each separated from the next by a single space: `ikigenba`, `Sign in to <apex>`, `Sign-in cancelled`, `Google didn't grant access, so you weren't signed in. You can try again.`, and `Continue with Google`, where `<apex>` is the request's apex name.
- R-EJAR-A0Q1: The non-member page's visible text MUST be exactly these strings, in this order, each separated from the next by a single space: `ikigenba`, `Sign in to <apex>`, `Workspace membership required`, `<email> isn't a verified account in the <workspace> workspace. Sign in with your @<workspace> account instead.`, `Try another account`, and `Think you should have access? Ask your <workspace> workspace admin to add you.`, where `<apex>` is the request's apex name, `<email>` is the verified ID token's `Claims.Email`, and `<workspace>` is the value of `WORKSPACE_DOMAIN`.
- R-0L18-DJPF: The written markup of every auth page MUST hold no tag span whose name is `script` matched ASCII case-insensitively, other than a `script` element that a requirement of auth's design places on that page.
- R-0M94-RBG4: auth's design defines an **attribute name** in a tag span as the longest non-empty run of characters other than ASCII whitespace, `/`, `>`, `=`, and `"` that starts at a place outside the span's quoted runs preceded by ASCII whitespace, by `/`, or by the closing `"` of a quoted run; in every tag span of the written markup of an auth page, every attribute name MUST be, matched ASCII case-insensitively, neither `srcdoc`, nor `http-equiv`, nor `attributionsrc`, nor `on` followed by one or more ASCII letters.
- R-0NH1-536T: In every tag span of the written markup of an auth page, every `=` that lies outside the span's quoted runs MUST immediately follow a non-empty attribute name and MUST be immediately followed by `"`, every quoted run MUST begin with a `"` that immediately follows such an `=`, and the span MUST hold no `'` outside its quoted runs.
- R-0W0B-THDO: Every auth page MUST begin, as the first characters of the response body, with `<!DOCTYPE html>`, matched ASCII case-insensitively.
- R-0OOX-IUXI: In the written markup of every auth page, apart from the leading `<!DOCTYPE html>`, every `<` that lies outside the content of every `script` element and outside every quoted run of a tag span MUST either be immediately followed by an ASCII letter, beginning a tag span, or begin an end tag written exactly as `</`, one or more ASCII letters, ASCII digits, or `-`, and `>`.
- R-0PWT-WMO7: The written markup of every auth page MUST hold no tag span whose name, matched ASCII case-insensitively, is `textarea`, `xmp`, `iframe`, `noembed`, `noframes`, `noscript`, `plaintext`, `template`, or `math`.
- R-0R4Q-AEEW: In the written markup of every auth page, every tag span whose name is `script` matched ASCII case-insensitively MUST be a start tag for `script`; the first `</script` after it, matched ASCII case-insensitively, MUST be exactly the end tag `</script>`; and that `script` element's content MUST hold no `<!--`.
