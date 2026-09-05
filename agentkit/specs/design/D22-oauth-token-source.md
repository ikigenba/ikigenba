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

**Rotation is reactive.** The rotator treats the bearer as opaque, never
parses an expiry, and rotates only when asked: `Rotate` posts the plain RFC
6749 refresh grant to `Rotation.RefreshURL`, and the conversation asks for it
exactly when a request comes back `401`. A vendor `401` is the only trigger; a
`403` is a permissions failure a fresh token cannot fix. Should the wasted
request per expiry ever matter, proactive rotation can be layered on later.

**The 401 path is keyed on the mode.** The conversation asks the authenticator
for its rotator's `AuthMode`. Under `oauth` it calls `Rotate` once with the
offering's `Rotation` and re-issues the request once, so a bad rotation cannot
loop. Under `api_key` it never rotates; the `401` surfaces as-is. This is not
D14 retry: `Retryable` still says auth is terminal, the retry driver is not
involved, and the whole exchange is silent: no event, no log entry.

**Errors come from the refresh endpoint.** A failed rotation is itself an HTTP
exchange with a status and a standard OAuth error body, which is more
informative than the vendor `401` that prompted it, so that is what surfaces:
a `*Error` of category auth carrying the refresh endpoint's status, `error`,
and `error_description`. An unreachable refresh endpoint is category
transport, like any other transport failure.

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

**Live rotation tests.** Every requirement below except the last is provable
offline: an `httptest` server standing in for the refresh endpoint, a
temporary directory for `FileTokenStore`, a fake `TokenStore` for the rotator,
and a fake `Rotator` for the conversation's `401` path. A real rotation
rotates the real refresh token, which is the strongest proof that the write
path is right, so two live tests exist as well, under the `live` build tag
that D23 defines, reading their token file paths from
`AGENTKIT_OPENAI_OAUTH_FILE` and `AGENTKIT_XAI_OAUTH_FILE` and failing, never
skipping, when a variable is unset. They run through `make live` (D23). The
requirement id for the live files rides on an offline architecture test that
proves the files exist with the build tag.

## REQUIREMENTS

- R-ZOPK-NW7S: `agentkit` MUST export the `TokenStore` interface whose method set is exactly `Read(ctx context.Context) ([]byte, error)` and `Write(ctx context.Context, data []byte) error`.
- R-ZPXH-1NYH: `agentkit` MUST export `func FileTokenStore(path string) TokenStore`.
- R-ZR5D-FFP6: `FileTokenStore(path).Read` MUST return the file's bytes, and when the file cannot be read MUST return the operating system's error unchanged, such that `errors.Is(err, fs.ErrNotExist)` holds for a missing file.
- R-ZSD9-T7FV: `FileTokenStore(path).Write` MUST replace the file's contents atomically by writing a temporary file in the same directory and renaming it over `path`, MUST create the file with mode `0600`, and MUST leave no temporary file behind whether the write succeeds or fails.
- R-KP30-B3OJ: `OAuthRotator(store)` MUST NOT call `store.Read` at construction; its `Token` MUST call `store.Read` exactly once on the first call and return the store's error unchanged when it fails, MUST return `ErrInvalidConfig` when the bytes read are not a JSON object with a non-empty `access_token` string, and otherwise MUST return a `Token` whose `Bearer` is the stored `access_token` and whose `AccountID` is the `chatgpt_account_id` claim under the `https://api.openai.com/auth` key of the access token's JWT payload when present and empty otherwise; every later `Token` call MUST return the same value without store or network access until `Rotate` succeeds.
- R-KQAW-OVF8: `Rotate(ctx, r)` on the rotator from `OAuthRotator(store)` MUST send exactly one `POST` to `r.RefreshURL` using `http.DefaultClient`, with `Content-Type: application/x-www-form-urlencoded`, `Accept: application/json`, and a form body whose fields are exactly `grant_type=refresh_token`, `refresh_token=<the stored refresh_token>`, and `client_id=<r.ClientID>`.
- R-KRIT-2N5X: `Rotate` MUST return `ErrInvalidConfig` and send no request when the stored bytes hold no non-empty `refresh_token` string or when `r.RefreshURL` is empty.
- R-KSQP-GEWM: When the refresh endpoint answers `Rotate` with a 2xx status and a JSON object holding a non-empty `access_token`, `Rotate` MUST call `store.Write` exactly once with that response body, except that a `refresh_token` absent from the response MUST be carried forward from the previously stored bytes, and MUST then return the new token, which every later `Token` call MUST also return.
- R-KTYL-U6NB: When the refresh endpoint answers `Rotate` with a non-2xx status, `Rotate` MUST NOT call `store.Write` and MUST return a `*Error` with `Category` `CategoryAuth`, `Status` the response status, `Code` the response body's `error` field, and `Message` its `error_description` field, either being empty when the body does not supply it; when the request fails before a response, `Rotate` MUST return a `*Error` with `Category` `CategoryTransport` and `Status` zero; and when `store.Write` fails, `Rotate` MUST return that error unchanged.
- R-KV6I-7YE0: Concurrent `Rotate` calls on one rotator MUST result in exactly one request to the refresh endpoint, and every such call MUST return the token that request produced.
- R-KWEE-LQ4P: When a request built with the authenticator from `o.Authenticator(r)` where `r.AuthMode()` is `AuthModeOAuth` receives an HTTP `401`, the `Conversation` MUST call `r.Rotate` exactly once with the `Rotation` of the `EndpointSpec` in `o.Endpoints` whose `AuthMode` is `AuthModeOAuth` and, when it succeeds, re-issue the request exactly once carrying the new token, emitting no `Event` for the exchange; a `401` on the re-issued request MUST surface from `Send` as that response's `*Error` with no further rotation, and any other status MUST NOT trigger a rotation.
- R-KXMA-ZHVE: When `r.Rotate` fails on the `401` path, the `Conversation` MUST NOT re-issue the request and MUST surface the rotation error from `Send` unchanged, such that `errors.As` finds the `*Error` that `Rotate` returned.
- R-KYU7-D9M3: A `401` received by a request built with the authenticator from `o.Authenticator(r)` where `r.AuthMode()` is `AuthModeAPIKey` MUST surface from `Send` as that response's `*Error` with the request issued exactly once and `r.Rotate` never called.
- R-L023-R1CS: The module MUST contain the files `oauth_refresh_openai_live_test.go` and `oauth_refresh_xai_live_test.go`, each beginning with the build constraint `//go:build live`, each containing a test that fails (never skips) unless `AGENTKIT_OPENAI_OAUTH_FILE` (respectively `AGENTKIT_XAI_OAUTH_FILE`) names a readable file, and otherwise builds `OAuthRotator` over `FileTokenStore` of that path, calls `Rotate` with the `Rotation` of the oauth `EndpointSpec` of the `openai`/`responses` offering of `gpt-5.4-mini` (respectively the `xai`/`responses` offering of `grok-4.3`), and asserts the file's `access_token` differs from its value before the call.
