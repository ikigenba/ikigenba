# Prompt caching probes

Observations of the real hosts behind the vendor facts D27 records. Gathered
2026-09-07 with `curl` against the production endpoints; every body below is
verbatim from the response, with the filler prose elided from requests. The
design references this file; the requirements name the shapes proven here.

## Anthropic — breakpoint on the last `system` block, no prior messages

`POST https://api.anthropic.com/v1/messages`, `anthropic-version: 2023-06-01`,
model `claude-haiku-4-5`, `max_tokens: 50`.

Every request carried one uncached tool (`read_file`, a `path` string
parameter), a `system` array of three text blocks — 60 chars, 39 chars, then
42,000 chars of varied English prose — and a single short user message. No
message precedes `system`. Probes 01–03 put `cache_control: {"type":"ephemeral"}`
on the third (last, large) block only; probe 04 moved it to the second (short)
block as a control.

| Probe | breakpoint on | user message | status | `usage` |
|---|---|---|---|---|
| 01 | last `system` block | "Reply with the single word OK." | 200 | `{"input_tokens":330,"cache_creation_input_tokens":6702,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":6702,"ephemeral_1h_input_tokens":0},"output_tokens":4,"service_tier":"standard","inference_geo":"not_available"}` |
| 02 | last `system` block | "Reply with the single word DONE." | 200 | `{"input_tokens":331,"cache_creation_input_tokens":0,"cache_read_input_tokens":6702,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":6,"service_tier":"standard","inference_geo":"not_available"}` |
| 03 | last `system` block | "Reply with the single word FINISHED." | 200 | `{"input_tokens":331,"cache_creation_input_tokens":0,"cache_read_input_tokens":6702,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":5,"service_tier":"standard","inference_geo":"not_available"}` |
| 04 | second (short) `system` block | "Reply with the single word OK." | 200 | `{"input_tokens":7032,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":4,"service_tier":"standard","inference_geo":"not_available"}` |

Established:

- A breakpoint on the last block of the top-level `system` array is written
  (probe 01, 6702 tokens created) and read back exactly on later requests with
  different user messages (probes 02 and 03, 6702 tokens read), with no
  message preceding `system` and an uncached `tools` array ahead of it.
- A breakpoint whose prefix is below the model's minimum (probe 04) is ignored:
  nothing is created, nothing is read, and the request succeeds at full price.
- With no `ttl`, the write lands in the five-minute bucket
  (`ephemeral_5m_input_tokens`).

## Gemini — explicit `cachedContents`

`https://generativelanguage.googleapis.com/v1beta`, model
`gemini-3.5-flash-lite`, key in `x-goog-api-key`. The large `contents` is one
user turn of 55,520 chars of varied English prose.

### 01 — create, below minimum size

`POST /cachedContents` with `model`, one 16-token user turn, and a
`systemInstruction`. Status 400:

```json
{"error":{"code":400,"message":"Cached content is too small. total_token_count=16, min_total_token_count=1024","status":"INVALID_ARGUMENT"}}
```

### 02 — create, with `tools` and `toolConfig`, no `ttl`

`POST /cachedContents` with keys `model`, `contents` (the large turn),
`systemInstruction`, `tools` (one `functionDeclarations` entry, `read_file`),
and `toolConfig: {"functionCallingConfig":{"mode":"ANY"}}`. Status 200:

```json
{"name":"cachedContents/ez2yh6u6my9gnjvyigtxvnuf6e5qc79vgd7cc668","model":"models/gemini-3.5-flash-lite","createTime":"2026-09-07T01:18:17.438678Z","updateTime":"2026-09-07T01:18:17.438678Z","expireTime":"2026-09-07T02:18:17.023637828Z","displayName":"","usageMetadata":{"totalTokenCount":7021}}
```

`toolConfig` is accepted. `expireTime - createTime` is one hour, the default
when `ttl` is omitted. The response echoes none of the cached members.

### 03 — generateContent referencing the cache, new message only

`POST /models/gemini-3.5-flash-lite:generateContent` with `cachedContent` set
to the name above and `contents` holding one new short user turn; no
`systemInstruction`, `tools`, or `toolConfig`. Status 200. The reply is a
`functionCall` part (the cached `mode: ANY` was honoured), and:

```json
"usageMetadata":{"promptTokenCount":7036,"candidatesTokenCount":19,"totalTokenCount":7055,"cachedContentTokenCount":7021,"promptTokensDetails":[{"modality":"TEXT","tokenCount":7036}],"cacheTokensDetails":[{"modality":"TEXT","tokenCount":7021}],"serviceTier":"standard"}
```

### 04 — cache plus `systemInstruction`

As 03 with the cached `systemInstruction` re-sent byte-identical. Status 400:

```json
{"error":{"code":400,"message":"CachedContent can not be used with GenerateContent request setting system_instruction, tools or tool_config.\n\nProposed fix: move those values to CachedContent from GenerateContent request.","status":"INVALID_ARGUMENT"}}
```

### 05 — cache plus `tools`

As 03 with the cached `tools` re-sent byte-identical. Status 400, body
identical to 04.

### 06 — cache plus the cached `contents` re-sent

As 03 with the large cached user turn re-sent ahead of the new message. Status
200, no error. The re-sent turn is billed again in full:

```json
"usageMetadata":{"promptTokenCount":13984,"candidatesTokenCount":16,"totalTokenCount":14000,"cachedContentTokenCount":7021,"promptTokensDetails":[{"modality":"TEXT","tokenCount":13984}],"cacheTokensDetails":[{"modality":"TEXT","tokenCount":7021}],"serviceTier":"standard"}
```

### 07 — delete, then reference the deleted name

`DELETE /cachedContents/ez2yh...` → status 200, body `{}`.

Probe 03 repeated against the deleted name → status 403:

```json
{"error":{"code":403,"message":"CachedContent not found (or permission denied)","status":"PERMISSION_DENIED"}}
```

`GET` of the deleted name → status 403, identical body.

`GET /cachedContents` afterwards → status 200, body `{}` (nothing left alive).

Established:

- Minimum cacheable size on this model is 1024 tokens; below it, creation is
  refused with 400 `INVALID_ARGUMENT` and a message beginning
  `Cached content is too small`.
- `toolConfig` is accepted in the cache and honoured by later requests.
- `systemInstruction`, `tools`, or `toolConfig` sent alongside `cachedContent`
  is a hard 400 rejection even when byte-identical; the message names all three
  regardless of which was sent.
- Cached `contents` sent alongside `cachedContent` is silently accepted and
  billed twice.
- A deleted or missing cache is 403 `PERMISSION_DENIED`, never 404, for both
  `generateContent` and `GET`.
- Default lifetime without `ttl` is one hour.
