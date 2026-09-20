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
**Token** is one personal access token: a random Crockford id used in its
action URLs (distinct from the secret), the owning user, a name, an optional
expiry, an enabled flag, when it was created, when it was last used, and the
hash of its secret — the plaintext secret is shown once at creation and never
stored.

Opaque ids and token secrets are Crockford base32. The alphabet is the digits
`0`–`9` and the letters `A`–`Z` with `I`, `L`, `O`, and `U` removed. An opaque
id is 16 random bytes encoded to 26 characters; auth uses that shape for user
ids, session ids, login-state values, and token ids alike. A token secret is
the literal prefix `ikp_` followed by the 52-character encoding of 32 random
bytes; only its SHA-256 hash is persisted, so a secret cannot be recovered from
the database.

Opening the store at `state/auth.db` creates the schema when the file is
absent, opens the existing database when the file is present, and fails when the
file is present but cannot be opened; a missing file is never that failure. The
operations cover provisioning a user on login, minting and ending sessions,
recording and consuming login state, and creating, listing, scoping, and
authenticating tokens. Two reads deliberately do not mutate — the identity
lookups behind `/me` and the profile — while their touching counterparts behind
`/check` update a last-use time, because a `/check` counts as use and a `/me`
does not.

Three windows bound validity, and their numbers are the contract. A session is
live only while its last use is within 15 minutes and its login is within 18
hours; either bound expires it. A token authenticates only while it is enabled,
unexpired, and its owner's most recent Google login is within 30 days. A token's
expiry, when it has one, is 30, 90, or 365 days after it was created; a token
may also have no expiry, which never expires.

## REQUIREMENTS

- R-41J3-NIWG: `internal/idcodec` MUST export `const Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"` and `const SecretPrefix = "ikp_"`.
- R-42R0-1AN5: `internal/idcodec` MUST export `func Encode(b []byte) string`.
- R-43YW-F2DU: `internal/idcodec` MUST export `func NewID(rand io.Reader) (string, error)`.
- R-456S-SU4J: `internal/idcodec` MUST export `func NewSecret(rand io.Reader) (string, error)`.
- R-46EP-6LV8: `internal/idcodec` MUST export `func HashSecret(secret string) string`.
- R-47ML-KDLX: `internal/store` MUST export `type User struct` with exactly the fields `ID string`, `Issuer string`, `Subject string`, `Email string`, and `LastGoogleLogin time.Time`.
- R-48UH-Y5CM: `internal/store` MUST export `type Session struct` with exactly the fields `ID string`, `UserID string`, `LoginAt time.Time`, and `LastUsedAt time.Time`.
- R-4A2E-BX3B: `internal/store` MUST export `type LoginState struct` with exactly the fields `State string`, `Verifier string`, and `ReturnURL string`.
- R-4CI7-3GKP: `internal/store` MUST export `type Token struct` with exactly the fields `ID string`, `UserID string`, `Name string`, `Hash string`, `Enabled bool`, `CreatedAt time.Time`, `ExpiresAt *time.Time`, and `LastUsedAt *time.Time`.
- R-4DQ3-H8BE: `internal/store` MUST export `type Identity struct` with exactly the fields `UserID string` and `Email string`.
- R-4EXZ-V023: `internal/store` MUST export `type Expiry string` and the constants `ExpiryNever Expiry = "never"`, `Expiry30d Expiry = "30d"`, `Expiry90d Expiry = "90d"`, and `Expiry365d Expiry = "365d"`.
- R-G4DV-A8BP: `internal/store` MUST export `const SessionIdle = 15 * time.Minute`.
- R-G5LR-O02E: `internal/store` MUST export `const SessionMax = 18 * time.Hour`.
- R-G81K-FJJS: `internal/store` MUST export `const TokenLoginWindow = 30 * 24 * time.Hour`.
- R-4HDS-MJJH: `internal/store` MUST export `var ErrNotFound error`, the sentinel every operation returns (wrapped or as-is, matchable with `errors.Is`) when the row it was asked for does not exist or is not the caller's.
- R-4ILP-0BA6: `internal/store` MUST export `type Store`, `func Open(source string, rand io.Reader) (*Store, error)`, and `func (*Store) Close() error`.
- R-4JTL-E30V: `internal/store` MUST export `func (*Store) UpsertUserOnLogin(issuer, subject, email string, now time.Time) (User, error)`.
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
- R-56ZO-NQ42: When `source` names a file that does not exist, `Open` MUST create that file, MUST create the schema in it, and MUST return a usable `*Store`.
- R-587L-1HUR: When `source` names an existing, openable database, `Open` MUST open it and return a usable `*Store` without recreating or discarding its existing rows.
- R-59FH-F9LG: When `source` names a file that exists but cannot be opened as this store's database, `Open` MUST return a non-nil error and no usable store; a `source` whose file is merely absent MUST NOT be treated as this failure.
- R-5AND-T1C5: On the first `UpsertUserOnLogin` for an `(issuer, subject)` pair, the store MUST create a `User` with a freshly minted opaque `ID` (via `NewID`), the given `Email`, and `LastGoogleLogin` equal to `now`, and MUST return that `User`.
- R-5BVA-6T2U: On a later `UpsertUserOnLogin` for an `(issuer, subject)` pair that already has a user, the store MUST keep the existing `ID`, MUST set `Email` to the given value and `LastGoogleLogin` to `now`, MUST NOT create a second row for that pair, and MUST return the updated `User`.
- R-5D36-KKTJ: `CreateSession` MUST create a `Session` with a freshly minted opaque `ID` (via `NewID`), `UserID` equal to the argument, and `LoginAt` and `LastUsedAt` both equal to `now`, and MUST return it.
- R-5EB2-YCK8: A session MUST be considered live at time `now` if and only if `now - LastUsedAt <= SessionIdle` and `now - LoginAt <= SessionMax`; exceeding either bound MUST make it not live.
- R-5GQV-PW1M: `LookupSessionIdentity` MUST return the owning user's `Identity` when the named session is live at `now`, MUST return `ErrNotFound` when the session is unknown or not live, and MUST NOT modify any row in either case.
- R-5HYS-3NSB: `TouchSession` MUST, when the named session is live at `now`, set that session's `LastUsedAt` to `now` and return the owning user's `Identity`; when the session is unknown or not live it MUST return `ErrNotFound` and MUST modify no row.
- R-5J6O-HFJ0: `DeleteSession` MUST remove the named session so that it no longer resolves; deleting a session that is absent MUST NOT be an error.
- R-5KEK-V79P: `CreateLoginState` MUST create a `LoginState` with a freshly minted opaque `State` (via `NewID`), the given `Verifier`, and the given `ReturnURL` (which may be empty), and MUST return it.
- R-5LMH-8Z0E: `ConsumeLoginState` MUST be single-use: for a state that exists it MUST return the stored `LoginState` and remove it so a second call with the same state returns `ErrNotFound`; for a state that is unknown or already consumed it MUST return `ErrNotFound`.
- R-5MUD-MQR3: `CreateToken` MUST create a `Token` owned by `userID` with a freshly minted `ID` (via `NewID`), the given `Name`, `Enabled` true, `CreatedAt` equal to `now`, `LastUsedAt` nil, and `Hash` equal to `HashSecret` of a freshly minted secret (via `NewSecret`); it MUST return that record together with the plaintext secret as its second result and MUST persist only the hash, never the plaintext.
- R-5O2A-0IHS: `CreateToken` MUST set `ExpiresAt` from `expiry`: nil for `ExpiryNever`, `now` plus 30 days for `Expiry30d`, `now` plus 90 days for `Expiry90d`, and `now` plus 365 days for `Expiry365d` (a day being 24 hours).
- R-5PA6-EA8H: `ListTokens` MUST return exactly the tokens owned by `userID` and no token owned by another user.
- R-5RPZ-5TPV: `SetTokenEnabled` MUST set `Enabled` to the argument for the token when `tokenID` is owned by `userID`, and MUST return `ErrNotFound` (changing nothing) when the id names a token owned by another user or names no token.
- R-5SXV-JLGK: `DeleteToken` MUST remove the token when `tokenID` is owned by `userID`, and MUST return `ErrNotFound` (changing nothing) when the id names a token owned by another user or names no token.
- R-5U5R-XD79: A token MUST be considered to authenticate at time `now` if and only if it is `Enabled`, it is unexpired (`ExpiresAt` is nil, or `ExpiresAt` is after `now`), and its owner's `LastGoogleLogin` satisfies `now - LastGoogleLogin <= TokenLoginWindow`.
- R-5VDO-B4XY: `LookupTokenIdentity` MUST return the owner's `Identity` when the token whose plaintext secret is `secret` authenticates at `now`, MUST return `ErrNotFound` in every other case (unknown secret, disabled token, expired token, or owner outside the login window — indistinguishable to the caller), and MUST modify no row in any case.
- R-5WLK-OWON: `TouchTokenIdentity` MUST, when the token whose plaintext secret is `secret` authenticates at `now`, set that token's `LastUsedAt` to `now` and return the owner's `Identity`; in every other case it MUST return `ErrNotFound` and MUST modify no row.
