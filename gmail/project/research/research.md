# gmail — Research

Collected external ground truth about the Gmail API that the design references.
Non-contractual; the build loop never reads this. Facts below were established
against the **live** Gmail API (the deployed connector, 2026-07-12) or from
Google's API reference, as noted.

## Attachment addressing: `attachmentId` is ephemeral, `partId` is stable

- **`attachmentId` rotates on every `users.messages.get`.** Observed live:
  two consecutive `messages.get` calls for the same message returned two
  different `attachmentId` values for the same MIME part (`ANGjdJ-p7Ap…` then
  `ANGjdJ-_Rpr…`). The id is a per-response retrieval token, not a durable
  handle. Google's docs never promise stability; observation confirms active
  rotation. Consequence: any design that stores or transmits an
  `attachmentId` for later comparison or resolution is broken by construction
  — a reference minted by one fetch never matches the ids seen by a later
  fetch.
- **`partId` is stable for the life of a message.** A Gmail message is
  immutable once created; its MIME part tree — and each part's `partId`
  (e.g. `"1"`, `"2"`, `"0.1"`) — is fixed. `partId` is therefore the correct
  durable component for addressing an attachment within a message.
- **A fresh `attachmentId` used promptly resolves.** `users.messages.attachments.get`
  accepts the token returned by the `messages.get` response it came from.
  Resolution must therefore mint and spend the token inside one request
  window: refetch the message, take the *current* `attachmentId` from that
  response, and call `attachments.get` with it immediately.

## Send-to-self and cleanup facts (for the live check)

- **`users.getProfile` returns the authorized mailbox's `emailAddress`** — a
  live test can discover "self" at runtime and hardcode no address.
- **A send-to-self lands in the same mailbox** (SENT and INBOX copies share
  the message/thread), so one authorized account covers send, fetch, and
  cleanup.
- **The connector's consent flow requests the full `https://mail.google.com/`
  scope** (`cmd/consent/main.go`), which permits permanent
  `users.messages.delete` — cleanup can remove the test message outright
  rather than leaving it in Trash (which `trash` — modify scope — would).
- **Which account the connector "is"** is decided solely by the refresh token
  installed on the box (minted by the one-time `cmd/consent` flow). The
  deployed box is being moved to michaelgreenly@logic-refinery.com as an ops
  action; nothing in the codebase names a mailbox.

## Options evaluated and not chosen

- **Filename as the stable part locator** — human-friendly but not unique
  (two attachments may share a filename in one message); `partId` is unique
  within the tree.
- **Caching `attachmentId`→bytes server-side to outlive rotation** — adds
  blob state to a stateless connector to work around a token that is free to
  re-mint per request.
