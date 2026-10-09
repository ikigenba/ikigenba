# D05-ingress

The public ingress: `internal/ingress`, which answers every request whose path begins `/in/` on webhooks' own host. A sender outside the suite — GitHub, n8n, curl — `POST`s a delivery to `/in/<slug>` and proves it holds the webhook's secret by its scheme; an accepted delivery is stored and then announced on the bus. The manifest's `guests = true` lets nginx pass such a request without a credential (`D02-cli`); whatever identity a request carries anyway is ignored, so a delivery is admitted by its secret alone.

## The answer, in a fixed order

1. A method other than `POST` is `405`, with `Allow: POST`.
2. A path whose remainder after `/in/` is not a valid slug, and a slug no webhook has, are `404`.
3. A database that cannot be read is `503`, with `store.Unreachable` and a newline as plain text.
4. For a `bearer` webhook, a request that does not carry exactly one `X-Webhook-Secret` header whose SHA-256 equals the kept hash, compared in constant time, is `404`.
5. The body is read up to one byte past `store.MaxBody`; a longer body is `413`. A body that cannot be read is `400`.
6. For a `github-hmac` webhook, a request that does not carry exactly one `X-Hub-Signature-256` header of `sha256=` and the hexadecimal HMAC-SHA256 of the raw body under the kept secret, compared with `hmac.Equal`, is `404`. The capped body is read before the signature is checked, as the decisions document says, so a `github-hmac` webhook's slug answers an oversized body `413` before any signature is checked.
7. The delivery and the webhook's last received are written in one transaction (`D04-store`); a database that cannot be written is `503`, and a webhook deleted meanwhile is `404`.
8. Otherwise the answer is `202` with an empty body, and then the `received` event is emitted (`D07-trail`).

Every `404` of steps 2, 4, 6 and 7 is one identical answer — status, headers and body — so the endpoint never says which slugs exist or why a delivery was refused; its body is empty, as are those of `405`, `413` and `202`. Any content type is accepted and stored as given.

## The delivery and its event

The delivery keeps the body exactly as sent and three headers, `Content-Type`, `X-GitHub-Event` and `X-GitHub-Delivery`, as given; no other sender header is kept anywhere. Store, then answer, then emit: the event is emitted after the `202`, so a crash between the two loses the event while the delivery stays fetchable, the same window cron accepts; it is stated, not prevented. The `received` event is emitted as the webhook's owner — the owner's user id, under the ingress request's id — with no cause and depth 0, whatever `X-Event-Cause`, `X-Event-Depth`, `X-User-Id` or `X-User-Email` the request carried.

## Recorded decisions

- The identical `404` has an empty body, no `Content-Type` and `Connection: close`, the cheapest answer that cannot differ between its causes: `net/http` closes a connection whose large unread body it will not drain, and a refusal that always closes cannot be told apart by that. The ingress is not a page, so it never draws the not-found page.
- A `bearer` webhook's secret is checked before its body is read, so an unauthenticated sender's body is never read; a `github-hmac` webhook's body must be read first.
- A sender header repeated, the secret or the signature twice, is refused as a wrong one: a check that picks one of several could be fooled.
- A body that cannot be read, the sender gone mid-body, is `400`, an answer nobody receives; no delivery is kept.

## REQUIREMENTS

- R-Z9QY-6VRD: The `internal/ingress` package, imported from the path `github.com/ikigenba/ikigenba/webhooks/internal/ingress` with the package name `ingress`, MUST export `type Config struct { Store *store.Store; Telemetry *telemetry.Writer; Events *events.Emitter }`, `func Handler(cfg Config) http.Handler`, `func Admits(h store.Webhook, header http.Header, body []byte) bool`, and the string constants `AuthHeader = "X-Webhook-Secret"`, `SignatureHeader = "X-Hub-Signature-256"`, `SignaturePrefix = "sha256="`, `GitHubEventHeader = "X-GitHub-Event"` and `GitHubDeliveryHeader = "X-GitHub-Delivery"`.
- R-ZC6Q-YF8R: For a webhook `h` whose `Scheme` is `store.Bearer`, `Admits` MUST return true exactly when `header` holds exactly one `X-Webhook-Secret` value `v` and `store.HashSecret(v)` equals `h.SecretSHA256`, whatever `body` is.
- R-ZDEN-C6ZG: For a webhook `h` whose `Scheme` is `store.GitHubHMAC`, `Admits` MUST return true exactly when `header` holds exactly one `X-Hub-Signature-256` value, that value is `sha256=` followed by hexadecimal digits that decode to the HMAC-SHA256 of `body` keyed by `h.SecretPlain`, and `h.SecretPlain` is not empty; so that a body signed with `openssl dgst -sha256 -hmac <secret>` and sent with its digest after `sha256=` is admitted, and the same body changed by one byte is not.
- R-ZEMJ-PYQ5: For a webhook whose `Scheme` is neither `store.Bearer` nor `store.GitHubHMAC`, `Admits` MUST return false.
- R-ZFUG-3QGU: The handler `ingress.Handler(cfg)` returns MUST answer a request whose method is not `POST` with status 405, an `Allow` header of exactly `POST` and an empty body, whatever its path below `/in/`, keeping no delivery and emitting nothing through `cfg.Events`.
- R-ZH2C-HI7J: webhooks' design defines **the refusal** as the answer with status 404, a `Connection: close` header, no other header but those `net/http` sets for an empty body, and an empty body; the handler MUST answer with the refusal a `POST` whose path's remainder after `/in/` is not a valid slug or is a slug no webhook has, a `POST` to a `bearer` webhook that `Admits` refuses, and a `POST` to a `github-hmac` webhook with a body of at most `store.MaxBody` bytes that `Admits` refuses; and every refusal MUST be the same bytes on the wire but for the `Date` header, keep no delivery and change no webhook.
- R-ZIA8-V9Y8: The handler MUST answer a `POST` to an existing webhook, admitted by its secret when its scheme is `bearer`, whose body is longer than `store.MaxBody` bytes with status 413 and an empty body, keeping no delivery.
- R-ZJI5-91OX: When the store cannot be read or written while a `POST` to `/in/<slug>` is answered, the handler MUST answer with status 503, a `Content-Type` of `text/plain; charset=utf-8` and the body `store.Unreachable` followed by a newline, keeping no delivery and emitting nothing through `cfg.Events`.
- R-ZKQ1-MTFM: The handler MUST answer a `POST` that `Admits` admits, with a body of at most `store.MaxBody` bytes, with status 202 and an empty body, having by then kept a delivery to that webhook whose body is the request's body byte for byte, whose `ContentType`, `GitHubEvent` and `GitHubDelivery` are the request's first `Content-Type`, `X-GitHub-Event` and `X-GitHub-Delivery` values, the empty string for each absent, and having set the webhook's `LastReceived` to that delivery's `Received`; whatever the request's content type, its `X-User-Id` and `X-User-Email` headers included.
- R-ZLXY-0L6B: For each delivery the handler keeps, it MUST emit exactly one `received` event as `D07-trail` states, under a context whose caller is the webhook's owner — `UserID` its `OwnerID` — and whose request id is the request's id, with no event cause, whatever `X-Event-Cause`, `X-Event-Depth`, `X-User-Id` and `X-User-Email` the request carried; and it MUST emit nothing through `cfg.Events` or `cfg.Telemetry` for a request it does not answer 202.
- R-ZN5U-ECX0: The handler MUST NOT write the secret a request carried, its signature, its body, or any of its headers but the three it keeps, to `cfg.Telemetry`, `cfg.Events`, or any stream; so that after accepted and refused deliveries carrying a marked secret, body and header value, no event in either capture and nothing on standard error contains them.
