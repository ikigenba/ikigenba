# D07-tokens

The token-management HTTP surface a signed-in Workspace member drives from
their profile: creating a personal access token, listing the personal tokens
they own, toggling one enabled or disabled, and deleting one; and listing the
MCP client tokens they hold and revoking one. These handlers live in
`internal/server` (D01) and are unexported; their contract is the observable
HTTP behaviour — method, path, status, fixed headers, and the template and
data a page is drawn from — and nothing about how the handlers are written. Every name they
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
them by name without restating them: an **auth page**, a page **drawn with the
banner** for a user, the **auth page data** the template `page` receives, and
the **profile** with its **profile data**. This design fixes the parts of that
data that concern tokens, and the two pages of its own, the token-created page
and the rejected-create page, which `page` draws through `chrome` alone in
main, with the banner. A page is the template's output for the data stated, so
a test renders `page` itself with that data and compares the bytes; the words,
the markup and the targets of the profile's forms are the templates'.

## The tokens on the profile

The profile shows each of the user's personal tokens as a **token row**: its
id, its name, its created and expiry times, its last use, and whether it is
enabled. D04's `ListTokens` returns both kinds; the personal ones are the
profile data's rows, drawn by the template `tokenList`, and the client ones
appear only in the MCP clients card. The rows run in **profile order**: most
recently used first, so the token used last is at the top; tokens last used at
the same instant run newest first by created time; and tokens never used come
after every used one, newest first by created time among themselves.
`ListTokens` promises which tokens it returns and not their order, so the order
is the profile's own; two tokens that tie on both times may stand in either
order.

A time reaches a template already written out, twice: for a machine, in UTC as
RFC 3339's `date-time` (§5.6: `full-date "T" full-time`, with `partial-time`
requiring seconds and `time-offset` allowing `"Z"`), in whole seconds with
`"Z"` as the offset; and for a person, to the minute. A token's created and
expiry times are each a **token time** holding both, drawn by `tokenTime`; a
missing expiry or last use is drawn by `tokenNever`. A last use, drawn by
`tokenLastUsed`, also carries its **elapsed data**: how long before the page
was drawn the token was last used, as a unit and a count, the largest whole
unit of minutes, hours or days, with days the largest, and the unit `now` for
under a minute. The units are identifiers, not words: the template `elapsed`
chooses the words, singular or plural, from them, so no word of it is in Go or
in this design. The page's draw time is one reading of the server's injected
clock, the `Now` of the `server.Config` it was built with (D03), so a test
fixes it exactly.

A personal token is enabled, disabled, or deleted by posting to its URL, which
the template builds from the row's id and whether it is enabled: what the
design fixes is that the row carries the token's own id, `tok_` followed by a
random Crockford id (the `Token.ID` field, D04), never its secret, which needs
no escaping in a path, and that posting to that path's enable, disable, or
delete action acts on that token. No plaintext secret appears anywhere on the
profile. Posting to a token's enable, disable, or delete URL returns the user
to the profile; a segment naming a token the user does not own, one of the
user's client tokens, or no token at all, is answered the same way delete and
toggle alike — a 404 — because the store cannot distinguish "not yours" from
"does not exist" and treats a token of the other kind as neither. A token an
earlier auth minted with a bare id has carried the prefixed id since auth
applied migration `0002` (D04), so the profile shows the prefixed id, and an
action URL still carrying the bare id names no token and is answered 404 like
any unknown id.

## The MCP clients on the profile

The profile's MCP clients card lists the user's client tokens, one entry per
approval, so approving a client again adds an entry and leaves the earlier ones
as they were. It is the human-authored template `mcp-clients` of auth's
template set (D01), which `profile` draws with the profile data's **MCP clients
data**. The data is one member, `Clients`, a list in profile order with one
entry per client token, each carrying the token's id and name and its times
already written out: the approved time (the token's `CreatedAt`) and the
expiry, each for a machine and for a person, the last-used time for a machine
and for a person and its elapsed data (or empty strings and zero elapsed data
when never used), and whether the token has expired, which is when its expiry
is not after the draw time. The card offers no enable, disable, delete, or
create, and shows no secret.

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

A personal token is created by posting `name` and `expires` to `/tokens`, as a
form: auth reads both from the request's form, the body as
`application/x-www-form-urlencoded` a browser submits. A `name` is valid when
it is 1 to 64 characters once leading and trailing whitespace is trimmed; an
`expires` is valid when it is one of the four `store.Expiry` values, and a
missing one is invalid like any other. The profile's create card, the template
`tokenCreate`, is drawn with the **empty create data**, which selects the
90-day choice.

A rejected submission is answered 400 with a page drawn with the banner, the
rejected-create page, which draws the same card with the **rejected create
data**: the name exactly as submitted, so the caller can correct it, the
submitted expiry when it is one of the four values and otherwise the 90-day
choice, and which of the two was wrong. Which words say so is the template's.

A successful creation is answered 200 with its own page drawn with the banner,
the token-created page, drawn by `tokenCreated` with the created token's
trimmed name and its secret. The secret occurs exactly once in the page, and
nothing auth sends afterward carries it. The plaintext is the second return of
`CreateToken`, of the form `ikp_` followed by 52 Crockford base32 characters;
only its hash is stored. Copying the secret from the page is the work of
appkit's button feedback script, which is appkit's, fixed by appkit's design
and not by this one, and no test here runs it: the gates have no JavaScript
engine.

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
- R-592E-M4CJ: auth's design defines that a token `a` **precedes** a token `b` **in profile order** when `a.LastUsedAt` is non-nil and `b.LastUsedAt` is nil; when both are non-nil and the time `a.LastUsedAt` points to is later than the time `b.LastUsedAt` points to; when both are non-nil, both point to the same instant, and `a.CreatedAt` is later than `b.CreatedAt`; and when both are nil and `a.CreatedAt` is later than `b.CreatedAt`; and in no other case; every requirement of auth's design that says a token precedes another in profile order MUST denote that.
- R-PWVP-KA8B: auth's design defines the profile's **draw time** as one value that a call of the `Now` field of the `server.Config` passed to `server.New` (D03) returned while the server handled the profile's request, the same value for every value the profile writes from it; every requirement of auth's design that names the profile's draw time MUST denote that.
- R-VV5F-YR1P: The `400 Bad Request` body R-VP1Y-1WC8 or R-VQ9U-FO2X requires — the **rejected-create page** — MUST be an auth page drawn with the banner for the cookie's user.
- R-VXL8-QAJ3: The `200 OK` body R-N5RR-K5GT requires — the **token-created page** — MUST be an auth page drawn with the banner for the cookie's user.
- R-W011-HU0H: The plaintext secret `CreateToken` (D04) returned for a successful `POST /tokens` MUST occur exactly once in the bytes of the token-created page, and MUST NOT occur in the bytes of any other response auth sends other than within a value a requirement of auth's design has auth write from that response's request or from a token's `Name`.
- R-TW3J-4Z41: For each `POST /tokens` auth answers `200 OK` (R-N5RR-K5GT), auth MUST record (D05) exactly one event named `token.minted`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, the owner of the created token, and whose attributes are exactly the one key `token` with the `string` value of the created token's `ID`.
- R-5HLP-AIJE: For each `POST /tokens/<id>/enable` (respectively `/disable`) auth answers `302 Found` (R-5BI7-DNTX) for which the token `<id>` names had `Enabled` false (respectively true) before the request, auth MUST record exactly one event named `token.enabled` (respectively `token.disabled`), whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`; for such a request whose token already had `Enabled` true (respectively false), auth MUST record no `token.enabled` or `token.disabled` event.
- R-5ITL-OAA3: For each `POST /tokens/<id>/delete` auth answers `302 Found` (R-5CQ3-RFKM), auth MUST record exactly one event named `token.deleted`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`.
- R-5OX3-L4ZK: For each `POST /tokens/<id>/revoke` auth answers `302 Found` (R-5MHA-TLI6), auth MUST record exactly one event named `token.revoked`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`.
- R-5K1I-220S: For a request whose path is `/tokens` or begins with `/tokens/`, auth MUST record no event other than its `request.started` and `request.finished` events and the events R-TW3J-4Z41, R-5HLP-AIJE, R-5ITL-OAA3, and R-5OX3-L4ZK require of it; so a request auth answers `400`, `403`, `404`, or `500`, and a toggle to the state the token already has, record no other event.
- R-14EN-8704: After `db.Open` with `auth.Migrations()` has given a token whose `ID` was a bare 26-character id `<bare>` the `ID` `tok_<bare>` (D04, R-F4KO-DW0N), a `POST /tokens/<bare>/enable`, `/disable`, or `/delete` carrying a valid `ikigenba_session` cookie of the token's owner and an `Origin` header equal to the service's own origin (D05) MUST respond `404 Not Found` and change nothing, and the same request naming `tok_<bare>` MUST act on the token as R-5BI7-DNTX and R-5CQ3-RFKM state.
- R-7DZ3-U9KX: The `internal/server` package MUST declare the unexported `type tokenRowData struct { ID, Name string; Created tokenTimeData; LastUsed *tokenLastUsedData; Expires *tokenTimeData; Enabled bool }`, with exactly these fields in this order.
- R-7F70-81BM: The `internal/server` package MUST declare the unexported `type tokenTimeData struct { Datetime, Text string }`, with exactly these fields in this order.
- R-7GEW-LT2B: The `internal/server` package MUST declare the unexported `type tokenLastUsedData struct { Datetime, Title string; Elapsed elapsedData }`, with exactly these fields in this order.
- R-7HMS-ZKT0: The `internal/server` package MUST declare the unexported `type elapsedData struct { Unit string; Count int64 }`, with exactly these fields in this order.
- R-7IUP-DCJP: The `internal/server` package MUST declare the unexported `type tokenCreateData struct { Name, Expiry string; Rejected, NameError, ExpiryError bool }`, with exactly these fields in this order.
- R-7K2L-R4AE: The `internal/server` package MUST declare the unexported `type tokenCreatedData struct { Name, Secret string }`, with exactly these fields in this order.
- R-7LAI-4W13: The `internal/server` package MUST declare the unexported `type mcpClientsData struct { Clients []mcpClientData }`, with exactly this field.
- R-7MIE-INRS: The `internal/server` package MUST declare the unexported `type mcpClientData struct { ID, Name, Approved, ApprovedText, LastUsed, LastUsedTitle string; LastUsedElapsed elapsedData; Expires, ExpiresText string; Expired bool }`, with exactly these fields in this order.
- R-7NQA-WFIH: auth's design defines, for a time `x`, its **datetime text** as `x` in UTC written as `YYYY-MM-DDTHH:MM:SSZ`, an RFC 3339 `date-time` (§5.6), whole seconds with any fraction dropped; its **minute text** as `x` in UTC written as `YYYY-MM-DD HH:MM UTC`, whole minutes with any seconds and fraction dropped; and its **token time** as the `tokenTimeData` whose `Datetime` is its datetime text and whose `Text` is its minute text; every requirement of auth's design that names any of these MUST denote that value.
- R-7OY7-A796: auth's design defines the **elapsed data** of a duration `e`, which may be negative, as the `elapsedData` whose `Unit` is `now` and whose `Count` is 0 when `e` is less than 60 seconds, every negative `e` included; whose `Unit` is `minute` and whose `Count` is the number of seconds in `e` divided by 60 and rounded down to a whole number when `e` is at least 60 seconds and less than 3600 seconds; whose `Unit` is `hour` and whose `Count` is the number of seconds in `e` divided by 3600 and rounded down to a whole number when `e` is at least 3600 seconds and less than 86400 seconds; and whose `Unit` is `day` and whose `Count` is the number of seconds in `e` divided by 86400 and rounded down to a whole number when `e` is at least 86400 seconds, however large that number is; every requirement of auth's design that names the elapsed data of a duration MUST denote that value.
- R-7Q63-NYZV: auth's design defines the **token row** of a token `t` for a time `d` as the `tokenRowData` whose `ID` is `t.ID`; whose `Name` is `t.Name` unaltered; whose `Created` is the token time (R-7NQA-WFIH) of `t.CreatedAt`; whose `LastUsed` is nil when `t.LastUsedAt` is nil and otherwise points to the `tokenLastUsedData` whose `Datetime` and `Title` are the datetime text and the minute text (R-7NQA-WFIH) of the time `t.LastUsedAt` points to and whose `Elapsed` is the elapsed data (R-7OY7-A796) of `d` minus that time; whose `Expires` is nil when `t.ExpiresAt` is nil and otherwise points to the token time of the time `t.ExpiresAt` points to; and whose `Enabled` is `t.Enabled`; every requirement of auth's design that names a token row MUST denote that value.
- R-7RE0-1QQK: auth's design defines the **MCP clients data** of a profile as the `mcpClientsData` whose `Clients` holds exactly one element for each token of `Kind` `store.TokenClient` (D04) among the tokens `ListTokens` (D04) returns for the profile's user, the element of a token `a` lying before that of a token `b` whenever `a` precedes `b` in profile order (R-592E-M4CJ); the element of a token `t` being the `mcpClientData` whose `ID` is `t.ID`; whose `Name` is `t.Name` unaltered; whose `Approved` and `ApprovedText` are the datetime text and the minute text (R-7NQA-WFIH) of `t.CreatedAt`; whose `LastUsed` and `LastUsedTitle` are the empty string and whose `LastUsedElapsed` is the zero `elapsedData` when `t.LastUsedAt` is nil, and otherwise are the datetime text and the minute text of the time `t.LastUsedAt` points to and the elapsed data (R-7OY7-A796) of the profile's draw time (R-PWVP-KA8B) minus that time; whose `Expires` and `ExpiresText` are the datetime text and the minute text of the time `t.ExpiresAt` points to; and whose `Expired` is true when, and only when, that time is not after the profile's draw time; every requirement of auth's design that names the MCP clients data MUST denote that value.
- R-3Z52-5TU2: auth's design defines the **trimmed form** of a text as what Go's `strings.TrimSpace` returns for it: the text with every leading and every trailing UTF-8 encoded character for which `unicode.IsSpace` reports true removed, a byte that is not part of a valid UTF-8 encoding counting as a character that is not a space and so ending the removal; and the **character count** of a text as what Go's `utf8.RuneCountInString` returns for it: the number of valid UTF-8 encoded characters it holds plus one for each byte that is not part of a valid UTF-8 encoding; every requirement of auth's design that speaks of a `name` after trimming leading and trailing whitespace, or of the number of characters in such a `name`, MUST denote the trimmed form and the character count.
- R-3T1K-8Z4L: auth's design defines the **submitted name** and the **submitted expiry** of a `POST /tokens` as the first values the request's `Form` holds, after Go's `Request.ParseForm`, for the keys `name` and `expires` respectively, each the empty string when it holds none, so that a body of type `application/x-www-form-urlencoded` supplies them; the **empty create data** as the `tokenCreateData` whose `Expiry` is `90d` and whose other fields are their zero values; and the **rejected create data** of a `POST /tokens` as the `tokenCreateData` whose `Name` is its submitted name exactly as submitted, untrimmed and unshortened; whose `Expiry` is its submitted expiry when that is one of `30d`, `90d`, `365d` and `never`, the values of the `store.Expiry` members (D04), and `90d` otherwise; whose `Rejected` is true; whose `NameError` is true when, and only when, the character count of the trimmed form (R-3Z52-5TU2) of its submitted name is less than 1 or more than 64; and whose `ExpiryError` is true when, and only when, its submitted expiry is none of `30d`, `90d`, `365d` and `never`; every requirement of auth's design that names any of these MUST denote that value.
- R-3U9G-MQVA: The rejected-create page (R-VV5F-YR1P) MUST be exactly the text that executing the template `page` of auth's template set (D01) writes with the `authPageData` whose `Banner` is the `page.Banner` that the server's one call of the `Banner` field of its `server.Config` while answering the request (R-PT80-EZ08) returned, whose `Create` points to the request's rejected create data (R-3T1K-8Z4L), and whose `SignIn`, `Profile` and `Created` are nil.
- R-3VHD-0ILZ: The token-created page (R-VXL8-QAJ3) MUST be exactly the text that executing the template `page` of auth's template set (D01) writes with the `authPageData` whose `Banner` is the `page.Banner` that the server's one call of the `Banner` field of its `server.Config` while answering the request (R-PT80-EZ08) returned, whose `Created` points to the `tokenCreatedData` whose `Name` is the request's submitted name's trimmed form (R-3T1K-8Z4L, R-3Z52-5TU2) and whose `Secret` is the plaintext secret `CreateToken` (D04) returned for the request, and whose `SignIn`, `Profile` and `Create` are nil.
- R-3WP9-EACO: For each element `w` of the `Rows` of a profile's profile data (D05), a `POST` whose path is `/tokens/` followed by `w.ID` and then `/disable` when `w.Enabled` is true or `/enable` when it is false, and a `POST` whose path is `/tokens/` followed by `w.ID` and `/delete`, each carrying a valid `ikigenba_session` cookie of the profile's user and an `Origin` header equal to the service's own origin (D05), each made while that user's tokens are exactly as that profile data describes them, MUST be answered as R-5BI7-DNTX and R-5CQ3-RFKM state for the token whose `ID` is `w.ID`, the first reversing that token's `Enabled`; and for each element `c` of the `Clients` of that profile data's `Clients`, such a `POST` whose path is `/tokens/` followed by `c.ID` and `/revoke` MUST be answered as R-5MHA-TLI6 states for the token whose `ID` is `c.ID`.
- R-41KU-XDBG: The profile (R-VK6C-ITDG) and the rejected-create page (R-VV5F-YR1P), their bytes read as the HTML Standard parses an HTML document, MUST each hold, among the forms they hold, a form whose method of submission is `POST` and whose target is `/tokens`, whatever other forms the page holds.
- R-8157-3WO4: For each element `w` of the `Rows` of the profile data (R-3LQ5-YCOF) a profile is drawn from, that profile's bytes, read as the HTML Standard parses an HTML document, MUST hold, among the forms they hold, a form whose method of submission is `POST` and whose target is `/tokens/` followed by `w.ID` and then `/disable` when `w.Enabled` is true or `/enable` when it is false, and a form whose method of submission is `POST` and whose target is `/tokens/` followed by `w.ID` and `/delete`; and for each element `c` of the `Clients` of that profile data's `Clients`, a form whose method of submission is `POST` and whose target is `/tokens/` followed by `c.ID` and `/revoke`; whatever other forms the page holds.
