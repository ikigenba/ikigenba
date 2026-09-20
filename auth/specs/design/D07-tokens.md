# D07-tokens

The token-management HTTP surface a signed-in Workspace member drives from
their profile: creating a personal access token, listing the tokens they own,
toggling one enabled or disabled, and deleting one. These handlers live in
`internal/server` (D01) and are unexported; their contract is the observable
HTTP behaviour — method, path, status, fixed headers, and the structure of the
body — and nothing about how the handlers are written. Every name they lean on
is already fixed elsewhere: the store operations and the `Token`/`Expiry`/
`Identity` shapes in D04, the `ikigenba_session` cookie and the service's own
origin in D05, the `idcodec` id and secret encodings in D04. D07 re-declares
none of them.

Every request in this design carries a valid `ikigenba_session` cookie (D05
owns the cookie; this design only reads it to identify the acting user), and
every state-changing request is a POST that also carries an `Origin` header
equal to the service's own origin (D05 owns how that origin is derived from the
request Host). A POST whose `Origin` is not the service's own origin is refused
outright, and nothing is changed.

Creating a token accepts a `name` (required, 1..64 characters once leading and
trailing whitespace is trimmed) and an `expires` value drawn from the four
`store.Expiry` members. On success the plaintext secret — the second return of
`CreateToken`, of the form `ikp_` followed by 52 Crockford base32 characters —
is presented exactly once, alongside a button that copies it and a link back to
the profile; only its hash is ever stored, so it appears in this one response
and never again. A blank or whitespace-only name creates nothing and returns the
create form for another try.

The profile page as a whole is D05's; D07 owns only the per-token rows within
it. For each token the acting user owns — as `ListTokens` returns them — the
page carries a row that exposes the token's name, created time, last-used time,
expiry, and whether it is enabled, together with a POST form to that token's
enable or disable URL and a POST form to its delete URL. The URL segment is the
token's own random Crockford id (`idcodec.NewID`, the `Token.ID` field), never
its secret, and no plaintext secret appears anywhere on the page. Posting to a
token's enable, disable, or delete URL returns the user to the profile; a
segment naming a token the user does not own, or no token at all, is answered
the same way delete and toggle alike — a 404 — because the store cannot
distinguish "not yours" from "does not exist".

## REQUIREMENTS

- R-N5RR-K5GT: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is 1..64 characters and whose `expires` is one of the `store.Expiry` members `ExpiryNever`, `Expiry30d`, `Expiry90d`, or `Expiry365d` (D04), MUST respond `200 OK` with `Content-Type: text/html; charset=utf-8` and MUST create exactly one token for the cookie's user by calling `CreateToken` (D04) with that trimmed `name` and the matching `store.Expiry`.
- R-N6ZN-XX7I: The `200 OK` body of a successful `POST /tokens` MUST present the created token's plaintext secret — the second return value of `CreateToken` (D04), of the form `ikp_` followed by 52 Crockford base32 characters — exactly once, and MUST contain a button element that copies that secret and a link whose target is `/`; the plaintext secret MUST appear in this response only and on no later page.
- R-N87K-BOY7: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is not 1..64 characters (empty, whitespace-only, or longer than 64), MUST respond `400 Bad Request` with `Content-Type: text/html; charset=utf-8`, MUST NOT call `CreateToken` (D04), and MUST return a body containing the create form: a form whose method is POST and whose action is `/tokens`, carrying a `name` field and an `expires` field.
- R-G35Y-WGL0: A `POST /tokens` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), whose `name` after trimming leading and trailing whitespace is 1..64 characters but whose `expires` is missing or is not one of the `store.Expiry` members `ExpiryNever`, `Expiry30d`, `Expiry90d`, or `Expiry365d` (D04), MUST respond `400 Bad Request` with `Content-Type: text/html; charset=utf-8`, MUST NOT call `CreateToken` (D04), and MUST return a body containing the create form: a form whose method is POST and whose action is `/tokens`, carrying a `name` field and an `expires` field.
- R-N9FG-PGOW: The `200 OK` body of `GET /` for a signed-in user (the whole page is D05's) MUST contain, for each token the cookie's user owns as returned by `ListTokens` (D04), one row exposing that token's name, its created time, its last-used time, its expiry, and whether it is enabled.
- R-NAND-38FL: Each token row on `GET /` MUST contain a form whose method is POST and whose action is `/tokens/<id>/enable` when the token is disabled or `/tokens/<id>/disable` when the token is enabled, and a form whose method is POST and whose action is `/tokens/<id>/delete`, where `<id>` is the token's `Token.ID` (the `idcodec.NewID` Crockford id, D04) and never its secret.
- R-NBV9-H06A: The `GET /` body MUST NOT contain any token's plaintext secret.
- R-ND35-URWZ: A `POST /tokens/<id>/enable` (respectively `/disable`) carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token the cookie's user owns, MUST call `SetTokenEnabled` (D04) with `enabled` true (respectively false) and respond `302 Found` with `Location: /`.
- R-NFIY-MBED: A `POST /tokens/<id>/delete` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where `<id>` names a token the cookie's user owns, MUST remove the token by calling `DeleteToken` (D04) and respond `302 Found` with `Location: /`; afterward the token no longer appears among `ListTokens` for that user and its secret authenticates no request.
- R-NGQV-0352: A `POST /tokens/<id>/enable`, `/disable`, or `/delete` carrying a valid `ikigenba_session` cookie and an `Origin` header equal to the service's own origin (D05), where the store operation (`SetTokenEnabled` or `DeleteToken`, D04) returns an error satisfying `errors.Is(err, store.ErrNotFound)` because `<id>` names a token the user does not own or names no token, MUST respond `404 Not Found` with `Content-Type: text/plain; charset=utf-8` and MUST change nothing.
- R-NHYR-DUVR: A `POST /tokens`, or a `POST /tokens/<id>/enable`, `/disable`, or `/delete`, whose `Origin` header is not equal to the service's own origin (D05) MUST respond `403 Forbidden` with `Content-Type: text/plain; charset=utf-8` and MUST NOT call `CreateToken`, `SetTokenEnabled`, or `DeleteToken` (D04) — nothing is changed.
