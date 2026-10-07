# D07-tokens

The token-management HTTP surface a signed-in Workspace member drives from
their profile: creating a personal access token, listing the personal tokens
they own, toggling one enabled or disabled, and deleting one; and listing the
MCP client tokens they hold and revoking one. These handlers live in
`internal/server` (D01) and are unexported; their contract is the observable
HTTP behaviour — method, path, status, fixed headers, and the markup and text
of the body — and nothing about how the handlers are written. Every name they
lean on is already fixed elsewhere: the store operations and the `Token`/
`TokenKind`/`Expiry`/`Identity` shapes in D04, the `ikigenba_session` cookie and the
service's own origin in D05, the `idcodec` id and secret encodings in D04,
auth's template set in D01. D07 re-declares none of them.

auth holds two kinds of token, told apart by `Token.Kind` (D04). A **personal
token** (`store.TokenPersonal`) is one the user creates from the
`Create a token` card and manages from the `API tokens` card: enable, disable,
delete. An **MCP client token** (`store.TokenClient`), the client token for
short, is minted only when the user approves an MCP client at D09's
`/authorize` and `/token`; it is named after the client, expires 90 days after
the approval, and is managed from the `MCP clients` card, whose one action is
to revoke it. Each kind's actions apply to that kind only: a personal token's
enable, disable, or delete URL carrying a client token's id, and a client
token's revoke URL carrying a personal token's id, are answered 404 like an
unknown id. The store enforces the same split (`SetTokenEnabled` and
`DeleteToken` act only on personal tokens, `RevokeToken` only on client
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

## The page vocabulary

D05 owns the words every page auth draws is described in, and this design uses
them by name without restating them: an **auth page** and what it carries in
its head (the title `auth`, the stylesheet at `/_appkit/theme.css`, the
favicon at `/_appkit/favicon.svg`, the deferred feedback script at
`/_appkit/feedback.js`, the phone-width viewport,
nothing loaded from another host, every outside value escaped); a page
**drawn with the banner** for a user, whose banner is appkit's, drawn from the data the server's `Banner` function returns, as is
the footer that ends it, and the page's **written markup**, the page with that
banner and footer taken out, which is what
auth's own markup rules read; a **card titled** a name; an
**alert titled** a name **reading** a text; a button **drawn with** an icon and
a word; and the reading procedures a requirement uses — start tag, end tag,
occurrence, carries and read value, content, holds, begins with, consists of,
normalisation, reads, visible text. D05 also defines the **profile** and fixes
where on it the `API tokens`, `Create a token`, and `MCP clients` cards sit
and what they are titled; everything those three cards hold is this design's.

This design adds, beside its cell, order, and data definitions below, three
definitions of its own: the **plus icon** and the
**copy icon**, defined by the path data of
Tabler's outline `plus` and `copy` icons in the platform's `design/` without
their invisible bounding-box path; and a start tag that **marks** an
attribute, read by D05's attribute name, case-insensitively, whatever the
form of its value. D05's occurrence rule reads only the `name="value"` form, and the
HTML standard lets a boolean attribute such as `selected` be written bare ("The
presence of a boolean attribute on an element represents the true value", its
value "either the empty string or a value that is an ASCII case-insensitive
match for the attribute's canonical name"; WHATWG HTML, common microsyntaxes,
boolean attributes), so the selected expiry option is read by presence.

## The tokens on the profile

The `API tokens` card is a flush card: its header carries the one-sentence
explanation beside the title, and beneath it sits either the table of the
user's personal tokens in a horizontally scrolling wrapper or, when the user
owns none, the empty state that says what the card is for. D04's `ListTokens`
returns both kinds; the card shows the personal ones only, and its empty state
appears whatever client tokens the user holds. The table's rows run in
**profile order**: most recently used first, so the token used last is at the
top; tokens last used at the same instant run newest first by created time;
and tokens never used come after every used one, newest first by created time
among themselves. `ListTokens` promises which tokens it returns and not their
order, so the order is this page's own; two tokens that tie on both times may
stand in either order.

A time cell shows a time as a `time` element whose `datetime` is the time in
UTC written as RFC 3339's `date-time` (§5.6: `full-date "T" full-time`, with
`partial-time` requiring seconds and `time-offset` allowing `"Z"`), in whole
seconds with `"Z"` as the offset, and whose text is the same time to the
minute with ` UTC` after it. That `datetime` is also valid HTML: a `time`
element's `datetime` may be "a valid global date and time string", which is a
date, `T`, a time whose seconds are optional, and a time-zone offset such as
`Z` (WHATWG HTML, the `time` element and common microsyntaxes). The `Created`
and `Expires` cells are time cells. A token never used, or one that never
expires, shows `Never` in a muted cell instead.

The `Last used` cell of a used token says instead how long before the page was
drawn the token was last used, rounded down to a whole unit: `just now` under
a minute, then minutes, hours, and days, singular at exactly one, with days the
largest unit, so a token last used a year ago reads `365 days ago`. The page's
draw time is one reading of the server's injected clock, the `Now` of the
`server.Config` it was built with (D03), so a test fixes it exactly. A
last-used time later than the draw time, which clock skew can produce, reads
`just now`. The cell keeps the same machine-readable `datetime` as a time
cell, and its `title` carries the exact last-used time to the minute, which a
browser shows on hover: the `title` attribute "represents advisory
information for the element, such as would be appropriate for a tooltip"
(WHATWG HTML, §3.2.6.1, the `title` attribute), and it is a global attribute,
which the `time` element accepts (§4.5.14, the `time` element: content
attributes "Global attributes" and `datetime`).

Each row ends with its actions: an inline form that disables an enabled token
or enables a disabled one, and an inline form that deletes it, each with one
small ghost button. The URL segment is the token's own id, `tok_` followed by
a random Crockford id (the `Token.ID` field, D04), never its secret, and no plaintext
secret appears anywhere on the profile. Posting to a token's enable, disable,
or delete URL returns the user to the profile; a segment naming a token the
user does not own, one of the user's client tokens, or no token at all, is
answered the same way delete and toggle alike — a 404 — because the store
cannot distinguish "not yours" from "does not exist" and treats a token of the
other kind as neither. A token an earlier auth minted with a bare id has carried
the prefixed id since auth applied migration `0002` (D04), so its row's forms name
the prefixed id, and an action URL still carrying the bare id names no token
and is answered 404 like any unknown id.

## The MCP clients on the profile

The `MCP clients` card, after `Create a token`, lists the user's client
tokens, one row per approval, so approving a client again adds a row and
leaves the earlier ones as they were. Unlike the other cards it is not markup
the code writes: it is the human-authored template `mcp-clients` of auth's
template set (D01), and the card is exactly what executing that template
writes with the profile's **MCP clients data**, which a test can render itself
and compare byte for byte. The data is one member, `Clients`, a list in
profile order with one entry per client token, each carrying the token's id
and name and its times already written out: the approved time (the token's
`CreatedAt`) and the expiry as a time cell writes them, the last-used time as
a last-used cell writes it (or empty strings when never used), and whether the
token has expired, which is when its expiry is not after the draw time. The
template decides nothing but where each value goes.

The template's top comment names the hooks, and the requirements below pin
them to the stories in this design's reading vocabulary, so an asset that
drifts from them fails here even though the code renders it faithfully. The
card is a flush card headed `MCP clients` with a sentence saying what an
approved client can do; beneath it sits either a table whose columns are
`Client`, `Approved`, `Last used`, `Expires`, and an empty heading over the
action, or an empty state saying how a client gets onto the card. The
`Client` cell is the token's name as the client registered it, escaped like
every outside value; `Approved` is a time cell; `Last used` is a last-used or
never cell exactly as in the `API tokens` table; `Expires` is a time cell
until the token expires and from then on reads `Expired`, keeping the exact
expiry as its `datetime` and its `title`, which a browser shows as a tooltip.
A row's one action is a `Revoke` form; the card offers no enable, disable,
delete, or create, and shows no secret.

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

The `Create a token` card on the profile holds the create form: a `Name` text
input capped at 64 characters with a hint beneath it, an `Expires` select
offering 30, 90, and 365 days and never, with 90 days chosen, and a submit
button carrying the plus icon. The labels name their controls by `for`, so a
label names the field it sits over. A `name` is valid when it is 1 to 64
characters once leading and trailing whitespace is trimmed; an `expires` is
valid when it is one of the four `store.Expiry` values, and a missing one is
invalid like any other.

A rejected submission is answered 400 with a page drawn with the banner that
holds only the `Create a token` card, keeping what the caller typed. Every
field is checked, and each field in error shows its message beneath it in
place of its hint, in a `span` whose id ends in `-error` — the hook the
stylesheet draws an error by — and which the field names in its
`aria-describedby` (WAI-ARIA 1.2: `aria-describedby` "identifies the element
(or elements) that describes the object"). An expiry in error cannot be shown
as submitted, since the select offers only its four choices, so it falls back
to 90 days as on a fresh form. The submit button and a `Cancel` link back to
the profile sit together in the form's actions row.

A successful creation is answered 200 with its own page drawn with the
banner: one card, `Token created`, whose quiet warning says this is the only
time the token is shown, then the secret in a `code` element beside a `Copy`
button, then a link back to the profile. The secret occurs exactly once in the
page: the button carries no copy of it, and nothing auth sends afterward
carries it. The plaintext is the second return of `CreateToken`, of the form
`ikp_` followed by 52 Crockford base32 characters; only its hash is stored.

`Copy` works through appkit's button feedback script, which every auth page
links in its head (D05): it copies the text of the `code` element beside a
`button` that is a child of a `.secret` element and shows a toast saying it
did, or, when copying fails, selects that text and shows an error toast. That
behaviour is appkit's, fixed by appkit's design and not by this one, and no
test here runs it: the gates have no JavaScript engine. The page carries no
script of its own beyond that link. What this design fixes is the hook the script keys on: the
`secret` div holding the `code` element and the `Copy` button, whose `type`
is `button`, the type that "does nothing" by itself (WHATWG HTML, the
`button` element), so a click submits nothing. Because the script finds the
secret by that class, no tag in the page's written markup but the secret
div's mentions `secret` in any letter case, and no tag there holds a
character reference that could spell it; the banner above it is appkit's
markup, whose own classes are fixed by appkit's design, and whose launcher
icons are written only by opsctl, which validates each (appkit's D02 and
D05). So the script's selector finds the Copy button and the secret and
nothing else.

## REQUIREMENTS

- R-N5RR-K5GT: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is 1..64 characters and whose `expires` is one of the `store.Expiry` members `ExpiryNever`, `Expiry30d`, `Expiry90d`, or `Expiry365d` (D04), MUST respond `200 OK` with `Content-Type: text/html; charset=utf-8` and MUST create exactly one token for the cookie's user by calling `CreateToken` (D04) with that trimmed `name` and the matching `store.Expiry`.
- R-N87K-BOY7: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is not 1..64 characters (empty, whitespace-only, or longer than 64), MUST respond `400 Bad Request` with `Content-Type: text/html; charset=utf-8`, MUST NOT call `CreateToken` (D04), and MUST return a body containing the create form: a form whose method is POST and whose action is `/tokens`, carrying a `name` field and an `expires` field.
- R-G35Y-WGL0: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is 1..64 characters but whose `expires` is missing or is not one of the `store.Expiry` members `ExpiryNever`, `Expiry30d`, `Expiry90d`, or `Expiry365d` (D04), MUST respond `400 Bad Request` with `Content-Type: text/html; charset=utf-8`, MUST NOT call `CreateToken` (D04), and MUST return a body containing the create form: a form whose method is POST and whose action is `/tokens`, carrying a `name` field and an `expires` field.
- R-5BI7-DNTX: A `POST /tokens/<id>/enable` (respectively `/disable`) carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token of `Kind` `store.TokenPersonal` (D04) the cookie's user owns, MUST call `SetTokenEnabled` (D04) with `enabled` true (respectively false) and respond `302 Found` with `Location: /`.
- R-5CQ3-RFKM: A `POST /tokens/<id>/delete` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token of `Kind` `store.TokenPersonal` (D04) the cookie's user owns, MUST remove the token by calling `DeleteToken` (D04) and respond `302 Found` with `Location: /`; afterward the token no longer appears among `ListTokens` for that user and its secret authenticates no request.
- R-5DY0-57BB: A `POST /tokens/<id>/enable`, `/disable`, or `/delete` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names no token, names a token another user owns, or names a token of `Kind` `store.TokenClient` (D04) the cookie's user owns, MUST respond `404 Not Found` with `Content-Type: text/plain; charset=utf-8` and MUST change nothing: every token keeps its `Enabled` and stays among the tokens `ListTokens` returns for its owner.
- R-5MHA-TLI6: A `POST /tokens/<id>/revoke` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token of `Kind` `store.TokenClient` (D04) the cookie's user owns, MUST remove that token by calling `RevokeToken` (D04) with the cookie's user's id and `<id>`, and MUST respond `302 Found` with `Location: /`; afterward that token no longer appears among `ListTokens` for that user and its secret authenticates no request, and every other token, another of that user's tokens of `Kind` `store.TokenClient` with the same `Name` included, is as it was before the request.
- R-5NP7-7D8V: A `POST /tokens/<id>/revoke` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names no token, names a token another user owns, or names a token of `Kind` `store.TokenPersonal` (D04) the cookie's user owns, MUST respond `404 Not Found` with `Content-Type: text/plain; charset=utf-8` and MUST change nothing: every token stays among the tokens `ListTokens` returns for its owner with its `Enabled` as it was.
- R-5F5W-IZ20: A `POST /tokens`, or a `POST /tokens/<id>/enable`, `/disable`, `/delete`, or `/revoke`, carrying no `Origin` header or one whose value is not equal to the service's own origin (D05) MUST respond `403 Forbidden` with `Content-Type: text/plain; charset=utf-8` and MUST NOT call `CreateToken`, `SetTokenEnabled`, `DeleteToken`, or `RevokeToken` (D04) — nothing is changed.
- R-5GDS-WQSP: The body of every `404 Not Found` response R-5DY0-57BB or R-5NP7-7D8V requires and of every `403 Forbidden` response R-5F5W-IZ20 requires MUST be a single line of plain text: one or more bytes none of which is `\n` or `\r`, followed by a single `\n`.
- R-T19M-SX9W: auth's design defines the **plus icon** as the icon whose `path` start tags carry `d` reading, in order, `M12 5l0 14` and `M5 12l14 0`; every requirement of auth's design that names the plus icon MUST denote that.
- R-14JM-HVKJ: auth's design defines the **copy icon** as the icon whose `path` start tags carry `d` reading, in order, `M7 9.667a2.667 2.667 0 0 1 2.667 -2.667h8.666a2.667 2.667 0 0 1 2.667 2.667v8.666a2.667 2.667 0 0 1 -2.667 2.667h-8.666a2.667 2.667 0 0 1 -2.667 -2.667l0 -8.666` and `M4.012 16.737a2.005 2.005 0 0 1 -1.012 -1.737v-10c0 -1.1 .9 -2 2 -2h10c.75 0 1.158 .385 1.5 1`; every requirement of auth's design that names the copy icon MUST denote that.
- R-VW7C-EV6R: auth's design defines a start tag that **marks** an attribute `A` as one that, read as a tag span, has an attribute name matching `A` ASCII case-insensitively; every requirement of auth's design that says a start tag marks, or does not mark, an attribute MUST denote that.
- R-15RI-VNB8: The profile's card titled `API tokens` MUST have a `section` start tag carrying `class` reading `card flush`, its content MUST consist of its `header` element followed by exactly one `div` element — the card's **token panel** — and that `header` element MUST hold a `p` element whose content reads `Personal access tokens let scripts and tools act as you. Send one as a bearer token.`
- R-52YW-P9N2: When `ListTokens` (D04) returns at least one token of `Kind` `store.TokenPersonal` for the profile's user, the start tag of the token panel of the profile's card titled `API tokens` MUST carry `class` reading `table-scroll`, the token panel MUST hold exactly one `table` start tag, and that card MUST hold no `div` start tag carrying `class` reading `empty`.
- R-546T-31DR: The content of the `table` element of R-52YW-P9N2 MUST consist of a `thead` element followed by a `tbody` element, and the `thead` element's content MUST consist of one `tr` element whose content consists of exactly six `th` elements whose contents read, in order, `Name`, `Created`, `Last used`, `Expires`, `Status`, and the empty string.
- R-55EP-GT4G: For each token `t` of `Kind` `store.TokenPersonal` that `ListTokens` (D04) returns for the profile's user, the `tbody` element of R-546T-31DR MUST hold exactly one `tr` element — `t`'s **row** — holding a `form` start tag carrying `action` reading `/tokens/<id>/delete`, where `<id>` is `t.ID`, and the `tbody` element's content MUST consist of exactly those rows, one per such token, so that no token of `Kind` `store.TokenClient` has a `tr` element there.
- R-592E-M4CJ: auth's design defines that a token `a` **precedes** a token `b` **in profile order** when `a.LastUsedAt` is non-nil and `b.LastUsedAt` is nil; when both are non-nil and the time `a.LastUsedAt` points to is later than the time `b.LastUsedAt` points to; when both are non-nil, both point to the same instant, and `a.CreatedAt` is later than `b.CreatedAt`; and when both are nil and `a.CreatedAt` is later than `b.CreatedAt`; and in no other case; every requirement of auth's design that says a token precedes another in profile order MUST denote that.
- R-57UI-8CLU: For any two tokens `a` and `b` whose rows R-55EP-GT4G requires, `a`'s row MUST precede `b`'s row whenever `a` precedes `b` in profile order.
- R-16ZF-9F1X: auth's design defines the **time cell** of a time `x` as a `td` element whose content consists of exactly one `time` element whose start tag carries `datetime` reading `x` in UTC written as an RFC 3339 `date-time` (§5.6) in the form `YYYY-MM-DDTHH:MM:SSZ`, whole seconds with any fraction dropped, and whose content reads `x` in UTC written as `YYYY-MM-DD HH:MM UTC`, whole minutes with any seconds and fraction dropped; and the **never cell** as a `td` element whose start tag carries `class` reading `muted` and whose content reads `Never`; every requirement of auth's design that names a time cell or a never cell MUST denote that.
- R-1D2X-69RE: auth's design defines the profile's **draw time** as one value that a call of the `Now` field of the `server.Config` passed to `server.New` (D03) returned while the server handled the profile's request, the same value for every row of that profile; every requirement of auth's design that names the profile's draw time MUST denote that.
- R-VQ10-ZQIN: auth's design defines the **elapsed text** of a duration `e`, which may be negative, as: `just now` when `e` is less than 60 seconds, every negative `e` included; when `e` is at least 60 seconds and less than 3600 seconds, with `n` the number of seconds in `e` divided by 60 and rounded down to a whole number, `1 minute ago` when `n` is 1 and `<n> minutes ago` otherwise; when `e` is at least 3600 seconds and less than 86400 seconds, with `n` the number of seconds in `e` divided by 3600 and rounded down to a whole number, `1 hour ago` when `n` is 1 and `<n> hours ago` otherwise; and when `e` is at least 86400 seconds, with `n` the number of seconds in `e` divided by 86400 and rounded down to a whole number, `1 day ago` when `n` is 1 and `<n> days ago` otherwise, however large `n` is; where `<n>` is `n` written in ASCII decimal digits with no leading zero; every requirement of auth's design that names the elapsed text of a duration MUST denote that.
- R-1EAT-K1I3: auth's design defines the **last-used cell** of a time `x` as a `td` element whose content consists of exactly one `time` element whose start tag carries `datetime` reading `x` in UTC written as an RFC 3339 `date-time` (§5.6) in the form `YYYY-MM-DDTHH:MM:SSZ`, whole seconds with any fraction dropped, and `title` reading `x` in UTC written as `YYYY-MM-DD HH:MM UTC`, whole minutes with any seconds and fraction dropped, and whose content reads the elapsed text of the profile's draw time minus `x`; every requirement of auth's design that names a last-used cell MUST denote that.
- R-U4MT-TDAW: The content of each token's row MUST consist of exactly six `td` elements, in this order: one whose content reads the token's `Name` with every run of ASCII whitespace in it replaced by a single space and any leading or trailing space removed; the time cell of its `CreatedAt`; the never cell when its `LastUsedAt` is nil and otherwise the last-used cell of the time `LastUsedAt` points to; the never cell when its `ExpiresAt` is nil and otherwise the time cell of the time `ExpiresAt` points to; its status cell (R-1GQM-BKZH); and its actions cell (R-U3EX-FLK7).
- R-1GQM-BKZH: A token's **status cell** MUST be a `td` element whose content consists of exactly one `span` element, which, when the token's `Enabled` is true, has a start tag carrying `class` reading `badge` and `data-kind` reading `ok` and content reading `Enabled`, and, when `Enabled` is false, has a start tag carrying `class` reading `badge` and not marking `data-kind`, and content reading `Disabled`.
- R-U3EX-FLK7: A token's **actions cell** MUST be a `td` element whose start tag carries `class` reading `row-actions` and whose content consists of exactly two `form` elements, each with a start tag carrying `class` reading `inline` and `method` reading `post` and each with content consisting of exactly one `button` element whose start tag carries `class` reading `ghost small` and `type` reading `submit`: first, one whose start tag carries `action` reading `/tokens/<id>/disable` and whose button's content reads `Disable` when the token is enabled, or `action` reading `/tokens/<id>/enable` and whose button's content reads `Enable` when it is disabled; then one whose start tag carries `action` reading `/tokens/<id>/delete` and whose button's content reads `Delete`; where `<id>` is the token's `Token.ID` (D04) and never its secret.
- R-5AAA-ZW38: When `ListTokens` (D04) returns no token of `Kind` `store.TokenPersonal` for the profile's user, whatever tokens of `Kind` `store.TokenClient` it returns, the profile's card titled `API tokens` MUST hold no `table` start tag and no `div` start tag carrying `class` reading `table-scroll`, the start tag of its token panel MUST carry `class` reading `empty`, and the token panel's content MUST consist of an `h3` element whose content reads `No tokens yet` followed by a `p` element whose content reads `Create one below when a script or tool needs to act as you.`
- R-5RCW-COGY: auth's design defines the **MCP clients data** of a profile as a value of a struct type or of type `map[string]any` whose fields, or keys, are exactly `Clients`, holding a slice with exactly one element for each token of `Kind` `store.TokenClient` (D04) among the tokens `ListTokens` (D04) returns for the profile's user, the element of a token `a` lying before that of a token `b` whenever `a` precedes `b` in profile order; each element being a value of a struct type or of type `map[string]any` whose fields, or keys, are exactly `ID`, the token's `ID`; `Name`, the token's `Name` unaltered; `Approved`, the token's `CreatedAt` in UTC written as `YYYY-MM-DDTHH:MM:SSZ`, whole seconds with any fraction dropped; `ApprovedText`, the token's `CreatedAt` in UTC written as `YYYY-MM-DD HH:MM UTC`, whole minutes with any seconds and fraction dropped; `LastUsed`, `LastUsedTitle`, and `LastUsedText`, each the empty string when the token's `LastUsedAt` is nil, and otherwise, respectively, the time `LastUsedAt` points to written as `Approved` is written, that time written as `ApprovedText` is written, and the elapsed text of the profile's draw time minus that time; `Expires` and `ExpiresText`, the time the token's `ExpiresAt` points to written as `Approved` and as `ApprovedText` are written; all of those of type `string`; and `Expired`, of type `bool`, true when, and only when, the time the token's `ExpiresAt` points to is not after the profile's draw time; every requirement of auth's design that names the MCP clients data MUST denote such a value.
- R-FVB8-Q4T7: The profile's `main` element's content MUST consist of the parts R-51R0-BHWD (D05) places before its card titled `MCP clients`, then any ASCII whitespace, possibly none, then exactly the text that executing the template `mcp-clients` of auth's template set (D01) writes with the profile's MCP clients data, then nothing but ASCII whitespace, possibly none, so that the profile's card titled `MCP clients` is that template's output.
- R-5TSP-47YC: The profile's card titled `MCP clients` MUST have a `section` start tag carrying `class` reading `card flush`; its content MUST consist of its `header` element followed by exactly one `div` element — the card's **client panel**; and that `header` element's content MUST consist of an `h2` element whose content reads `MCP clients` followed by a `p` element whose content reads `MCP clients you approve act as you at the MCP gateway. Revoke one to sign it out.`
- R-5V0L-HZP1: When `ListTokens` (D04) returns at least one token of `Kind` `store.TokenClient` for the profile's user, the start tag of the client panel of the profile's card titled `MCP clients` MUST carry `class` reading `table-scroll`, the client panel's content MUST consist of exactly one `table` element, and that card MUST hold no `div` start tag carrying `class` reading `empty`.
- R-5W8H-VRFQ: The content of the `table` element of R-5V0L-HZP1 MUST consist of a `thead` element followed by a `tbody` element, and the `thead` element's content MUST consist of one `tr` element whose content consists of exactly five `th` elements whose contents read, in order, `Client`, `Approved`, `Last used`, `Expires`, and the empty string.
- R-5XGE-9J6F: For each token `t` of `Kind` `store.TokenClient` that `ListTokens` (D04) returns for the profile's user, the `tbody` element of R-5W8H-VRFQ MUST hold exactly one `tr` element — `t`'s **client row** — holding a `form` start tag carrying `action` reading `/tokens/<id>/revoke`, where `<id>` is `t.ID`; the `tbody` element's content MUST consist of exactly those client rows, one per such token, so that no token of `Kind` `store.TokenPersonal` has a `tr` element there; and for any two such tokens `a` and `b`, `a`'s client row MUST precede `b`'s whenever `a` precedes `b` in profile order.
- R-5ZW7-12NT: auth's design defines the **expired cell** of a time `x` as a `td` element whose content consists of exactly one `time` element whose start tag carries `datetime` reading `x` in UTC written as an RFC 3339 `date-time` (§5.6) in the form `YYYY-MM-DDTHH:MM:SSZ`, whole seconds with any fraction dropped, and `title` reading `x` in UTC written as `YYYY-MM-DD HH:MM UTC`, whole minutes with any seconds and fraction dropped, and whose content reads `Expired`; every requirement of auth's design that names an expired cell MUST denote that.
- R-5YOA-NAX4: The content of each token's client row MUST consist of exactly five `td` elements, in this order: one whose content reads the token's `Name` with every run of ASCII whitespace in it replaced by a single space and any leading or trailing space removed; the time cell of its `CreatedAt`; the never cell when its `LastUsedAt` is nil and otherwise the last-used cell of the time `LastUsedAt` points to; the time cell of the time its `ExpiresAt` points to when that time is after the profile's draw time, and otherwise the expired cell of that time; and its revoke cell (R-6143-EUEI).
- R-6143-EUEI: A client token's **revoke cell** MUST be a `td` element whose start tag carries `class` reading `row-actions` and whose content consists of exactly one `form` element, whose start tag carries `class` reading `inline`, `method` reading `post`, and `action` reading `/tokens/<id>/revoke`, where `<id>` is the token's `Token.ID` (D04) and never its secret, and whose content consists of exactly one `button` element, whose start tag carries `class` reading `ghost small` and `type` reading `submit` and whose content reads `Revoke`.
- R-62BZ-SM57: When `ListTokens` (D04) returns no token of `Kind` `store.TokenClient` for the profile's user, whatever tokens of `Kind` `store.TokenPersonal` it returns, the profile's card titled `MCP clients` MUST hold no `table` start tag and no `div` start tag carrying `class` reading `table-scroll`, the start tag of its client panel MUST carry `class` reading `empty`, and the client panel's content MUST consist of an `h3` element whose content reads `No MCP clients yet` followed by a `p` element whose content reads `A client appears here once you approve it from its login.`
- R-63JW-6DVW: The profile's card titled `MCP clients` MUST hold no `form`, `button`, `input`, `select`, or `a` start tag other than the `form` and `button` start tags its revoke cells hold.
- R-TH4B-RXWX: auth's design defines a **create-token form** as a `form` element whose start tag carries `method` reading `post` and `action` reading `/tokens`; every requirement of auth's design that names a create-token form MUST denote that.
- R-TIC8-5PNM: Every create-token form MUST hold exactly one `label` start tag carrying `for` reading `token-name`, whose element's content reads `Name`; exactly one `label` start tag carrying `for` reading `token-expires`, whose element's content reads `Expires`; exactly one `input` start tag, which carries `id` reading `token-name`, `type` reading `text`, `name` reading `name`, `maxlength` reading `64`, and `placeholder` reading `e.g. ci-deploy`; and exactly one `select` start tag, which carries `id` reading `token-expires` and `name` reading `expires`.
- R-TJK4-JHEB: The `select` element of every create-token form MUST have content consisting of exactly four `option` elements whose start tags carry `value` reading, in order, `30d`, `90d`, `365d`, and `never`, and whose contents read, in order, `In 30 days`, `In 90 days`, `In 365 days`, and `Never`, and exactly one of those `option` start tags MUST mark `selected`.
- R-TKS0-X950: Every create-token form MUST hold exactly one `button` start tag, and that `button` element's start tag MUST carry `type` reading `submit` and the element MUST be drawn with the plus icon and the word `Create token`.
- R-TLZX-B0VP: In every create-token form, the `label` start tag for `token-name`, the `input` start tag, the `label` start tag for `token-expires`, the `select` start tag, and the `button` start tag MUST appear in this order, and the form's `span` start tags MUST all lie either between the `input` start tag and the `label` start tag for `token-expires` or between the `</select>` end tag and the `button` start tag.
- R-1HYI-PCQ6: The profile's card titled `Create a token` MUST have a `section` start tag carrying `class` reading `card`, and its content MUST consist of its `header` element followed by a create-token form whose `input` start tag does not mark `value` and whose `option` start tag carrying `value` reading `90d` is the one that marks `selected`.
- R-00AX-VG3M: The `400 Bad Request` body R-N87K-BOY7 or R-G35Y-WGL0 requires — the **rejected-create page** — MUST be an auth page drawn with the banner for the cookie's user, whose `main` element's content consists of exactly one card titled `Create a token`, whose `section` start tag carries `class` reading `card` and whose content consists of its `header` element followed by a create-token form.
- R-19F8-0YJB: The `input` start tag of the rejected-create page's create-token form MUST carry `value` reading the request's submitted `name` exactly as submitted, untrimmed and unshortened, and reading the empty string when the request carries no `name`.
- R-1AN4-EQA0: In the rejected-create page's create-token form, when the request's `expires` is one of `30d`, `90d`, `365d`, or `never`, the `option` start tag that marks `selected` MUST be the one carrying `value` reading that `expires`; when the request carries no `expires`, or one that is none of those four, it MUST be the one carrying `value` reading `90d`.
- R-TS3F-7VL6: auth's design defines a create-token form's **name is in error** when, and only when, the form is the rejected-create page's and the request's `name` after trimming leading and trailing whitespace is not 1..64 characters, the request carrying no `name` included, and its **expiry is in error** when, and only when, the form is the rejected-create page's and the request carries no `expires` or one that is none of `30d`, `90d`, `365d`, or `never`; every requirement of auth's design that says a create-token form's name or expiry is in error MUST denote that.
- R-W12X-XY5J: A create-token form whose name is not in error MUST hold, between its `input` start tag and its `label` start tag for `token-expires`, exactly one `span` element, whose start tag carries `class` reading `hint` and whose content reads `Something that tells you where it's used. Up to 64 characters.`, MUST hold no start tag carrying `id` reading `token-name-error`, and its `input` start tag MUST NOT mark `aria-describedby`; a create-token form whose name is in error MUST hold, between those two start tags, exactly one `span` element, whose start tag carries `id` reading `token-name-error` and whose content reads `the name must be 1 to 64 characters`, MUST hold no `span` start tag carrying `class` reading `hint`, and its `input` start tag MUST carry `aria-describedby` reading `token-name-error`.
- R-W2AU-BPW8: A create-token form whose expiry is not in error MUST hold no `span` start tag after its `</select>` end tag and no start tag carrying `id` reading `token-expires-error`, and its `select` start tag MUST NOT mark `aria-describedby`; a create-token form whose expiry is in error MUST hold, between its `</select>` end tag and its `button` start tag, exactly one `span` element, whose start tag carries `id` reading `token-expires-error`, and whose content reads `choose one of the listed expiry options`, and its `select` start tag MUST carry `aria-describedby` reading `token-expires-error`.
- R-TWZ0-QYJY: The rejected-create page's create-token form MUST hold exactly one `div` start tag, which carries `class` reading `actions`, and that `div` element's content MUST consist of the form's `button` element followed by an `a` element whose start tag carries `class` reading `button ghost` and `href` reading `/` and whose content reads `Cancel`; the form MUST hold no other `a` start tag.
- R-ZCG2-ZOOK: The `200 OK` body R-N5RR-K5GT requires — the **token-created page** — MUST be an auth page drawn with the banner for the cookie's user, whose `main` element's content consists of exactly one card titled `Token created`, whose `section` start tag carries `class` reading `card`.
- R-1KEB-GW7K: The token-created page's card MUST have content consisting of, in this order: its `header` element; a `div` element whose start tag carries `class` reading `alert quiet` and `data-kind` reading `warn` and which is an alert titled `Copy it now` reading `This is the only time <name> is shown. Only its hash is stored.`, where `<name>` is the created token's trimmed name with every run of ASCII whitespace in it replaced by a single space; a `div` element whose start tag carries `class` reading `secret`; and either an `a` element or a `p` element whose content consists of an `a` element, that `a` element's start tag carrying `href` reading `/` and its content reading `Back to your account`; and the card MUST hold no other `a` start tag.
- R-1BV0-SI0P: The content of the token-created page's `div` element whose start tag carries `class` reading `secret` MUST consist of a `code` element whose content is exactly the plaintext secret `CreateToken` (D04) returned as its second result, followed by a `button` element whose start tag carries `class` reading `secondary` and `type` reading `button` and which is drawn with the copy icon and the word `Copy`.
- R-1LM7-UNY9: The plaintext secret `CreateToken` (D04) returned for a successful `POST /tokens` MUST occur exactly once in the bytes of the token-created page, inside the content of the `code` element of R-1BV0-SI0P, and MUST NOT occur in the bytes of any other response auth sends other than within a value a requirement of auth's design has auth write from that response's request or from a token's `Name`.
- R-0TKJ-1XWA: In the token-created page's written markup (R-056J-EJ2E), the start tag of the `div` element R-1BV0-SI0P describes MUST be the only tag span that contains `secret` matched ASCII case-insensitively, and every tag span of it MUST NOT contain `&`.
- R-TW3J-4Z41: For each `POST /tokens` auth answers `200 OK` (R-N5RR-K5GT), auth MUST record (D05) exactly one event named `token.minted`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, the owner of the created token, and whose attributes are exactly the one key `token` with the `string` value of the created token's `ID`.
- R-5HLP-AIJE: For each `POST /tokens/<id>/enable` (respectively `/disable`) auth answers `302 Found` (R-5BI7-DNTX) for which the token `<id>` names had `Enabled` false (respectively true) before the request, auth MUST record exactly one event named `token.enabled` (respectively `token.disabled`), whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`; for such a request whose token already had `Enabled` true (respectively false), auth MUST record no `token.enabled` or `token.disabled` event.
- R-5ITL-OAA3: For each `POST /tokens/<id>/delete` auth answers `302 Found` (R-5CQ3-RFKM), auth MUST record exactly one event named `token.deleted`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`.
- R-5OX3-L4ZK: For each `POST /tokens/<id>/revoke` auth answers `302 Found` (R-5MHA-TLI6), auth MUST record exactly one event named `token.revoked`, whose envelope request id is the request's request id (D05), whose envelope user is the user id of the `ikigenba_session` cookie's user, and whose attributes are exactly the one key `token` with the `string` value `<id>`.
- R-5K1I-220S: For a request whose path is `/tokens` or begins with `/tokens/`, auth MUST record no event other than its `request.started` and `request.finished` events and the events R-TW3J-4Z41, R-5HLP-AIJE, R-5ITL-OAA3, and R-5OX3-L4ZK require of it; so a request auth answers `400`, `403`, `404`, or `500`, and a toggle to the state the token already has, record no other event.
- R-14EN-8704: After `db.Open` with `auth.Migrations()` has given a token whose `ID` was a bare 26-character id `<bare>` the `ID` `tok_<bare>` (D04, R-F4KO-DW0N), a `POST /tokens/<bare>/enable`, `/disable`, or `/delete` carrying a valid `ikigenba_session` cookie of the token's owner and an `Origin` header equal to the service's own origin (D05) MUST respond `404 Not Found` and change nothing, and the same request naming `tok_<bare>` MUST act on the token as R-5BI7-DNTX and R-5CQ3-RFKM state.
