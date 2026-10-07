# D04-store

The persistence contract every auth endpoint shares. `internal/store` (D01)
owns the domain entities and the operations that read and write them;
`internal/idcodec` (D01) owns the opaque-id and secret encoding the store
mints. The serve, sign-in, check, token and OAuth designs (D02–D07, D09)
reference the names fixed here and re-declare none of them.

Six entities cross the store boundary. A **User** is one Workspace member,
keyed by the `(issuer, subject)` pair from a Google ID token, carrying an
opaque user id auth minted (never Google's subject, never the email), the email
refreshed on every login, and the time of the member's most recent Google
login. A **Session** is one browser login: an opaque id (the cookie value), the
owning user, when it was created, and when it was last used. A **LoginState**
is one in-flight sign-in: an opaque state value carried through the Google round
trip, the PKCE verifier, and an optional return URL; it is single-use. A
**Token** is one access token: an id used in its action URLs and in the trail
(distinct from the secret), the owning user, a name, its kind, the host it is
bound to, an optional expiry, an enabled flag, when it was created, when it was
last used, and the hash of its secret — the plaintext secret is shown once at
creation and never stored. A **Client** is one MCP client's registration
(RFC 7591): an id, the name it gave, the redirect URIs it registered, and when
it registered. An **AuthCode** is one authorization code: the code itself, the
client it was issued to, the user who approved it, the redirect URI, the PKCE
challenge and the resource it was approved for, and when it was issued; it is
single-use.

A token is one of two kinds. A **personal token** is the one a user creates on
the profile with `CreateToken`; it is bound to no host, so it authenticates on
every host, and its owner can enable, disable and delete it. A **client
token**, an MCP client token, is the one auth mints at `/token` with
`CreateClientToken` once the user has approved a client (D09); it is named
after the client, expires 90 days after its code was issued, is bound to the
space's MCP host, and its owner can only revoke it. `ListTokens` returns both
kinds; `SetTokenEnabled` and `DeleteToken` act only on a personal token and
`RevokeToken` only on a client token, each answering `ErrNotFound` for the
other kind exactly as for a token that is not there.

Opaque ids and token secrets are Crockford base32. The alphabet is the digits
`0`–`9` and the letters `A`–`Z` with `I`, `L`, `O`, and `U` removed. An opaque
id is 16 random bytes encoded to 26 characters; auth uses that shape for user
ids, session ids, login-state values and authorization codes. A token id is
the registered entity prefix `tok_` followed by such an opaque id, and a
client id is the registered prefix `cli_` followed by one, so a token or a
client named in the trail reads as what it is wherever it appears; `ikp_` is
not an id prefix but the secret's, and a secret is never recorded. User ids
carry no prefix: they flow through the whole suite as `X-User-Id` and other
services store them, so they stay exactly as they are. An authorization code
is stored plainly, like a login state: it is short-lived and single-use, and
it is worth nothing without the verifier behind its challenge. A token secret
is the literal prefix `ikp_` followed by the 52-character encoding of 32 random
bytes, for both kinds; only its SHA-256 hash is persisted, so a secret cannot
be recovered from the database.

## One database, through appkit

The database is the one the manifest declares, `state/auth.db` on a host, and
auth is its only writer. Opening it is appkit's: `db.Open` (appkit's D15 and
D16) creates the missing directories and the file, configures it, refuses a
database it cannot open or write, applies the migrations the database has not
had, and refuses one that records a migration it was not given; `DB.Read` and
`DB.Write` run the transactions, `Write` on the one writer. That contract is
appkit's, and auth's design restates none of it and its tests re-prove none of
it. `cli.Run` opens the handle (D03) and builds the store over it with
`store.New`, handing it the random source ids and secrets are minted from; the
store neither opens nor closes a handle, so whoever opened it closes it, and a
test builds a store the same way over a handle on a file in its own temporary
directory. Every operation that changes a row runs in one `Write`, so the
read-then-write steps of a login, a touch, a consumed login state or code, or
a minted client token are one transaction on the one writer; every operation
that only reads runs in one `Read`. Users, sessions, login states, tokens,
clients and codes written through a store are there for a store over a later
handle on the same file, which is what lets them outlive every restart and
deploy.

The schema is contract now, because a migration is written against it and an
operator and the host's replication see it. It is three migrations, which the
root package carries (D01). `0001` is the baseline: the tables `users`,
`sessions`, `login_states` and `tokens`, with the columns they had before
`0003`, the two indexes auth has always had on a session's and a token's
owner, `sessions_user_id_idx` and `tokens_user_id_idx`, each created only if
it does not exist, and no rows. A token's row holds its id in the `tokens`
table's `id` column and nowhere else. Because the baseline is the schema auth
always created, a database an earlier auth wrote before it carried migrations
is adopted as it is: `db.Open` applies `0001`, which changes nothing there,
and records it as applied. A test proves the schema the way an operator would
read it: it opens a fresh database in its own temporary directory with
`auth.Migrations()` and, inside a `DB.Read`, queries `sqlite_master` and
`PRAGMA table_info` (SQLite's documentation of the schema table and of that
pragma).

`0002` is a data transform, and it is auth's own code. An earlier auth gave
each token a bare 26-character id. `0002` gives each such token the prefixed
id, `tok_` followed by the same 26 characters, and changes nothing else about
it: its secret, name, times, and state stay as they were, so the token keeps
authenticating, and its bare id names no token afterward. It leaves a token
already carrying the prefix alone, and it touches only token ids: users,
sessions, and login states keep the values they had. appkit applies it once,
in order after `0001`, and records it, so a later start runs it no more.

`0003_mcp_clients.sql` is what MCP clients need. It appends two columns to
`tokens`, after `last_used_at`: `kind`, which it sets to `personal` on every
row already there, and `host`, which it sets to the empty string; so every
token an earlier auth created becomes a personal token, bound to no host, and
keeps authenticating exactly as before, and nothing else about any row
changes. It creates the tables `clients` and `auth_codes`, empty, and no
index. A registration records whether its client has ever received a token,
which is what decides whether it may be pruned.

A test builds the database an earlier auth wrote in its own tree, undoing what
the later migrations added: it opens a database with `db.Open` and
`auth.Migrations()`, creates tokens with `CreateToken` through a store over
that handle, then through `DB.Write` drops the tables `clients` and
`auth_codes` and the columns `kind` and `host` of `tokens` (SQLite's
`ALTER TABLE ... DROP COLUMN`, documented for a column that no index,
constraint, trigger or view names), and either deletes the `schema_migrations`
row of version 3, which leaves the database an auth that carried `0001` and
`0002` wrote, or strips `tok_` from some of those tokens' `id` and drops
`schema_migrations`, which leaves the database an auth that carried no
migrations wrote. It closes the handle, opens the file again with
`auth.Migrations()`, and reads the tokens back through a store over the new
handle and the rows through `DB.Read`.

A store whose handle has been made to fail answers every operation with an
error that is not `ErrNotFound`, which is what lets the server tell a database
it cannot reach from a session, a login state, a token, a client or a code
that is not there (D03 answers the first `500`). The cannot-read and
cannot-write cases are made testable by appkit's failure seam, not by a
fixture on the filesystem: a test calls `SetFailing(true)` on the handle its
store was built over, sees every operation fail that way, then calls
`SetFailing(false)` and sees the same store answer again with nothing
reopened.

The identity a lookup resolves says which token it came through, when it came
through one, so the check can name the honored token in the trail (D06). And
provisioning a user on login reports whether the call created the user, so the
sign-in flow can record a first sign-in (D05).

The operations cover provisioning a user on login, minting and ending sessions,
recording and consuming login state, creating, listing, scoping, revoking and
authenticating tokens, registering and looking up clients, and issuing and
consuming authorization codes. Two reads deliberately do not mutate — the
identity lookups behind `/me` and the profile — while their touching
counterparts behind `/check` and `/check/open` update a last-use time, because
a check counts as use and a `/me` does not. Both token lookups take the host
the token is being used on: a client token authenticates only on the host it
was bound to, compared without regard to the case of ASCII letters, and a
personal token, bound to none, on every host. What host a caller passes is
D06's.

The store keeps itself tidy without a timer. Registering a client removes
every registration that never received a token and is more than 24 hours old,
and looking one up removes it when it is such a registration; a registration
that received a token is kept whatever its age, even after that token is
revoked or expires, so the client can come back through `/authorize` without
registering again. Issuing or consuming a code removes every code more than
10 minutes old, and consuming a code removes it whatever the outcome. A test
sees each removal as a lookup answering `ErrNotFound` and as the row being
gone from the table, read inside a `DB.Read`.

Windows bound validity, and their numbers are the contract. A session is
live only while its last use is within 15 minutes and its login is within 18
hours; either bound expires it. A token authenticates only while it is enabled,
unexpired, and its owner's most recent Google login is within 30 days. A
personal token's expiry, when it has one, is 30, 90, or 365 days after it was
created; it may also have no expiry, which never expires. A client token
always expires, 90 days after its code was issued. A code is good for 10
minutes after it was issued, and an unused registration for 24 hours after it
was created.

## REQUIREMENTS

- R-41J3-NIWG: `internal/idcodec` MUST export `const Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"` and `const SecretPrefix = "ikp_"`.
- R-SVIJ-NUV7: `internal/idcodec` MUST export `TokenIDPrefix` as an untyped string constant whose value is `"tok_"`.
- R-EJUD-VSEU: `internal/idcodec` MUST export `ClientIDPrefix` as an untyped string constant whose value is `"cli_"`.
- R-42R0-1AN5: `internal/idcodec` MUST export `func Encode(b []byte) string`.
- R-43YW-F2DU: `internal/idcodec` MUST export `func NewID(rand io.Reader) (string, error)`.
- R-456S-SU4J: `internal/idcodec` MUST export `func NewSecret(rand io.Reader) (string, error)`.
- R-46EP-6LV8: `internal/idcodec` MUST export `func HashSecret(secret string) string`.
- R-47ML-KDLX: `internal/store` MUST export `type User struct` with exactly the fields `ID string`, `Issuer string`, `Subject string`, `Email string`, and `LastGoogleLogin time.Time`.
- R-48UH-Y5CM: `internal/store` MUST export `type Session struct` with exactly the fields `ID string`, `UserID string`, `LoginAt time.Time`, and `LastUsedAt time.Time`.
- R-4A2E-BX3B: `internal/store` MUST export `type LoginState struct` with exactly the fields `State string`, `Verifier string`, and `ReturnURL string`.
- R-EL2A-9K5J: `internal/store` MUST export `type TokenKind string` and the constants `TokenPersonal TokenKind = "personal"` and `TokenClient TokenKind = "client"`.
- R-EMA6-NBW8: `internal/store` MUST export `type Token struct` with exactly the fields `ID string`, `UserID string`, `Name string`, `Hash string`, `Kind TokenKind`, `Host string`, `Enabled bool`, `CreatedAt time.Time`, `ExpiresAt *time.Time`, and `LastUsedAt *time.Time`.
- R-ENI3-13MX: `internal/store` MUST export `type Client struct` with exactly the fields `ID string`, `Name string`, `RedirectURIs []string`, and `CreatedAt time.Time`.
- R-EOPZ-EVDM: `internal/store` MUST export `type AuthCode struct` with exactly the fields `Code string`, `ClientID string`, `UserID string`, `RedirectURI string`, `Challenge string`, `Resource string`, and `IssuedAt time.Time`.
- R-SWQG-1MLW: `internal/store` MUST export `type Identity struct` with exactly the fields `UserID string`, `Email string`, and `TokenID string`.
- R-4EXZ-V023: `internal/store` MUST export `type Expiry string` and the constants `ExpiryNever Expiry = "never"`, `Expiry30d Expiry = "30d"`, `Expiry90d Expiry = "90d"`, and `Expiry365d Expiry = "365d"`.
- R-J5A1-Z776: `internal/store` MUST export `SessionIdle` as a constant of type `time.Duration` whose value is `15 * time.Minute`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
- R-J6HY-CYXV: `internal/store` MUST export `SessionMax` as a constant of type `time.Duration` whose value is `18 * time.Hour`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
- R-J7PU-QQOK: `internal/store` MUST export `TokenLoginWindow` as a constant of type `time.Duration` whose value is `30 * 24 * time.Hour`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
- R-EPXV-SN4B: `internal/store` MUST export `AuthCodeTTL` as a constant of type `time.Duration` whose value is `10 * time.Minute`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
- R-ER5S-6EV0: `internal/store` MUST export `UnusedClientTTL` as a constant of type `time.Duration` whose value is `24 * time.Hour`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
- R-ESDO-K6LP: `internal/store` MUST export `ClientTokenTTL` as a constant of type `time.Duration` whose value is `90 * 24 * time.Hour`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
- R-2SV9-DI1W: `internal/store` MUST export `var ErrNotFound error`, the sentinel an operation returns (wrapped or as-is, matchable with `errors.Is`) wherever a requirement of this design states that the operation returns `ErrNotFound`.
- R-8CEV-D3QG: `internal/store` MUST export `type Store` and `func New(d *db.DB, rand io.Reader) *Store`, where `db` is the package `github.com/ikigenba/ikigenba/appkit/db`, `d` is the handle every operation of the returned `*Store` reads and writes through, and `rand` is the source from which it mints ids and secrets.
- R-SZ68-T63A: `internal/store` MUST export `func (*Store) UpsertUserOnLogin(issuer, subject, email string, now time.Time) (User, bool, error)`, whose second result reports whether the call created the user.
- R-4L1H-RURK: `internal/store` MUST export `func (*Store) CreateSession(userID string, now time.Time) (Session, error)`.
- R-4M9E-5MI9: `internal/store` MUST export `func (*Store) LookupSessionIdentity(sessionID string, now time.Time) (Identity, error)`.
- R-4NHA-JE8Y: `internal/store` MUST export `func (*Store) TouchSession(sessionID string, now time.Time) (Identity, error)`.
- R-4OP6-X5ZN: `internal/store` MUST export `func (*Store) DeleteSession(sessionID string) error`.
- R-4PX3-AXQC: `internal/store` MUST export `func (*Store) CreateLoginState(verifier, returnURL string) (LoginState, error)`.
- R-4R4Z-OPH1: `internal/store` MUST export `func (*Store) ConsumeLoginState(state string) (LoginState, error)`.
- R-4SCW-2H7Q: `internal/store` MUST export `func (*Store) CreateToken(userID, name string, expiry Expiry, now time.Time) (Token, string, error)`, whose second result is the plaintext secret.
- R-4TKS-G8YF: `internal/store` MUST export `func (*Store) ListTokens(userID string) ([]Token, error)`.
- R-4X8H-LK6I: `internal/store` MUST export `func (*Store) SetTokenEnabled(userID, tokenID string, enabled bool) error`.
- R-4YGD-ZBX7: `internal/store` MUST export `func (*Store) DeleteToken(userID, tokenID string) error`.
- R-F24V-MCJ9: `internal/store` MUST export `func (*Store) LookupTokenIdentity(secret, host string, now time.Time) (Identity, error)`.
- R-F3CS-049Y: `internal/store` MUST export `func (*Store) TouchTokenIdentity(secret, host string, now time.Time) (Identity, error)`.
- R-EUTH-BQ33: `internal/store` MUST export `func (*Store) RegisterClient(name string, redirectURIs []string, now time.Time) (Client, error)`.
- R-EW1D-PHTS: `internal/store` MUST export `func (*Store) LookupClient(clientID string, now time.Time) (Client, error)`.
- R-EX9A-39KH: `internal/store` MUST export `func (*Store) CreateAuthCode(clientID, userID, redirectURI, challenge, resource string, now time.Time) (AuthCode, error)`.
- R-EYH6-H1B6: `internal/store` MUST export `func (*Store) ConsumeAuthCode(code string, now time.Time) (AuthCode, error)`.
- R-EZP2-UT1V: `internal/store` MUST export `func (*Store) CreateClientToken(code AuthCode, host string, now time.Time) (Token, string, error)`, whose second result is the plaintext secret.
- R-F0WZ-8KSK: `internal/store` MUST export `func (*Store) RevokeToken(userID, tokenID string) error`.
- R-5243-4N5A: `Encode` MUST map its input bytes to a string that uses only characters from `Alphabet`, MUST be deterministic for a given input, MUST return the empty string for empty input, and MUST return a string of `ceil(8*len(b)/5)` characters (26 for 16 bytes, 52 for 32 bytes).
- R-53BZ-IEVZ: `NewID` MUST read exactly 16 bytes from `rand`, MUST return their `Encode` (26 characters from `Alphabet`), and MUST return a non-nil error and no id if the read fails.
- R-54JV-W6MO: `NewSecret` MUST read exactly 32 bytes from `rand`, MUST return `SecretPrefix` followed by their `Encode` (`ikp_` then 52 characters from `Alphabet`), and MUST return a non-nil error and no secret if the read fails.
- R-G99G-TBAH: `HashSecret` MUST return the lowercase-hex SHA-256 of its input (64 hex characters) and MUST be deterministic; the only persisted representation of a token secret MUST be its `HashSecret` value (lowercase-hex SHA-256, 64 characters), and the plaintext secret MUST NOT be persisted.
- R-F4KO-DW0N: When a database file was created by `db.Open` with a `db.Config` whose `Migrations` is the root package's `auth.Migrations()`, tokens were created in it through `CreateToken` on a `*Store` that `New` returned over that handle, and then, through `DB.Write` on a handle on that file, for one or more of those tokens, the `id` of its row in `tokens` was set to its `ID` with the leading `TokenIDPrefix` removed, leaving 26 characters from `Alphabet`, the tables `schema_migrations`, `clients`, and `auth_codes` were dropped, and the columns `kind` and `host` were dropped from `tokens`, as in a database an auth that carried no migrations wrote, every handle on the file then being closed, a later `db.Open` of that file with `auth.Migrations()` MUST return a non-nil `*db.DB` and a nil error, and in a `*Store` that `New` returns over that handle each such token's `ID` MUST be `TokenIDPrefix` followed by those same 26 characters, `ListTokens` for its owner MUST return no token whose `ID` is those bare 26 characters, the token's `Kind` MUST be `TokenPersonal` and its `Host` the empty string, and its `UserID`, `Name`, `Hash`, `Enabled`, `CreatedAt`, `ExpiresAt`, and `LastUsedAt` MUST be unchanged, so that `LookupTokenIdentity` and `TouchTokenIdentity` honor its secret, whatever host they are given, exactly as before.
- R-F5SK-RNRC: After the later `db.Open` R-F4KO-DW0N describes has returned, queried inside a `DB.Read` on its handle, every row of the tables `users`, `sessions`, and `login_states` MUST hold exactly the values it held before that `db.Open`; every row of `tokens` MUST hold, in every column it held before that `db.Open` other than `id`, exactly the value it held before, `kind` equal to `personal`, and `host` equal to the empty string; and every row of `tokens` whose `id` the `DB.Write` R-F4KO-DW0N describes did not change, its `id` already beginning with `TokenIDPrefix`, MUST hold the `id` it held before that `db.Open`.
- R-F70H-5FI1: When a database file was created by `db.Open` with a `db.Config` whose `Migrations` is `auth.Migrations()`, tokens were created in it through `CreateToken` on a `*Store` that `New` returned over that handle, and then, through `DB.Write` on a handle on that file, the tables `clients` and `auth_codes` were dropped, the columns `kind` and `host` were dropped from `tokens`, and the row of `schema_migrations` whose `version` is `3` was deleted, as in a database an auth that carried only the migrations `0001` and `0002` wrote, every handle on the file then being closed, a later `db.Open` of that file with `auth.Migrations()` MUST return a non-nil `*db.DB` and a nil error, and in a `*Store` that `New` returns over that handle every such token MUST have `Kind` equal to `TokenPersonal`, `Host` equal to the empty string, and the `ID`, `UserID`, `Name`, `Hash`, `Enabled`, `CreatedAt`, `ExpiresAt`, and `LastUsedAt` it had before the `DB.Write`, so that `LookupTokenIdentity` and `TouchTokenIdentity` honor its secret, whatever host they are given, exactly as before.
- R-F88D-J78Q: After the later `db.Open` R-F70H-5FI1 describes has returned, queried inside a `DB.Read` on its handle, every row of the tables `users`, `sessions`, and `login_states` MUST hold exactly the values it held before that `db.Open`, every row of `tokens` MUST hold in every column it held before that `db.Open` exactly the value it held before, `kind` equal to `personal`, and `host` equal to the empty string, and the tables `clients` and `auth_codes` MUST exist and hold no rows.
- R-F9G9-WYZF: When nothing exists at a path, `db.Open` called with a `db.Config` whose `Path` is that path and whose `Migrations` is `auth.Migrations()` MUST return a non-nil `*db.DB` and a nil error, and, queried inside a `DB.Read` on that handle, `sqlite_master` MUST hold exactly seven rows whose `type` is `table` and whose `name` does not begin with `sqlite_`, those whose `name` is `auth_codes`, `clients`, `login_states`, `schema_migrations`, `sessions`, `tokens`, and `users`, and the tables `auth_codes`, `clients`, `login_states`, `sessions`, `tokens`, and `users` MUST hold no rows.
- R-FAO6-AQQ4: For a database that `db.Open` created with `auth.Migrations()` as R-F9G9-WYZF states, queried inside a `DB.Read` on a handle on it, `PRAGMA table_info('users')` MUST list the columns `id`, `issuer`, `subject`, `email`, and `last_google_login`; `PRAGMA table_info('sessions')` the columns `id`, `user_id`, `login_at`, and `last_used_at`; `PRAGMA table_info('login_states')` the columns `state`, `verifier`, and `return_url`; `PRAGMA table_info('tokens')` the columns `id`, `user_id`, `name`, `hash`, `enabled`, `created_at`, `expires_at`, `last_used_at`, `kind`, and `host`; `PRAGMA table_info('clients')` the columns `id`, `name`, `redirect_uris`, `created_at`, and `received_token`; and `PRAGMA table_info('auth_codes')` the columns `code`, `client_id`, `user_id`, `redirect_uri`, `challenge`, `resource`, and `issued_at`; each in that order and no other; and for every token a `*Store` over a handle on that database has created and neither deleted nor revoked, the `tokens` table MUST hold exactly one row whose `id` is that token's `ID`.
- R-FD3Z-2A7I: For a database that `db.Open` created with `auth.Migrations()` as R-F9G9-WYZF states, `sqlite_master` queried as R-F9G9-WYZF queries it MUST show exactly two rows whose `type` is `index` and whose `name` does not begin with `sqlite_`, `sessions_user_id_idx` with `tbl_name` `sessions` and `tokens_user_id_idx` with `tbl_name` `tokens`, and `PRAGMA index_info` of each MUST list exactly the one column `user_id`.
- R-FEBV-G1Y7: For a database that `db.Open` created with `auth.Migrations()` as R-F9G9-WYZF states, queried inside a `DB.Read` on a handle on it, for every client a `*Store` over a handle on that database has registered with `RegisterClient` and that no call has removed, the `clients` table MUST hold exactly one row whose `id` is that client's `ID`; and for every authorization code such a `*Store` has created with `CreateAuthCode` and that no call has removed, the `auth_codes` table MUST hold exactly one row whose `code` is that code's `Code`.
- R-8IID-9YFX: When users, sessions, login states, and tokens were written through a `*Store` over a handle that `db.Open` returned for a path with `auth.Migrations()`, and that handle was then closed, then on a `*Store` that `New` returns over a later handle that `db.Open` returns for the same path with `auth.Migrations()`, every `LookupSessionIdentity`, `ListTokens`, and `LookupTokenIdentity` call MUST return what the same call with the same arguments returned on the first `*Store` just before its handle was closed, and `ConsumeLoginState` MUST return, for the `State` of every login state created and not consumed through the first `*Store`, that login state.
- R-FFJR-TTOW: When clients were registered and authorization codes created through a `*Store` over a handle that `db.Open` returned for a path with `auth.Migrations()`, and that handle was then closed, then on a `*Store` that `New` returns over a later handle that `db.Open` returns for the same path with `auth.Migrations()`, `LookupClient` MUST return, for the `ID` of every such client and a `now` at which it is not stale, the client the first `*Store`'s `RegisterClient` returned, and `ConsumeAuthCode` MUST return, for the `Code` of every such code not consumed through the first `*Store` and a `now` no more than `AuthCodeTTL` after its `IssuedAt`, the code the first `*Store`'s `CreateAuthCode` returned.
- R-FGRO-7LFL: While `SetFailing(true)` is in force on the handle a `*Store` was built over, a `SetFailing(true)` call on it having returned and no `SetFailing(false)` call on it having followed, every `UpsertUserOnLogin`, `CreateSession`, `LookupSessionIdentity`, `TouchSession`, `DeleteSession`, `CreateLoginState`, `ConsumeLoginState`, `CreateToken`, `ListTokens`, `SetTokenEnabled`, `DeleteToken`, `RevokeToken`, `LookupTokenIdentity`, `TouchTokenIdentity`, `RegisterClient`, `LookupClient`, `CreateAuthCode`, `ConsumeAuthCode`, and `CreateClientToken` call on that `*Store` MUST return a non-nil error for which `errors.Is(err, ErrNotFound)` is false; once a `SetFailing(false)` call on that handle has returned, the same `*Store` MUST again behave as the other requirements of this design state, with no new handle and no `New` call in between; and every other requirement of auth's design that states what one of those calls returns or does MUST be read as applying only to a `*Store` whose handle is open and on which `SetFailing(true)` is not in force.
- R-5AND-T1C5: On the first `UpsertUserOnLogin` for an `(issuer, subject)` pair, the store MUST create a `User` with a freshly minted opaque `ID` (via `NewID`), the given `Email`, and `LastGoogleLogin` equal to `now`, and MUST return that `User`.
- R-5BVA-6T2U: On a later `UpsertUserOnLogin` for an `(issuer, subject)` pair that already has a user, the store MUST keep the existing `ID`, MUST set `Email` to the given value and `LastGoogleLogin` to `now`, MUST NOT create a second row for that pair, and MUST return the updated `User`.
- R-T0E5-6XTZ: `UpsertUserOnLogin` MUST return `true` as its second result when the call created the user (the case of R-5AND-T1C5) and `false` when the `(issuer, subject)` pair already had a user (the case of R-5BVA-6T2U).
- R-5D36-KKTJ: `CreateSession` MUST create a `Session` with a freshly minted opaque `ID` (via `NewID`), `UserID` equal to the argument, and `LoginAt` and `LastUsedAt` both equal to `now`, and MUST return it.
- R-5EB2-YCK8: A session MUST be considered live at time `now` if and only if `now - LastUsedAt <= SessionIdle` and `now - LoginAt <= SessionMax`; exceeding either bound MUST make it not live.
- R-5GQV-PW1M: `LookupSessionIdentity` MUST return the owning user's `Identity` when the named session is live at `now`, MUST return `ErrNotFound` when the session is unknown or not live, and MUST NOT modify any row in either case.
- R-5HYS-3NSB: `TouchSession` MUST, when the named session is live at `now`, set that session's `LastUsedAt` to `now` and return the owning user's `Identity`; when the session is unknown or not live it MUST return `ErrNotFound` and MUST modify no row.
- R-5J6O-HFJ0: `DeleteSession` MUST remove the named session so that it no longer resolves; deleting a session that is absent MUST NOT be an error.
- R-5KEK-V79P: `CreateLoginState` MUST create a `LoginState` with a freshly minted opaque `State` (via `NewID`), the given `Verifier`, and the given `ReturnURL` (which may be empty), and MUST return it.
- R-5LMH-8Z0E: `ConsumeLoginState` MUST be single-use: for a state that exists it MUST return the stored `LoginState` and remove it so a second call with the same state returns `ErrNotFound`; for a state that is unknown or already consumed it MUST return `ErrNotFound`.
- R-FHZK-LD6A: `CreateToken` MUST create a `Token` owned by `userID` whose `ID` is `TokenIDPrefix` followed by a freshly minted opaque id (via `NewID`), with the given `Name`, `Kind` equal to `TokenPersonal`, `Host` the empty string, `Enabled` true, `CreatedAt` equal to `now`, `LastUsedAt` nil, and `Hash` equal to `HashSecret` of a freshly minted secret (via `NewSecret`); it MUST return that record together with the plaintext secret as its second result and MUST persist only the hash, never the plaintext.
- R-5O2A-0IHS: `CreateToken` MUST set `ExpiresAt` from `expiry`: nil for `ExpiryNever`, `now` plus 30 days for `Expiry30d`, `now` plus 90 days for `Expiry90d`, and `now` plus 365 days for `Expiry365d` (a day being 24 hours).
- R-5PA6-EA8H: `ListTokens` MUST return exactly the tokens owned by `userID` and no token owned by another user.
- R-FJ7G-Z4WZ: `SetTokenEnabled` MUST set `Enabled` to the argument for the token when `tokenID` names a token whose `Kind` is `TokenPersonal` owned by `userID`, and MUST return `ErrNotFound` (changing nothing) when the id names a token whose `Kind` is `TokenClient`, a token owned by another user, or no token.
- R-FKFD-CWNO: `DeleteToken` MUST remove the token when `tokenID` names a token whose `Kind` is `TokenPersonal` owned by `userID`, and MUST return `ErrNotFound` (changing nothing) when the id names a token whose `Kind` is `TokenClient`, a token owned by another user, or no token.
- R-G3XR-H8IS: `RevokeToken` MUST remove the token when `tokenID` names a token whose `Kind` is `TokenClient` owned by `userID`, so that `ListTokens` no longer returns it and neither `LookupTokenIdentity` nor `TouchTokenIdentity` honors its secret, and MUST return `ErrNotFound` (changing nothing) when the id names a token whose `Kind` is `TokenPersonal`, a token owned by another user, or no token.
- R-FMRY-1QMC: A token MUST be considered to authenticate at time `now` on host `h` if and only if it is `Enabled`, it is unexpired (`ExpiresAt` is nil, or `ExpiresAt` is after `now`), its owner's `LastGoogleLogin` satisfies `now - LastGoogleLogin <= TokenLoginWindow`, and either its `Kind` is `TokenPersonal`, whatever `h` and its `Host` are, or its `Kind` is `TokenClient`, `h` is not the empty string, and its `Host` equals `h` when ASCII letters are compared without regard to case; so that a token whose `Kind` is `TokenClient` and whose `Host` is the empty string authenticates on no host.
- R-FNZU-FID1: `LookupTokenIdentity` MUST return the owner's `Identity` when the token whose plaintext secret is `secret` authenticates at `now` on host `host`, MUST return `ErrNotFound` in every other case (unknown secret, disabled token, expired token, owner outside the login window, or a token whose `Kind` is `TokenClient` whose `Host` is empty or does not equal a non-empty `host` compared ASCII case-insensitively, or any token whose `Kind` is `TokenClient` when `host` is empty — indistinguishable to the caller), and MUST modify no row in any case.
- R-FO32-I7VR: `TouchTokenIdentity` MUST, when the token whose plaintext secret is `secret` authenticates at `now` on host `host`, set that token's `LastUsedAt` to `now` and return the owner's `Identity`; in every other case it MUST return `ErrNotFound` and MUST modify no row.
- R-SXYC-FECL: Every `Identity` that `LookupTokenIdentity` or `TouchTokenIdentity` returns with a nil error MUST have `TokenID` equal to the `ID` of the token whose plaintext secret is `secret`, and every `Identity` that `LookupSessionIdentity` or `TouchSession` returns with a nil error MUST have an empty `TokenID`.
- R-FPAY-VZMG: auth's design defines that a client **has received a token** once a `CreateClientToken` call whose `code.ClientID` is that client's `ID` has returned a nil error, whether or not the token it created has since been revoked or has expired, and that a client is **stale** at time `now` when it has not received a token and `now - CreatedAt > UnusedClientTTL`; every requirement of auth's design that says a client has received a token or is stale MUST denote this.
- R-FQIV-9RD5: `RegisterClient` MUST create a `Client` whose `ID` is `ClientIDPrefix` followed by a freshly minted opaque id (via `NewID`), with `Name` equal to `name`, `RedirectURIs` holding the elements of `redirectURIs` in the same order and nothing else, and `CreatedAt` equal to `now`, and MUST return it.
- R-FRQR-NJ3U: When `RegisterClient` called with time `now` returns a nil error, every other client that is stale at `now` MUST have been removed, so that, queried inside a `DB.Read`, the `clients` table holds no row whose `id` is its `ID` and `LookupClient` returns `ErrNotFound` for its `ID`, and every client that is not stale at `now` MUST remain, `LookupClient` returning it as before.
- R-FSYO-1AUJ: `LookupClient` MUST return the client whose `ID` is `clientID` when the store holds it and it is not stale at `now`, whatever its age when it has received a token; MUST return `ErrNotFound` when the store holds no client whose `ID` is `clientID`, or holds one that is stale at `now`, in which case that client MUST be removed, so that, queried inside a `DB.Read`, the `clients` table holds no row whose `id` is `clientID`; and MUST change no row of any other client.
- R-FU6K-F2L8: `CreateAuthCode` MUST create an `AuthCode` whose `Code` is a freshly minted opaque id (via `NewID`), with `ClientID`, `UserID`, `RedirectURI`, `Challenge`, and `Resource` equal to the arguments `clientID`, `userID`, `redirectURI`, `challenge`, and `resource`, and `IssuedAt` equal to `now`, and MUST return it.
- R-FWMD-6M2M: When `CreateAuthCode` called with time `now` returns a nil error, every other authorization code whose `IssuedAt` satisfies `now - IssuedAt > AuthCodeTTL` MUST have been removed, so that, queried inside a `DB.Read`, the `auth_codes` table holds no row whose `code` is its `Code`, and every code created and not consumed whose `IssuedAt` satisfies `now - IssuedAt <= AuthCodeTTL` MUST remain.
- R-FXU9-KDTB: `ConsumeAuthCode` MUST be single-use: when the store holds the code named by `code` and `now - IssuedAt <= AuthCodeTTL` for it, it MUST return that `AuthCode` and remove it, so that a second call with the same `code` returns `ErrNotFound`; when the store holds no such code, the code having never been created or having already been consumed, or when `now - IssuedAt > AuthCodeTTL` for it, it MUST return `ErrNotFound`; and in every case, once it has returned a nil error or `ErrNotFound`, the `auth_codes` table queried inside a `DB.Read` MUST hold no row whose `code` is `code`.
- R-FZ25-Y5K0: When `ConsumeAuthCode` called with time `now` returns a nil error or `ErrNotFound`, every authorization code whose `IssuedAt` satisfies `now - IssuedAt > AuthCodeTTL` MUST have been removed, so that, queried inside a `DB.Read`, the `auth_codes` table holds no row whose `code` is its `Code`.
- R-G0A2-BXAP: When the store holds a client whose `ID` is `code.ClientID`, `CreateClientToken` MUST create a `Token` owned by `code.UserID` whose `ID` is `TokenIDPrefix` followed by a freshly minted opaque id (via `NewID`), with `Name` equal to that client's `Name`, `Kind` equal to `TokenClient`, `Host` equal to `host`, `Enabled` true, `CreatedAt` equal to `now`, `ExpiresAt` equal to `code.IssuedAt` plus `ClientTokenTTL`, `LastUsedAt` nil, and `Hash` equal to `HashSecret` of a freshly minted secret (via `NewSecret`); it MUST return that record together with the plaintext secret as its second result and MUST persist only the hash, never the plaintext.
- R-A4QA-XTHH: After a `CreateClientToken` call has returned a nil error, `LookupClient` MUST return the client whose `ID` is `code.ClientID` for every later `now`, and a later `RegisterClient` or `LookupClient` call MUST NOT remove that client, whether or not the token the call created has since been revoked or has expired.
- R-G2PV-3GS3: When the store holds no client whose `ID` is `code.ClientID`, `CreateClientToken` MUST return `ErrNotFound` and create no token, so that `ListTokens` for `code.UserID` returns what it returned before the call.
