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
enabled. The personal tokens are the profile data's rows, drawn by the
template `tokenList`, which receives the whole profile data and reads its
`Rows`, `Page`, `Prev` and `Next`, and its MCP clients data's `ClientsPage`; the client
tokens appear only in the MCP clients card. The
rows run in **profile order**, which D04 defines because the store reads in
it: most recently used first, so the token used last is at the top; tokens
last used at the same instant run newest first by created time; tokens never
used come after every used one, newest first by created time among
themselves; and tokens that tie on both times run in byte order of their ids,
so the order is total and a window is one set of tokens.

The profile's two lists navigate, as the suite's listing rule asks
(`shape/`, Listing and Storage; appkit's D25, cited and never restated), each
with First, Prev, Next and Last. Two lists share the page, so each has its own
query parameter: `tokens_page` for the personal tokens and `clients_page` for
the MCP client tokens. Each value is the first of its name in the query, and
nothing else of the query is read. A parameter holds one of four things, and
the four together are the values the page can decode: nothing, an absent or
empty parameter, which is the list's first window; an **after-token**, naming
the window after a position, which Next carries; a **before-token**, naming the
window before a position, which Prev carries; and the literal word `last`,
which decodes as no token and names the list's last window, which Last
carries. No tool lists either set, so the page issues its own tokens:
`paging.Token{Tool: "tokens"}` and `paging.Token{Tool: "clients"}` for the
after-tokens, and the same names with `-before` appended for the
before-tokens; the checksum keeps each apart from the other three, so one
parameter serves both directions and one list's token never reads as the
other's. Each window holds `paging.DefaultLimit` tokens, so the page size has
one home in the suite.

A token is the keyset position of one token of the list, the three fields of
profile order: whether it was ever used and when, its created time, and its
id. It carries four fields, in order: an `int64` that is 1 when the token was
used and 0 when never, an `int64` that is the last use in Unix nanoseconds (0
when never used), an `int64` that is the created time in Unix nanoseconds, and
the `string` id. The flag keeps the never-used tokens, which sort after every
used one, apart from any real time, and nanoseconds are the whole precision the
store keeps, so the position compares exactly. An after-token names the last
token a window shows and a before-token the first, so a window starts just
after the last one shown or ends just before the first one shown, and never
repeats one or skips one. The handler decodes a parameter into D04's
`store.TokenPosition`. From the empty value or an after-token it reads forward
with D04's `ListTokens`, asking for one token more than the window and
learning from the extra one that more remain. From a before-token it reads
backward with D04's `ListTokensBefore`, the tokens before the position nearest
first, again one more than the window, learning from the extra one whether the
window it shows is the first, and shows them in profile order; `last` reads
backward from the end, `ListTokensBefore` with no position. Either way it
never holds more than a window and one row, and the database filters by owner
and kind, seeks to the position, orders and windows, backed by the index
migration `0004` adds (D04).

A before-token or `last` whose window would be the list's first window,
because no more than `paging.DefaultLimit` tokens lie before its position or in
the whole list, **underflows**: the page does not draw the first window under
another name but answers `303 See Other` to `/`, with that list's parameter
dropped and the other list's kept as the request carried it, or with both
dropped when both underflow (D05). So Prev from the second window lands on the
first window's own URL, where First is the current item.

The profile data's `Page` is the request's tokens parameter, `last` included.
Its `Prev` is the before-token of the window's first token, empty on the first
window and on an empty window read forward. Its `Next` is the after-token of
the window's last token: on a window read forward only while more remain, on a
window read from a before-token always, and on the `last` window never. The MCP
clients data's `ClientsPage`, `ClientsPrev` and `ClientsNext` are the same for
the client tokens, and its `TokensPage` repeats the profile's `Page`, because
the clients card's links carry the tokens list's current token and the card's
template sees only its own data. Paging one list leaves the other where it
was: each window depends on its own parameter alone, and every link either nav
offers carries the other list's value.

A value the page cannot decode, in either parameter, is answered `404` (D05);
a token issued for the other list fails its checksum and is such a value, and
so is every word but `last`, `LAST` included. The
token actions keep their routes and their answers: each returns the user to
`/`, where both lists show their first windows.

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

An action on a token reads that one token, D04's `FindToken` by its owner and
id, to learn whether a toggle changes it; what it answers and records is
unchanged.

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
data**. The data is `Clients`, a list in profile order with one entry per
client token of the window `ClientsPage` names, then `ClientsPage`,
`ClientsPrev`, `ClientsNext` and `TokensPage`, the page tokens above. Each entry carries
the token's id and name and its times
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
- R-9U5P-OUMW: A `POST /tokens/<id>/delete` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token of `Kind` `store.TokenPersonal` (D04) the cookie's user owns, MUST remove the token by calling `DeleteToken` (D04) and respond `302 Found` with `Location: /`; afterward `FindToken` (D04) for that user and `<id>` returns `ErrNotFound` and the token's secret authenticates no request.
- R-9VDM-2MDL: A `POST /tokens/<id>/enable`, `/disable`, or `/delete` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names no token, names a token another user owns, or names a token of `Kind` `store.TokenClient` (D04) the cookie's user owns, MUST respond `404 Not Found` with `Content-Type: text/plain; charset=utf-8` and MUST change nothing: every token keeps its `Enabled`, and `FindToken` (D04) for its owner and its `ID` still returns it.
- R-9XTE-U5UZ: A `POST /tokens/<id>/revoke` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token of `Kind` `store.TokenClient` (D04) the cookie's user owns, MUST remove that token by calling `RevokeToken` (D04) with the cookie's user's id and `<id>`, and MUST respond `302 Found` with `Location: /`; afterward `FindToken` (D04) for that user and `<id>` returns `ErrNotFound` and that token's secret authenticates no request, and every other token, another of that user's tokens of `Kind` `store.TokenClient` with the same `Name` included, is as it was before the request.
- R-9Z1B-7XLO: A `POST /tokens/<id>/revoke` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names no token, names a token another user owns, or names a token of `Kind` `store.TokenPersonal` (D04) the cookie's user owns, MUST respond `404 Not Found` with `Content-Type: text/plain; charset=utf-8` and MUST change nothing: `FindToken` (D04) for each token's owner and its `ID` still returns it, with its `Enabled` as it was.
- R-5F5W-IZ20: A `POST /tokens`, or a `POST /tokens/<id>/enable`, `/disable`, `/delete`, or `/revoke`, carrying no `Origin` header or one whose value is not equal to the service's own origin (D05) MUST respond `403 Forbidden` with `Content-Type: text/plain; charset=utf-8` and MUST NOT call `CreateToken`, `SetTokenEnabled`, `DeleteToken`, or `RevokeToken` (D04) — nothing is changed.
- R-A097-LPCD: The body of every `404 Not Found` response R-9VDM-2MDL or R-9Z1B-7XLO requires and of every `403 Forbidden` response R-5F5W-IZ20 requires MUST be a single line of plain text: one or more bytes none of which is `\n` or `\r`, followed by a single `\n`.
- R-PWVP-KA8B: auth's design defines the profile's **draw time** as one value that a call of the `Now` field of the `server.Config` passed to `server.New` (D03) returned while the server handled the profile's request, the same value for every value the profile writes from it; every requirement of auth's design that names the profile's draw time MUST denote that.
- R-VV5F-YR1P: The `400 Bad Request` body R-VP1Y-1WC8 or R-VQ9U-FO2X requires — the **rejected-create page** — MUST be an auth page drawn with the banner for the cookie's user.
- R-VXL8-QAJ3: The `200 OK` body R-N5RR-K5GT requires — the **token-created page** — MUST be an auth page drawn with the banner for the cookie's user.
- R-W011-HU0H: The plaintext secret `CreateToken` (D04) returned for a successful `POST /tokens` MUST occur exactly once in the bytes of the token-created page, and MUST NOT occur in the bytes of any other response auth sends other than within a value a requirement of auth's design has auth write from that response's request or from a token's `Name`.
- R-TW3J-4Z41: For each `POST /tokens` auth answers `200 OK` (R-N5RR-K5GT), auth MUST record (D05) exactly one event named `token.minted`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, the owner of the created token, and whose attributes are exactly the one key `token` with the `string` value of the created token's `ID`.
- R-5HLP-AIJE: For each `POST /tokens/<id>/enable` (respectively `/disable`) auth answers `302 Found` (R-5BI7-DNTX) for which the token `<id>` names had `Enabled` false (respectively true) before the request, auth MUST record exactly one event named `token.enabled` (respectively `token.disabled`), whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`; for such a request whose token already had `Enabled` true (respectively false), auth MUST record no `token.enabled` or `token.disabled` event.
- R-A3WW-R0KG: For each `POST /tokens/<id>/delete` auth answers `302 Found` (R-9U5P-OUMW), auth MUST record exactly one event named `token.deleted`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`.
- R-A54T-4SB5: For each `POST /tokens/<id>/revoke` auth answers `302 Found` (R-9XTE-U5UZ), auth MUST record exactly one event named `token.revoked`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`.
- R-A6CP-IK1U: For a request whose path is `/tokens` or begins with `/tokens/`, auth MUST record no event other than its `request.started` and `request.finished` events and the events R-TW3J-4Z41, R-5HLP-AIJE, R-A3WW-R0KG, and R-A54T-4SB5 require of it; so a request auth answers `400`, `403`, `404`, or `500`, and a toggle to the state the token already has, record no other event.
- R-A1H3-ZH32: After `db.Open` with `auth.Migrations()` has given a token whose `ID` was a bare 26-character id `<bare>` the `ID` `tok_<bare>` (D04, R-9EB0-PTZV), a `POST /tokens/<bare>/enable`, `/disable`, or `/delete` carrying a valid `ikigenba_session` cookie of the token's owner and an `Origin` header equal to the service's own origin (D05) MUST respond `404 Not Found` and change nothing, and the same request naming `tok_<bare>` MUST act on the token as R-5BI7-DNTX and R-9U5P-OUMW state.
- R-7DZ3-U9KX: The `internal/server` package MUST declare the unexported `type tokenRowData struct { ID, Name string; Created tokenTimeData; LastUsed *tokenLastUsedData; Expires *tokenTimeData; Enabled bool }`, with exactly these fields in this order.
- R-7F70-81BM: The `internal/server` package MUST declare the unexported `type tokenTimeData struct { Datetime, Text string }`, with exactly these fields in this order.
- R-7GEW-LT2B: The `internal/server` package MUST declare the unexported `type tokenLastUsedData struct { Datetime, Title string; Elapsed elapsedData }`, with exactly these fields in this order.
- R-7HMS-ZKT0: The `internal/server` package MUST declare the unexported `type elapsedData struct { Unit string; Count int64 }`, with exactly these fields in this order.
- R-7IUP-DCJP: The `internal/server` package MUST declare the unexported `type tokenCreateData struct { Name, Expiry string; Rejected, NameError, ExpiryError bool }`, with exactly these fields in this order.
- R-7K2L-R4AE: The `internal/server` package MUST declare the unexported `type tokenCreatedData struct { Name, Secret string }`, with exactly these fields in this order.
- R-UXWT-OSGV: The `internal/server` package MUST declare the unexported `type mcpClientsData struct { Clients []mcpClientData; ClientsPage, ClientsPrev, ClientsNext, TokensPage string }`, with exactly these fields in this order.
- R-7MIE-INRS: The `internal/server` package MUST declare the unexported `type mcpClientData struct { ID, Name, Approved, ApprovedText, LastUsed, LastUsedTitle string; LastUsedElapsed elapsedData; Expires, ExpiresText string; Expired bool }`, with exactly these fields in this order.
- R-7NQA-WFIH: auth's design defines, for a time `x`, its **datetime text** as `x` in UTC written as `YYYY-MM-DDTHH:MM:SSZ`, an RFC 3339 `date-time` (§5.6), whole seconds with any fraction dropped; its **minute text** as `x` in UTC written as `YYYY-MM-DD HH:MM UTC`, whole minutes with any seconds and fraction dropped; and its **token time** as the `tokenTimeData` whose `Datetime` is its datetime text and whose `Text` is its minute text; every requirement of auth's design that names any of these MUST denote that value.
- R-7OY7-A796: auth's design defines the **elapsed data** of a duration `e`, which may be negative, as the `elapsedData` whose `Unit` is `now` and whose `Count` is 0 when `e` is less than 60 seconds, every negative `e` included; whose `Unit` is `minute` and whose `Count` is the number of seconds in `e` divided by 60 and rounded down to a whole number when `e` is at least 60 seconds and less than 3600 seconds; whose `Unit` is `hour` and whose `Count` is the number of seconds in `e` divided by 3600 and rounded down to a whole number when `e` is at least 3600 seconds and less than 86400 seconds; and whose `Unit` is `day` and whose `Count` is the number of seconds in `e` divided by 86400 and rounded down to a whole number when `e` is at least 86400 seconds, however large that number is; every requirement of auth's design that names the elapsed data of a duration MUST denote that value.
- R-7Q63-NYZV: auth's design defines the **token row** of a token `t` for a time `d` as the `tokenRowData` whose `ID` is `t.ID`; whose `Name` is `t.Name` unaltered; whose `Created` is the token time (R-7NQA-WFIH) of `t.CreatedAt`; whose `LastUsed` is nil when `t.LastUsedAt` is nil and otherwise points to the `tokenLastUsedData` whose `Datetime` and `Title` are the datetime text and the minute text (R-7NQA-WFIH) of the time `t.LastUsedAt` points to and whose `Elapsed` is the elapsed data (R-7OY7-A796) of `d` minus that time; whose `Expires` is nil when `t.ExpiresAt` is nil and otherwise points to the token time of the time `t.ExpiresAt` points to; and whose `Enabled` is `t.Enabled`; every requirement of auth's design that names a token row MUST denote that value.
- R-UZ4Q-2K7K: auth's design defines the **MCP clients data** of a profile (D05, R-VA3T-IHVT) as the `mcpClientsData` whose `Clients` holds, for each token of the clients window (R-UT18-5PI3, R-UVH0-X8ZH, R-UWOX-B0Q6) of the request's clients page value (R-UQLF-E60P) for the profile's user at one moment while the profile's request is answered, the element of that token, in that window's order, and nothing else; whose `ClientsPage` is the request's clients page value; whose `ClientsPrev` and `ClientsNext` are the clients prev token and the clients next token (R-UT18-5PI3, R-UVH0-X8ZH, R-UWOX-B0Q6) of that page value for that user at that same moment; and whose `TokensPage` is the request's tokens page value, so that it equals the `Page` of the profile data (D05, R-V7O0-QYEF); the element of a token `t` being the `mcpClientData` whose `ID` is `t.ID`; whose `Name` is `t.Name` unaltered; whose `Approved` and `ApprovedText` are the datetime text and the minute text (R-7NQA-WFIH) of `t.CreatedAt`; whose `LastUsed` and `LastUsedTitle` are the empty string and whose `LastUsedElapsed` is the zero `elapsedData` when `t.LastUsedAt` is nil, and otherwise are the datetime text and the minute text of the time `t.LastUsedAt` points to and the elapsed data (R-7OY7-A796) of the profile's draw time (R-PWVP-KA8B) minus that time; whose `Expires` and `ExpiresText` are the datetime text and the minute text of the time `t.ExpiresAt` points to; and whose `Expired` is true when, and only when, that time is not after the profile's draw time; every requirement of auth's design that names the MCP clients data MUST denote that value.
- R-UQLF-E60P: auth's design defines the **tokens page value** of a request `r` as the value that `r.URL.Query().Get("tokens_page")` returns and its **clients page value** as the value that `r.URL.Query().Get("clients_page")` returns, each the empty string when its URL carries no key of that name; defines a tokens page value `p` to be a **tokens after-token** when `p` is not empty and `paging.Token{Tool: "tokens"}.Decode(p, &u, &l, &c, &i)`, `u`, `l` and `c` being `int64`s and `i` a `string`, returns a nil error; a **tokens before-token** when `p` is not empty, is not a tokens after-token, and `paging.Token{Tool: "tokens-before"}.Decode(p, &u, &l, &c, &i)` returns a nil error; the **last value** when `p` is exactly `last` and is neither a tokens after-token nor a tokens before-token; **decodable** when it is empty, a tokens after-token, a tokens before-token or the last value; and **undecodable** otherwise; defines its **position** as a nil `*store.TokenPosition` (D04) when `p` is empty or the last value, and when `p` is a tokens after-token or a tokens before-token as a pointer to the `store.TokenPosition` whose `LastUsedAt` is nil when the `u` that call stores is 0 and otherwise points to `time.Unix(0, l)`, whose `CreatedAt` is `time.Unix(0, c)` and whose `ID` is `i`, `l`, `c` and `i` being as the call that accepted `p` stores them; and defines a clients page value to be a **clients after-token**, a **clients before-token**, the last value, decodable or undecodable, and its position, likewise with `paging.Token{Tool: "clients"}` and `paging.Token{Tool: "clients-before"}`; `paging` being appkit's package `github.com/ikigenba/ikigenba/appkit/paging` (appkit's D25); every requirement of auth's design that names a request's tokens or clients page value, a tokens or clients after-token or before-token, the last value, a page value being decodable or undecodable, or its position, MUST denote these; so that `/?tokens_page=bogus` has the undecodable tokens page value `bogus` and the decodable empty clients page value, `/?tokens_page=&tokens_page=bogus` the decodable empty tokens page value, `/?clients_page=last` the clients page value `last`, the last value, `/?tokens_page=LAST` and `/?tokens_page=last/` undecodable tokens page values, and a clients page value that `paging.Token{Tool: "tokens"}` or `paging.Token{Tool: "tokens-before"}` encodes an undecodable one unless the names `tokens` and `clients`, or `tokens-before` and `clients-before`, give it the same checksum.
- R-URTB-RXRE: auth's design defines, for a token `t`, its **tokens after-token** as `paging.Token{Tool: "tokens"}.Encode(f, l, c, t.ID)` and its **tokens before-token** as `paging.Token{Tool: "tokens-before"}.Encode(f, l, c, t.ID)`, `f`, `l` and `c` being `int64`s, `f` and `l` being 0 when `t.LastUsedAt` is nil and otherwise `f` being 1 and `l` the `UnixNano()` of the time `t.LastUsedAt` points to, and `c` being `t.CreatedAt.UnixNano()`; and its **clients after-token** and **clients before-token** likewise with `paging.Token{Tool: "clients"}` and `paging.Token{Tool: "clients-before"}`; `paging` being appkit's package `github.com/ikigenba/ikigenba/appkit/paging` (appkit's D25); every requirement of auth's design that names a token's tokens or clients after-token or before-token MUST denote these; so that a token's tokens after-token is a tokens after-token (R-UQLF-E60P), and its tokens before-token a tokens before-token, whose position holds that token's `LastUsedAt`, `CreatedAt` and `ID`, and likewise for its clients after-token and clients before-token.
- R-UT18-5PI3: auth's design defines, for the `*store.Store` `s` that the `Store` of the `server.Config` passed to `server.New` (D03) holds, a user id `u`, a tokens page value `p` (R-UQLF-E60P) that is empty or a tokens after-token, whose position is `a`, and a moment, the **tokens window** of `p` for `u` at that moment as what `s.ListTokens(u, store.TokenPersonal, a, paging.DefaultLimit)` (D04, R-E3ER-XX5M) returns at that moment; the **tokens next token** of `p` for `u` at that moment as the tokens after-token (R-URTB-RXRE) of the last token of that tokens window when `s.ListTokens(u, store.TokenPersonal, a, paging.DefaultLimit+1)` would return more than `paging.DefaultLimit` tokens at that moment, and as the empty string otherwise; and the **tokens prev token** of `p` for `u` at that moment as the empty string when `p` is empty or that tokens window is empty, and otherwise as the tokens before-token of the first token of that tokens window; defines the **clients window**, the **clients next token** and the **clients prev token** of a clients page value that is empty or a clients after-token likewise with `store.TokenClient` and the clients after-token and clients before-token; every requirement of auth's design that names any of these for such a page value MUST denote that value; so that over `paging.DefaultLimit` plus one personal tokens of a user the empty tokens page value's window holds the first `paging.DefaultLimit` of them in profile order, its tokens prev token is empty, and its tokens next token is a value `x` whose window holds the last alone, whose tokens next token is empty and whose tokens prev token is that last token's tokens before-token; over `paging.DefaultLimit` of them the empty value's tokens next token is empty; and over none its window is empty and its tokens next token and tokens prev token are empty.
- R-UVH0-X8ZH: auth's design defines, for `s` and a user id `u` as R-UT18-5PI3 has them, a tokens before-token `p` (R-UQLF-E60P) whose position is `b`, and a moment, that `p` **underflows** at that moment when `s.ListTokensBefore(u, store.TokenPersonal, b, paging.DefaultLimit+1)` (D04, R-UO5M-MMJB) would return no more than `paging.DefaultLimit` tokens at that moment; the **tokens window** of `p` for `u` at that moment as the tokens `s.ListTokensBefore(u, store.TokenPersonal, b, paging.DefaultLimit)` returns at that moment, in the reverse of the order it returns them, so in profile order; the **tokens next token** of `p` for `u` at that moment as the tokens after-token (R-URTB-RXRE) of the last token of that window when that window is not empty, and the empty string otherwise; and the **tokens prev token** of `p` for `u` at that moment as the tokens before-token of the first token of that window when `p` does not underflow at that moment, and the empty string otherwise; defines a clients before-token underflowing, and its clients window, clients next token and clients prev token, likewise with `store.TokenClient` and the clients after-token and clients before-token; every requirement of auth's design that names a before-token underflowing, or any of these for a before-token, MUST denote that; so that over `paging.DefaultLimit` plus two personal tokens of a user, in profile order, the tokens before-token of the last of them, and a tokens before-token whose position is no token's and lies in profile order between the last two, each do not underflow and each have as window the second of them to the second-last, as tokens next token the second-last's tokens after-token, and as tokens prev token the second's tokens before-token, which underflows.
- R-UWOX-B0Q6: auth's design says, for `s` and a user id `u` as R-UT18-5PI3 has them and a moment, that the last value (R-UQLF-E60P) as a tokens page value **underflows** at that moment when `s.ListTokensBefore(u, store.TokenPersonal, nil, paging.DefaultLimit+1)` (D04, R-UO5M-MMJB) would return no more than `paging.DefaultLimit` tokens at that moment, and that no tokens page value but a tokens before-token (R-UVH0-X8ZH) or the last value ever underflows; defines the **tokens window** of the last value for `u` at that moment as the tokens `s.ListTokensBefore(u, store.TokenPersonal, nil, paging.DefaultLimit)` returns at that moment, in the reverse of the order it returns them, so in profile order; its **tokens next token** as the empty string; and its **tokens prev token** as the tokens before-token (R-URTB-RXRE) of the first token of that window when the last value does not underflow at that moment, and the empty string otherwise; defines the last value as a clients page value underflowing, and its clients window, clients next token and clients prev token, likewise with `store.TokenClient` and the clients before-token, no clients page value but a clients before-token or the last value ever underflowing; every requirement of auth's design that names the last value underflowing, or any of these for the last value, MUST denote that; so that over `paging.DefaultLimit` client tokens of a user the last value as a clients page value underflows, and over `paging.DefaultLimit` plus two its clients window is the third of them to the last in profile order and its clients prev token the third's clients before-token, which underflows.
- R-V0CM-GBY9: For two `GET /` requests carrying live sessions of the same user, each with a decodable tokens page value and a decodable clients page value (R-UQLF-E60P) neither of which underflows (R-UVH0-X8ZH, R-UWOX-B0Q6), answered by one server while its store can be read, no token is created, changed or removed, and the `Now` of the `server.Config` it was built with returns the same time, the two profile data (D05, R-V7O0-QYEF) the answers are drawn from MUST have equal `Rows`, `Page`, `Prev` and `Next` when the two tokens page values are equal, whatever the two clients page values are, and `Clients` whose `Clients`, `ClientsPage`, `ClientsPrev` and `ClientsNext` are equal when the two clients page values are equal, whatever the two tokens page values are; so that, over more than twice `paging.DefaultLimit` personal tokens and more than twice `paging.DefaultLimit` client tokens, the requests whose clients page value is the same clients after-token `x` and whose tokens page values are, in turn, empty, a tokens after-token, a tokens before-token that does not underflow and `last` show the same clients window, and likewise with the two lists exchanged.
- R-V1KI-U3OY: For a user whose personal tokens number more than `paging.DefaultLimit`, whatever their last uses and created times, tokens never used and tokens last used at the same instant and created at the same instant included, and are not created, changed or removed while the requests below are answered, a server whose store can be read, and a clients page value `o` (R-UQLF-E60P) that is decodable and does not underflow (R-UVH0-X8ZH, R-UWOX-B0Q6) for that user, let the **forward walk** be the tokens page values `p0`, …, `pk` where `p0` is the empty string, each later one is the tokens next token (R-UT18-5PI3) of the one before it for that user, and `pk` is the first whose tokens next token is empty; and let the **backward walk** be the tokens page values `q0`, …, `qm` where `q0` is `pk`, each later one is the tokens prev token (R-UT18-5PI3, R-UVH0-X8ZH) of the one before it for that user, and `qm` is the first that underflows. Then `m` MUST equal `k`; a `GET /` carrying a live session of that user whose tokens page value is `qj` and whose clients page value is `o` MUST, for every `j` from 0 to `k − 1`, be answered `200` with the profile (D05, R-VBBP-W9MI) drawn from profile data whose `Rows` have, in order, the `ID`s of the tokens of the tokens window of `p(k−j)`; and such a request whose tokens page value is `qk` MUST be answered `303 See Other` (D05, R-VEZF-1KUL) with `Location: /` when `o` is empty and `Location: /?clients_page=` followed by `o` otherwise; and likewise for a user whose client tokens number more than `paging.DefaultLimit`, with the clients page value, the clients next token, clients prev token and clients window, the `Clients` of the profile data's `Clients`, a tokens page value `o`, and `Location: /?tokens_page=` followed by `o`; so that stepping Next from the first window of either list to its last and Prev back visits the same windows in the opposite order, keeping the other list's page value throughout, and Prev from the second window is a redirect to the first window's URL that keeps the other list's parameter.
- R-V2SF-7VFN: For a user whose personal tokens number more than `paging.DefaultLimit` and are not created, changed or removed while the request is answered, a server whose store can be read, and a clients page value (R-UQLF-E60P) that is decodable and does not underflow (R-UVH0-X8ZH, R-UWOX-B0Q6) for that user, a `GET /` carrying a live session of that user whose tokens page value is `last` MUST be answered `200` with the profile (D05, R-VBBP-W9MI) drawn from profile data whose `Rows` have, in order, the last `paging.DefaultLimit` of the `ID`s of the tokens of the tokens windows of the forward walk (R-V1KI-U3OY) taken in order, whose `Page` is `last`, whose `Next` is empty, and whose `Prev` is the tokens before-token (R-URTB-RXRE) of the first of those tokens; and likewise for a user whose client tokens number more than `paging.DefaultLimit`, with `clients_page`, a decodable tokens page value that does not underflow, the clients windows of the forward walk of the clients list, the `Clients`, `ClientsPage`, `ClientsNext` and `ClientsPrev` of the profile data's `Clients`, and the clients before-token; so that the last window is the final window of the Next walk when the tokens number a multiple of `paging.DefaultLimit`, and otherwise ends with it, and over `paging.DefaultLimit` plus two personal tokens the last tokens window is the third of them to the last in profile order.
- R-3Z52-5TU2: auth's design defines the **trimmed form** of a text as what Go's `strings.TrimSpace` returns for it: the text with every leading and every trailing UTF-8 encoded character for which `unicode.IsSpace` reports true removed, a byte that is not part of a valid UTF-8 encoding counting as a character that is not a space and so ending the removal; and the **character count** of a text as what Go's `utf8.RuneCountInString` returns for it: the number of valid UTF-8 encoded characters it holds plus one for each byte that is not part of a valid UTF-8 encoding; every requirement of auth's design that speaks of a `name` after trimming leading and trailing whitespace, or of the number of characters in such a `name`, MUST denote the trimmed form and the character count.
- R-3T1K-8Z4L: auth's design defines the **submitted name** and the **submitted expiry** of a `POST /tokens` as the first values the request's `Form` holds, after Go's `Request.ParseForm`, for the keys `name` and `expires` respectively, each the empty string when it holds none, so that a body of type `application/x-www-form-urlencoded` supplies them; the **empty create data** as the `tokenCreateData` whose `Expiry` is `90d` and whose other fields are their zero values; and the **rejected create data** of a `POST /tokens` as the `tokenCreateData` whose `Name` is its submitted name exactly as submitted, untrimmed and unshortened; whose `Expiry` is its submitted expiry when that is one of `30d`, `90d`, `365d` and `never`, the values of the `store.Expiry` members (D04), and `90d` otherwise; whose `Rejected` is true; whose `NameError` is true when, and only when, the character count of the trimmed form (R-3Z52-5TU2) of its submitted name is less than 1 or more than 64; and whose `ExpiryError` is true when, and only when, its submitted expiry is none of `30d`, `90d`, `365d` and `never`; every requirement of auth's design that names any of these MUST denote that value.
- R-RW49-5DMW: The rejected-create page (R-VV5F-YR1P) MUST be exactly the text that executing the template `page` of auth's template set (D01) writes with the `authPageData` whose `Banner` is the response's page banner (R-RL55-PFYN), whose `Create` points to the request's rejected create data (R-3T1K-8Z4L), and whose `SignIn`, `Profile` and `Created` are nil.
- R-RXC5-J5DL: The token-created page (R-VXL8-QAJ3) MUST be exactly the text that executing the template `page` of auth's template set (D01) writes with the `authPageData` whose `Banner` is the response's page banner (R-RL55-PFYN), whose `Created` points to the `tokenCreatedData` whose `Name` is the request's submitted name's trimmed form (R-3T1K-8Z4L, R-3Z52-5TU2) and whose `Secret` is the plaintext secret `CreateToken` (D04) returned for the request, and whose `SignIn`, `Profile` and `Create` are nil.
- R-A2P0-D8TR: For each element `w` of the `Rows` of a profile's profile data (D05), a `POST` whose path is `/tokens/` followed by `w.ID` and then `/disable` when `w.Enabled` is true or `/enable` when it is false, and a `POST` whose path is `/tokens/` followed by `w.ID` and `/delete`, each carrying a valid `ikigenba_session` cookie of the profile's user and an `Origin` header equal to the service's own origin (D05), each made while that user's tokens are exactly as that profile data describes them, MUST be answered as R-5BI7-DNTX and R-9U5P-OUMW state for the token whose `ID` is `w.ID`, the first reversing that token's `Enabled`; and for each element `c` of the `Clients` of that profile data's `Clients`, such a `POST` whose path is `/tokens/` followed by `c.ID` and `/revoke` MUST be answered as R-9XTE-U5UZ states for the token whose `ID` is `c.ID`.
- R-V587-ZEX1: The profile (D05, R-VA3T-IHVT) and the rejected-create page (R-VV5F-YR1P), their bytes read as the HTML Standard parses an HTML document, MUST each hold, among the forms they hold, a form whose method of submission is `POST` and whose target is `/tokens`, whatever other forms the page holds.
- R-V40B-LN6C: For each element `w` of the `Rows` of the profile data (D05, R-V7O0-QYEF) a profile is drawn from, that profile's bytes, read as the HTML Standard parses an HTML document, MUST hold, among the forms they hold, a form whose method of submission is `POST` and whose target is `/tokens/` followed by `w.ID` and then `/disable` when `w.Enabled` is true or `/enable` when it is false, and a form whose method of submission is `POST` and whose target is `/tokens/` followed by `w.ID` and `/delete`; and for each element `c` of the `Clients` of that profile data's `Clients`, a form whose method of submission is `POST` and whose target is `/tokens/` followed by `c.ID` and `/revoke`; whatever other forms the page holds.
