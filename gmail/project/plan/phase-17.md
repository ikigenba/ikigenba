# Phase 17 — Attachment addressing by stable `part_id`: fix the rotating-token 404 and purge the ephemeral id from the reference surface

*Realizes design Decision 16 (holder endpoint, rewritten: `message_id` + `part_id` addressing) and 17 (references, rewritten: durable `content_url`, no `attachment_id`). Depends on Phase 14 and Phase 15.*

The live smoke test proved the shipped surface broken: Gmail rotates
`attachmentId` on every `messages.get`, so the handler's fresh fetch never
matches the id in a minted URL and every put-by-reference 404s
(`project/research/research.md`). This phase moves both sides of the surface
onto immutable addressing.

End state:

- `internal/gmail/attachment.go`: the handler reads `message_id` + `part_id`,
  walks the fetched MIME tree for the part whose `PartID` matches, and calls
  `AttachmentGet` with the `Body.AttachmentID` **from its own `MessageGet`
  response**. No `attachment_id` query parameter exists; a matching part with
  an empty `Body.AttachmentID` (inline) is 404. The guard, Content-Type/Length
  behavior, and the 404/502 taxonomy otherwise stand as in D16.
- `internal/mcp/tools.go`: `collectAttachments`/`renderMessage` stamp
  `content_url` from `message_id` + the part's `PartID` (via
  `url.Values.Encode`) and emit **no `attachment_id` field**; entries lacking
  a non-empty `PartID` or `Body.AttachmentID` stay metadata-only. The
  `read`/`thread` descriptions state the durable-reference truth per D17.
- Tests for the deleted ids (R-WYFA-DK0C, R-WZN6-RBR1, R-X0V3-53HQ,
  R-X3AV-WMZ4, R-X4IS-AEPT, R-X5QO-O6GI) are removed with their behaviors;
  the new ids' tests replace them, including the rotating fake
  `AttachmentSource` whose `MessageGet` mints a different token per call.

**Done when:** each of R-3G57-009Q, R-3HD3-DS0F, R-3IKZ-RJR4 (handler:
rotation-surviving happy path; missing/blank/legacy params → 404 with zero
source calls; not-found/inline/fault mapping 404 vs 502) and R-3JSW-5BHT,
R-3L0S-J38I, R-3M8O-WUZ7 (tools: `read` reference shape with no
`attachment_id` key and no token leak; `thread` per-message references;
inline/part-id-less parts metadata-only) is covered by a clearly named test,
and the suite is green per design Conventions (`go build ./...`, `go vet
./...`, `gofmt -l .` empty, `go test ./...` — all from `gmail/`).
