# Stories — tokens

The token actions a signed-in user drives from their profile. Every request
here is a curl against auth a developer serves with
`systemd-socket-activate -l 127.0.0.1:3001 auth` (`S2-serve.md`), at
`http://127.0.0.1:3001`, carrying a valid
`ikigenba_session` cookie, and every state-changing request is a POST that also
carries an `Origin` header matching the service's own origin (in development
that is the local origin, here `http://127.0.0.1:3001`; on a space it is
`https://auth.<space>`). A user may hold many tokens. A token's secret has the
form `ikp_` followed by 52 Crockford base32 characters (`0`-`9` and `A`-`Z`
without `I`, `L`, `O`, `U`), the encoding of 32 random bytes; it is shown once
at creation and never again, and only a hash of it is stored. Separately, each
token carries its own random Crockford identifier used in the action URLs
(`POST /tokens/<id>/enable`, `/disable`, `/delete`); this id is not the secret.
Every action is a POST: acting on a token id that is not the user's own or does
not exist answers 404, and a POST whose `Origin` is not the service's own origin
answers 403. Pages are described by their structure — links, forms, and field
names — never by any wording or label they display. The whole-profile page is
S3's story; the stories here fix only the token actions and the token table.

## A user creates a token

The create form submits a chosen `name` (required, 1..64 characters after
trimming) and an `expires` value, one of `never`, `30d`, `90d`, or `365d`. On
success the new token's plaintext secret is returned in the response body once
and is never retrievable afterward; the server keeps only its hash.

Request:

```
$ curl -si -X POST http://127.0.0.1:3001/tokens \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://127.0.0.1:3001' \
    --data 'name=<name>' \
    --data 'expires=<never|30d|90d|365d>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML page that presents the newly created token's
plaintext secret exactly once, in a form of `ikp_` then 52 Crockford base32
characters; alongside it is a copy control (a button element that copies the
secret) and a link whose target is `/` (the profile). The plaintext appears in
this response only and in no later page.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.

Postconditions:

- One token record now belongs to the user: enabled, with its created-at set,
  its last-used empty, and its expiry set from the chosen `expires`.
- Only a hash of the secret is stored; the plaintext is not persisted.

## A user creates a token with no name

A name that is empty or only whitespace is not a valid name, so no token is
created and the create form is returned for the user to try again.

Request:

```
$ curl -si -X POST http://127.0.0.1:3001/tokens \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://127.0.0.1:3001' \
    --data-urlencode 'name=   ' \
    --data 'expires=never'
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/html; charset=utf-8
```

Status 400. The body is an HTML page containing the create form: a form whose
method is POST and whose action is `/tokens`, with a `name` field and an
`expires` field.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.

Postconditions:

- No token record is created.

## A user lists their tokens

The profile lists the tokens the user owns so they can manage each one. This
story fixes the contents of that token table; the profile page as a whole is
S3's story.

Request:

```
$ curl -si http://127.0.0.1:3001/ \
    -H 'Cookie: ikigenba_session=<id>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML page in which, for each token the user owns,
there is a row exposing that token's name, its created time, its last-used
time, its expiry, and whether it is enabled. Each row also contains a form
whose method is POST and whose action is that token's enable URL
(`/tokens/<id>/enable`) when the token is disabled or its disable URL
(`/tokens/<id>/disable`) when it is enabled, and a form whose method is POST
and whose action is that token's delete URL (`/tokens/<id>/delete`). No plaintext
secret appears anywhere on the page.

Preconditions:

- The user is signed in; the cookie names a live session.
- The user owns zero or more tokens.

Postconditions:

- No token is created, changed, or removed.

## A user disables a token and enables it again

The enabled toggle is flipped by posting to the token's disable or enable URL.
Each returns the user to the profile. Whether a disabled token is refused by
the check endpoint is S4's story; it is not re-proven here.

Request:

```
$ curl -si -X POST http://127.0.0.1:3001/tokens/<id>/disable \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://127.0.0.1:3001'
```

```
$ curl -si -X POST http://127.0.0.1:3001/tokens/<id>/enable \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://127.0.0.1:3001'
```

Response (each):

```
HTTP/1.1 302 Found
Location: /
```

Status 302. Each response redirects to `/` (the profile). Posting to the
disable URL leaves the token disabled; posting to the enable URL leaves it
enabled again.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.
- The `<id>` names a token the user owns; before the first request it is
  enabled.

Postconditions:

- After the disable request the token is disabled; after the enable request it
  is enabled again. A disabled token is rejected by `/check` exactly as an
  unknown one is (S4).

## A user deletes a token

Deleting a token revokes it: the record is removed and the secret can no longer
authenticate.

Request:

```
$ curl -si -X POST http://127.0.0.1:3001/tokens/<id>/delete \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://127.0.0.1:3001'
```

Response:

```
HTTP/1.1 302 Found
Location: /
```

Status 302. The response redirects to `/` (the profile).

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.
- The `<id>` names a token the user owns.

Postconditions:

- The token record is gone. Its secret no longer authenticates any request
  (S4).

## A user acts on a token that is not theirs

A token id that belongs to another user, or that names no token at all, is not
the user's to act on. All three actions behave the same way.

Request:

```
$ curl -si -X POST http://127.0.0.1:3001/tokens/<id>/disable \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://127.0.0.1:3001'
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The enable, disable, and delete URLs each answer 404 the same way
for such an id.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.
- The `<id>` names a token owned by a different user or names no token.

Postconditions:

- Nothing has changed.

## Another site posts to the profile

A cross-site POST is refused on two lines: `SameSite=Lax` already keeps the
session cookie from riding a cross-site request, and, as the explicit second
line, a POST whose `Origin` is not the service's own origin is refused
outright. This applies to `/tokens` and to every token action URL.

Request:

```
$ curl -si -X POST http://127.0.0.1:3001/tokens \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: https://evil.example' \
    --data 'name=<name>' \
    --data 'expires=never'
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. A POST to any token action URL (`/tokens/<id>/enable`, `/disable`,
`/delete`) with a foreign `Origin` is refused the same way.

Preconditions:

- The cookie names a live session (this is what a cross-site attacker would
  hope to ride).
- The `Origin` header is some origin other than the service's own.

Postconditions:

- Nothing has changed.
