# Stories — tokens

The token actions a signed-in user drives from their profile. The requests go
to a running auth (`S2-serve.md`), started with its Google settings and
`IKIGENBA_PUBLIC_URL` unset, as on a host; they reach it through nginx on a
space. Each request is shown as the HTTP request auth receives, with the
headers the story depends on. Every request is on a space; a request that shows
no `Host` header carries `Host: auth.sbx.ikigenba.dev`, on the space
`sbx.ikigenba.dev` (`S3-sign-in.md`). Every request carries a valid
`ikigenba_session` cookie, and every state-changing request is a POST that also
carries an `Origin` header matching the service's own origin:
`IKIGENBA_PUBLIC_URL` when it is set, as in a sandbox (`S9-in-a-sandbox.md`),
and otherwise `https://auth.<space>`, here `https://auth.sbx.ikigenba.dev`. A
user may hold many tokens. A token's secret has the form `ikp_` followed by 52
Crockford base32 characters (`0`-`9` and `A`-`Z` without `I`, `L`, `O`, `U`),
the encoding of 32 random bytes; it is shown once at creation and never again,
and only a hash of it is stored. Separately, each token carries its own id,
`tok_` followed by 26 random Crockford base32 characters, written `<token-id>`
below; it is the segment of the action URLs (`POST /tokens/<token-id>/enable`,
`/disable`, `/delete`), and it is not the secret. Every action is a POST:
acting on a token id that is not the user's own or does not exist answers 404,
and a POST whose `Origin` is not the service's own origin answers 403. Those
failures, like every text/plain failure auth answers, are one line of plain
text with no banner. The whole profile page is S3's story; the stories here fix
the token table and its empty state, the `Create a token` card, and the two
pages token creation draws.

Every request here is in the trail through the request events every request
records (`S2-serve.md`). An action that changes a token also records one token
event, under the request's id and the acting user's id: `token.minted` when a
token is created, `token.disabled`, `token.enabled`, and `token.deleted` when
one is disabled, enabled, or deleted. Each carries exactly one attribute,
`token`, the token's `<token-id>`. None carries the token's secret, its name,
or its expiry. A request that changes no token — a rejected submission, a 404,
a 403, or a 500 because auth's database failed (`S4-check.md`) — records no
token event.

Every HTML page these stories fix is drawn with the banner (S3): its
`<title>` is `auth`, it links `/_appkit/theme.css` as its stylesheet, links
`/_appkit/favicon.svg` as its icon, loads `/_appkit/feedback.js`, so that in
a browser an enabled button visibly reacts as the user presses it (S3), and declares the phone-width viewport, it opens
with the banner — when auth's services file lists services with an icon, the
launcher (S3), whose button comes first, immediately before the mark; then
the mark, the profile icon titled with the user's email as a link to `/`, and
the `Sign out` button
POSTing to `/logout` — its content sits in the page's one `<main>`, and it ends
with the page footer reading `auth <version>` (S3). A card is a
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
POST /tokens HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

name=<name>&expires=<never|30d|90d|365d>
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
inside that `<code>` element: the button carries no copy of it. In a browser,
pressing `Copy` puts the text of the `<code>` element, the secret, on the
clipboard and shows a toast reading `Copied to clipboard`; if copying fails,
the secret's text is selected instead and an error toast shows. That is the
button feedback script's doing (`S8-assets.md`); the page carries no script
of its own.
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
- The new token has a fresh id, `<token-id>`.
- auth records `token.minted` with `token=<token-id>`, under the request's id
  and the user's id.

## A user creates a token with no name

A name that is empty or only whitespace is not a valid name, so no token is
created and the create form is returned for the user to try again.

Request:

```
POST /tokens HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

name=%20%20%20&expires=never
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

- No token record is created. auth records no `token.minted`.

## A user creates a token with a name longer than 64 characters

64 characters is the limit, so 65 is a rejection. The input's `maxlength` stops
a browser from typing more, but a caller that is not a browser sends what it
likes, so auth checks the length itself. The name below is 65 characters.

Request:

```
POST /tokens HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

name=a-token-name-that-runs-well-past-the-sixty-four-character-limit-x&expires=90d
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

- No token record is created. auth records no `token.minted`.

## A caller creates a token with an expiry that is not offered

A person using the browser cannot reach this rejection: the expiry field is a
select offering `30d`, `90d`, `365d`, and `never`. A caller that is not a
browser sends whatever it likes, which is why auth checks the value against the
four rather than trusting that the form produced it. A missing `expires` is
rejected the same way.

Request:

```
POST /tokens HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

name=<name>&expires=1y
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

- No token record is created. auth records no `token.minted`.

## A caller creates a token with both fields wrong

Every field is checked and every field that is wrong is reported, so a
caller who got both wrong learns both from one answer instead of one
submission at a time.

Request:

```
POST /tokens HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
Content-Type: application/x-www-form-urlencoded

name=%20%20%20&expires=1y
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

- No token record is created. auth records no `token.minted`.

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
GET / HTTP/1.1
Cookie: ikigenba_session=<id>
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
  action is the token's disable URL (`/tokens/<token-id>/disable`) with the
  button reading `Disable` when the token is enabled, or its enable URL
  (`/tokens/<token-id>/enable`) with the button reading `Enable` when it is
  disabled. The second's action is the token's delete URL
  (`/tokens/<token-id>/delete`) with the button reading `Delete`.

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
GET / HTTP/1.1
Cookie: ikigenba_session=<id>
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
GET / HTTP/1.1
Cookie: ikigenba_session=<id>
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
POST /tokens/<token-id>/disable HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
```

```
POST /tokens/<token-id>/enable HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
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
- The `<token-id>` names a token the user owns; before the first request it is
  enabled.

Postconditions:

- After the disable request the token is disabled; after the enable request it
  is enabled again. A disabled token is rejected by `/check` exactly as an
  unknown one is (S4).
- The disable request records `token.disabled` and the enable request records
  `token.enabled`, each with `token=<token-id>`, under its own request's id
  and the user's id.

## A user deletes a token

Deleting a token revokes it: the record is removed and the secret can no longer
authenticate.

Request:

```
POST /tokens/<token-id>/delete HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
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
- The `<token-id>` names a token the user owns.

Postconditions:

- The token record is gone. Its secret no longer authenticates any request
  (S4).
- auth records `token.deleted` with `token=<token-id>`, under the request's id
  and the user's id.

## A user acts on a token that is not theirs

A token id that belongs to another user, or that names no token at all, is not
the user's to act on. All three actions behave the same way.

Request:

```
POST /tokens/<token-id>/disable HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://auth.sbx.ikigenba.dev
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
- The `<token-id>` names a token owned by a different user or names no token.

Postconditions:

- Nothing has changed. auth records no token event.

## Another site posts to the profile

A cross-site POST is refused on two lines: `SameSite=Lax` already keeps the
session cookie from riding a cross-site request, and, as the explicit second
line, a POST whose `Origin` is not the service's own origin is refused
outright. This applies to `/tokens` and to every token action URL.

Request:

```
POST /tokens HTTP/1.1
Cookie: ikigenba_session=<id>
Origin: https://evil.example
Content-Type: application/x-www-form-urlencoded

name=<name>&expires=never
```

Response:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain; charset=utf-8
```

Status 403. A POST to any token action URL (`/tokens/<token-id>/enable`,
`/disable`, `/delete`) with a foreign `Origin` is refused the same way.

Preconditions:

- The cookie names a live session (this is what a cross-site attacker would
  hope to ride).
- The `Origin` header is some origin other than the service's own.

Postconditions:

- Nothing has changed. auth records no token event.

## A user's token from before ids were prefixed keeps working

An auth that predates the `tok_` prefix gave a token a bare 26-character
Crockford id. auth's migration `0002` gives each such token the prefixed id,
`tok_` followed by the same 26 characters, and changes nothing else about it,
so a token minted before keeps its secret, name, times, and state, and keeps
authenticating. auth applies `0002` when it starts on a database that has not
had it (`S2-serve.md`), and applies it once: starting again changes nothing
more, and `auth db status` lists it applied (`S1-bootstrap.md`). From then on
the token's URLs and events name it by the prefixed id; its bare id names no
token, so an action URL carrying it answers 404 as any unknown id does.

Request:

```
GET / HTTP/1.1
Cookie: ikigenba_session=<id>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the profile page (S3) whose `API tokens` table holds
a row for the token, as in `A user lists their tokens`, with its name,
times, and status as they were. The row's forms POST to
`/tokens/tok_<bare-id>/disable` and `/tokens/tok_<bare-id>/delete`, where
`<bare-id>` is the 26-character id the token had before.

Preconditions:

- auth's database was written by an earlier auth, has not had migration
  `0002` applied, and holds a token the user owns, enabled, whose id is the
  bare `<bare-id>` and whose secret is `ikp_<token>`.
- auth has since been started on that database and is serving.
- The user is signed in; the cookie names a live session.

Postconditions:

- Nothing has changed by this request. The token's id has been
  `tok_<bare-id>` since auth started on the database and applied `0002`; its
  secret, name, times, and state are unchanged.
- `ikp_<token>` still authenticates: `/check` honors it as in `nginx checks a
  request with a token` (S4), and records `check.allowed` with
  `token=tok_<bare-id>`.
