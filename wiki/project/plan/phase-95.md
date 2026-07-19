# Phase 95 — Pin the `/complete` message wire spelling (`text`, not `content`)

*Realizes design Decision 5 (the LLM seam), slice R-8H1B-9CCI only.*

The `internal/llm` client's serialized `/complete` request must carry each
message as `{"role": …, "text": …}` per prompts' D29 contract (research §12).
The current client emits the key `content`, which prompts unmarshals as empty
user text and rejects (observed live: every wiki inference call fails with
`502: agentkit: invalid input`). End state: the client's message serialization
matches the contract, and a test pins the raw wire spelling so a regression to
`content` (or any other key) fails the suite — asserted on the captured raw
JSON bytes decoded independently of the client's own types.

**Done when:** the suite is green and this id is covered by a tagged test:

- R-8H1B-9CCI — each element of the captured raw `/complete` request's
  `messages` array carries exactly the keys `role` and `text` (decoded as
  `[]map[string]any`, never through the client's own struct), with `text`
  holding the message body and no `content` key present.
