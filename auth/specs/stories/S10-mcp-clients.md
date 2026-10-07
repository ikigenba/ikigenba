# Stories — MCP clients

How an MCP client, such as Claude Code, gets a token to act as a person at the
space's MCP gateway: auth is the space's OAuth 2.1 authorization server. A
client reads auth's metadata at `/.well-known/oauth-authorization-server`,
registers itself at `/register`, sends the person's browser to `/authorize`,
where the person approves or denies it, and exchanges the code it is given at
`/token` for an access token. The requests go to a running auth
(`S02-serve.md`), started with its Google settings and `IKIGENBA_PUBLIC_URL`
unset, as on a host; they reach it through nginx on a space. Each request is
shown as the HTTP request auth receives, with the headers the story depends on.
Every request is on a space; a request that shows no `Host` header carries
`Host: auth.sbx.ikigenba.dev`, on the space `sbx.ikigenba.dev`
(`S03-sign-in.md`). auth's own origin is `IKIGENBA_PUBLIC_URL` when it is set,
as in a sandbox (`S09-in-a-sandbox.md`), and otherwise `https://auth.<space>`,
here `https://auth.sbx.ikigenba.dev`. The space's MCP origin is auth's own
origin with its leading `auth.` label replaced by `mcp.`, here
`https://mcp.sbx.ikigenba.dev`; the MCP gateway is the one resource auth issues
tokens for, and a request that names no `resource` means
`https://mcp.sbx.ikigenba.dev/mcp`.

A client registers with a JSON object naming itself in `client_name` and the
URIs auth may send the browser back to in `redirect_uris`; auth reads those two
members and ignores any other it is sent. A registration is identified by its
client id, an opaque value auth mints, written `<client-id>` below. A client
has no secret. A redirect URI is acceptable when it is an absolute URL with no
fragment that is either `http://localhost` or `http://127.0.0.1`, with or
without a port and with any path, or any `https` URL; anything else — another
`http` host, a custom scheme, a fragment, a value that is not a URL — is not.
A requested redirect URI matches a registered one when the two are equal
character for character, or, for a registered `http://localhost` or
`http://127.0.0.1` URI, when they differ only in their port. A registration
that has never received a token is removed 24 hours after it was created; one
that has received a token is kept. The examples use the client `Claude Code`,
registered with the one redirect URI `http://localhost:53682/callback`.

An authorization request carries, in its query, `response_type`, `client_id`,
`redirect_uri`, `code_challenge`, `code_challenge_method`, `state`, `resource`,
and `scope`, written `<challenge>` and `<state>` below where their values do
not matter. `scope` is ignored, whatever it holds. A request whose `redirect_uri`
is absent uses the client's one registered URI when it registered exactly one.
auth does not send a browser back to a URI it cannot trust: a request naming an
unknown client, or a redirect URI the client did not register, is answered
`400` with no redirect. Once the client and redirect URI are known, every other
fault sends the browser back to the client with `302 Found` and a `Location` of
the redirect URI with `error=<code>` added to its query, and `state=<state>`
after it when the request carried a `state`; a redirect URI that already has a
query gets them after a `&`. The approve page and the person's answer to it are
the only pages here; the approve page is drawn with the banner, like every page
for a signed-in user (`S03-sign-in.md`). Approving answers the client with a
code, written `<code>` below: opaque, good for one exchange, valid for 10
minutes, and bound to the client, the redirect URI, the code challenge, the
resource, and the person who approved. Any exchange at `/token` that presents a
known code uses it up, whatever the outcome. A POST to `/authorize` carries an
`ikigenba_session` cookie and an `Origin` header equal to auth's own origin;
one whose `Origin` is anything else, or missing, answers 403. Its parameters
are checked again exactly as a GET's are, with the same answers.

`/.well-known/oauth-authorization-server`, `/register`, and `/token` take no
credential and check no `Origin`. `/register` and `/token` answer a failure
with `400`, `Content-Type: application/json`, and a JSON object carrying
`error`, the code the story names, and `error_description`, one line whose text
is not fixed; `/token`'s failures also carry
`Cache-Control: no-store`. A token `/token` mints is an MCP client token: its
secret has the shape of a personal token's, `ikp_` followed by 52 Crockford
base32 characters, shown only in that response, with only its hash stored, and
it has its own `<token-id>`, `tok_` followed by 26 Crockford base32 characters
(`S05-tokens.md`). It is honored only on the space's MCP host
(`S04-check.md`), and the person sees and revokes it from their profile
(`S05-tokens.md`). auth's other failures here — the 400s and the 403 of
`/authorize` — are one line of plain text with no banner, like every
text/plain failure auth answers.

Every request here is in the trail through the request events every request
records (`S02-serve.md`). A registration records `client.registered`, under the
request's id and no user. An approval records `client.approved` and a denial
`client.denied`, each under the request's id and the signed-in user's id. Each
of these carries exactly one attribute, `client`, the `<client-id>`; none
carries the client's name, a redirect URI, a code, or a `state`. A token minted
at `/token` records `token.minted`, as a personal token's creation does
(`S05-tokens.md`), with exactly one attribute, `token`, the new token's
`<token-id>`, under the request's id and the id of the user who approved. A
request that registers nothing, decides nothing, or mints nothing — every
failure below — records none of these events. A request answered 500 because
auth's database failed answers as `S04-check.md` tells it for every route, and
records none of them either.

## An MCP client reads auth's authorization-server metadata

A client that has been told only the gateway's address finds the rest here:
where to register, where to send the person, where to trade a code for a
token, and what auth supports. The metadata is public.

Request:

```
GET /.well-known/oauth-authorization-server HTTP/1.1
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON object with exactly these members:
`issuer` `"https://auth.sbx.ikigenba.dev"`; `authorization_endpoint`
`"https://auth.sbx.ikigenba.dev/authorize"`; `token_endpoint`
`"https://auth.sbx.ikigenba.dev/token"`; `registration_endpoint`
`"https://auth.sbx.ikigenba.dev/register"`; `response_types_supported`
`["code"]`; `grant_types_supported` `["authorization_code"]`;
`code_challenge_methods_supported` `["S256"]`; and
`token_endpoint_auth_methods_supported` `["none"]`. It has no
`scopes_supported` and no `client_id_metadata_document_supported`.

Preconditions:

- auth is serving.
- The request carries no credential.

Postconditions:

- Nothing has changed.

## An MCP client in a sandbox reads auth's metadata

In a sandbox auth's own origin is `IKIGENBA_PUBLIC_URL`, so the issuer and every
endpoint the metadata names sit under it. This is the sandbox of
`S09-in-a-sandbox.md`; everything else about the metadata is the previous
story's.

Request:

```
$ curl -si http://auth.wip.localhost:7400/.well-known/oauth-authorization-server
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is the JSON object of `An MCP client reads auth's
authorization-server metadata`, except that `issuer` is
`"http://auth.wip.localhost:7400"`, `authorization_endpoint` is
`"http://auth.wip.localhost:7400/authorize"`, `token_endpoint` is
`"http://auth.wip.localhost:7400/token"`, and `registration_endpoint` is
`"http://auth.wip.localhost:7400/register"`.

Preconditions:

- The sandbox of `S09-in-a-sandbox.md` is up, and auth's environment carries
  `IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400`.
- The request carries no credential.

Postconditions:

- Nothing has changed.

## An MCP client registers itself

Registration is open to anyone and grants nothing: it gives the client an id to
ask a person with, and only the person's approval leads to a token. auth answers
with its own values for the members it ignores, so a client asking for a refresh
token or a scope learns it gets neither.

Request:

```
POST /register HTTP/1.1
Content-Type: application/json

{"client_name":"Claude Code","redirect_uris":["http://localhost:53682/callback"],"grant_types":["authorization_code","refresh_token"],"token_endpoint_auth_method":"none","scope":"mcp"}
```

Response:

```
HTTP/1.1 201 Created
Content-Type: application/json
Cache-Control: no-store
```

Status 201. The body is a JSON object with exactly these members: `client_id`,
the new `<client-id>`; `client_id_issued_at`, the time of registration in Unix
seconds; `client_name` `"Claude Code"`; `redirect_uris`
`["http://localhost:53682/callback"]`; `grant_types` `["authorization_code"]`;
`response_types` `["code"]`; and `token_endpoint_auth_method` `"none"`. It has
no client secret.

Preconditions:

- auth is serving.
- The request carries no credential.

Postconditions:

- A registration `<client-id>` exists, named `Claude Code`, with the one
  redirect URI `http://localhost:53682/callback`. Nothing else it was sent is
  kept.
- No token and no session were created.
- auth records `client.registered` with `client=<client-id>`, under the
  request's id and no user.

## An MCP client registers without a name

`client_name` is optional; a client that sends none is registered under the
name `MCP client`, which is what the approve page and the person's profile then
show for it.

Request:

```
POST /register HTTP/1.1
Content-Type: application/json

{"redirect_uris":["http://localhost:53682/callback"]}
```

Response:

```
HTTP/1.1 201 Created
Content-Type: application/json
Cache-Control: no-store
```

Status 201. The body is the JSON object of `An MCP client registers itself`,
except that `client_name` is `"MCP client"`.

Preconditions:

- auth is serving.

Postconditions:

- A registration `<client-id>` exists, named `MCP client`, with the one
  redirect URI `http://localhost:53682/callback`.
- auth records `client.registered` with `client=<client-id>`, under the
  request's id and no user.

## An MCP client registers a redirect URI auth does not accept

A redirect URI is where auth sends a code, so auth accepts only one that lands
on the person's own machine or on an `https` site. One unacceptable entry
refuses the whole registration.

Request:

```
POST /register HTTP/1.1
Content-Type: application/json

{"client_name":"Claude Code","redirect_uris":["http://localhost:53682/callback","http://example.com/callback"]}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body's `error` is `invalid_redirect_uri`. An entry that is
refused the same way is any other `http` host, a custom scheme such as
`myapp://callback`, a URL with a fragment such as
`http://localhost:53682/callback#x`, a value that is not an absolute URL, or an
entry longer than 2048 bytes.

Preconditions:

- auth is serving.

Postconditions:

- Nothing has changed: no registration was stored. auth records no
  `client.registered`.

## An MCP client registers no redirect URI, or more than ten

A registration names between one and ten redirect URIs. `redirect_uris`
missing, an empty array, or an array of eleven or more entries is refused.

Request:

```
POST /register HTTP/1.1
Content-Type: application/json

{"client_name":"Claude Code","redirect_uris":[]}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body's `error` is `invalid_redirect_uri`. A body with no
`redirect_uris` member, or with eleven acceptable redirect URIs, is answered the
same way.

Preconditions:

- auth is serving.

Postconditions:

- Nothing has changed: no registration was stored. auth records no
  `client.registered`.

## An MCP client registers metadata auth cannot read

auth keeps registrations small: a body is at most 64 KiB and a `client_name`,
when sent, is a string of 1 to 200 characters. A body that is not a JSON object
is not metadata at all.

Request:

```
POST /register HTTP/1.1
Content-Type: application/json

{"client_name":"","redirect_uris":["http://localhost:53682/callback"]}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body's `error` is `invalid_client_metadata`. A body that is not
a JSON object (a JSON array, or text that is not JSON), a body longer than
64 KiB, and a `client_name` that is not a string or is longer than 200
characters are each answered the same way.

Preconditions:

- auth is serving.

Postconditions:

- Nothing has changed: no registration was stored. auth records no
  `client.registered`.

## A person following an MCP client's login is not signed in

The client opens the person's browser at `/authorize`. A person with no live
session is sent to sign in first, with the authorization request as the return
URL, so that signing in brings them back to it; auth's own host is under the
space, so the return is honored (`S03-sign-in.md`).

Request:

```
GET /authorize?response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&resource=https%3A%2F%2Fmcp.sbx.ikigenba.dev%2Fmcp HTTP/1.1
```

Response:

```
HTTP/1.1 302 Found
Location: /?return=<return>
```

Status 302. `<return>` is the request's own URL at auth's origin —
`https://auth.sbx.ikigenba.dev/authorize?` followed by the request's query as
sent — URL-encoded. The sign-in page it leads to says
`Sign in to continue to auth.sbx.ikigenba.dev.` (`A visitor arrives at the
sign-in page with a return URL`, `S03-sign-in.md`).

Preconditions:

- A registration `<client-id>` exists with the redirect URI
  `http://localhost:53682/callback`.
- The request carries no `ikigenba_session` cookie, or one that names no live
  session.

Postconditions:

- Nothing has changed: nothing about the request is stored.
- Once the person signs in, the callback sends them back to the request
  (`Google returns a member with a return URL waiting`, `S03-sign-in.md`),
  which then shows the approve page.

## A signed-in person is asked to approve an MCP client

A person with a live session sees what the client asks for and decides. The
page is shown for every authorization request, even when the person has
approved the same client before, so no client gets a token without the person
saying yes to it this time. In the example the page is drawn at
`2026-10-07 10:00 UTC`.

Request:

```
GET /authorize?response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&scope=mcp HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML page drawn with the banner (`S03-sign-in.md`).
Its content's visible text is the heading `Connect Claude Code`, the sentence
`Claude Code wants to act as you at the MCP gateway, mcp.sbx.ikigenba.dev, until 2027-01-05.`,
the sentence
`Approve only if you started this from a client you trust. You can revoke it from your profile at any time.`,
the sentence `After you approve, you return to localhost:53682.`, and two
buttons, `Approve` and `Deny`. The client's name is its registered name, shown
as text exactly as registered and never read as markup. The gateway's host is
the MCP origin's host name, a port included when the origin has one, and the
date is 90 days after the page is drawn, in UTC, as `YYYY-MM-DD`. The host the
person returns to is the resolved redirect URI's host as that URI writes it, a
port included when it has one and nothing else of the URI, so for a client
registered at an `https` URI the page names that URI's host. Both buttons
submit one form whose method is POST and whose action is `/authorize`. The form
carries, as fields the person does not see, `response_type` `code`, `client_id`
`<client-id>`, `redirect_uri` `http://localhost:53682/callback`,
`code_challenge` `<challenge>`, `code_challenge_method` `S256`, `state`
`<state>`, and `resource` `https://mcp.sbx.ikigenba.dev/mcp`, the request's
values with the redirect URI and resource as auth resolved them; `Approve`
sends `decision=approve` and `Deny` sends `decision=deny`. The form carries no
`scope`, and no `state` field when the request had none.

Preconditions:

- A registration `<client-id>` named `Claude Code` exists with the one
  redirect URI `http://localhost:53682/callback`.
- The cookie names a live session.
- The page is drawn at `2026-10-07 10:00 UTC`.

Postconditions:

- Nothing has changed: drawing the page stores nothing and records no client
  event.

## An MCP client asks for authorization on another loopback port

A client on the person's machine listens on whatever port is free when it runs,
so a registered `http://localhost` or `http://127.0.0.1` redirect URI matches a
requested one that differs from it only in its port. The redirect URI used from
here on is the requested one.

Request:

```
GET /authorize?response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A61000%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state> HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the approve page of `A signed-in person is asked to
approve an MCP client`, except that it reads
`After you approve, you return to localhost:61000.` and its form's
`redirect_uri` field is `http://localhost:61000/callback`. A requested URI
that differs from the registered one in anything but the port — its host
(`127.0.0.1` for `localhost`), its scheme, or its path — does not match (`An
MCP client asks for authorization with an unknown client or a redirect URI it
did not register`).

Preconditions:

- A registration `<client-id>` named `Claude Code` exists with the one
  redirect URI `http://localhost:53682/callback`.
- The cookie names a live session.

Postconditions:

- Nothing has changed.

## An MCP client asks for authorization with an unknown client or a redirect URI it did not register

auth cannot tell where such a request came from, so it sends the browser
nowhere and answers it itself.

Request:

```
GET /authorize?response_type=code&client_id=<client-id>&redirect_uri=https%3A%2F%2Fevil.example%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state> HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8
```

Status 400. The body is one line of plain text, with no banner; its wording is
not fixed. The response has no `Location`. auth answers the same way, with or
without a session, when `client_id` is missing or names no registration —
never registered, or pruned (`An MCP client returns a day after registering
without receiving a token`) — and when `redirect_uri` is missing and the client
registered more than one.

Preconditions:

- A registration `<client-id>` exists with the one redirect URI
  `http://localhost:53682/callback`.

Postconditions:

- Nothing has changed. auth records no client event.

## An MCP client asks for authorization without PKCE S256

A code can be exchanged only by the client that holds the verifier behind its
challenge, so a request must carry a `code_challenge` and the method `S256`.

Request:

```
GET /authorize?response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=plain&state=<state> HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 302 Found
Location: http://localhost:53682/callback?error=invalid_request&state=<state>
```

Status 302. A request with no `code_challenge`, or with no
`code_challenge_method`, is answered the same way, as is one with no
`response_type`.

Preconditions:

- A registration `<client-id>` exists with the one redirect URI
  `http://localhost:53682/callback`.

Postconditions:

- Nothing has changed: no code was issued. auth records no client event.

## An MCP client asks for a response type other than code

Request:

```
GET /authorize?response_type=token&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state> HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 302 Found
Location: http://localhost:53682/callback?error=unsupported_response_type&state=<state>
```

Status 302. Only a `response_type` that is present gets this answer; a
request with no `response_type` is missing a parameter and is answered
`error=invalid_request`, as in `An MCP client asks for authorization without
PKCE S256`.

Preconditions:

- A registration `<client-id>` exists with the one redirect URI
  `http://localhost:53682/callback`.

Postconditions:

- Nothing has changed: no code was issued. auth records no client event.

## An MCP client asks for authorization for a resource outside the space's MCP gateway

auth issues tokens for the space's MCP gateway only, so a `resource` whose
origin is not the space's MCP origin is refused.

Request:

```
GET /authorize?response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&resource=https%3A%2F%2Frepos.sbx.ikigenba.dev%2Fmcp HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 302 Found
Location: http://localhost:53682/callback?error=invalid_target&state=<state>
```

Status 302.

Preconditions:

- A registration `<client-id>` exists with the one redirect URI
  `http://localhost:53682/callback`.

Postconditions:

- Nothing has changed: no code was issued. auth records no client event.

## A person approves an MCP client

Pressing `Approve` sends the browser back to the client with a code the client
can exchange once, within 10 minutes, for a token.

Request:

```
POST /authorize HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&resource=https%3A%2F%2Fmcp.sbx.ikigenba.dev%2Fmcp&decision=approve
```

Response:

```
HTTP/1.1 302 Found
Location: http://localhost:53682/callback?code=<code>&state=<state>
```

Status 302.

Preconditions:

- A registration `<client-id>` exists with the one redirect URI
  `http://localhost:53682/callback`.
- The cookie names a live session of the user `<user-id>`.
- The `Origin` header equals auth's own origin.

Postconditions:

- A code `<code>` exists, unused, valid for 10 minutes, bound to
  `<client-id>`, `http://localhost:53682/callback`, `<challenge>`,
  `https://mcp.sbx.ikigenba.dev/mcp`, and `<user-id>`.
- No token was created.
- auth records `client.approved` with `client=<client-id>`, under the
  request's id and `<user-id>`.

## A person denies an MCP client

Pressing `Deny` sends the browser back to the client with a refusal and no
code.

Request:

```
POST /authorize HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&resource=https%3A%2F%2Fmcp.sbx.ikigenba.dev%2Fmcp&decision=deny
```

Response:

```
HTTP/1.1 302 Found
Location: http://localhost:53682/callback?error=access_denied&state=<state>
```

Status 302.

Preconditions:

- A registration `<client-id>` exists with the one redirect URI
  `http://localhost:53682/callback`.
- The cookie names a live session of the user `<user-id>`.
- The `Origin` header equals auth's own origin.

Postconditions:

- No code and no token were created.
- auth records `client.denied` with `client=<client-id>`, under the request's
  id and `<user-id>`.

## A person approves a client that is no longer registered

The person drew the approve page, and the client's registration was pruned
before they pressed `Approve`. A POST to `/authorize` checks its parameters
again exactly as a GET does, so the now-unknown client is answered as at
`GET /authorize`, with no redirect.

Request:

```
POST /authorize HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&resource=https%3A%2F%2Fmcp.sbx.ikigenba.dev%2Fmcp&decision=approve
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8
```

Status 400. The body is one line of plain text, with no banner; its wording is
not fixed. The response has no `Location`.

Preconditions:

- The registration `<client-id>` was shown on the approve page and has since
  been pruned (`An MCP client returns a day after registering without
  receiving a token`), so `<client-id>` names no registration.
- The cookie names a live session.
- The `Origin` header equals auth's own origin.

Postconditions:

- Nothing has changed: no code was issued. auth records no client event.

## Another site posts an approval

An approval made on the person's behalf by another site would hand that site's
client a token, so a POST to `/authorize` is accepted only from auth's own
origin, as the profile's POSTs are (`S05-tokens.md`).

Request:

```
POST /authorize HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://evil.example
Content-Type: application/x-www-form-urlencoded

response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&decision=approve
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. The body is one line of plain text. A POST with no `Origin` is
refused the same way.

Preconditions:

- The cookie names a live session.
- The `Origin` header is some origin other than auth's own, or is missing.

Postconditions:

- Nothing has changed: no code was issued. auth records no client event.

## A person approves after their session has lapsed

The person left the approve page open long enough for their session to end.
auth sends them to sign in again with the authorization request as the return
URL, so they come back to the approve page and decide again.

Request:

```
POST /authorize HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&resource=https%3A%2F%2Fmcp.sbx.ikigenba.dev%2Fmcp&decision=approve
```

Response:

```
HTTP/1.1 302 Found
Location: /?return=<return>
```

Status 302. `<return>` is `https://auth.sbx.ikigenba.dev/authorize?` followed
by a query carrying the posted parameters other than `decision`,
URL-encoded, as in `A person following an MCP client's login is not signed
in`.

Preconditions:

- A registration `<client-id>` exists with the one redirect URI
  `http://localhost:53682/callback`.
- The cookie names no live session: it has passed its idle window or its cap.
- The `Origin` header equals auth's own origin.

Postconditions:

- Nothing has changed: no code was issued. auth records no client event.

## A caller posts an approval with no decision auth knows

A POST whose `decision` is neither `approve` nor `deny`, or is missing, did
not come from the approve page's buttons, and auth neither approves nor denies
on it.

Request:

```
POST /authorize HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state>&decision=maybe
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8
```

Status 400. The body is one line of plain text, with no banner. The response
has no `Location`.

Preconditions:

- A registration `<client-id>` exists with the one redirect URI
  `http://localhost:53682/callback`.
- The cookie names a live session.
- The `Origin` header equals auth's own origin.

Postconditions:

- Nothing has changed: no code was issued. auth records no client event.

## An MCP client exchanges its code for a token

The client proves it is the one that asked by sending the verifier behind the
challenge, and gets the token it will send to the MCP gateway. In the example
the person approved at `2026-10-07 10:00 UTC` and the client exchanges the code
at once.

Request:

```
POST /token HTTP/1.1
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&code=<code>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&client_id=<client-id>&code_verifier=<verifier>&resource=https%3A%2F%2Fmcp.sbx.ikigenba.dev%2Fmcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
Cache-Control: no-store
```

Status 200. The body is a JSON object with exactly these members:
`access_token`, the new token's secret, `ikp_` then 52 Crockford base32
characters; `token_type` `"Bearer"`; and `expires_in` `7776000`, 90 days in
seconds. It has no `refresh_token` and no `scope`. A request with no
`resource` is answered the same way.

Preconditions:

- The code `<code>` was issued at `2026-10-07 10:00 UTC` when the user
  `<user-id>` approved the registration `<client-id>`, named `Claude Code`, for
  the redirect URI `http://localhost:53682/callback`, the challenge
  `<challenge>`, and the resource `https://mcp.sbx.ikigenba.dev/mcp`; it has
  not been exchanged.
- `<verifier>` is the verifier whose S256 is `<challenge>`.
- The request carries no credential.

Postconditions:

- One MCP client token now belongs to `<user-id>`: named `Claude Code`, with
  a fresh `<token-id>`, created now, never used, expiring at
  `2027-01-05 10:00 UTC`, 90 days after the approval, and bound to the host
  `mcp.sbx.ikigenba.dev`, the MCP origin's host name without any port; in a
  sandbox (`S09-in-a-sandbox.md`) it would be bound to `mcp.wip.localhost`.
  Only a hash of its secret is stored.
- Any earlier token the user holds for the same client is unchanged; each
  approval and exchange adds a token.
- The code is used and cannot be exchanged again.
- The registration `<client-id>` has received a token and is no longer pruned.
- auth records `token.minted` with `token=<token-id>`, under the request's id
  and `<user-id>`.

## An MCP client exchanges a code that cannot be honored

A code is honored once, by the client and for the redirect URI it was issued
to, within 10 minutes, and only with the verifier behind its challenge. Every
way of failing that is answered with the same `error`. The code is used by its first exchange, whatever the
outcome: a client that sends a wrong verifier cannot try again with the same
code.

Request:

```
POST /token HTTP/1.1
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&code=<code>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&client_id=<client-id>&code_verifier=<verifier>
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
Cache-Control: no-store
```

Status 400. The body's `error` is `invalid_grant`.

Preconditions:

- One of: `<code>` names no code; `<code>` has already been exchanged;
  `<code>` was issued more than 10 minutes ago; `<code>` was issued to a
  client other than `<client-id>`; `<code>` was issued for a redirect URI other
  than `http://localhost:53682/callback`; or the S256 of `<verifier>` is not
  the code's challenge.

Postconditions:

- No token was minted. auth records no `token.minted`.
- A code `<code>` names, if any, is used and cannot be exchanged again.

## An MCP client exchanges a code with a parameter missing

`code`, `redirect_uri`, `client_id`, and `code_verifier` are each required.

Request:

```
POST /token HTTP/1.1
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&code=<code>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&client_id=<client-id>
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
Cache-Control: no-store
```

Status 400. The body's `error` is `invalid_request`. A request missing `code`,
`redirect_uri`, or `client_id` instead is answered the same way.

Preconditions:

- The request carries `grant_type=authorization_code` and lacks
  `code_verifier`.
- `<code>` is an unused code issued less than 10 minutes ago to `<client-id>`.

Postconditions:

- No token was minted. auth records no `token.minted`.
- The code `<code>` is used and cannot be exchanged again. A request that
  presents a known code and lacks another parameter uses it up the same way; a
  request with no `code` uses up none.

## An MCP client asks for a grant other than an authorization code

auth issues tokens only for an approved code: it has no refresh tokens and no
client credentials.

Request:

```
POST /token HTTP/1.1
Content-Type: application/x-www-form-urlencoded

grant_type=refresh_token&refresh_token=<refresh-token>&client_id=<client-id>
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
Cache-Control: no-store
```

Status 400. The body's `error` is `unsupported_grant_type`. Any other
`grant_type`, such as `client_credentials`, is answered the same way.

Preconditions:

- auth is serving.

Postconditions:

- No token was minted. auth records no `token.minted`.

## An MCP client exchanges a code for a resource outside the space's MCP gateway

Request:

```
POST /token HTTP/1.1
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&code=<code>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&client_id=<client-id>&code_verifier=<verifier>&resource=https%3A%2F%2Frepos.sbx.ikigenba.dev%2Fmcp
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
Cache-Control: no-store
```

Status 400. The body's `error` is `invalid_target`.

Preconditions:

- `<code>` is an unused code issued less than 10 minutes ago to `<client-id>`
  for the redirect URI `http://localhost:53682/callback`, and `<verifier>` is
  its verifier.
- The `resource`'s origin is not the space's MCP origin.

Postconditions:

- No token was minted. auth records no `token.minted`.
- The code `<code>` is used and cannot be exchanged again.

## An MCP client returns a day after registering without receiving a token

Registration is open to anyone, so auth does not keep a registration that never
led to a token. Once it is gone its client id is unknown, as if it had never
been registered.

Request:

```
GET /authorize?response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state> HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/plain; charset=utf-8
```

Status 400. The answer of `An MCP client asks for authorization with an unknown
client or a redirect URI it did not register`: one line of plain text and no `Location`.

Preconditions:

- The registration `<client-id>` was created more than 24 hours ago with the
  redirect URI `http://localhost:53682/callback`, and no token was ever minted
  for it.
- The cookie names a live session.

Postconditions:

- The registration `<client-id>` is gone. auth records no client event.

## An MCP client that received a token returns after a day

A registration that has led to a token stays, so the client can send the person
through `/authorize` again — when its token expires, say — without registering
again.

Request:

```
GET /authorize?response_type=code&client_id=<client-id>&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2Fcallback&code_challenge=<challenge>&code_challenge_method=S256&state=<state> HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the approve page of `A signed-in person is asked to
approve an MCP client`.

Preconditions:

- The registration `<client-id>`, named `Claude Code`, was created more than
  24 hours ago with the redirect URI `http://localhost:53682/callback`, and a
  token was minted for it.
- The cookie names a live session.

Postconditions:

- Nothing has changed. The registration `<client-id>` is kept.
