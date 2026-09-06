# D22-oauth-token-source

D7 defines the OAuth credential as a rotator, `OAuthRotator`, and says nothing
about where tokens come from. This design supplies that: the rotator keeps its
bytes behind a tiny storage seam, `TokenStore`, and the consumer hands in the
store. The root ships one store, a file path; a consumer with a keyring or a
database writes their own two-method store, and that is the only intended
extension point. The library never persists anything on its own.

The bytes a store holds are **opaque to the store** and are, by convention, the
verbatim token-endpoint response the `oauth` CLI writes:

```sh
oauth --auth-url ... --token-url ... --client-id ... > ~/.agentkit/x-ai-auth.json
```

The rotator is what understands them: it reads `access_token` and
`refresh_token` from the stored JSON object, and after a rotation writes the
new response back in the same shape, so a file produced by the CLI and a file
produced by a rotation are indistinguishable and either program can read the
other's.

What a rotation needs beyond the bytes, the refresh endpoint and the public
client id, is per-provider knowledge, so it lives on the catalog offering
(D21) as the `Rotation` of the offering's oauth `EndpointSpec`. The rotator
does not know the offering; the authenticator hands it the `Rotation` when it
asks for a rotation:

```go
offering, _ := agentkit.Lookup("grok-4.5", "xai", "")
auth, _     := offering.Authenticator(agentkit.OAuthRotator(agentkit.FileTokenStore(path)))
ep, _       := agentkit.NewEndpoint(auth)
conv, _     := agentkit.New(offering.WireFormat, ep, offering.WireModel, cfg)
```

Only OpenAI's responses protocol and xAI accept OAuth; Anthropic's terms do not
permit it, so its offerings list an `api_key` spec alone (D7, D21).

```go
package agentkit

// TokenStore is where an OAuthRotator keeps its bytes. The bytes are opaque
// to the store. This is the sole intended extension point of the OAuth path.
type TokenStore interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
}

// FileTokenStore is a TokenStore over one file. Read passes the OS error
// through unchanged, so a consumer can detect "not logged in yet" with
// errors.Is(err, fs.ErrNotExist). Write is atomic and creates the file 0600.
func FileTokenStore(path string) TokenStore
```

**Token reads lazily and caches.** `OAuthRotator(store)` touches nothing at
construction. The first `Token` call reads the store once and keeps the parsed
token; later `Token` calls return it without store or network access until a
`Rotate` replaces it. An unreadable store surfaces from `Token` as the store's
own error; unusable bytes surface as `ErrInvalidConfig`.

**Rotation happens in two places, and both are needed.** The rotator
itself rotates only when asked: `Rotate` posts the plain RFC 6749 refresh
grant to `Rotation.RefreshURL`. Who asks, and when, is the authenticator
and the conversation:

- *Proactively*, before a request, when the stored access token says it is
  about to expire. Both vendors' access tokens are JWTs carrying an `exp`
  claim (observed in both stored files), so the rotator reads it, unverified,
  the way it reads the account id, into `Token.ExpiresAt`, zero when the
  bearer is not a JWT or carries no numeric `exp`. The OAuth authenticator
  compares it against the clock on every request and rotates when the token
  expires within `OAuthRefreshWindow`, five minutes, the same window
  OpenAI's own Codex client uses. A token that would expire mid-request is
  as useless as one already expired, hence a window rather than the instant.
  A proactive rotation that fails is the request's failure: nothing is sent,
  and the refresh endpoint's error surfaces.
- *Reactively*, after a request, when the vendor rejects the credential. This
  cannot be replaced by the proactive check: a token can be revoked, or
  simply refused, before its `exp`, and the tampered-but-unexpired capture
  proved xAI refuses one exactly like an expired one.

**The vendor says "rejected credential" in its own dialect, so the wire
translates it.** The conversation never reads a status code to decide on a
rotation. It asks whether the wire classified the response as a *rejected
credential* (D5), a classification each wire assigns from live-captured
facts: `OpenAIResponsesWire()` on a `401`; `XAIResponsesWire()` and
`XAIChatWire()` on a `403` whose body carries
`code: unauthenticated:bad-credentials`, because xAI never answers a bad
token with `401` (its `401` means no credential was sent at all, which no
rotation can fix) and answers a bad API key with `400`. The earlier form of
this design hard-coded `401` as the only trigger and declared `403` a
permission failure; on xAI that left an expired token unrecoverable until the
file was replaced by hand, which is the defect this revision fixes.

**The re-issue path is keyed on the mode.** The conversation asks the
authenticator for its rotator's `AuthMode`. Under `oauth`, on a
rejected-credential response it calls `Rotate` once with the offering's
`Rotation` and re-issues the request once, so a bad rotation cannot loop; a
second rejection surfaces as-is. Under `api_key` it never rotates; the
response surfaces as-is whatever the wire classified. This is not D14 retry:
`Retryable` still says auth is terminal, the retry driver is not involved,
and the whole exchange is silent: no event, no log entry.

**Errors come from the refresh endpoint.** A failed rotation is itself an HTTP
exchange with a status and a standard OAuth error body, which is more
informative than the vendor rejection that prompted it, so that is what
surfaces: a `*Error` of category auth carrying the refresh endpoint's status,
`error`, and `error_description`. Captured live: xAI answers a bad refresh
token `400 invalid_grant` and a bad client id `401 invalid_client`; OpenAI
answers `401 token_expired` and `401 invalid_client` respectively. An
unreachable refresh endpoint is category transport, like any other transport
failure.

**Refresh tokens rotate, or don't.** OpenAI returns a new `refresh_token` on
every rotation and invalidates the old one; xAI does too, as observed live.
Other services may omit the field, meaning keep the one you have. The rotator
writes the response back with a missing `refresh_token` carried forward from
the previous bytes, so the store always holds a usable refresh token
afterwards.

**One rotation at a time.** A rotator may back several conversations.
Concurrent `Rotate` calls collapse into one refresh request; the others wait
and receive its result. With rotating refresh tokens a second concurrent
rotation would present a token the first had just killed.

**OpenAI's account id** is not a field of the token response; it is the
`chatgpt_account_id` claim under the `https://api.openai.com/auth` key in the
access token's JWT payload. The rotator decodes it (unverified; it is the
vendor's own token being handed back to the vendor) whenever the claim is
present and leaves `AccountID` empty otherwise. Only the OpenAI responses wire
reads it (D7).

**Live rotation and re-issue tests.** Every requirement below except the
live-file ones is provable offline: an `httptest` server standing in for the
refresh endpoint or the vendor, a temporary directory for `FileTokenStore`, a
fake `TokenStore` for the rotator, a fake `Rotator` for the conversation's
re-issue path, and a crafted JWT (any three base64url segments whose payload
carries `exp`; the signature is never checked) for `ExpiresAt`. A real
rotation rotates the real refresh token, which is the strongest proof that
the write path is right, so live tests exist as well, under the `live` build
tag that D23 defines, reading their token file paths from
`AGENTKIT_OPENAI_OAUTH_FILE` and `AGENTKIT_XAI_OAUTH_FILE` and failing, never
skipping, when a variable is unset. Two rotate directly. Two more prove the
reactive path end to end on each host: they wrap the real file in a store
whose `Read` hands back the bytes with the access token's signature segment
altered, so the vendor rejects it exactly as it rejects an expired one, and
whose `Write` goes to the real file, so the rotated refresh token is never
lost; a text turn then succeeds only if the wire classified the rejection,
the rotation ran, and the re-issue carried the new token. They run through
`make live` (D23). The requirement ids for the live files ride on offline
architecture tests that prove the files exist with the build tag.

## REQUIREMENTS

- R-ZOPK-NW7S: `agentkit` MUST export the `TokenStore` interface whose method set is exactly `Read(ctx context.Context) ([]byte, error)` and `Write(ctx context.Context, data []byte) error`.
- R-ZPXH-1NYH: `agentkit` MUST export `func FileTokenStore(path string) TokenStore`.
- R-ZR5D-FFP6: `FileTokenStore(path).Read` MUST return the file's bytes, and when the file cannot be read MUST return the operating system's error unchanged, such that `errors.Is(err, fs.ErrNotExist)` holds for a missing file.
- R-ZSD9-T7FV: `FileTokenStore(path).Write` MUST replace the file's contents atomically by writing a temporary file in the same directory and renaming it over `path`, MUST create the file with mode `0600`, and MUST leave no temporary file behind whether the write succeeds or fails.
- R-J0CD-4UM5: `OAuthRotator(store)` MUST NOT call `store.Read` at construction; its `Token` MUST call `store.Read` exactly once on the first call and return the store's error unchanged when it fails, MUST return `ErrInvalidConfig` when the bytes read are not a JSON object with a non-empty `access_token` string, and otherwise MUST return a `Token` whose `Bearer` is the stored `access_token`, whose `AccountID` is the `chatgpt_account_id` claim under the `https://api.openai.com/auth` key of the access token's JWT payload when present and empty otherwise, and whose `ExpiresAt` is the `exp` claim of that payload read as whole seconds since the Unix epoch when present and numeric and the zero `time.Time` otherwise, the token `Rotate` returns being derived the same way from the new `access_token`; every later `Token` call MUST return the same value without store or network access until `Rotate` succeeds.
- R-KQAW-OVF8: `Rotate(ctx, r)` on the rotator from `OAuthRotator(store)` MUST send exactly one `POST` to `r.RefreshURL` using `http.DefaultClient`, with `Content-Type: application/x-www-form-urlencoded`, `Accept: application/json`, and a form body whose fields are exactly `grant_type=refresh_token`, `refresh_token=<the stored refresh_token>`, and `client_id=<r.ClientID>`.
- R-KRIT-2N5X: `Rotate` MUST return `ErrInvalidConfig` and send no request when the stored bytes hold no non-empty `refresh_token` string or when `r.RefreshURL` is empty.
- R-KSQP-GEWM: When the refresh endpoint answers `Rotate` with a 2xx status and a JSON object holding a non-empty `access_token`, `Rotate` MUST call `store.Write` exactly once with that response body, except that a `refresh_token` absent from the response MUST be carried forward from the previously stored bytes, and MUST then return the new token, which every later `Token` call MUST also return.
- R-KTYL-U6NB: When the refresh endpoint answers `Rotate` with a non-2xx status, `Rotate` MUST NOT call `store.Write` and MUST return a `*Error` with `Category` `CategoryAuth`, `Status` the response status, `Code` the response body's `error` field, and `Message` its `error_description` field, either being empty when the body does not supply it; when the request fails before a response, `Rotate` MUST return a `*Error` with `Category` `CategoryTransport` and `Status` zero; and when `store.Write` fails, `Rotate` MUST return that error unchanged.
- R-KV6I-7YE0: Concurrent `Rotate` calls on one rotator MUST result in exactly one request to the refresh endpoint, and every such call MUST return the token that request produced.
- R-J1K9-IMCU: When a request built with the authenticator from `o.Authenticator(r)` where `r.AuthMode()` is `AuthModeOAuth` receives a response that `o.WireFormat` classifies as a rejected credential (D5), the `Conversation` MUST call `r.Rotate` exactly once with the `Rotation` of the `EndpointSpec` in `o.Endpoints` whose `AuthMode` is `AuthModeOAuth` and, when it succeeds, re-issue the request exactly once carrying the new token, emitting no `Event` for the exchange; a rejected-credential response to the re-issued request MUST surface from `Send` as that response's `*Error` with no further rotation, and a response the wire does not so classify MUST NOT trigger a rotation.
- R-J2S5-WE3J: When `r.Rotate` fails on the rejected-credential re-issue path, the `Conversation` MUST NOT re-issue the request and MUST surface the rotation error from `Send` unchanged, such that `errors.As` finds the `*Error` that `Rotate` returned.
- R-J402-A5U8: A response `o.WireFormat` classifies as a rejected credential, received by a request built with the authenticator from `o.Authenticator(r)` where `r.AuthMode()` is `AuthModeAPIKey`, MUST surface from `Send` as that response's `*Error` with the request issued exactly once and `r.Rotate` never called.
- R-J6FV-1PBM: `agentkit` MUST export the constant `OAuthRefreshWindow time.Duration = 5 * time.Minute`.
- R-J7NR-FH2B: The authenticator from `o.Authenticator(r)` where `r.AuthMode()` is `AuthModeOAuth` MUST, on every request, when the `Token` returned by `r.Token(ctx)` has a non-zero `ExpiresAt` that is not after `time.Now().Add(OAuthRefreshWindow)`, call `r.Rotate(ctx, rotation)` exactly once with the `Rotation` of the `EndpointSpec` in `o.Endpoints` whose `AuthMode` is `AuthModeOAuth` before the request is sent and transmit the `Bearer` (and, where the wire places it, `AccountID`) of the token `Rotate` returned; and MUST NOT call `r.Rotate` when `ExpiresAt` is zero or is after that instant.
- R-EBV0-BHS5: When the `r.Rotate` call made before a request because `Token.ExpiresAt` fell within `OAuthRefreshWindow` fails, `Authenticate` on the authenticator from `o.Authenticator(r)` MUST return that error unchanged, the `Conversation` MUST send no request, and `Send` MUST surface the error such that `errors.As` finds the `*Error` that `Rotate` returned.
- R-ED2W-P9IU: The module MUST contain the files `oauth_reissue_openai_live_test.go` and `oauth_reissue_xai_live_test.go`, each beginning with the build constraint `//go:build live`, each containing a test whose name begins with `TestLive` that fails (never skips) unless `AGENTKIT_OPENAI_OAUTH_FILE` (respectively `AGENTKIT_XAI_OAUTH_FILE`) names a readable file, and otherwise builds a `Conversation` for the `openai`/`responses` offering of `gpt-5.4-mini` (respectively the `xai`/`responses` offering of `grok-4.3`) from `Offering.Authenticator` with `OAuthRotator` over a `TokenStore` whose `Read` returns the file's bytes with the last four characters of the `access_token`'s JWT signature segment replaced by different base64url characters and whose `Write` writes to the file, `NewEndpoint(auth)` with no `WithBaseURL`, and `New`; runs a text turn; and asserts `Stream.Err()` is nil, a `MessageDone` holds a non-empty `Text` block, and the file's `access_token` differs from its value before the turn.
- R-L023-R1CS: The module MUST contain the files `oauth_refresh_openai_live_test.go` and `oauth_refresh_xai_live_test.go`, each beginning with the build constraint `//go:build live`, each containing a test that fails (never skips) unless `AGENTKIT_OPENAI_OAUTH_FILE` (respectively `AGENTKIT_XAI_OAUTH_FILE`) names a readable file, and otherwise builds `OAuthRotator` over `FileTokenStore` of that path, calls `Rotate` with the `Rotation` of the oauth `EndpointSpec` of the `openai`/`responses` offering of `gpt-5.4-mini` (respectively the `xai`/`responses` offering of `grok-4.3`), and asserts the file's `access_token` differs from its value before the call.
