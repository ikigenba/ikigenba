# D04-store

The persistence contract every auth endpoint shares. `internal/store` (D01)
owns the domain entities and the operations that read and write them;
`internal/idcodec` (D01) owns the opaque-id and secret encoding the store
mints. The serve, sign-in, check, and token designs (D02–D07) reference the
names fixed here and re-declare none of them.

Four entities cross the store boundary. A **User** is one Workspace member,
keyed by the `(issuer, subject)` pair from a Google ID token, carrying an
opaque user id auth minted (never Google's subject, never the email), the email
refreshed on every login, and the time of the member's most recent Google
login. A **Session** is one browser login: an opaque id (the cookie value), the
owning user, when it was created, and when it was last used. A **LoginState**
is one in-flight sign-in: an opaque state value carried through the Google round
trip, the PKCE verifier, and an optional return URL; it is single-use. A
**Token** is one personal access token: an id used in its action URLs and in
the trail (distinct from the secret), the owning user, a name, an optional
expiry, an enabled flag, when it was created, when it was last used, and the
hash of its secret — the plaintext secret is shown once at creation and never
stored.

Opaque ids and token secrets are Crockford base32. The alphabet is the digits
`0`–`9` and the letters `A`–`Z` with `I`, `L`, `O`, and `U` removed. An opaque
id is 16 random bytes encoded to 26 characters; auth uses that shape for user
ids, session ids, and login-state values. A token id is the registered entity
prefix `tok_` followed by such an opaque id, so a token named in the trail
reads as a token wherever it appears; `ikp_` is not an id prefix but the
secret's, and a secret is never recorded. User ids carry no prefix: they flow
through the whole suite as `X-User-Id` and other services store them, so they
stay exactly as they are. A token secret is
the literal prefix `ikp_` followed by the 52-character encoding of 32 random
bytes; only its SHA-256 hash is persisted, so a secret cannot be recovered from
the database.

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
read-then-write steps of a login, a touch, or a consumed login state are one
transaction on the one writer; every operation that only reads runs in one
`Read`. Users, sessions, login states and tokens written through a store are
there for a store over a later handle on the same file, which is what lets
them outlive every restart and deploy.

The schema is contract now, because a migration is written against it and an
operator and the host's replication see it. It is two migrations, which the
root package carries (D01). `0001` is the baseline: the tables `users`,
`sessions`, `login_states` and `tokens`, with the columns below, the two
indexes auth has always had on a session's and a token's owner,
`sessions_user_id_idx` and `tokens_user_id_idx`, each created
only if it does not exist, and no rows. A token's row holds its id in the
`tokens` table's `id` column and nowhere else. Because the baseline is the
schema auth always created, a database an earlier auth wrote before it carried
migrations is adopted as it is: `db.Open` applies `0001`, which changes
nothing there, and records it as applied. A test proves the schema the way an
operator would read it: it opens a fresh database in its own temporary
directory with `auth.Migrations()` and, inside a `DB.Read`, queries
`sqlite_master` and `PRAGMA table_info` (SQLite's documentation of the schema
table and of that pragma).

`0002` is a data transform, and it is auth's own code. An earlier auth gave
each token a bare 26-character id. `0002` gives each such token the prefixed
id, `tok_` followed by the same 26 characters, and changes nothing else about
it: its secret, name, times, and state stay as they were, so the token keeps
authenticating, and its bare id names no token afterward. It leaves a token
already carrying the prefix alone, and it touches only token ids: users,
sessions, and login states keep the values they had. appkit applies it once,
in order after `0001`, and records it, so a later start runs it no more. A
test builds the database an earlier auth wrote in its own tree: it opens a
database with `db.Open` and `auth.Migrations()`, creates tokens with
`CreateToken` through a store over that handle, then through `DB.Write` strips
`tok_` from some of those tokens' `id` in `tokens` and drops
`schema_migrations`, closes the handle, and opens the file again with
`auth.Migrations()`; it reads the tokens back through a store over the new
handle. The schema the old database holds is the one `0001` creates, because
the prefix never changed it.

A store whose handle has been made to fail answers every operation with an
error that is not `ErrNotFound`, which is what lets the server tell a database
it cannot reach from a session, a login state or a token that is not there
(D03 answers the first `500`). The cannot-read and cannot-write cases are made
testable by appkit's failure seam, not by a fixture on the filesystem: a test
calls `SetFailing(true)` on the handle its store was built over, sees every
operation fail that way, then calls `SetFailing(false)` and sees the same
store answer again with nothing reopened.

The identity a lookup resolves says which token it came through, when it came
through one, so the check can name the honored token in the trail (D06). And
provisioning a user on login reports whether the call created the user, so the
sign-in flow can record a first sign-in (D05).

The operations cover provisioning a user on login, minting and ending sessions,
recording and consuming login state, and creating, listing, scoping, and
authenticating tokens. Two reads deliberately do not mutate — the identity
lookups behind `/me` and the profile — while their touching counterparts behind
`/check` and `/check/open` update a last-use time, because a check counts as
use and a `/me` does not.

Three windows bound validity, and their numbers are the contract. A session is
live only while its last use is within 15 minutes and its login is within 18
hours; either bound expires it. A token authenticates only while it is enabled,
unexpired, and its owner's most recent Google login is within 30 days. A token's
expiry, when it has one, is 30, 90, or 365 days after it was created; a token
may also have no expiry, which never expires.

## REQUIREMENTS

- R-41J3-NIWG: `internal/idcodec` MUST export `const Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"` and `const SecretPrefix = "ikp_"`.
- R-SVIJ-NUV7: `internal/idcodec` MUST export `TokenIDPrefix` as an untyped string constant whose value is `"tok_"`.
- R-42R0-1AN5: `internal/idcodec` MUST export `func Encode(b []byte) string`.
- R-43YW-F2DU: `internal/idcodec` MUST export `func NewID(rand io.Reader) (string, error)`.
- R-456S-SU4J: `internal/idcodec` MUST export `func NewSecret(rand io.Reader) (string, error)`.
- R-46EP-6LV8: `internal/idcodec` MUST export `func HashSecret(secret string) string`.
- R-47ML-KDLX: `internal/store` MUST export `type User struct` with exactly the fields `ID string`, `Issuer string`, `Subject string`, `Email string`, and `LastGoogleLogin time.Time`.
- R-48UH-Y5CM: `internal/store` MUST export `type Session struct` with exactly the fields `ID string`, `UserID string`, `LoginAt time.Time`, and `LastUsedAt time.Time`.
- R-4A2E-BX3B: `internal/store` MUST export `type LoginState struct` with exactly the fields `State string`, `Verifier string`, and `ReturnURL string`.
- R-4CI7-3GKP: `internal/store` MUST export `type Token struct` with exactly the fields `ID string`, `UserID string`, `Name string`, `Hash string`, `Enabled bool`, `CreatedAt time.Time`, `ExpiresAt *time.Time`, and `LastUsedAt *time.Time`.
- R-SWQG-1MLW: `internal/store` MUST export `type Identity struct` with exactly the fields `UserID string`, `Email string`, and `TokenID string`.
- R-4EXZ-V023: `internal/store` MUST export `type Expiry string` and the constants `ExpiryNever Expiry = "never"`, `Expiry30d Expiry = "30d"`, `Expiry90d Expiry = "90d"`, and `Expiry365d Expiry = "365d"`.
- R-J5A1-Z776: `internal/store` MUST export `SessionIdle` as a constant of type `time.Duration` whose value is `15 * time.Minute`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
- R-J6HY-CYXV: `internal/store` MUST export `SessionMax` as a constant of type `time.Duration` whose value is `18 * time.Hour`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
- R-J7PU-QQOK: `internal/store` MUST export `TokenLoginWindow` as a constant of type `time.Duration` whose value is `30 * 24 * time.Hour`, so that it is usable wherever Go requires a constant expression, such as the initializer of a `const` declaration.
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
- R-4ZOA-D3NW: `internal/store` MUST export `func (*Store) LookupTokenIdentity(secret string, now time.Time) (Identity, error)`.
- R-50W6-QVEL: `internal/store` MUST export `func (*Store) TouchTokenIdentity(secret string, now time.Time) (Identity, error)`.
- R-5243-4N5A: `Encode` MUST map its input bytes to a string that uses only characters from `Alphabet`, MUST be deterministic for a given input, MUST return the empty string for empty input, and MUST return a string of `ceil(8*len(b)/5)` characters (26 for 16 bytes, 52 for 32 bytes).
- R-53BZ-IEVZ: `NewID` MUST read exactly 16 bytes from `rand`, MUST return their `Encode` (26 characters from `Alphabet`), and MUST return a non-nil error and no id if the read fails.
- R-54JV-W6MO: `NewSecret` MUST read exactly 32 bytes from `rand`, MUST return `SecretPrefix` followed by their `Encode` (`ikp_` then 52 characters from `Alphabet`), and MUST return a non-nil error and no secret if the read fails.
- R-G99G-TBAH: `HashSecret` MUST return the lowercase-hex SHA-256 of its input (64 hex characters) and MUST be deterministic; the only persisted representation of a token secret MUST be its `HashSecret` value (lowercase-hex SHA-256, 64 characters), and the plaintext secret MUST NOT be persisted.
- R-8DMR-QVH5: When a database file was created by `db.Open` with a `db.Config` whose `Migrations` is the root package's `auth.Migrations()`, tokens were created in it through `CreateToken` on a `*Store` that `New` returned over that handle, and then, through `DB.Write` on a handle on that file, for one or more of those tokens, the `id` of its row in `tokens` was set to its `ID` with the leading `TokenIDPrefix` removed, leaving 26 characters from `Alphabet`, and the table `schema_migrations` was dropped, as in a database an auth that carried no migrations wrote, every handle on the file then being closed, a later `db.Open` of that file with `auth.Migrations()` MUST return a non-nil `*db.DB` and a nil error, and in a `*Store` that `New` returns over that handle each such token's `ID` MUST be `TokenIDPrefix` followed by those same 26 characters, `ListTokens` for its owner MUST return no token whose `ID` is those bare 26 characters, and the token's `UserID`, `Name`, `Hash`, `Enabled`, `CreatedAt`, `ExpiresAt`, and `LastUsedAt` MUST be unchanged, so that `LookupTokenIdentity` and `TouchTokenIdentity` honor its secret exactly as before.
- R-8EUO-4N7U: After the later `db.Open` R-8DMR-QVH5 describes has returned, queried inside a `DB.Read` on its handle, every row of the tables `users`, `sessions`, and `login_states`, and every row of `tokens` that the `DB.Write` R-8DMR-QVH5 describes did not change, its `id` already beginning with `TokenIDPrefix`, MUST hold exactly the values it held before that `db.Open`, and every row of `tokens` that `DB.Write` changed MUST hold in every column but `id` exactly the values it held before that `db.Open`.
- R-8G2K-IEYJ: When nothing exists at a path, `db.Open` called with a `db.Config` whose `Path` is that path and whose `Migrations` is `auth.Migrations()` MUST return a non-nil `*db.DB` and a nil error, and, queried inside a `DB.Read` on that handle, `sqlite_master` MUST hold exactly five rows whose `type` is `table` and whose `name` does not begin with `sqlite_`, those whose `name` is `login_states`, `schema_migrations`, `sessions`, `tokens`, and `users`, and the tables `login_states`, `sessions`, `tokens`, and `users` MUST hold no rows.
- R-8HAG-W6P8: For a database that `db.Open` created with `auth.Migrations()` as R-8G2K-IEYJ states, queried inside a `DB.Read` on a handle on it, `PRAGMA table_info('users')` MUST list the columns `id`, `issuer`, `subject`, `email`, and `last_google_login`; `PRAGMA table_info('sessions')` the columns `id`, `user_id`, `login_at`, and `last_used_at`; `PRAGMA table_info('login_states')` the columns `state`, `verifier`, and `return_url`; and `PRAGMA table_info('tokens')` the columns `id`, `user_id`, `name`, `hash`, `enabled`, `created_at`, `expires_at`, and `last_used_at`; each in that order and no other; and for every token a `*Store` over a handle on that database has created and not deleted, the `tokens` table MUST hold exactly one row whose `id` is that token's `ID`.
- R-78DO-JF6Z: For a database that `db.Open` created with `auth.Migrations()` as R-8G2K-IEYJ states, `sqlite_master` queried as R-8G2K-IEYJ queries it MUST show exactly two rows whose `type` is `index` and whose `name` does not begin with `sqlite_`, `sessions_user_id_idx` with `tbl_name` `sessions` and `tokens_user_id_idx` with `tbl_name` `tokens`, and `PRAGMA index_info` of each MUST list exactly the one column `user_id`.
- R-8IID-9YFX: When users, sessions, login states, and tokens were written through a `*Store` over a handle that `db.Open` returned for a path with `auth.Migrations()`, and that handle was then closed, then on a `*Store` that `New` returns over a later handle that `db.Open` returns for the same path with `auth.Migrations()`, every `LookupSessionIdentity`, `ListTokens`, and `LookupTokenIdentity` call MUST return what the same call with the same arguments returned on the first `*Store` just before its handle was closed, and `ConsumeLoginState` MUST return, for the `State` of every login state created and not consumed through the first `*Store`, that login state.
- R-8JQ9-NQ6M: While `SetFailing(true)` is in force on the handle a `*Store` was built over, a `SetFailing(true)` call on it having returned and no `SetFailing(false)` call on it having followed, every `UpsertUserOnLogin`, `CreateSession`, `LookupSessionIdentity`, `TouchSession`, `DeleteSession`, `CreateLoginState`, `ConsumeLoginState`, `CreateToken`, `ListTokens`, `SetTokenEnabled`, `DeleteToken`, `LookupTokenIdentity`, and `TouchTokenIdentity` call on that `*Store` MUST return a non-nil error for which `errors.Is(err, ErrNotFound)` is false; once a `SetFailing(false)` call on that handle has returned, the same `*Store` MUST again behave as the other requirements of this design state, with no new handle and no `New` call in between; and every other requirement of auth's design that states what one of those calls returns or does MUST be read as applying only to a `*Store` whose handle is open and on which `SetFailing(true)` is not in force.
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
- R-T1M1-KPKO: `CreateToken` MUST create a `Token` owned by `userID` whose `ID` is `TokenIDPrefix` followed by a freshly minted opaque id (via `NewID`), with the given `Name`, `Enabled` true, `CreatedAt` equal to `now`, `LastUsedAt` nil, and `Hash` equal to `HashSecret` of a freshly minted secret (via `NewSecret`); it MUST return that record together with the plaintext secret as its second result and MUST persist only the hash, never the plaintext.
- R-5O2A-0IHS: `CreateToken` MUST set `ExpiresAt` from `expiry`: nil for `ExpiryNever`, `now` plus 30 days for `Expiry30d`, `now` plus 90 days for `Expiry90d`, and `now` plus 365 days for `Expiry365d` (a day being 24 hours).
- R-5PA6-EA8H: `ListTokens` MUST return exactly the tokens owned by `userID` and no token owned by another user.
- R-5RPZ-5TPV: `SetTokenEnabled` MUST set `Enabled` to the argument for the token when `tokenID` is owned by `userID`, and MUST return `ErrNotFound` (changing nothing) when the id names a token owned by another user or names no token.
- R-5SXV-JLGK: `DeleteToken` MUST remove the token when `tokenID` is owned by `userID`, and MUST return `ErrNotFound` (changing nothing) when the id names a token owned by another user or names no token.
- R-5U5R-XD79: A token MUST be considered to authenticate at time `now` if and only if it is `Enabled`, it is unexpired (`ExpiresAt` is nil, or `ExpiresAt` is after `now`), and its owner's `LastGoogleLogin` satisfies `now - LastGoogleLogin <= TokenLoginWindow`.
- R-5VDO-B4XY: `LookupTokenIdentity` MUST return the owner's `Identity` when the token whose plaintext secret is `secret` authenticates at `now`, MUST return `ErrNotFound` in every other case (unknown secret, disabled token, expired token, or owner outside the login window — indistinguishable to the caller), and MUST modify no row in any case.
- R-5WLK-OWON: `TouchTokenIdentity` MUST, when the token whose plaintext secret is `secret` authenticates at `now`, set that token's `LastUsedAt` to `now` and return the owner's `Identity`; in every other case it MUST return `ErrNotFound` and MUST modify no row.
- R-SXYC-FECL: Every `Identity` that `LookupTokenIdentity` or `TouchTokenIdentity` returns with a nil error MUST have `TokenID` equal to the `ID` of the token whose plaintext secret is `secret`, and every `Identity` that `LookupSessionIdentity` or `TouchSession` returns with a nil error MUST have an empty `TokenID`.
