# Stories — tokens

The token actions a signed-in user drives from their profile. Every request
here is a curl against auth a developer serves with
`systemd-socket-activate -E GOOGLE_CLIENT_ID -E GOOGLE_CLIENT_SECRET -E WORKSPACE_DOMAIN -l 127.0.0.1:3001 auth` (`S2-serve.md`), at
`http://localhost:3001`, carrying a valid
`ikigenba_session` cookie, and every state-changing request is a POST that also
carries an `Origin` header matching the service's own origin (in development
that is the local origin, here `http://localhost:3001`; on a space it is
`https://auth.<space>`). A user may hold many tokens. A token's secret has the
form `ikp_` followed by 52 Crockford base32 characters (`0`-`9` and `A`-`Z`
without `I`, `L`, `O`, `U`), the encoding of 32 random bytes; it is shown once
at creation and never again, and only a hash of it is stored. Separately, each
token carries its own random Crockford identifier used in the action URLs
(`POST /tokens/<id>/enable`, `/disable`, `/delete`); this id is not the secret.
Every action is a POST: acting on a token id that is not the user's own or does
not exist answers 404, and a POST whose `Origin` is not the service's own origin
answers 403. Those failures, like every text/plain failure auth answers, are
one line of plain text with no banner. The whole profile page is S3's story; the
stories here fix the token table and its empty state, the `Create a token`
card, and the two pages token creation draws.

Every HTML page these stories fix is drawn with the banner (S3): its
`<title>` is `auth`, it links `/_appkit/theme.css` as its stylesheet and
declares the phone-width viewport, it opens with the banner — the mark, the
user's email as a link to `/`, the `Sign out` button POSTing to `/logout`,
and, when auth's services file lists services, the launcher (S3) — and its
content sits in the page's one `<main>`. A card is a
`<section class="card">` whose `<header>` holds an `<h2>` naming it. An icon
is an inline `<svg class="ico" aria-hidden="true">` drawn before a button's
text, so the button's accessible text is the word alone.

The `Create a token` card, which also sits on the profile (S3), holds a form
whose method is POST and whose action is `/tokens`. It has a field labelled
`Name`, a text input `name` with `maxlength` 64 and placeholder `e.g. ci-deploy`,
followed by the hint
`<span class="hint">Something that tells you where it's used. Up to 64 characters.</span>`;
a field labelled `Expires`, a select `expires` offering, in this order,
`30d` reading `In 30 days`, `90d` reading `In 90 days`, `365d` reading
`In 365 days`, and `never` reading `Never`, with `90d` selected; and a submit
button reading `Create token` behind the plus icon. A `name` is valid when it
is 1 to 64 characters once leading and trailing whitespace is trimmed; an
`expires` is valid when it is one of the four option values. A missing
`expires` is invalid in the same way as a value outside the list.

A submission auth rejects is answered `400` with a page drawn with the
banner, holding only the `Create a token` card, still carrying the values the
caller submitted, with its submit button inside a `<div class="actions">`
followed by `<a class="button ghost" href="/">Cancel</a>`. Each field that is
wrong has its own error message in a `<span>` beneath it, whose `id` that
field's input or select names in its `aria-describedby`; a field in error
shows its message in place of its hint, so the Name hint shows only when the
name is not in error. An `expires` that is wrong is not kept: the select shows
`90d` (`In 90 days`) selected, as on a fresh form. The name's message is
`the name must be 1 to 64 characters`; the expiry's message is
`choose one of the listed expiry options`. Every field is checked, so a
submission with both fields wrong carries both messages. A field that is
right carries no message.

## A user creates a token

The create form submits a chosen `name` and an `expires` value, one of
`never`, `30d`, `90d`, or `365d`. On success the new token's plaintext secret
is returned in the response body once and is never retrievable afterward; the
server keeps only its hash.

Request:

```
$ curl -si -X POST http://localhost:3001/tokens \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001' \
    --data 'name=<name>' \
    --data 'expires=<never|30d|90d|365d>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML page drawn with the banner (S3) whose
`<main>` holds one card headed `Token created`. The card holds a
`<div class="alert quiet" data-kind="warn">` titled `Copy it now`, whose text
is `This is the only time <name> is shown. Only its hash is stored.`, with
`<name>` the token's trimmed name; then a `<div class="secret">` holding the
newly created token's plaintext secret, `ikp_` then 52 Crockford base32
characters, in a `<code>` element, and a `<button class="secondary" type="button">`
reading `Copy` behind the copy icon; then a link reading `Back to your account`
whose target is `/` (the profile). The secret occurs exactly once in the body,
inside that `<code>` element: the button carries no copy of it, and clicking
`Copy` puts the text of the `<code>` element, the secret, on the clipboard.
The secret appears in this response only and in no later page.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.
- `<name>` is 1 to 64 characters once trimmed.

Postconditions:

- One token record now belongs to the user: enabled, named with the trimmed
  name, with its created-at set, its last-used empty, and its expiry set from
  the chosen `expires`.
- Only a hash of the secret is stored; the plaintext is not persisted.

## A user creates a token with no name

A name that is empty or only whitespace is not a valid name, so no token is
created and the create form is returned for the user to try again.

Request:

```
$ curl -si -X POST http://localhost:3001/tokens \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001' \
    --data-urlencode 'name=   ' \
    --data 'expires=never'
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/html; charset=utf-8
```

Status 400. The body is an HTML page drawn with the banner (S3) whose
`<main>` holds only the `Create a token` card, its form POSTing to `/tokens`.
The form carries the values the caller submitted: the name field holding the
three spaces, the expiry field with `never` selected. The message
`the name must be 1 to 64 characters` sits beneath the name field in place of
its hint, wired to it by `aria-describedby`; no message sits beneath the
expiry field. The
`Create token` button and the `Cancel` link to `/` sit together in the
card's `<div class="actions">`.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.

Postconditions:

- No token record is created.

## A user creates a token with a name longer than 64 characters

64 characters is the limit, so 65 is a rejection. The input's `maxlength`
stops a browser from typing more, but a caller with `curl` sends what they
like, so auth checks the length itself. The name below is 65 characters.

Request:

```
$ curl -si -X POST http://localhost:3001/tokens \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001' \
    --data 'name=a-token-name-that-runs-well-past-the-sixty-four-character-limit-x' \
    --data 'expires=90d'
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/html; charset=utf-8
```

Status 400. The body is an HTML page drawn with the banner (S3) whose
`<main>` holds only the `Create a token` card, its form POSTing to `/tokens`.
The form carries the values the caller submitted: the name field holding the
65-character name in full, unshortened, the expiry field with `90d` selected.
The message `the name must be 1 to 64 characters` sits beneath the name field
in place of its hint, wired to it by `aria-describedby`; no message sits
beneath the expiry field.
The `Create token` button and the `Cancel` link to `/` sit together in the
card's `<div class="actions">`.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.

Postconditions:

- No token record is created.

## A caller creates a token with an expiry that is not offered

A person using the browser cannot reach this rejection: the expiry field is a
select offering `30d`, `90d`, `365d`, and `never`. A caller with `curl` sends
whatever they like, which is why auth checks the value against the four
rather than trusting that the form produced it. A missing `expires` is
rejected the same way.

Request:

```
$ curl -si -X POST http://localhost:3001/tokens \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001' \
    --data 'name=<name>' \
    --data 'expires=1y'
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/html; charset=utf-8
```

Status 400. The body is an HTML page drawn with the banner (S3) whose
`<main>` holds only the `Create a token` card, its form POSTing to `/tokens`.
The form carries the values the caller submitted: the name field holding
`<name>` with its hint beneath it; the expiry field, which offers the same
four choices, cannot hold `1y`, so it shows `90d` (`In 90 days`) selected, as
on a fresh form. The message `choose one of the listed expiry options` sits
beneath the expiry field, wired to it by `aria-describedby`; no message sits
beneath the name field.
The `Create token` button and the `Cancel` link to `/` sit together in the
card's `<div class="actions">`.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.
- `<name>` is 1 to 64 characters once trimmed.

Postconditions:

- No token record is created.

## A caller creates a token with both fields wrong

Every field is checked and every field that is wrong is reported, so a
caller who got both wrong learns both from one answer instead of one
submission at a time.

Request:

```
$ curl -si -X POST http://localhost:3001/tokens \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001' \
    --data-urlencode 'name=   ' \
    --data 'expires=1y'
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: text/html; charset=utf-8
```

Status 400. The body is an HTML page drawn with the banner (S3) whose
`<main>` holds only the `Create a token` card, its form POSTing to `/tokens`.
The form carries the values the caller submitted: the name field holding the
three spaces, and the expiry field with `90d` (`In 90 days`) selected, as on
a fresh form, since `1y` is not one of its choices. Two messages appear:
`the name must be 1 to 64 characters` beneath the name field in place of its
hint, and `choose one of the listed expiry options` beneath the expiry field,
each wired to its field by `aria-describedby`. The `Create token` button and
the `Cancel` link to `/` sit together in the card's `<div class="actions">`.

Preconditions:

- The user is signed in; the cookie names a live session.
- The `Origin` header equals the service's own origin.

Postconditions:

- No token record is created.

## A user lists their tokens

The profile lists the tokens the user owns so they can manage each one. This
story fixes the `API tokens` card on the profile; the profile page as a whole
is S3's story. The rows run most recently used first, by last-used time, so
the token used last is at the top. Tokens with the same last-used time run
newest first by created time. Tokens never used come after every token that
has been used, newest first by created time among themselves. In the example
the page is drawn at `2026-09-28 10:00:00 UTC` and the user owns six tokens:

```
name            created (UTC)     last used (UTC)      expires (UTC)     enabled
ci-deploy       2026-08-02 10:11  2026-09-28 09:59:30  2026-10-31 10:11  yes
laptop-cli      2026-06-11 16:02  2026-09-25 09:15:00  never             yes
nightly-sync    2026-07-20 07:45  2026-09-25 09:15:00  2026-10-18 07:45  yes
grafana-scrape  2026-09-27 08:30  never used           2027-09-27 08:30  yes
spare-key       2026-09-01 11:00  never used           2026-11-30 11:00  yes
old-backup      2025-10-01 12:00  2026-01-14 03:15:00  2026-10-01 12:00  no
```

Request:

```
$ curl -si http://localhost:3001/ \
    -H 'Cookie: ikigenba_session=<id>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the profile page (S3), which holds a
`<section class="card flush">` headed `API tokens`, whose header also carries
the text
`Personal access tokens let scripts and tools act as you. Send one as a bearer token.`
Beneath it a `<div class="table-scroll">` holds a table whose header cells are
`Name`, `Created`, `Last used`, `Expires`, `Status`, and a last, empty cell
over the actions. There is one row for each token the user owns, most
recently used first — for the example, `ci-deploy`, `nightly-sync`,
`laptop-cli`, `old-backup`, `grafana-scrape`, `spare-key`, in that order:

- `Name` is the token's name.
- `Created` and `Expires` each show a time as
  `<time datetime="<RFC 3339 time in UTC>">YYYY-MM-DD HH:MM UTC</time>`, so
  `ci-deploy`'s `Created` reads `2026-08-02 10:11 UTC`.
- `Last used` shows how long before the page was drawn the token was last
  used, as
  `<time datetime="<RFC 3339 time in UTC>" title="YYYY-MM-DD HH:MM UTC"><elapsed></time>`,
  where the `title` holds the exact last-used time and `<elapsed>` is the
  wording the next story fixes. For the example, `ci-deploy`'s reads
  `<time datetime="2026-09-28T09:59:30Z" title="2026-09-28 09:59 UTC">just now</time>`,
  `nightly-sync`'s and `laptop-cli`'s each read `3 days ago` with the title
  `2026-09-25 09:15 UTC`, and `old-backup`'s reads `257 days ago` with the
  title `2026-01-14 03:15 UTC`.
- A token never used shows `Never` in `Last used` (`grafana-scrape`,
  `spare-key`), and a token that never expires shows `Never` in `Expires`
  (`laptop-cli`); each such cell is `<td class="muted">`.
- `Status` is `<span class="badge" data-kind="ok">Enabled</span>` for an
  enabled token and `<span class="badge">Disabled</span>` for a disabled one
  (`old-backup`).
- The last cell is `<td class="row-actions">` holding two inline forms whose
  method is POST, each with one `<button class="ghost small">`. The first's
  action is the token's disable URL (`/tokens/<id>/disable`) with the button
  reading `Disable` when the token is enabled, or its enable URL
  (`/tokens/<id>/enable`) with the button reading `Enable` when it is
  disabled. The second's action is the token's delete URL
  (`/tokens/<id>/delete`) with the button reading `Delete`.

No plaintext secret appears anywhere on the page.

Preconditions:

- The user is signed in; the cookie names a live session.
- The user owns the six example tokens above, and no others.
- The page is drawn at `2026-09-28 10:00:00 UTC`.

Postconditions:

- No token is created, changed, or removed.

## A user reads how long ago each token was last used

The `Last used` cell says how long ago the token was last used, measured from
the moment the page is drawn and rounded down to a whole unit, so the user
sees recency at a glance and can hover for the exact time. Under a minute
reads `just now`; from a minute to under an hour, `1 minute ago` or
`<n> minutes ago`; from an hour to under a day, `1 hour ago` or
`<n> hours ago`; from a day on, `1 day ago` or `<n> days ago`. Days is the
largest unit: a token last used a year ago reads `365 days ago`. In the
example the page is drawn at `2026-09-28 10:00:00 UTC` and the user owns
eight tokens, each created `2025-09-01 00:00 UTC`, enabled, and never
expiring, last used as follows:

```
name              last used (UTC)
used-59s          2026-09-28 09:59:01
used-1m           2026-09-28 09:59:00
used-59m59s       2026-09-28 09:00:01
used-1h           2026-09-28 09:00:00
used-23h59m59s    2026-09-27 10:00:01
used-24h          2026-09-27 10:00:00
used-2d           2026-09-26 09:59:59
used-365d         2025-09-28 10:00:00
```

Request:

```
$ curl -si http://localhost:3001/ \
    -H 'Cookie: ikigenba_session=<id>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the profile page (S3), whose `API tokens` table has
one row for each of the eight tokens, in the order listed above. Each row's
`Last used` cell is
`<time datetime="<RFC 3339 time in UTC>" title="YYYY-MM-DD HH:MM UTC"><elapsed></time>`,
and reads, row by row:

- `used-59s`: `just now`, titled `2026-09-28 09:59 UTC`, with
  `datetime="2026-09-28T09:59:01Z"`.
- `used-1m`: `1 minute ago`, titled `2026-09-28 09:59 UTC`.
- `used-59m59s`: `59 minutes ago`, titled `2026-09-28 09:00 UTC`.
- `used-1h`: `1 hour ago`, titled `2026-09-28 09:00 UTC`.
- `used-23h59m59s`: `23 hours ago`, titled `2026-09-27 10:00 UTC`.
- `used-24h`: `1 day ago`, titled `2026-09-27 10:00 UTC`.
- `used-2d`: `2 days ago`, titled `2026-09-26 09:59 UTC`.
- `used-365d`: `365 days ago`, titled `2025-09-28 10:00 UTC`.

Preconditions:

- The user is signed in; the cookie names a live session.
- The user owns the eight example tokens above, and no others.
- The page is drawn at `2026-09-28 10:00:00 UTC`.

Postconditions:

- No token is created, changed, or removed.

## A user with no tokens opens the profile

A user who has created no tokens, or has deleted them all, sees why the card
is there and what to do next rather than an empty table.

Request:

```
$ curl -si http://localhost:3001/ \
    -H 'Cookie: ikigenba_session=<id>'
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the profile page (S3), whose
`<section class="card flush">` headed `API tokens` carries the same header
text as when tokens exist, and in place of the table holds a
`<div class="empty">` containing an `<h3>` reading `No tokens yet` and the
text `Create one below when a script or tool needs to act as you.` The page
has no token table and no token row.

Preconditions:

- The user is signed in; the cookie names a live session.
- The user owns no tokens.

Postconditions:

- Nothing has changed.

## A user disables a token and enables it again

The enabled toggle is flipped by posting to the token's disable or enable URL.
Each returns the user to the profile. Whether a disabled token is refused by
the check endpoint is S4's story; it is not re-proven here.

Request:

```
$ curl -si -X POST http://localhost:3001/tokens/<id>/disable \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001'
```

```
$ curl -si -X POST http://localhost:3001/tokens/<id>/enable \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001'
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
$ curl -si -X POST http://localhost:3001/tokens/<id>/delete \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001'
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
$ curl -si -X POST http://localhost:3001/tokens/<id>/disable \
    -H 'Cookie: ikigenba_session=<id>' \
    -H 'Origin: http://localhost:3001'
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
$ curl -si -X POST http://localhost:3001/tokens \
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
