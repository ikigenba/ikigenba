# D22-oauth-token-source

D7 defines the OAuth credential around a `TokenSource` the consumer supplies and
says nothing about where tokens come from. This design supplies the one
concrete source almost every consumer wants, without breaking D7's rule that
the library never persists anything itself: the source keeps its bytes behind a
tiny storage seam, `TokenStore`, and the consumer hands in the store. The root
ships one store, a file path; a consumer with a keyring or a database writes
their own two-method store, and that is the only intended extension point. The
`TokenSource` interface stays public so a test can fake it, but the design is
not user-extendable beyond the store.

The bytes a store holds are **opaque to the store** and are, by convention, the
verbatim token-endpoint response the `oauth` CLI writes:

```sh
oauth --auth-url ... --token-url ... --client-id ... > ~/.agentkit/x-ai-auth.json
```

The source is what understands them: it reads `access_token` and
`refresh_token` from the stored JSON object, and after a refresh writes the new
response back in the same shape, so a file produced by the CLI and a file
produced by a refresh are indistinguishable and either program can read the
other's.

What the source needs beyond the bytes — the token endpoint and the public
client id — is per-provider knowledge, so it lives on the catalog offering
(D21) as `Offering.OAuth`, and the source is constructed from the offering:

```go
offering, _ := agentkit.Lookup("grok-4.5", "xai", "")
src, _      := offering.TokenSource(agentkit.FileTokenStore(path))
auth, _     := offering.Authenticator(agentkit.OAuth(src))
ep, _       := agentkit.NewEndpoint(offering.BaseURL, auth)
conv, _     := agentkit.New(offering.WireFormat, ep, offering.WireModel, cfg)
```

Only OpenAI and xAI accept OAuth; Anthropic's terms do not permit it, so its
offerings list `api_key` alone.

```go
package agentkit

// TokenStore is where a TokenSource keeps its bytes. The bytes are opaque to
// the store. This is the sole intended extension point of the OAuth path.
type TokenStore interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
}

// FileTokenStore is a TokenStore over one file. Read passes the OS error
// through unchanged, so a consumer can detect "not logged in yet" with
// errors.Is(err, fs.ErrNotExist). Write is atomic and creates the file 0600.
func FileTokenStore(path string) TokenStore

// TokenSource builds the concrete OAuth source for this offering over store.
// It reads the store once, here, and fails fast on an unusable store.
func (o Offering) TokenSource(store TokenStore) (TokenSource, error)
```

**Refresh is reactive.** The source treats the bearer as opaque, never parses
an expiry, and refreshes only when asked: `Refresh` posts the plain RFC 6749
refresh grant to `Offering.OAuth.TokenURL`, and the conversation asks for it
exactly when a request comes back `401`. A vendor `401` is the only trigger; a
`403` is a permissions failure a fresh token cannot fix. Should the wasted
request per expiry ever matter, proactive refresh can be layered on later.

**The 401 path is credential-agnostic.** The conversation does not know about
`TokenSource`; it asks the *authenticator* whether it can re-mint, through an
unexported hook the OAuth authenticator implements by calling `Refresh`. A future
body-signing authenticator (SigV4, whose STS session credentials also expire) would
implement the same hook, so nothing here boxes that in. On a `401` the
conversation re-mints once and re-issues the request once, so a bad refresh
cannot loop. This is not D14 retry: `Retryable` still says auth is terminal,
the retry driver is not involved, and the whole exchange is silent — no event,
no log entry.

**Errors come from the token endpoint.** A failed refresh is itself an HTTP
exchange with a status and a standard OAuth error body, which is more
informative than the vendor `401` that prompted it, so that is what surfaces: a
`*Error` of category auth carrying the token endpoint's status, `error`, and
`error_description`. An unreachable token endpoint is category transport, like
any other transport failure.

**Refresh tokens rotate, or don't.** OpenAI returns a new `refresh_token` on
every refresh and invalidates the old one; other services omit the field,
meaning keep the one you have. The source writes the response back with a
missing `refresh_token` carried forward from the previous bytes, so the store
always holds a usable refresh token afterwards.

**One refresh at a time.** A source may back several conversations. Concurrent
`Refresh` calls collapse into one token request; the others wait and receive
its result. With rotating refresh tokens a second concurrent refresh would
present a token the first had just killed.

**OpenAI's account id** is not a field of the token response; it is the
`chatgpt_account_id` claim under the `https://api.openai.com/auth` key in the
access token's JWT payload. The source decodes it (unverified — it is the
vendor's own token being handed back to the vendor) for the two OpenAI
offerings and leaves `AccountID` empty otherwise. The authenticator (D7)
already refuses an empty `AccountID` for OpenAI.

**Live refresh tests.** Every requirement below except the last two is provable
offline: an `httptest` server standing in for the token endpoint, a temporary
directory for `FileTokenStore`, a fake `TokenStore` for the source, and a fake
`TokenSource` for the conversation's `401` path. A real refresh rotates the
real refresh token, which is the strongest proof that the write path is right,
so two live tests exist as well: `oauth_refresh_openai_live_test.go` and
`oauth_refresh_xai_live_test.go`, under `//go:build integration`, each reading
its token file path from `AGENTKIT_OPENAI_OAUTH_FILE` or
`AGENTKIT_XAI_OAUTH_FILE` and skipping when the variable is unset. They are run
only by hand through `make live-oauth`, which sets both variables to the files
under `~/.agentkit` and runs `go test -tags integration`. The normal gates
never set the variables and never pass the tag, so they never rotate a token.
The requirement id for the live files rides on an offline architecture test
that proves the files exist with the build tag; the behavior itself is a
human-run check, per AGENTS.md.

## REQUIREMENTS

- R-ZOPK-NW7S: `agentkit` MUST export the `TokenStore` interface whose method set is exactly `Read(ctx context.Context) ([]byte, error)` and `Write(ctx context.Context, data []byte) error`.
- R-ZPXH-1NYH: `agentkit` MUST export `func FileTokenStore(path string) TokenStore`.
- R-ZR5D-FFP6: `FileTokenStore(path).Read` MUST return the file's bytes, and when the file cannot be read MUST return the operating system's error unchanged, such that `errors.Is(err, fs.ErrNotExist)` holds for a missing file.
- R-ZSD9-T7FV: `FileTokenStore(path).Write` MUST replace the file's contents atomically by writing a temporary file in the same directory and renaming it over `path`, MUST create the file with mode `0600`, and MUST leave no temporary file behind whether the write succeeds or fails.
- R-ZTL6-6Z6K: `agentkit` MUST export `func (o Offering) TokenSource(store TokenStore) (TokenSource, error)`.
- R-ZUT2-KQX9: `Offering.TokenSource` MUST return `ErrInvalidConfig` without calling `store.Read` when `store` is nil or `o.AuthModes` does not contain `AuthModeOAuth`; MUST call `store.Read` exactly once and return its error unchanged when it fails; and MUST return `ErrInvalidConfig` when the bytes read are not a JSON object with a non-empty `access_token` string.
- R-KMON-3K7L: The source from `Offering.TokenSource` MUST return from `Token` a `Token` whose `Bearer` is the stored `access_token` without any store or network access, and whose `AccountID` is the `chatgpt_account_id` claim under the `https://api.openai.com/auth` key of the access token's JWT payload when `o.ID` is `OfferingOpenAIResponses` or `OfferingOpenAIChat` and the claim is present, and empty otherwise.
- R-ZX8V-CAEN: `Refresh` on the source from `Offering.TokenSource` MUST send exactly one `POST` to `o.OAuth.TokenURL` using `http.DefaultClient`, with `Content-Type: application/x-www-form-urlencoded`, `Accept: application/json`, and a form body whose fields are exactly `grant_type=refresh_token`, `refresh_token=<the stored refresh_token>`, and `client_id=<o.OAuth.ClientID>`.
- R-ZYGR-Q25C: `Refresh` MUST return `ErrInvalidConfig` and send no request when the stored bytes hold no non-empty `refresh_token` string.
- R-ZZOO-3TW1: When the token endpoint answers `Refresh` with a 2xx status and a JSON object holding a non-empty `access_token`, `Refresh` MUST call `store.Write` exactly once with that response body, except that a `refresh_token` absent from the response MUST be carried forward from the previously stored bytes, and MUST then return the new token, which every later `Token` call MUST also return.
- R-00WK-HLMQ: When the token endpoint answers `Refresh` with a non-2xx status, `Refresh` MUST NOT call `store.Write` and MUST return a `*Error` with `Category` `CategoryAuth`, `Status` the response status, `Code` the response body's `error` field, and `Message` its `error_description` field, either being empty when the body does not supply it; when the request fails before a response, `Refresh` MUST return a `*Error` with `Category` `CategoryTransport` and `Status` zero; and when `store.Write` fails, `Refresh` MUST return that error unchanged.
- R-024G-VDDF: Concurrent `Refresh` calls on one source MUST result in exactly one request to the token endpoint, and every such call MUST return the token that request produced.
- R-KNWJ-HBYA: When a request built with the authenticator from `Authenticator(OAuth(src))` receives an HTTP `401`, the `Conversation` MUST call `src.Refresh` exactly once and, when it succeeds, re-issue the request exactly once carrying the new token, emitting no `Event` for the exchange; a `401` on the re-issued request MUST surface from `Send` as that response's `*Error` with no further refresh, and any other status MUST NOT trigger a refresh.
- R-04K9-MWUT: When `src.Refresh` fails on the `401` path, the `Conversation` MUST NOT re-issue the request and MUST surface the refresh error from `Send` unchanged, such that `errors.As` finds the `*Error` that `Refresh` returned.
- R-KP4F-V3OZ: A `401` received by a request built with the authenticator from `Authenticator(APIKey(k))` MUST surface from `Send` as that response's `*Error` with the request issued exactly once.
- R-FL18-TBCM: The module MUST contain the files `oauth_refresh_openai_live_test.go` and `oauth_refresh_xai_live_test.go`, each beginning with the build constraint `//go:build integration`, each containing a test that skips unless `AGENTKIT_OPENAI_OAUTH_FILE` (respectively `AGENTKIT_XAI_OAUTH_FILE`) names a readable file, and otherwise constructs a source from the `openai`/`responses` (respectively `xai`/`responses`) offering over `FileTokenStore` of that path, calls `Refresh`, and asserts the file's `access_token` differs from its value before the call.
- R-FM95-733B: The module's `Makefile` MUST declare a `live-oauth` target that sets `AGENTKIT_OPENAI_OAUTH_FILE` to `$(HOME)/.agentkit/openai-auth.json` and `AGENTKIT_XAI_OAUTH_FILE` to `$(HOME)/.agentkit/x-ai-auth.json` and runs `go test -tags integration -run OAuthRefresh ./...`, and no other target or gate MUST pass `-tags integration`.
