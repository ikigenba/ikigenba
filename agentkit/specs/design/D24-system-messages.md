# D24-system-messages

A system message is the consumer asking for elevated attention: standing
instructions, a persona, a rule that should outrank ordinary chat text. D2
already models it as a role — `RoleSystem` inside `History`, never a side
channel — but nothing could put one there. `Conversation.AddSystem` closes that
gap. It appends one `RoleSystem` message to the transcript, synchronously, with
no round trip; the next `Send` renders it as part of the base history, and each
wire delivers it in the strongest form its vendor documents.

```go
package agentkit

// AddSystem appends one RoleSystem message holding a single Text block to the
// conversation's History. It makes no provider call and returns no Stream; the
// message is part of History before AddSystem returns. Empty or whitespace-only
// text is rejected with ErrInvalidArgument; after the conversation's Log is
// closed AddSystem returns ErrClosed. In both cases History is unchanged.
func (c *Conversation) AddSystem(text string) error
```

**Only the transcript grows.** `AddSystem` is not a second kind of turn. `Send`
means "talk to the model and stream the reply"; `AddSystem` never leaves the
process, so it has its own verb, no context, and no stream. Because `Send` runs
its tool loop to completion before committing, a system message can only ever
sit between turns, never between a `tool_use` and its result. That invariant
holds by construction and every wire relies on it.

**Interleaving is allowed and deliberate.** The typical consumer calls
`AddSystem` once, before the first `Send`. A consumer who calls it later — after
turns have run — is asking for the elevated attention a system message carries
at that point in the conversation, and accepts whatever that costs on a given
vendor (a cache miss on a hoisting wire, a vendor rejection on a model without
in-band support). A consumer who wants a cheap contextual note sends it as user
text instead; the library offers no third kind of message.

**Every wire renders History fresh; the rule per wire is "in band where the
vendor documents it, hoisted where it does not".** Tolerance is not support: a
vendor that merely accepts an undocumented role is treated as not supporting
it, since the text is likely rendered as ordinary user content.

| Wire | Vendor mechanism (documented, proven live 2026-09-06) | Rendering |
|---|---|---|
| `ChatWire`, `OpenAIChatWire`, `XAIChatWire` | in-band message, role `system`, any position | in place |
| `ResponsesWire`, `OpenAIResponsesWire`, `XAIResponsesWire` | in-band input item, role `system`, any position | in place |
| `AnthropicMessagesWire` | top-level `system` array for the initial prompt; in-band `role: "system"` after a user turn on Opus 4.8, Opus 5, Fable 5 and later; `claude-haiku-4-5` and `claude-sonnet-4-6` reject the in-band form | leading messages hoisted, later ones in band |
| `GeminiGenerateContentWire` | top-level `systemInstruction`; no documented in-band role | hoisted |

The in-band wires send `system`, not `developer`: `system` is the one value
every vendor documents, and OpenAI treats the two identically on the models
that distinguish them.

**Anthropic has two placements, and the second reorders.** A system message
that precedes the first user message in History is *leading* and belongs in the
top-level `system` field — Anthropic forbids a content-carrying system message
as the first entry of `messages`. A later system message is rendered in band,
but Anthropic's placement rule requires it to follow a user turn and precede the
assistant reply. In History it sits *before* the user message of the next turn,
so the wire renders it immediately *after* that user message. The model sees
the instruction before it answers the turn it precedes, which is the intent.
Consecutive system messages render consecutively in History order; Anthropic
treats them as one system section.

```json
{"system":[{"type":"text","text":"leading"}],
 "messages":[
   {"role":"user","content":[{"type":"text","text":"first prompt"}]},
   {"role":"assistant","content":[{"type":"text","text":"first reply"}]},
   {"role":"user","content":[{"type":"text","text":"second prompt"}]},
   {"role":"system","content":[{"type":"text","text":"added between the turns"}]}
 ]}
```

**No model gating.** The Anthropic wire renders the same request for every
model string. Some Anthropic models, `claude-haiku-4-5` and `claude-sonnet-4-6`
at the time of writing, reject the in-band form with a 400, and
that error surfaces through the ordinary path (D4, D12) with History unchanged.
The library cannot know a free-form model's support in advance, the vendor's
documentation is already inexact about which models qualify, and hoisting
silently on some models would give one call two meanings. A consumer who hits
the error sends the text as a user message or picks a newer model.

**Hoisting collects, never concatenates.** Every vendor's single slot takes a
list, so each `RoleSystem` message becomes one block in that list. No join
separator is invented, and the blocks stay individually addressable.

**The event log is untouched.** The log (D15) records a turn's protocol events
and never the consumer's own input; `AddSystem` is consumer input and writes no
record, and `Send` emits no event for the system messages it renders. Recording
consumer input in the log is a separate design.

## REQUIREMENTS

- R-WEW7-DNV4: `agentkit` MUST export the method `func (c *Conversation) AddSystem(text string) error`.
- R-WG43-RFLT: A successful `AddSystem(text)` MUST append exactly one `Message{Role: RoleSystem, Blocks: []Block{Text{Text: text}}}` to the conversation's `History` before returning, MUST make no provider call, and MUST return no `Stream`.
- R-CM96-IGK8: `AddSystem` MUST return an error satisfying `errors.Is(err, ErrInvalidArgument)` (D4) and leave `History` unchanged when `text` is empty or consists only of Unicode white space.
- R-WIJW-IZ37: After the conversation's `Log` has been closed, `AddSystem` MUST return an error satisfying `errors.Is(err, ErrClosed)` and leave `History` unchanged.
- R-WJRS-WQTW: A `RoleSystem` message appended by `AddSystem` MUST be a turn boundary of `History`: a subsequent `Send` MUST splice its turn after it, and consecutive `AddSystem` calls MUST yield consecutive `RoleSystem` messages in call order, never merged into one message.
- R-WM7L-OABA: `ChatWire()`, `OpenAIChatWire()`, and `XAIChatWire()` MUST render each `RoleSystem` message at its History position as `{"role":"system","content":"<text>"}`, and `ResponsesWire()`, `OpenAIResponsesWire()`, and `XAIResponsesWire()` MUST render it at its History position as `{"role":"system","content":[{"type":"input_text","text":"<text>"}]}`; no shipped wire may emit the role `developer`; pinned by golden request fixtures holding a leading and an interleaved system message.
- R-WNFI-221Z: `AnthropicMessagesWire()` MUST render every `RoleSystem` message that precedes the first non-`RoleSystem` message in `History` as one `{"type":"text","text":"<text>"}` element of the top-level `system` array, in History order, MUST omit `system` when there are none, and MUST NOT render those messages inside `messages`; pinned by a golden request fixture.
- R-WONE-FTSO: `AnthropicMessagesWire()` MUST render every `RoleSystem` message that follows a non-`RoleSystem` message in `History` as the `messages` entry `{"role":"system","content":[{"type":"text","text":"<text>"}]}` placed immediately after the rendering of the first `RoleUser` message that follows it in `History`, consecutive such messages keeping their History order, and MUST NOT place any of them in the top-level `system` array; pinned by a golden request fixture.
- R-WPVA-TLJD: `AnthropicMessagesWire()` MUST produce the same `system` and `messages` rendering for a given `History` regardless of the model string; a vendor rejection of an in-band system message MUST surface as the turn's terminal `*Error` on `Stream.Err()` with `History` unchanged, and the wire MUST NOT hoist or drop the message in response to the model.
- R-WR37-7DA2: `GeminiGenerateContentWire()` MUST render every `RoleSystem` message, at any History position, as one `{"text":"<text>"}` element of the top-level `systemInstruction.parts` array in History order with no `role` field on `systemInstruction`, MUST omit `systemInstruction` when there are none, and MUST NOT render any of them inside `contents`; pinned by golden request fixtures holding a leading and an interleaved system message.
- R-WSB3-L50R: `AddSystem` MUST write no record to the conversation's `Log`, and `Send` MUST emit no `Event` and no log record for the `RoleSystem` messages it renders.
