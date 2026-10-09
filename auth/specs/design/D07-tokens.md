# D07-tokens

The token-management HTTP surface a signed-in Workspace member drives from
their profile: creating a personal access token, listing the personal tokens
they own, toggling one enabled or disabled, and deleting one; and listing the
MCP client tokens they hold and revoking one. These handlers live in
`internal/server` (D01) and are unexported; their contract is the observable
HTTP behaviour — method, path, status, fixed headers, and the values the body
shows — and nothing about how the handlers are written. Every name they
lean on is already fixed elsewhere: the store operations and the `Token`/
`TokenKind`/`Expiry`/`Identity` shapes in D04, the `ikigenba_session` cookie and the
service's own origin in D05, the `idcodec` id and secret encodings in D04,
auth's template set in D01. D07 re-declares none of them.

auth holds two kinds of token, told apart by `Token.Kind` (D04). A **personal
token** (`store.TokenPersonal`) is one the user creates and manages from the
profile: enable, disable, delete. An **MCP client token** (`store.TokenClient`),
the client token for short, is minted only when the user approves an MCP client
at D09's `/authorize` and `/token`; it is named after the client, expires 90
days after the approval, and is managed from the profile's MCP clients card,
whose one action is to revoke it. Each kind's actions apply to that kind only:
a personal token's enable, disable, or delete URL carrying a client token's id,
and a client token's revoke URL carrying a personal token's id, are answered
404 like an unknown id. The store enforces the same split (`SetTokenEnabled`
and `DeleteToken` act only on personal tokens, `RevokeToken` only on client
tokens, D04), so the handlers need no check of their own.

Every request in this design carries a valid `ikigenba_session` cookie (D05
owns the cookie; this design only reads it to identify the acting user), and
every state-changing request is a POST that also carries an `Origin` header
equal to the service's own origin (D05 owns how that origin is derived: from
the request Host as `https://auth.<space>` on a host, and exactly
`IKIGENBA_PUBLIC_URL` in a sandbox that sets it). A POST whose `Origin` is not the service's own origin is refused
outright, and nothing is changed, the revoke URL included. That refusal and
the 404 for a token that is not the user's, or not of the action's kind, are
auth's own failures, not pages: one line of plain text, with no banner.

## Pages

D05 owns the terms every page auth draws is described in, and this design uses
them by name without restating them: an **auth page**, a value a page
**shows**, and a page **drawn with the banner** for a user. D05 also defines
the **profile**. The profile's token parts, the token-created page and the
rejected-create page are written by `internal/server` without an asset
template, so this design fixes of them only the route, the status, the headers,
and the values each shows. The profile's MCP clients card is the template
`mcp-clients` (D01).

## The tokens on the profile

The profile shows each of the user's personal tokens: its id, its name, and
its created, last-used and expiry times. D04's `ListTokens` returns both kinds;
the personal ones are shown as personal tokens, and the client ones only
through the MCP clients card. The tokens run in **profile order**, each
token's id first appearing in the page before the next one's: most
recently used first, so the token used last is at the top; tokens last used at
the same instant run newest first by created time; and tokens never used come
after every used one, newest first by created time among themselves.
`ListTokens` promises which tokens it returns and not their order, so the order
is the profile's own; two tokens that tie on both times may stand in either
order.

A personal token's times are shown in UTC as RFC 3339's `date-time` (§5.6:
`full-date "T" full-time`, with `partial-time` requiring seconds and
`time-offset` allowing `"Z"`), in whole seconds with `"Z"` as the offset; how
else the page writes them is its copy. The MCP clients card also carries each
time for a person, to the minute, and a used client token's last-used time as
its elapsed text: how long before the page was drawn it was last used, rounded
down to a whole unit of minutes, hours, or days, with days the largest unit,
in words that are copy constants named by the design and never quoted
(R-PKOP-QKTD, R-PDDB-FYD7). The page's draw time is one reading of the
server's injected clock, the `Now` of the `server.Config` it was built with
(D03), so a test fixes it exactly.

A personal token is enabled, disabled, or deleted by posting to its URL. The
URL segment is the token's own id, `tok_` followed by a random Crockford id
(the `Token.ID` field, D04), never its secret, and no plaintext secret appears
anywhere on the profile. Posting to a token's enable, disable, or delete URL
returns the user to the profile; a segment naming a token the user does not
own, one of the user's client tokens, or no token at all, is answered the same
way delete and toggle alike — a 404 — because the store cannot distinguish
"not yours" from "does not exist" and treats a token of the other kind as
neither. A token an earlier auth minted with a bare id has carried the
prefixed id since auth applied migration `0002` (D04), so the profile shows
the prefixed id, and an action URL still carrying the bare id names no token
and is answered 404 like any unknown id.

## The MCP clients on the profile

The profile's MCP clients card lists the user's client tokens, one entry per
approval, so approving a client again adds an entry and leaves the earlier ones
as they were. It is the human-authored template `mcp-clients` of auth's
template set (D01), and the card is exactly what executing that template
writes with the profile's **MCP clients data**, which a test can render itself
and compare byte for byte. The data is one member, `Clients`, a list in
profile order with one entry per client token, each carrying the token's id
and name and its times already written out: the approved time (the token's
`CreatedAt`) and the expiry, each for a machine and for a person, the last-used
time for a machine, for a person, and as elapsed time (or empty strings when
never used), and whether the token has expired, which is when its expiry is not
after the draw time. The template decides nothing but where each value goes.
The card offers no enable, disable, delete, or create, and shows no secret.

Revoking signs the client out: `POST /tokens/<id>/revoke` removes the token
through `RevokeToken`, so its secret no longer authenticates anywhere, and
returns the user to the profile. Other tokens are untouched, another approval
of the same client included; a client comes back only through a new approval.

## What token actions record

Every request here is in the trail through its request events (D03). An
action that changes a token also records one token event, under the request's id and
the acting user's id, with the middleware's request id kept and the user
supplied by the handler, since auth's own pages carry no `X-User-Id`:
`token.minted` when a personal token is created; `token.disabled`,
`token.enabled`, or `token.deleted` when one is disabled, enabled, or
deleted; and `token.revoked` when a client token is revoked. A client token's
own `token.minted` is recorded at `/token` and is D09's. Disabling a token
already disabled, or enabling one already enabled,
still answers as a toggle does, but it changes no token, so it records no
event.
Each event carries exactly one attribute, `token`, the token's `tok_` id —
never the token's secret, its name, or its expiry. A request that changes no
token — a toggle to the state the token already has, a rejected submission, a 404, a 403, or a 500 because auth's database
failed — records no token event.

## Creating a token

A personal token is created by posting `name` and `expires` to `/tokens`. A
`name` is valid when it is 1 to 64 characters once leading and trailing
whitespace is trimmed; an `expires` is valid when it is one of the four
`store.Expiry` values, and a missing one is invalid like any other.

A rejected submission is answered 400 with a page drawn with the banner, the
rejected-create page, which shows the name the caller submitted, exactly as
submitted, so the caller can correct it.

A successful creation is answered 200 with its own page drawn with the banner,
the token-created page, which shows the created token's name and its secret.
The secret occurs exactly once in the page, and nothing auth sends afterward
carries it. The plaintext is the second return of `CreateToken`, of the form
`ikp_` followed by 52 Crockford base32 characters; only its hash is stored.
Copying the secret from the page is the work of appkit's button feedback
script, which is appkit's, fixed by appkit's design and not by this one, and no
test here runs it: the gates have no JavaScript engine.

## REQUIREMENTS

- R-N5RR-K5GT: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is 1..64 characters and whose `expires` is one of the `store.Expiry` members `ExpiryNever`, `Expiry30d`, `Expiry90d`, or `Expiry365d` (D04), MUST respond `200 OK` with `Content-Type: text/html; charset=utf-8` and MUST create exactly one token for the cookie's user by calling `CreateToken` (D04) with that trimmed `name` and the matching `store.Expiry`.
- R-VP1Y-1WC8: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is not 1..64 characters (empty, whitespace-only, or longer than 64), MUST respond `400 Bad Request` with `Content-Type: text/html; charset=utf-8` and MUST NOT call `CreateToken` (D04).
- R-VQ9U-FO2X: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is 1..64 characters but whose `expires` is missing or is not one of the `store.Expiry` members `ExpiryNever`, `Expiry30d`, `Expiry90d`, or `Expiry365d` (D04), MUST respond `400 Bad Request` with `Content-Type: text/html; charset=utf-8` and MUST NOT call `CreateToken` (D04).
- R-5BI7-DNTX: A `POST /tokens/<id>/enable` (respectively `/disable`) carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token of `Kind` `store.TokenPersonal` (D04) the cookie's user owns, MUST call `SetTokenEnabled` (D04) with `enabled` true (respectively false) and respond `302 Found` with `Location: /`.
- R-5CQ3-RFKM: A `POST /tokens/<id>/delete` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token of `Kind` `store.TokenPersonal` (D04) the cookie's user owns, MUST remove the token by calling `DeleteToken` (D04) and respond `302 Found` with `Location: /`; afterward the token no longer appears among `ListTokens` for that user and its secret authenticates no request.
- R-5DY0-57BB: A `POST /tokens/<id>/enable`, `/disable`, or `/delete` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names no token, names a token another user owns, or names a token of `Kind` `store.TokenClient` (D04) the cookie's user owns, MUST respond `404 Not Found` with `Content-Type: text/plain; charset=utf-8` and MUST change nothing: every token keeps its `Enabled` and stays among the tokens `ListTokens` returns for its owner.
- R-5MHA-TLI6: A `POST /tokens/<id>/revoke` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token of `Kind` `store.TokenClient` (D04) the cookie's user owns, MUST remove that token by calling `RevokeToken` (D04) with the cookie's user's id and `<id>`, and MUST respond `302 Found` with `Location: /`; afterward that token no longer appears among `ListTokens` for that user and its secret authenticates no request, and every other token, another of that user's tokens of `Kind` `store.TokenClient` with the same `Name` included, is as it was before the request.
- R-5NP7-7D8V: A `POST /tokens/<id>/revoke` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names no token, names a token another user owns, or names a token of `Kind` `store.TokenPersonal` (D04) the cookie's user owns, MUST respond `404 Not Found` with `Content-Type: text/plain; charset=utf-8` and MUST change nothing: every token stays among the tokens `ListTokens` returns for its owner with its `Enabled` as it was.
- R-5F5W-IZ20: A `POST /tokens`, or a `POST /tokens/<id>/enable`, `/disable`, `/delete`, or `/revoke`, carrying no `Origin` header or one whose value is not equal to the service's own origin (D05) MUST respond `403 Forbidden` with `Content-Type: text/plain; charset=utf-8` and MUST NOT call `CreateToken`, `SetTokenEnabled`, `DeleteToken`, or `RevokeToken` (D04) — nothing is changed.
- R-5GDS-WQSP: The body of every `404 Not Found` response R-5DY0-57BB or R-5NP7-7D8V requires and of every `403 Forbidden` response R-5F5W-IZ20 requires MUST be a single line of plain text: one or more bytes none of which is `\n` or `\r`, followed by a single `\n`.
- R-VRHQ-TFTM: For each token `t` of `Kind` `store.TokenPersonal` that `ListTokens` (D04) returns for the profile's user, the profile (D05) MUST show (D05) `t.ID` and `t.Name`.
- R-592E-M4CJ: auth's design defines that a token `a` **precedes** a token `b` **in profile order** when `a.LastUsedAt` is non-nil and `b.LastUsedAt` is nil; when both are non-nil and the time `a.LastUsedAt` points to is later than the time `b.LastUsedAt` points to; when both are non-nil, both point to the same instant, and `a.CreatedAt` is later than `b.CreatedAt`; and when both are nil and `a.CreatedAt` is later than `b.CreatedAt`; and in no other case; every requirement of auth's design that says a token precedes another in profile order MUST denote that.
- R-T8UC-H064: For any two tokens `a` and `b` of `Kind` `store.TokenPersonal` that `ListTokens` (D04) returns for the profile's user, where `a` precedes `b` in profile order and no value the profile shows, other than `a.ID` and `b.ID` themselves, contains `a.ID` or `b.ID`, the first occurrence of `a.ID` in the profile's bytes MUST lie before the first occurrence of `b.ID`.
- R-PWVP-KA8B: auth's design defines the profile's **draw time** as one value that a call of the `Now` field of the `server.Config` passed to `server.New` (D03) returned while the server handled the profile's request, the same value for every value the profile writes from it; every requirement of auth's design that names the profile's draw time MUST denote that.
- R-PKOP-QKTD: The `internal/server` package MUST export the copy constants `const ElapsedJustNow string`, `const ElapsedMinute string`, `const ElapsedHour string`, and `const ElapsedDay string`, each non-empty, and `const ElapsedMinutes string`, `const ElapsedHours string`, and `const ElapsedDays string`, each a `fmt` format string holding exactly one verb, `%d`, and no other verb.
- R-PDDB-FYD7: auth's design defines the **elapsed text** of a duration `e`, which may be negative, as: `ElapsedJustNow` (R-PKOP-QKTD) when `e` is less than 60 seconds, every negative `e` included; when `e` is at least 60 seconds and less than 3600 seconds, with `n` the number of seconds in `e` divided by 60 and rounded down to a whole number, `ElapsedMinute` when `n` is 1 and `fmt.Sprintf(ElapsedMinutes, n)` otherwise; when `e` is at least 3600 seconds and less than 86400 seconds, with `n` the number of seconds in `e` divided by 3600 and rounded down to a whole number, `ElapsedHour` when `n` is 1 and `fmt.Sprintf(ElapsedHours, n)` otherwise; and when `e` is at least 86400 seconds, with `n` the number of seconds in `e` divided by 86400 and rounded down to a whole number, `ElapsedDay` when `n` is 1 and `fmt.Sprintf(ElapsedDays, n)` otherwise, however large `n` is; so that `n` is written in ASCII decimal digits with no leading zero; every requirement of auth's design that names the elapsed text of a duration MUST denote that.
- R-VSPN-77KB: For each token `t` of `Kind` `store.TokenPersonal` that `ListTokens` (D04) returns for the profile's user, the profile MUST show `t.CreatedAt` in UTC written as an RFC 3339 `date-time` (§5.6) in the form `YYYY-MM-DDTHH:MM:SSZ`, whole seconds with any fraction dropped, and, written the same way, the time `t.LastUsedAt` points to when it is non-nil and the time `t.ExpiresAt` points to when it is non-nil.
- R-5RCW-COGY: auth's design defines the **MCP clients data** of a profile as a value of a struct type or of type `map[string]any` whose fields, or keys, are exactly `Clients`, holding a slice with exactly one element for each token of `Kind` `store.TokenClient` (D04) among the tokens `ListTokens` (D04) returns for the profile's user, the element of a token `a` lying before that of a token `b` whenever `a` precedes `b` in profile order; each element being a value of a struct type or of type `map[string]any` whose fields, or keys, are exactly `ID`, the token's `ID`; `Name`, the token's `Name` unaltered; `Approved`, the token's `CreatedAt` in UTC written as `YYYY-MM-DDTHH:MM:SSZ`, whole seconds with any fraction dropped; `ApprovedText`, the token's `CreatedAt` in UTC written as `YYYY-MM-DD HH:MM UTC`, whole minutes with any seconds and fraction dropped; `LastUsed`, `LastUsedTitle`, and `LastUsedText`, each the empty string when the token's `LastUsedAt` is nil, and otherwise, respectively, the time `LastUsedAt` points to written as `Approved` is written, that time written as `ApprovedText` is written, and the elapsed text of the profile's draw time minus that time; `Expires` and `ExpiresText`, the time the token's `ExpiresAt` points to written as `Approved` and as `ApprovedText` are written; all of those of type `string`; and `Expired`, of type `bool`, true when, and only when, the time the token's `ExpiresAt` points to is not after the profile's draw time; every requirement of auth's design that names the MCP clients data MUST denote such a value.
- R-VTXJ-KZB0: The profile's bytes MUST contain exactly the text that executing the template `mcp-clients` of auth's template set (D01) writes with the profile's MCP clients data, so that the profile's MCP clients card is that template's output.
- R-VV5F-YR1P: The `400 Bad Request` body R-VP1Y-1WC8 or R-VQ9U-FO2X requires — the **rejected-create page** — MUST be an auth page drawn with the banner for the cookie's user.
- R-VWDC-CISE: When the request carries a `name`, the rejected-create page MUST show the request's submitted `name` exactly as submitted, untrimmed and unshortened.
- R-VXL8-QAJ3: The `200 OK` body R-N5RR-K5GT requires — the **token-created page** — MUST be an auth page drawn with the banner for the cookie's user.
- R-VYT5-429S: The token-created page MUST show the created token's `name` after trimming leading and trailing whitespace.
- R-W011-HU0H: The plaintext secret `CreateToken` (D04) returned for a successful `POST /tokens` MUST occur exactly once in the bytes of the token-created page, and MUST NOT occur in the bytes of any other response auth sends other than within a value a requirement of auth's design has auth write from that response's request or from a token's `Name`.
- R-TW3J-4Z41: For each `POST /tokens` auth answers `200 OK` (R-N5RR-K5GT), auth MUST record (D05) exactly one event named `token.minted`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, the owner of the created token, and whose attributes are exactly the one key `token` with the `string` value of the created token's `ID`.
- R-5HLP-AIJE: For each `POST /tokens/<id>/enable` (respectively `/disable`) auth answers `302 Found` (R-5BI7-DNTX) for which the token `<id>` names had `Enabled` false (respectively true) before the request, auth MUST record exactly one event named `token.enabled` (respectively `token.disabled`), whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`; for such a request whose token already had `Enabled` true (respectively false), auth MUST record no `token.enabled` or `token.disabled` event.
- R-5ITL-OAA3: For each `POST /tokens/<id>/delete` auth answers `302 Found` (R-5CQ3-RFKM), auth MUST record exactly one event named `token.deleted`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`.
- R-5OX3-L4ZK: For each `POST /tokens/<id>/revoke` auth answers `302 Found` (R-5MHA-TLI6), auth MUST record exactly one event named `token.revoked`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`.
- R-5K1I-220S: For a request whose path is `/tokens` or begins with `/tokens/`, auth MUST record no event other than its `request.started` and `request.finished` events and the events R-TW3J-4Z41, R-5HLP-AIJE, R-5ITL-OAA3, and R-5OX3-L4ZK require of it; so a request auth answers `400`, `403`, `404`, or `500`, and a toggle to the state the token already has, record no other event.
- R-14EN-8704: After `db.Open` with `auth.Migrations()` has given a token whose `ID` was a bare 26-character id `<bare>` the `ID` `tok_<bare>` (D04, R-F4KO-DW0N), a `POST /tokens/<bare>/enable`, `/disable`, or `/delete` carrying a valid `ikigenba_session` cookie of the token's owner and an `Origin` header equal to the service's own origin (D05) MUST respond `404 Not Found` and change nothing, and the same request naming `tok_<bare>` MUST act on the token as R-5BI7-DNTX and R-5CQ3-RFKM state.
