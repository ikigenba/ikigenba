# D07-trail

The webhook events: the four kinds of event webhooks emits on the suite's event bus, their declaration to the events app, and the record of each in webhooks' own trail. It is cron's trail with webhooks' kinds.

## The four webhook events

A webhook's life is told by `created`, `rotated` and `deleted`, the **lifecycle** kinds, each made by the one tool call that makes that change, and by `received`, made by each accepted delivery. Each is named `webhook.<slug>.<kind>`: webhooks owns the first and the last word, so an agent naming a slug cannot make it emit another producer's event, and the slug rule (`D04-store`) makes every such name a valid event name. A lifecycle event carries `hook`, the webhook's id, and `scheme`. A `received` event carries `hook`; `delivery`, the delivery's id; `type`, the delivery's `X-GitHub-Event` for a `github-hmac` webhook and the empty string for a `bearer` one; `content_type`, as sent; and `bytes`, the body's length. The body never travels on the bus: a consumer fetches it with the `delivery` tool. No event carries a secret, a signature, a body, an email or any sender header but the content type and GitHub event the attributes name.

Every webhook event goes two ways at once, under the same name, attributes, request id and user: recorded in the trail through the run's writer, and emitted on the bus through the run's emitter. A lifecycle event takes the tool call's context: its request id, its caller, and the event cause `events.Middleware` read. A `received` event takes the context the ingress builds: the webhook's owner, the ingress request's id, no cause and depth 0 (`D05-ingress`).

## The declarations

`trail.Emits()` declares one pattern per kind, `webhook.*.<kind>`, with its attribute keys in order: the lifecycle kinds `hook`, `scheme`; `received` `hook`, `delivery`, `type`, `content_type`, `bytes`.

## REQUIREMENTS

- R-06O8-IOS4: The `internal/trail` package, imported from the path `github.com/ikigenba/ikigenba/webhooks/internal/trail` with the package name `trail`, MUST export the string constants `Created = "created"`, `Rotated = "rotated"`, `Deleted = "deleted"` and `Received = "received"`, `func Emits() []events.Emission`, `func Name(slug, kind string) string`, `func Lifecycle(ctx context.Context, w *telemetry.Writer, em *events.Emitter, kind string, h store.Webhook)` and `func Receive(ctx context.Context, w *telemetry.Writer, em *events.Emitter, h store.Webhook, d store.Delivery)`.
- R-07W4-WGIT: `trail.Emits` MUST return, on every call, a new slice of exactly four `events.Emission` values: `webhook.*.created`, `webhook.*.rotated` and `webhook.*.deleted`, each with the `Attrs` `hook`, `scheme` in that order, and `webhook.*.received` with the `Attrs` `hook`, `delivery`, `type`, `content_type`, `bytes` in that order.
- R-0941-A89I: `trail.Name(slug, kind)` MUST return `"webhook." + slug + "." + kind`.
- R-0ABX-O007: `trail.Lifecycle` with `kind` one of `Created`, `Rotated` and `Deleted` MUST record through `w` exactly one event and emit through `em` exactly one bus event, both named `trail.Name(h.Slug, kind)`, with `Attrs` exactly `hook`, `h.ID`, and `scheme`, `h.Scheme`, and the request id and user of the caller on `ctx`; and with any other `kind` it MUST record and emit nothing.
- R-0BJU-1RQW: `trail.Receive` MUST record through `w` exactly one event and emit through `em` exactly one bus event, both named `trail.Name(h.Slug, trail.Received)`, with `Attrs` exactly `hook`, `h.ID`; `delivery`, `d.ID`; `type`, `d.GitHubEvent` when `h.Scheme` is `store.GitHubHMAC` and the empty string otherwise; `content_type`, `d.ContentType`; and `bytes`, the length of `d.Body` as an integer; and the request id and user of the caller on `ctx`.
- R-0CRQ-FJHL: The bus event `trail.Lifecycle` or `trail.Receive` emits MUST carry as its `Cause` and `Depth` the event cause on `ctx`, its `ID` and its `Depth` plus one, and an empty `Cause` and `Depth` 0 when `ctx` carries none.
