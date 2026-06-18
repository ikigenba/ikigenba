# Plan — `notify` MCP `send`: an agent-driven push verb

## Context

`notify` is the suite's first event-plane **consumer**. Today its entire reason
for existing is reactive: two background consumer loops watch crm's
`contact.created` and prompts's `run.succeeded`/`run.failed` and fire a
best-effort ntfy.sh push for each (`internal/push`). Its MCP surface is only the
two read-only chassis tools — `health` (the north/south auth proof) and
`reflection` (its event-graph edges). An agent connected to notify can inspect
it but cannot *use* it.

We want to let a connected agent **proactively** push a notification to the
owner's device — "the build failed", "your report is ready", "three invoices are
overdue" — without first wiring up an event. That is a single new write verb over
the same ntfy publish that the consumer already performs, exposed over MCP.

The guiding constraint is **MCP surface economy**. ntfy's publish API has a long
tail (action buttons, attachments, scheduled delay, markdown, email/SMS
forwarding, custom icons). Exposing all of it is bloat. We expose the tight core
that makes a notification *useful and actionable* and defer the rest — each
deferred feature is additive later without breaking the verb's shape.

**Read first — do not re-derive:**

- `../crm` — the reference MCP service. Its `internal/mcp/{mcp.go,tools.go}` is the
  idiom this plan follows: one tool per verb, synchronous calls, a typed
  closed-vocabulary error envelope (`errorEnvelope`/`toolErr`), and a tight success
  shape. "Tool count is a function of verbs, not entities" (crm `CLAUDE.md`).
- `notify/CLAUDE.md` — this service's two planes, its best-effort consumer
  contract, and the ntfy **topic/key are deployment secrets** flowed only via env.
- `docs/adr-mcp-tool-bare-names.md` — tool names are **bare** verbs (`toolPrefix = ""`),
  so the new tool surfaces to clients as `mcp__ikigenba_notify__send`.
- `notify/internal/push/push.go` — the existing best-effort `Client.Send`, which
  this plan extends but does **not** change.

If anything here conflicts with those, they win — flag the conflict.

## What this adds

One MCP tool — **`send`** — that publishes a single notification to the owner's
ntfy topic and reports the real outcome synchronously. `health` and `reflection`
are unchanged. notify remains a consumer (no `/feed`, no published events); `send`
is a north/south owner-facing verb, orthogonal to the east/west consumer loops.

## Decisions settled in discussion

### 1. One verb, not a tool family

The whole domain is "push a notification" — exactly one verb. There is no entity
model, so there is nothing to add tools *for*. Deferred ntfy features (buttons,
attachments, scheduling, markdown, forwarding) become **fields** on `send` if and
when they land, never new tools — the same "verbs, not entities" discipline crm
follows.

### 2. The `send` tool surface

`send` — *"Push a notification to the owner's device. Returns {ok:true} on
delivery."*

| field | type | required | default | meaning |
|---|---|---|---|---|
| `message` | string | ✅ | — | the notification body; must be non-empty |
| `title` | string | | none | short headline shown above the body |
| `priority` | enum | | `default` | one of `min`/`low`/`default`/`high`/`urgent`; drives device alerting |
| `tags` | string[] | | none | ntfy tags: known emoji shortcodes render as leading emoji, others as text labels |
| `click` | string | | none | absolute URL opened when the owner taps the notification |

Everything but `message` is optional. A bare `send(message: "...")` is the
minimal valid call.

- **`priority`** is a string enum at the MCP boundary (self-documenting for the
  agent) mapped to ntfy's numeric `Priority` (1–5): `min`=1, `low`=2,
  `default`=3, `high`=4, `urgent`=5.
- **`tags`** is a clean `string[]`; the tool joins it into ntfy's comma-separated
  `Tags` header. ntfy decides per-string whether it is an emoji shortcode
  (rendered as a leading emoji, left-to-right) or a literal text label. Purely
  cosmetic/organizational — notify passes it through, no filtering.
- **`click`** is the recipient-side tap target — one URL per notification, opened
  on the owner's device. It is **not** an acknowledgment or callback: nothing
  reports back to the agent or to notify. notify is push-only and cannot observe
  delivery or taps.

### 3. Topic stays server-side

`send` does **not** take a topic. The configured topic (the `NTFY_TOPIC` secret)
is the owner's single channel on a single-tenant box. Exposing it would be a
footgun and adds nothing — the agent always notifies the one owner.

### 4. Synchronous, with a real outcome — NOT best-effort

The consumer path is deliberately fire-and-forget (best-effort external hop,
event-protocol.md §11.2): it returns immediately and the engine commits the
cursor regardless of the push outcome. `send` is the opposite by design: it is an
**explicit synchronous request**, so it follows the crm idiom and reports whether
the publish landed.

- Success → `{"ok":true}` (mirrors crm `delete`'s tight success shape).
- Failure → the uniform closed-vocabulary error envelope `{"error":{code,message,field?}}`,
  rendered through the same `toolErr`/`errorEnvelope` pattern crm uses.

`{"ok":true}` means **ntfy accepted the publish**, not that the owner saw it.
That is the strongest claim a push-only service can honestly make.

### 5. Error vocabulary (closed)

- `validation` — caller error, fully described so the agent self-corrects:
  missing/empty `message`; a `priority` outside the enum (message lists the valid
  values); a `click` that is not a well-formed absolute URL. `field` names the
  offending field where applicable.
- `upstream` — ntfy rejected the publish (non-2xx) or was unreachable/timed out.
  The message is **generic** — it never includes the topic or token (the secrets
  hard rule). The agent learns the push did not land, nothing more.

`click` validation is intentionally light: require a well-formed absolute URL
(any scheme the device may understand — `https:`, `mailto:`, `tel:`, app
deep-links — is allowed) and otherwise pass it through untouched.

### 6. `internal/push` shape — add, don't overload

The existing best-effort `Client.Send(ctx, title, message)` is the consumer's
fire-and-forget hop and its contract is unchanged (it swallows errors and logs at
WARN). `send` needs the opposite (a richer payload that returns an error), so we
**add** rather than overload:

- A `Notification` struct carrying `Message`, `Title`, `Priority`, `Tags`,
  `Click`.
- A new `Client.Publish(ctx, Notification) error` that sets the `Title`,
  `Priority`, `Tags`, and `Click` headers as present, POSTs to `<base>/<topic>`,
  and **returns** a typed error on transport failure or non-2xx instead of
  logging-and-dropping. The topic/token are still never logged.

The existing `Send` may be refactored to delegate to `Publish` (build a
`Notification{Title, Message}`, call `Publish`, log-and-drop any error) so there
is a single ntfy-POST code path — but its external best-effort behaviour must not
change.

### 7. Wiring

`send` requires a `*push.Client` inside the MCP handler — which the read-only
handler does not have today. The mcp `Handler` gains a `push *push.Client` field;
`cmd/notify/main.go` builds one publish client at the composition root (reusing
`resolveConsumerCfg`'s ntfy base/topic/token, which already fails loudly if a
secret is absent) and passes it to `mcp.NewHandler`. Following crm's
non-nil-service discipline, a nil push client at this seam is a wiring error.

The `serverInfo`/`instructions` blurb in `initialize` is updated from
"check health … and reflection" to also mention that the agent can `send` a
notification.

## Out of scope (deferred, additive later)

- ntfy `actions` (buttons), `attach`/`filename` (attachments), `delay`/`at`
  (scheduled delivery), `markdown`, `email`/`call` (forwarding), custom `icon`.
- Any acknowledgment / callback / read-receipt mechanism. That would need ntfy
  actions wired to an HTTP callback endpoint plus pending-ack state — a different,
  much larger feature, explicitly not this verb.
- Agent-selectable topic.

## Tests

Following `internal/mcp/tools_test.go` and `internal/push/push_test.go`:

- **`tools/list`** includes `send` alongside `health`/`reflection`, with an
  object input schema.
- **`send` happy path** against a **mock** ntfy server: a full
  `message/title/priority/tags/click` call yields one correctly-shaped POST
  (right path `/<topic>`, `Title`/`Priority`/`Tags`/`Click` headers, bearer auth)
  and returns `{"ok":true}`. Real ntfy.sh is never contacted.
- **Validation errors:** missing/empty `message`, bad `priority`, malformed
  `click` each return an `isError` envelope with code `validation` and a
  corrective message; no POST is made.
- **Upstream error:** the mock returns non-2xx (and, separately, is unreachable)
  → `send` returns an `upstream` error envelope, and the message contains neither
  the topic nor the token.
- **`Publish` unit test** in `internal/push`: header mapping (priority
  name→number, tags join) and error return on non-2xx, asserted against the mock.
- The existing consumer e2e is untouched and must still pass (the best-effort
  `Send` contract is preserved).
</content>
</invoke>
