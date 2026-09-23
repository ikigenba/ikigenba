# D05-sign-in

The browser sign-in flow and its Google/OIDC client. A visitor lands on `/`,
which is the sign-in page when there is no live session and the profile when
there is. `/login/google` starts a Google sign-in; `/login/google/callback`
finishes it; `POST /logout` ends a session. All of this attaches its observable
HTTP behavior to `internal/server`'s router (D03 owns the router itself), and it
speaks to Google through `internal/google`, whose exported surface this design
declares.

`internal/google` wraps `golang.org/x/oauth2` and `github.com/coreos/go-oidc/v3`
behind a small client this sub-project constructs once from configuration. The
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
authorization and exchange — so it is derived per request from the `Host` and
threaded into both `AuthCodeURL` and `Exchange`. Because the client is
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
`redirect_uri`, the cookie `Domain`, its own origin (for the logout `Origin`
check), and which return URLs count as being under the space. Run locally these
take the fixed development forms S3 states. Users are keyed by the verified ID
token's `(issuer, subject)`; the email is a copy refreshed on every login; auth's
own `X-User-Id` is a fresh opaque id minted by `idcodec.NewID`, never Google's
`sub`.

The profile page's presence, its logout form, and its create-token form are
owned here; the per-token row contents and their enable/disable/delete forms are
D07's. The identity headers `X-User-Id`/`X-User-Email` and bearer parsing are
D06's. Server construction and Google-config validation are D03's, and so is
the one line auth writes to its diagnostic stream for a request it answers
`502` or `500`: `auth: request <id>: <reason>`, naming the request by its
`X-Request-Id`. The two `502`s here — Google unreachable at the start of a
sign-in, and a failed exchange at its end — are the sign-in flow's own trouble,
and each writes that line with Google's error as the reason. S7's routing
of *other* apps through `/check`, the public `/check` 404, and the nginx-side
redirect that carries `?return` are properties of the space produced by opsctl,
a separate sub-project, and are out of scope here; the only S7 fact this design
owns is that auth answers its own host's `/` with the sign-in page.

## REQUIREMENTS

- R-I82C-VOP7: `internal/google` MUST export `type Claims struct { Issuer string; Subject string; Email string; EmailVerified bool; HostedDomain string }`, carrying the verified ID token's `iss`, `sub`, `email`, `email_verified`, and `hd` claims (`HostedDomain` empty when the `hd` claim is absent).
- R-KUGP-2ZZD: `internal/google` MUST export `func NewClient(clientID, clientSecret, workspaceDomain, issuer string) *Client` returning a `*Client`, the exported type through which this project reaches Google; `NewClient` MUST NOT return an error.
- R-KVOL-GRQ2: `*Client` MUST export `func (*Client) AuthCodeURL(state, verifier, redirectURI string) (string, error)`, returning the Google authorization redirect URL for the given login state, PKCE code verifier, and per-request `redirectURI`, or a non-nil error when it cannot be built.
- R-FX2G-ZLVJ: `*Client` MUST export `func (*Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (Claims, error)`, exchanging an authorization code, its PKCE verifier, and the per-request `redirectURI`, and returning verified ID-token `Claims` or an error.
- R-ICXY-ERNZ: `internal/server` MUST export `const SessionCookieName = "ikigenba_session"`, the name of the session cookie; other designs reference this name rather than re-declaring it.
- R-KWWH-UJGR: `NewClient` MUST build the client from `GOOGLE_CLIENT_ID` (as `clientID`), `GOOGLE_CLIENT_SECRET` (as `clientSecret`), `WORKSPACE_DOMAIN` (as `workspaceDomain`), and `Process.OIDCIssuer` (as `issuer`) without performing any I/O, and MUST NOT contact the `issuer` at construction; the client MUST discover the Google OAuth 2.0 / OIDC endpoints and JWKS from that `issuer`'s OpenID configuration on demand when a sign-in needs them (in `AuthCodeURL` and `Exchange`), so that a loopback fake standing in as `Process.OIDCIssuer` fully replaces Google in tests; a discovery that fails MUST NOT be remembered — a later sign-in retries it; the `redirect_uri` MUST NOT be fixed at construction — it is derived per request and passed to `AuthCodeURL` and `Exchange`.
- R-KZCA-M2Y5: When the client cannot discover the `issuer`'s endpoints (the `issuer` is unreachable or its OpenID configuration cannot be fetched), `AuthCodeURL` MUST return a non-nil error and an empty URL string.
- R-IFDR-6B5D: `internal/google` MUST implement its OAuth 2.0 exchange and OIDC verification using `golang.org/x/oauth2` and `github.com/coreos/go-oidc/v3` (named by import path; no version is stated).
- R-IGLN-K2W2: `AuthCodeURL` MUST return a URL addressed to the `authorization_endpoint` discovered from the client's issuer (for Google's issuer this is `https://accounts.google.com/o/oauth2/v2/auth` per `/tmp/scratch.aYV818.md`), whose query carries the OAuth client id (from `GOOGLE_CLIENT_ID`), `hd` set to `WORKSPACE_DOMAIN`, the `redirect_uri`, the given `state`, a `code_challenge` that is the S256 hash of the given `verifier`, and `code_challenge_method=S256`.
- R-G0Q6-4X3M: `Exchange` MUST POST the code, PKCE `verifier`, and the given `redirectURI` to the `token_endpoint` discovered from the client's issuer (for Google's issuer this is `https://oauth2.googleapis.com/token` per `/tmp/scratch.aYV818.md`), MUST send to that endpoint the same `redirect_uri` value it was given (matching the one used at authorization), MUST verify the returned ID token's RS256 signature against the discovered JWKS, MUST accept an issuer claim of either `accounts.google.com` or `https://accounts.google.com` (per `/tmp/scratch.aYV818.md`), and MUST return `Claims` populated from the verified token; a failed exchange, unreachable endpoint, or failed verification MUST return a non-nil error.
- R-YQS0-XIQ4: On a successful sign-in the response MUST set the `SessionCookieName` cookie to the created session's opaque id with attributes `Path=/`, `Secure`, `HttpOnly`, and `SameSite=Lax`; on a space it MUST also set `Domain=<space>`, and run locally it MUST set no `Domain`.
- R-YRZX-BAGT: On logout the response MUST clear the `SessionCookieName` cookie with an empty value, `Max-Age=0`, and the same path, domain scope, and security attributes as the login cookie (`Path=/`, `Secure`, `HttpOnly`, `SameSite=Lax`, `Domain=<space>` on a space and no `Domain` locally).
- R-ILH9-35UU: auth MUST derive the *space* for a request as the request's `Host` with a single leading `auth.` label removed.
- R-IMP5-GXLJ: The callback `redirect_uri` auth sends to Google MUST be `https://auth.<space>/login/google/callback` when serving on a space, and MUST be `http://localhost:3001/login/google/callback` when run locally.
- R-INX1-UPC8: auth's own origin (used for the logout `Origin` check) MUST be `https://auth.<space>` when serving on a space, and MUST be `http://127.0.0.1:3001` when run locally.
- R-IQCU-M8TM: A return URL MUST be treated as in-space if and only if its host is the space or a subdomain of the space; any other return URL MUST be treated as out-of-space.
- R-IRKR-00KB: `GET /` with no live session MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body containing a link whose target is `/login/google`; a `?return=<url>` on the request MUST be carried to the login start and MUST NOT be persisted.
- R-ISSN-DSB0: `GET /` with a live session MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body containing a form that POSTs to `/logout` and a form that POSTs to `/tokens` with fields `name` and `expires`; it MUST resolve the identity via `LookupSessionIdentity` (no touch), MUST ignore any `?return`, and MUST NOT change any state.
- R-KY4E-8B7G: `GET /login/google` MUST mint a PKCE verifier from `Process.Rand`, derive the `redirect_uri` from the request `Host`, record a login state via `CreateLoginState` carrying that verifier and any return URL carried from the sign-in page, and obtain the authorization redirect URL via `AuthCodeURL(state, verifier, redirectURI)` for the recorded login state's `State` and that derived `redirectURI`; when `AuthCodeURL` returns a nil error it MUST respond `302` whose `Location` is that URL, so that the `state` value in `Location` names that login state.
- R-NG2E-9BDI: When `AuthCodeURL` returns a non-nil error during `GET /login/google` (the `issuer`'s endpoints could not be discovered), auth MUST respond `502` with `Content-Type: text/plain; charset=utf-8` and a single line of body, MUST write the line R-NDML-HRW4 states with that error as its reason, MUST create no user, session, or cookie, and MUST leave no login state recorded — removing via `ConsumeLoginState` any login state it created for the request.
- R-IV8G-5BSE: `GET /login/google/callback` whose `state` matches no recorded login state, including a request with no `state`, MUST respond `400` with `Content-Type: text/plain; charset=utf-8` and a single line of body, and MUST create no user, session, or cookie.
- R-G1Y2-IOUB: `GET /login/google/callback` carrying `error=access_denied` MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body containing a link whose target is `/login/google`, MUST send no `Set-Cookie`, MUST consume the login state named by `state` if one exists, and MUST create no user or session; this `error=access_denied` response takes precedence over the unknown/missing-state `400` case (R-IV8G-5BSE), so when `error=access_denied` is present the response is `200` regardless of whether `state` matches a recorded login state.
- R-IXO8-WV9S: `GET /login/google/callback` with a matched login state and a member result MUST exchange the code and verifier via `Exchange`, call `UpsertUserOnLogin`, call `CreateSession`, set the session cookie, consume the login state via `ConsumeLoginState`, and respond `302`.
- R-IYW5-AN0H: On a member sign-in the `issuer`, `subject`, and `email` passed to `UpsertUserOnLogin` MUST be the verified ID token's `Claims.Issuer`, `Claims.Subject`, and `Claims.Email`, so that users are keyed by the verified `(issuer, subject)` and the stored email is refreshed to the token's value on every login.
- R-J041-OER6: The `302` `Location` of a successful member sign-in MUST be the login state's carried return URL when that URL is in-space (per R-IQCU-M8TM), and MUST be `/` when there is no return URL or the carried return URL is out-of-space.
- R-J1BY-26HV: A callback result MUST be treated as a member if and only if the verified `Claims.HostedDomain` equals `WORKSPACE_DOMAIN` and `Claims.EmailVerified` is true (an absent `hd` claim, i.e. empty `HostedDomain`, is not a member, per `/tmp/scratch.aYV818.md`); a non-member result MUST respond `403` with `Content-Type: text/html; charset=utf-8`, send no `Set-Cookie`, consume the login state, and create or change no user, session, or cookie.
- R-NHAA-N347: When a matched-state callback's token exchange fails or Google is unreachable, auth MUST respond `502` with `Content-Type: text/plain; charset=utf-8` and a single line of body, MUST write the line R-NDML-HRW4 states with the `Exchange` error as its reason, and MUST create no user, session, or cookie.
- R-J3RQ-TPZ9: `POST /logout` whose `Origin` equals auth's own origin MUST respond `302` with `Location: /`, clear the session cookie, delete the session server-side via `DeleteSession`, and leave the user row and the user's tokens untouched.
- R-J4ZN-7HPY: `POST /logout` whose `Origin` is not auth's own origin MUST respond `403` with `Content-Type: text/plain; charset=utf-8`, send no `Set-Cookie`, and leave the session untouched; this `Origin` check is the second line of cross-site defense after the cookie's `SameSite=Lax`.
- R-J67J-L9GN: When auth serves its own host on a space, `GET https://auth.<space>/` with no live session MUST respond `200` with `Content-Type: text/html; charset=utf-8` and a body containing a link whose target is `/login/google`, auth answering the request from its own server block.
