# Catalog probes, October 2026

Observations of the real hosts behind the catalog refresh of 2026-10-18: the reasoning controls, output caps, prices and Codex membership of every row the refresh adds, and the OpenRouter models API snapshot every OpenRouter price is taken from. Gathered 2026-10-18 between 17:40 and 17:46 UTC with `curl` against the production endpoints, one section per research item (R1 to R7); every body is verbatim from the response except where a section says what it elides. No key, token or account id appears in this note; credentials are named or redacted wherever a request carried one. Where a page and a probe disagree, the probe settles it, and the section records both.

## R1 — OpenAI effort vocabularies: `gpt-6-luna`, `gpt-6-sol`, `gpt-6.1-sol`

Gathered 2026-10-18 (17:40–17:45 UTC) with `curl` against `https://api.openai.com`, project API key in `Authorization: Bearer` (not reproduced). Every body below is verbatim from the response; on 200s only the fields named in the column are quoted, and Responses' `encrypted_content` reasoning blobs and response ids are omitted.

### Smallest output cap

`POST /v1/responses`, model `gpt-6-luna`, `input: "Say OK."`, `max_output_tokens: 1`. Status 400:

```json
{"error":{"message":"Invalid 'max_output_tokens': integer below minimum value. Expected a value >= 16, but got 1 instead.","type":"invalid_request_error","param":"max_output_tokens","code":"integer_below_min_value"}}
```

`POST /v1/chat/completions`, model `gpt-6-luna`, one user message `"Say OK."`, `max_completion_tokens: 1`. Status 400 (the parameter is accepted; the generation then fails):

```json
{"error":{"message":"Could not finish the message because max_tokens or model output limit was reached. Please try again with higher max_tokens.","type":"invalid_request_error","param":null,"code":null}}
```

At 16 both wires answer 200, so every probe below uses 16 (`max_output_tokens` on Responses, `max_completion_tokens` on Chat).

### Effort probes — Responses

`POST /v1/responses` with `{"model":M,"input":"Say OK.","max_output_tokens":16,"store":false}`, plus `"reasoning":{"effort":E}` except on the `default` row, which sends no reasoning member. `status` and `reasoning` are the response's top-level members.

| model | effort sent | HTTP | `status` | `reasoning` | `usage` / error body |
|---|---|---|---|---|---|
| gpt-6-luna | default | 200 | `incomplete` | `{"context":"all_turns","effort":"medium","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":9},"total_tokens":25}` |
| gpt-6-luna | none | 200 | `completed` | `{"context":"all_turns","effort":"none","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":14}` |
| gpt-6-luna | low | 200 | `completed` | `{"context":"all_turns","effort":"low","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":14}` |
| gpt-6-luna | medium | 200 | `completed` | `{"context":"all_turns","effort":"medium","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":9},"total_tokens":25}` |
| gpt-6-luna | high | 200 | `incomplete` | `{"context":"all_turns","effort":"high","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":9},"total_tokens":25}` |
| gpt-6-luna | xhigh | 200 | `completed` | `{"context":"all_turns","effort":"xhigh","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":9},"total_tokens":25}` |
| gpt-6-luna | max | 200 | `completed` | `{"context":"all_turns","effort":"max","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":14}` |
| gpt-6-sol | default | 200 | `completed` | `{"context":"all_turns","effort":"medium","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}` |
| gpt-6-sol | none | 200 | `completed` | `{"context":"all_turns","effort":"none","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}` |
| gpt-6-sol | low | 200 | `completed` | `{"context":"all_turns","effort":"low","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}` |
| gpt-6-sol | medium | 200 | `completed` | `{"context":"all_turns","effort":"medium","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}` |
| gpt-6-sol | high | 200 | `completed` | `{"context":"all_turns","effort":"high","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}` |
| gpt-6-sol | xhigh | 200 | `completed` | `{"context":"all_turns","effort":"xhigh","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}` |
| gpt-6-sol | max | 200 | `incomplete` | `{"context":"all_turns","effort":"max","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":9},"total_tokens":25}` |
| gpt-6.1-sol | default | 200 | `completed` | `{"context":"all_turns","effort":"medium","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":14}` |
| gpt-6.1-sol | none | 400 | — | — | `{"error":{"message":"Unsupported value: 'none' is not supported with the 'gpt-6.1-sol' model. Supported values are: 'low', 'medium', 'high', 'xhigh', and 'max'.","type":"invalid_request_error","param":"reasoning.effort","code":"unsupported_value"}}` |
| gpt-6.1-sol | low | 200 | `completed` | `{"context":"all_turns","effort":"low","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}` |
| gpt-6.1-sol | medium | 200 | `completed` | `{"context":"all_turns","effort":"medium","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":14}` |
| gpt-6.1-sol | high | 200 | `completed` | `{"context":"all_turns","effort":"high","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":14}` |
| gpt-6.1-sol | xhigh | 200 | `completed` | `{"context":"all_turns","effort":"xhigh","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":14}` |
| gpt-6.1-sol | max | 200 | `incomplete` | `{"context":"all_turns","effort":"max","mode":"standard","summary":null}` | `{"input_tokens":9,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":25}` |

### Effort probes — Chat Completions

`POST /v1/chat/completions` with `{"model":M,"messages":[{"role":"user","content":"Say OK."}],"max_completion_tokens":16,"store":false}`, plus `"reasoning_effort":E` except on the `default` row. Chat responses carry no reasoning member, so the default effort is not reported on this wire.

| model | effort sent | HTTP | `finish_reason` | `usage` / error body |
|---|---|---|---|---|
| gpt-6-luna | default | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-luna | none | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-luna | low | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-luna | medium | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-luna | high | 200 | `length` | `{"prompt_tokens":9,"completion_tokens":16,"total_tokens":25,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":16,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-luna | xhigh | 200 | `length` | `{"prompt_tokens":9,"completion_tokens":16,"total_tokens":25,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":16,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-luna | max | 400 | — | `{"error":{"message":"Unsupported value: 'reasoning_effort' does not support 'max' with this model. Supported values are: 'none', 'low', 'medium', 'high', and 'xhigh'.","type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_value"}}` |
| gpt-6-sol | default | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-sol | none | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-sol | low | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-sol | medium | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-sol | high | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-sol | xhigh | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-sol | max | 400 | — | `{"error":{"message":"Unsupported value: 'reasoning_effort' does not support 'max' with this model. Supported values are: 'none', 'low', 'medium', 'high', and 'xhigh'.","type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_value"}}` |
| gpt-6.1-sol | default | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6.1-sol | none | 400 | — | `{"error":{"message":"Unsupported value: 'reasoning_effort' does not support 'none' with this model. Supported values are: 'low', 'medium', 'high', and 'xhigh'.","type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_value"}}` |
| gpt-6.1-sol | low | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6.1-sol | medium | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6.1-sol | high | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6.1-sol | xhigh | 200 | `stop` | `{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6.1-sol | max | 400 | — | `{"error":{"message":"Unsupported value: 'reasoning_effort' does not support 'max' with this model. Supported values are: 'low', 'medium', 'high', and 'xhigh'.","type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_value"}}` |

### Function tools on Chat Completions

`POST /v1/chat/completions` with `{"model":M,"messages":[{"role":"user","content":"What time is it? Use the tool."}],"max_completion_tokens":16,"store":false,"tools":[{"type":"function","function":{"name":"get_time","description":"Return the current time.","parameters":{"type":"object","properties":{},"additionalProperties":false}}}]}`, plus `"reasoning_effort":E` where the row names one. The Responses rows are a control: `POST /v1/responses` with the same tool in Responses form (`{"type":"function","name":"get_time",...}`), no reasoning member, `max_output_tokens: 64`.

| model | wire | effort sent | HTTP | result |
|---|---|---|---|---|
| gpt-6-luna | chat | low | 400 | `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-6-luna in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","type":"invalid_request_error","param":"reasoning_effort","code":null}}` |
| gpt-6-luna | chat | default | 400 | `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-6-luna in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","type":"invalid_request_error","param":"reasoning_effort","code":null}}` |
| gpt-6-luna | chat | none | 200 | `finish_reason` `tool_calls`, `tool_calls` `[{"name":"get_time","arguments":"{}"}]`, `usage` `{"prompt_tokens":126,"completion_tokens":13,"total_tokens":139,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-luna | responses | default | 200 | `status` `completed`, `reasoning.effort` `medium`, output `[{"type":"function_call","name":"get_time","arguments":"{}"}]`, `usage` `{"input_tokens":44,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":14,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":58}` |
| gpt-6-sol | chat | low | 400 | `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-6-sol in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","type":"invalid_request_error","param":"reasoning_effort","code":null}}` |
| gpt-6-sol | chat | default | 400 | `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-6-sol in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","type":"invalid_request_error","param":"reasoning_effort","code":null}}` |
| gpt-6-sol | chat | none | 200 | `finish_reason` `tool_calls`, `tool_calls` `[{"name":"get_time","arguments":"{}"}]`, `usage` `{"prompt_tokens":126,"completion_tokens":13,"total_tokens":139,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| gpt-6-sol | responses | default | 200 | `status` `completed`, `reasoning.effort` `medium`, output `[{"type":"function_call","name":"get_time","arguments":"{}"}]`, `usage` `{"input_tokens":44,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":14,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":58}` |
| gpt-6.1-sol | chat | low | 400 | `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-6.1-sol in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","type":"invalid_request_error","param":"reasoning_effort","code":null}}` |
| gpt-6.1-sol | chat | default | 400 | `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-6.1-sol in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","type":"invalid_request_error","param":"reasoning_effort","code":null}}` |
| gpt-6.1-sol | chat | none | 400 | `{"error":{"message":"Unsupported value: 'reasoning_effort' does not support 'none' with this model. Supported values are: 'low', 'medium', 'high', and 'xhigh'.","type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_value"}}` |
| gpt-6.1-sol | responses | default | 200 | `status` `completed`, `reasoning.effort` `medium`, output `[{"type":"function_call","name":"get_time","arguments":"{}"}]`, `usage` `{"input_tokens":44,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":14,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":58}` |

### Model and pricing pages

Read 2026-10-18. `https://platform.openai.com/docs/models/<model>` and `/docs/pricing` answer 301 to `https://developers.openai.com/api/docs/models/<model>` and `https://developers.openai.com/api/docs/pricing`; the figures are from those. Prices in $ per 1M tokens.

| model | context | max output | max input | effort per page | long-context rule | input / cached / cache write / output (≤272K) | above 272K | Chat Completions note |
|---|---|---|---|---|---|---|---|---|
| `gpt-6-luna` | 1,050,000 | 128,000 | not stated | `none`, `low`, `medium` (default), `high`, `xhigh`, `max` | "Prompts with more than 272K input tokens are priced at 2x input and cache rates and 1.5x output for the full request." | $0.10 / $0.01 / $0.125 / $0.50 | $0.20 / $0.02 / $0.25 / $0.75 | "Chat Completions supports function calling only with `reasoning_effort` set to `none`." |
| `gpt-6-sol` | 1,050,000 | 128,000 | 922,000 | `none`, `low`, `medium` (default), `high`, `xhigh`, `max` | same rule, 272K | $2.00 / $0.20 / $2.50 / $10.00 | $4.00 / $0.40 / $5.00 / $15.00 | "Chat Completions supports function calling only with `reasoning_effort` set to `none`." |
| `gpt-6.1-sol` | 1,050,000 | 128,000 | 922,000 | `low`, `medium` (default), `high`, `xhigh`, `max` | same rule, 272K | $2.00 / $0.10 / $2.50 / $10.00 | $4.00 / $0.20 / $5.00 / $15.00 | "Use the Responses API for tool calling. Chat Completions is supported without tool calling." |

The pricing page defines "Short context: ≤272K input tokens. Long context: >272K input tokens." No model page gives a release date.

Established:

- Responses accepts `none` through `max` on `gpt-6-luna` and `gpt-6-sol`, and `low` through `max` on `gpt-6.1-sol`; `none` on `gpt-6.1-sol` is a 400 `unsupported_value` on `reasoning.effort`. With no reasoning member, Responses reports `reasoning.effort` `medium` on all three.
- Chat Completions accepts `none` through `xhigh` on `gpt-6-luna` and `gpt-6-sol`, and `low` through `xhigh` on `gpt-6.1-sol`. `max` is a 400 `unsupported_value` on `reasoning_effort` on all three, and `none` is a 400 on `gpt-6.1-sol`. The pages advertise `max` without naming a wire; the probe settles that Chat does not take it.
- The minimum output cap is 16 on Responses (a schema bound) and 1 is parsed on Chat, but a cap reasoning exhausts is a 400 on Chat, while Responses answers 200 `incomplete`; Chat at 16 answered 200 `length`.
- On Chat Completions any function tool with an effort other than `none` is a 400 naming `reasoning_effort`, and so is a tool with no `reasoning_effort` at all: the default effort counts as reasoning. `gpt-6-luna` and `gpt-6-sol` call the tool at `none`. `gpt-6.1-sol` cannot call a function on Chat at all: with a tool, `none` is rejected as an unsupported value even though the tool error tells the caller to set it. Responses calls the tool at the default effort on all three.
- Both wires' usage blocks carry a `cache_write_tokens` member beside `cached_tokens` (Responses `input_tokens_details`, Chat `prompt_tokens_details`); it was 0 in every probe here (bears on R7).
- Pages and probes agree on context (1,050,000), the 272K boundary and the Responses effort vocabulary; the Chat wire's narrower vocabulary is the only page-vs-probe difference.

## Codex backend membership (R2)

Observations of which OpenAI wire models the Codex backend answers under a ChatGPT OAuth token, the evidence decision 7 rests on. Gathered 2026-10-18 between 17:41 and 17:43 UTC with `curl` against the production endpoint; every body below is verbatim from the response, with tokens and identifiers redacted. Per orchestrator decision P3, every OpenAI row in the catalog was probed, not only the new ones.

`POST https://chatgpt.com/backend-api/codex/responses`, headers `Authorization: Bearer <ChatGPT OAuth access token, redacted>`, `ChatGPT-Account-Id: <the access token's https://api.openai.com/auth.chatgpt_account_id claim, redacted>`, `Content-Type: application/json`, `Accept: text/event-stream`. The access token was unexpired (JWT `exp` 2026-10-28); no refresh was made and `~/.agentkit/openai-auth.json` was not modified.

Every request carried the body agentkit's `openai-responses` wire builds, with only the model varied:

```json
{"model":"<model>","input":[{"role":"user","content":[{"type":"input_text","text":"Reply with the single word OK."}]}],"stream":true,"store":false,"max_output_tokens":16}
```

### 00 — smallest output cap

`gpt-6-luna` with `max_output_tokens: 16` → status 200, a completion (probe 01). With `max_output_tokens: 15` → status 400:

```json
{"error":{"message":"Invalid 'max_output_tokens': integer below minimum value. Expected a value >= 16, but got 15 instead.","type":"invalid_request_error","param":"max_output_tokens","code":"integer_below_min_value"}}
```

With `max_output_tokens: 1` → status 400, `invalid_request_error` (body not kept). Every probe below uses 16.

### Membership probes

A 200 is a server-sent event stream; the row records the terminal event's type, `model`, `status`, the concatenated `response.output_text.delta` text, and its `usage` with the per-item `attribution` object elided. A 400 is a plain JSON body, recorded whole.

| Probe | model | status | response |
|---|---|---|---|
| 01 | `gpt-6-luna` | 200 | `response.completed`, model `gpt-6-luna`, status `completed`, text `"OK"`, usage `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,...}` |
| 02 | `gpt-6-sol` | 200 | `response.completed`, model `gpt-6-sol`, status `completed`, text `"OK"`, usage `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,...}` |
| 03 | `gpt-6.1-sol` | 200 | `response.completed`, model `gpt-6.1-sol`, status `completed`, text `"OK"`, usage `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,...}` |
| 04 | `gpt-6-astra` | 200 | `response.completed`, model `gpt-6-astra`, status `completed`, text `"OK"`, usage `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,...}` |
| 05 | `gpt-5.4` | 400 | `{"detail":"The 'gpt-5.4' model is not supported when using Codex with a ChatGPT account."}` |
| 06 | `gpt-5.4-mini` | 400 | `{"detail":"The 'gpt-5.4-mini' model is not supported when using Codex with a ChatGPT account."}` |
| 07 | `gpt-5.4-nano` | 400 | `{"detail":"The 'gpt-5.4-nano' model is not supported when using Codex with a ChatGPT account."}` |
| 08 | `gpt-5.5` | 400 | `{"detail":"The 'gpt-5.5' model is not supported when using Codex with a ChatGPT account."}` |
| 09 | `gpt-5.5-pro` | 400 | `{"detail":"The 'gpt-5.5-pro' model is not supported when using Codex with a ChatGPT account."}` |
| 10 | `gpt-5.6-sol` | 200 | `response.completed`, model `gpt-5.6-sol`, status `completed`, text `"OK"`, usage `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,...}` |
| 11 | `gpt-5.6-terra` | 200 | `response.completed`, model `gpt-5.6-terra`, status `completed`, text `"OK"`, usage `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,...}` |
| 12 | `gpt-5.6-luna` | 200 | `response.completed`, model `gpt-5.6-luna`, status `completed`, text `"OK"`, usage `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,...}` |

The full `usage` of probe 01, attribution included, ids redacted:

```json
{"input_tokens":13,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"attribution":{"items":{"msg_<redacted-1>":{"input_tokens":11,"cached_tokens":0,"cache_write_tokens":0,"output_tokens":0,"content":[{"input_tokens":11,"cached_tokens":0,"cache_write_tokens":0,"output_tokens":0}]},"msg_<redacted-2>":{"input_tokens":2,"cached_tokens":0,"cache_write_tokens":0,"output_tokens":5,"content":[{"input_tokens":2,"cached_tokens":0,"cache_write_tokens":0,"output_tokens":5}]}}}}
```

Probe 01's `response.created` event echoed `"reasoning":{"context":"all_turns","effort":"medium","summary":null,"mode":"standard"}` and `"max_output_tokens":16`.

Established:

- The Codex backend answers a completion under a ChatGPT OAuth token for `gpt-6-luna`, `gpt-6-sol`, `gpt-6.1-sol`, `gpt-6-astra`, `gpt-5.6-sol`, `gpt-5.6-terra` and `gpt-5.6-luna` (probes 01–04, 10–12). OpenAI's Codex page is silent on `gpt-6-sol`; the probe settles that it answers.
- It rejects `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.4-nano`, `gpt-5.5` and `gpt-5.5-pro` with status 400 and a `detail` of the form `The '<model>' model is not supported when using Codex with a ChatGPT account.` (probes 05–09). `gpt-5.5` carries `codex()` in the catalog today and is now rejected.
- No probe failed for a reason other than membership: no 401, 403 or 429.
- The backend's smallest accepted `max_output_tokens` is 16 (probe 00).
- The Codex backend's Responses `usage` carries `input_tokens_details.cache_write_tokens` beside `cached_tokens`, and a per-item `attribution` object with the same fields; every value was zero here. This bears on R7 and decision 8 and is recorded, not acted on.

## Anthropic — Claude 5.5 models (R3)

Gathered 2026-10-18 between 17:41 and 17:44 UTC with `curl` against the production endpoint. Every body below is verbatim from the response; the API key is redacted from every request and appears nowhere in this note.

`POST https://api.anthropic.com/v1/messages`, headers `x-api-key: <redacted>`, `anthropic-version: 2023-06-01`, `content-type: application/json`. Non-streaming (the codec streams; the usage block is the same object, and a single JSON body records it whole). Every request is:

```json
{"model":"<model>","max_tokens":1,"messages":[{"role":"user","content":"Reply with the single word OK."}]}
```

plus the member the probe names. `max_tokens: 1` is the smallest value the host accepts, and it accepted it on every thinking request below; no request needed a larger cap. Effort is sent the way `configureAnthropicRequest` in `wire_anthropic_messages.go` sends it, `"output_config":{"effort":"<level>"}` with no `thinking` member. The codec's off form is `"thinking":{"type":"disabled","budget_tokens":0}` (`anthropicThinking.BudgetTokens` has no `omitempty`; `wire_test.go` pins that body); probes send it as the codec does and also bare, `"thinking":{"type":"disabled"}`.

### Default and effort levels

Every response in this table is status 200 with `"stop_reason":"max_tokens"`. Content is `[{"type":"text","text":"O"}]` except at `max`, where it is `[]` on all three models, with `output_tokens` 1 and `output_tokens_details.thinking_tokens` 0.

| Model | member | status | `usage` |
|---|---|---|---|
| `claude-opus-5-5` | none (default) | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-opus-5-5` | `"output_config":{"effort":"low"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-opus-5-5` | `"output_config":{"effort":"medium"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-opus-5-5` | `"output_config":{"effort":"high"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-opus-5-5` | `"output_config":{"effort":"xhigh"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-opus-5-5` | `"output_config":{"effort":"max"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-sonnet-5-5` | none (default) | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-sonnet-5-5` | `"output_config":{"effort":"low"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-sonnet-5-5` | `"output_config":{"effort":"medium"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-sonnet-5-5` | `"output_config":{"effort":"high"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-sonnet-5-5` | `"output_config":{"effort":"xhigh"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-sonnet-5-5` | `"output_config":{"effort":"max"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | none (default) | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"output_config":{"effort":"low"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"output_config":{"effort":"medium"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"output_config":{"effort":"high"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"output_config":{"effort":"xhigh"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"output_config":{"effort":"max"}` | 200 | `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |

### Effort values outside the vocabulary

| Model | member | status | body |
|---|---|---|---|
| `claude-opus-5-5` | `"output_config":{"effort":"none"}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"output_config.effort: Input should be 'low', 'medium', 'high', 'xhigh' or 'max'"},"request_id":"req_011CgA7dxyZnDs6KQ3CFxNCp"}` |
| `claude-sonnet-5-5` | `"output_config":{"effort":"none"}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"output_config.effort: Input should be 'low', 'medium', 'high', 'xhigh' or 'max'"},"request_id":"req_011CgA7e24cUzr5WagUWFfjP"}` |
| `claude-haiku-5-5` | `"output_config":{"effort":"none"}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"output_config.effort: Input should be 'low', 'medium', 'high', 'xhigh' or 'max'"},"request_id":"req_011CgA7e53BwwBsGXU4LUxPK"}` |

### `thinking` disabled

| Model | member | status | body or `usage` |
|---|---|---|---|
| `claude-opus-5-5` | `"thinking":{"type":"disabled","budget_tokens":0}` (codec shape) | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.disabled.budget_tokens: Extra inputs are not permitted"},"request_id":"req_011CgA7dyyM4VBoDH8xg6YKA"}` |
| `claude-opus-5-5` | `"thinking":{"type":"disabled"}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"\"thinking.type.disabled\" is not supported for this model. Use \"thinking.type.adaptive\" and \"output_config.effort\" to control thinking behavior."},"request_id":"req_011CgA7e11rJavzDhn4vqXcF"}` |
| `claude-sonnet-5-5` | `"thinking":{"type":"disabled","budget_tokens":0}` (codec shape) | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.disabled.budget_tokens: Extra inputs are not permitted"},"request_id":"req_011CgA7e348wQtTM5X7Qy3p7"}` |
| `claude-sonnet-5-5` | `"thinking":{"type":"disabled"}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"To turn thinking off on this model, send \"thinking\": {\"type\": \"between_tools\"} instead of {\"type\": \"disabled\"}. The model does not think before responding. The short updates it writes between tool calls come back as thinking blocks."},"request_id":"req_011CgA7e42gBS4HAd4npbULe"}` |
| `claude-haiku-5-5` | `"thinking":{"type":"disabled","budget_tokens":0}` (codec shape) | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.disabled.budget_tokens: Extra inputs are not permitted"},"request_id":"req_011CgA7e63iL9AdUQvHH2Awk"}` |
| `claude-haiku-5-5` | `"thinking":{"type":"disabled"}` | 200 | `{"input_tokens":16,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"thinking":{"type":"disabled"}`, `"output_config":{"effort":"low"}` | 200 | `{"input_tokens":16,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"thinking":{"type":"disabled"}`, `"output_config":{"effort":"medium"}` | 200 | `{"input_tokens":16,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"thinking":{"type":"disabled"}`, `"output_config":{"effort":"high"}` | 200 | `{"input_tokens":16,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |
| `claude-haiku-5-5` | `"thinking":{"type":"disabled"}`, `"output_config":{"effort":"xhigh"}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"output_config.effort 'xhigh' is not supported when thinking is disabled on this model. Use effort 'high' or below, or enable thinking."},"request_id":"req_011CgA7f5wnq6ifjZnxtLzW5"}` |
| `claude-haiku-5-5` | `"thinking":{"type":"disabled"}`, `"output_config":{"effort":"max"}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"output_config.effort 'max' is not supported when thinking is disabled on this model. Use effort 'high' or below, or enable thinking."},"request_id":"req_011CgA7f71ni3FESyUupqPQF"}` |
| `claude-sonnet-5-5` | `"thinking":{"type":"between_tools"}` | 200 | `{"input_tokens":16,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":1,"output_tokens_details":{"thinking_tokens":0},"service_tier":"standard","inference_geo":"global","speed":"standard"}` |

Control, existing rows (not re-probed for their own sake; sent once to learn whether the codec shape's rejection is new to 5.5):

| Model | member | status | body |
|---|---|---|---|
| `claude-sonnet-5` | `"thinking":{"type":"disabled","budget_tokens":0}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.disabled.budget_tokens: Extra inputs are not permitted"},"request_id":"req_011CgA7fAcqREZK92D56u1GJ"}` |
| `claude-haiku-4-5` | `"thinking":{"type":"disabled","budget_tokens":0}` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.disabled.budget_tokens: Extra inputs are not permitted"},"request_id":"req_011CgA7fBbsaAC79NcfSkfu3"}` |

### Output cap

`"max_tokens":10000000`, no other member.

| Model | status | body |
|---|---|---|
| `claude-opus-5-5` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: 10000000 > 128000, which is the maximum allowed number of output tokens for claude-opus-5-5"},"request_id":"req_011CgA7fXRy7X81UoCV3ycQj"}` |
| `claude-sonnet-5-5` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: 10000000 > 128000, which is the maximum allowed number of output tokens for claude-sonnet-5-5"},"request_id":"req_011CgA7fYUE2GX3BhaVxqVBz"}` |
| `claude-haiku-5-5` | 400 | `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: 10000000 > 128000, which is the maximum allowed number of output tokens for claude-haiku-5-5"},"request_id":"req_011CgA7fZRnJKB1NzoWguq54"}` |

### Models API

`GET https://api.anthropic.com/v1/models/<model>`, same headers. Status 200 each.

`claude-opus-5-5`:

```json
{"type":"model","id":"claude-opus-5-5","display_name":"Claude Opus 5.5","created_at":"2026-09-21T16:24:00Z","line":"opus","max_input_tokens":1000000,"max_tokens":128000,"capabilities":{"batch":{"supported":true},"citations":{"supported":true},"code_execution":{"supported":true},"context_management":{"supported":true,"clear_tool_uses_20250919":{"supported":true},"clear_thinking_20251015":{"supported":true},"compact_20260112":{"supported":true}},"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":true},"max":{"supported":true}},"image_input":{"supported":true},"pdf_input":{"supported":true},"server_tools":{"supported":true,"code_execution":{"supported":true},"web_search":{"supported":true}},"structured_outputs":{"supported":true},"thinking":{"supported":true,"types":{"enabled":{"supported":false},"adaptive":{"supported":true},"disabled":{"supported":false},"between_tools":{"supported":false}}}},"lifecycle":"active","deprecated_at":null,"retires_at":null}
```

`claude-sonnet-5-5`:

```json
{"type":"model","id":"claude-sonnet-5-5","display_name":"Claude Sonnet 5.5","created_at":"2026-09-28T00:00:00Z","line":"sonnet","max_input_tokens":1000000,"max_tokens":128000,"capabilities":{"batch":{"supported":true},"citations":{"supported":true},"code_execution":{"supported":true},"context_management":{"supported":true,"clear_tool_uses_20250919":{"supported":true},"clear_thinking_20251015":{"supported":true},"compact_20260112":{"supported":true}},"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":true},"max":{"supported":true}},"image_input":{"supported":true},"pdf_input":{"supported":true},"server_tools":{"supported":true,"code_execution":{"supported":true},"web_search":{"supported":true}},"structured_outputs":{"supported":true},"thinking":{"supported":true,"types":{"enabled":{"supported":false},"adaptive":{"supported":true},"disabled":{"supported":false},"between_tools":{"supported":true}}}},"lifecycle":"active","deprecated_at":null,"retires_at":null}
```

`claude-haiku-5-5`:

```json
{"type":"model","id":"claude-haiku-5-5","display_name":"Claude Haiku 5.5","created_at":"2026-10-07T18:00:00Z","line":"haiku","max_input_tokens":1000000,"max_tokens":128000,"capabilities":{"batch":{"supported":true},"citations":{"supported":true},"code_execution":{"supported":true},"context_management":{"supported":true,"clear_tool_uses_20250919":{"supported":true},"clear_thinking_20251015":{"supported":true},"compact_20260112":{"supported":true}},"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":true},"max":{"supported":true}},"image_input":{"supported":true},"pdf_input":{"supported":true},"server_tools":{"supported":true,"code_execution":{"supported":true},"web_search":{"supported":true}},"structured_outputs":{"supported":true},"thinking":{"supported":true,"types":{"enabled":{"supported":false},"adaptive":{"supported":true},"disabled":{"supported":true},"between_tools":{"supported":false}}}},"lifecycle":"active","deprecated_at":null,"retires_at":null}
```

### Haiku 5.5 prompt-length tier

Two requests to `claude-haiku-5-5`, `max_tokens: 1`, one user message of random words drawn from a twenty-word list followed by `Ignore the words above. Reply with the single word OK.`, sized with `POST /v1/messages/count_tokens` to either side of 100,000 input tokens. Both were answered 200 with a refusal (`stop_reason: "refusal"`, the word list tripped a usage-policy classifier); the usage block is what the probe reads. The `stop_details.explanation` text is elided here; the rest is verbatim.

| prompt | status | `stop_reason` | `usage` |
|---|---|---|---|
| 98,686 tokens by count_tokens | 200 | `refusal` | `{"input_tokens":98731,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":0,"inference_geo":"not_available","speed":"standard"}` |
| 101,379 tokens by count_tokens | 200 | `refusal` | `{"input_tokens":101424,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":0,"inference_geo":"not_available","speed":"standard"}` |

Response headers on both carry the same names and no others: `anthropic-ratelimit-{input-tokens,output-tokens,requests,tokens}-{limit,remaining,reset}`, `anthropic-organization-id`, `anthropic-workspace-id`, `x-robots-tag`, and transport headers. Neither the usage block nor any header names a price tier.

### Pricing pages

Read 2026-10-18 from `https://platform.claude.com/docs/en/about-claude/pricing` (the `.md` rendering; `docs.anthropic.com/en/docs/about-claude/pricing` redirects there) and `https://www.anthropic.com/pricing` (redirects to `https://claude.com/pricing`). Docs model-pricing table, verbatim rows:

```
| Model                 | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |
| Claude Opus 5.5       | $4 / MTok             | $5 / MTok       | $8 / MTok       | $0.20 / MTok<sup>2</sup> | $20 / MTok             |
| Claude Sonnet 5.5     | $2 / MTok             | $2.50 / MTok    | $4 / MTok       | $0.10 / MTok<sup>2</sup> | $10 / MTok             |
| Claude Haiku 5.5 (for prompts up to 100,000 tokens) | $0.10 / MTok          | $0.125 / MTok   | $0.20 / MTok    | $0.01 / MTok             | $0.50 / MTok           |
| Claude Haiku 5.5 (for prompts over 100,000 tokens)  | $0.50 / MTok          | $0.625 / MTok   | $1 / MTok       | $0.05 / MTok             | $2.50 / MTok           |
```

(Model-name column padding trimmed.) Footnote 2: "Cache hits and refreshes on Claude Opus 5.5 and Claude Sonnet 5.5 are priced at 0.05x the base input price."

Long context pricing, verbatim: "Claude Haiku 5.5 is priced by prompt length: a request whose prompt is over 100,000 tokens pays higher prices. A request's prompt length counts all of its input tokens, including cache reads and cache writes. Each request is priced on its own: a request over the threshold pays the higher prices even when part of its prompt is a cache hit, and earlier requests keep the prices they were billed at." And: "Claude 4.6 and later models (except Claude Haiku 5.5) and Claude Mythos Preview include the full 1M token context window at standard pricing."

`claude.com/pricing` agrees on every figure it shows: Opus 5.5 read $0.20, write $5, input $4, output $20; Sonnet 5.5 read $0.10, write $2.50, input $2, output $10; Haiku 5.5 "Prompts ≤ 100K tokens" read $0.01, write $0.125, input $0.10, output $0.50, "Prompts > 100K tokens" read $0.05, write $0.625, input $0.50, output $2.50. It shows only the five-minute write ("Prompt caching pricing reflects 5-minute TTL").

Effort documentation (`https://platform.claude.com/docs/en/build-with-claude/effort.md`), verbatim: "Claude Opus 5.5 supports all five effort levels, and `medium` is the default … Adaptive thinking is always on and can't be turned off … Requests that set `thinking: {"type": "disabled"}` return a 400 error at every effort level." "Claude Sonnet 5.5 supports all five effort levels, and `high` is the default on the Claude API." "To turn off up-front thinking, send `thinking: {"type": "between_tools"}` instead of `"disabled"`. It's the lowest thinking setting on Claude Sonnet 5.5, and it works at `low`, `medium`, and `high` effort. At `xhigh` or `max`, a request with it returns a 400 error". "Claude Haiku 5.5 supports all five effort levels, and `medium` is the default." "You can also send `thinking: {"type": "disabled"}` at `high` effort or below. At `xhigh` or `max`, it returns a 400 error". "Setting `effort` to the model's default (`"medium"` on Claude Opus 5.5 and Claude Haiku 5.5, `"high"` on other models) produces exactly the same behavior as omitting the `effort` parameter entirely."

Established:

- All three models accept `max_tokens: 1` with and without an effort, and accept `output_config.effort` at each of `low`, `medium`, `high`, `xhigh`, `max`. The host validates the value: `none` is rejected with the five-value vocabulary named in the message. The vocabulary on all three is `low` through `max`; the models API reports the same five levels supported.
- The default effort is not observable in a one-token response: the usage block and headers carry no effort. The default rests on the effort page alone: `medium` on Opus 5.5 and Haiku 5.5, `high` on Sonnet 5.5.
- `thinking: {"type":"disabled"}` is rejected on Opus 5.5 (message points to adaptive plus effort) and on Sonnet 5.5 (message names `between_tools` as the off form), and accepted on Haiku 5.5 alone and at `low`, `medium` and `high`; at `xhigh` and `max` it is rejected with "Use effort 'high' or below". The models API's `thinking.types.disabled.supported` matches: false, false, true. `between_tools` is accepted on Sonnet 5.5.
- The codec's off body, `{"type":"disabled","budget_tokens":0}`, is rejected on every model probed, the 5.5 models and the existing `claude-sonnet-5` and `claude-haiku-4-5` rows alike, with `thinking.disabled.budget_tokens: Extra inputs are not permitted`. Only the bare `{"type":"disabled"}` is accepted, and only on Haiku 5.5 here.
- The output cap is 128,000 on all three, read from the rejection; the models API's `max_tokens` agrees, and its `max_input_tokens` is 1,000,000 on all three.
- The Haiku 5.5 tier boundary is not observable on the wire: requests at 98,686 and 101,424 input tokens return the same usage fields and the same headers. The boundary rests on the pricing page: higher prices for a prompt "over 100,000 tokens", counted over all input tokens including cache reads and writes.
- Prices are as the docs table above; the marketing page agrees and omits the one-hour write.

## Gemini — `gemini-4-argon` and `gemini-3.6-flash` thinking controls, 2.5 access, routing (R4)

Gathered 2026-10-18 between 17:43 and 17:46 UTC with `curl` against the production endpoint; every body below is verbatim from the response. Pages read the same day: `/gemini-api/docs/pricing`, `/models`, `/models/gemini-4-argon`, `/models/gemini-3.6-flash`, `/models/gemini-3.8-flash`, `/thinking`, `/latest-model`, `/whats-new-gemini-3.6`, `/deprecations`, `/changelog` under `https://ai.google.dev`.

`POST https://generativelanguage.googleapis.com/v1beta/models/<model>:generateContent`, key in `x-goog-api-key` (redacted). Every request is one user turn and a `generationConfig` holding `maxOutputTokens` and, where shown, `thinkingConfig`; nothing else (no `systemInstruction`, `tools`, sampling parameters):

```json
{"contents":[{"role":"user","parts":[{"text":"Reply with the single word OK."}]}],"generationConfig":{"maxOutputTokens":512,"thinkingConfig":{"thinkingLevel":"low"}}}
```

The A, F and L probes use the prompt above. The B probes use prompts meant to draw thinking: B01–B05 "What is 3797 times 4271? Reply with the number only.", B06–B09 "Find the smallest positive integer n such that n squared ends in the digits 444. Reply with the number only.", B10–B12 "How many ways can a 3 by 12 rectangle be tiled with 1 by 2 dominoes? Reply with the number only." Every 200 answered with `finishReason` `STOP`.

### `gemini-4-argon`, trivial prompt

| Probe | model | `thinkingConfig` | `maxOutputTokens` | status | `usageMetadata` or error body |
|---|---|---|---|---|---|
| A01 | `gemini-4-argon` | — | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":43,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}]}` (`modelVersion` `gemini-4-argon`) |
| A02 | `gemini-4-argon` | — | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":116,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}],"thoughtsTokenCount":73}` (`modelVersion` `gemini-4-argon`) |
| A03 | `gemini-4-argon` | — | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":43,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}]}` (`modelVersion` `gemini-4-argon`) |
| A04 | `gemini-4-argon` | `{"thinkingLevel":"low"}` | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":43,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}]}` (`modelVersion` `gemini-4-argon`) |
| A05 | `gemini-4-argon` | `{"thinkingLevel":"medium"}` | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":101,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}],"thoughtsTokenCount":58}` (`modelVersion` `gemini-4-argon`) |
| A06 | `gemini-4-argon` | `{"thinkingLevel":"high"}` | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":136,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}],"thoughtsTokenCount":93}` (`modelVersion` `gemini-4-argon`) |
| A07 | `gemini-4-argon` | `{"thinkingLevel":"minimal"}` | 512 | 400 | `{"error":{"code":400,"message":"Thinking level MINIMAL is not supported for this model. Please retry with other thinking level.","status":"INVALID_ARGUMENT"}}` |
| A08 | `gemini-4-argon` | `{"thinkingLevel":"none"}` | 512 | 400 | `{"error":{"code":400,"message":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"none\"","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"generation_config.thinking_config.thinking_level","description":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"none\""}]}]}}` |
| A09 | `gemini-4-argon` | `{"thinkingLevel":"off"}` | 512 | 400 | `{"error":{"code":400,"message":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"off\"","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"generation_config.thinking_config.thinking_level","description":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"off\""}]}]}}` |
| A10 | `gemini-4-argon` | `{"thinkingLevel":"xhigh"}` | 512 | 400 | `{"error":{"code":400,"message":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"xhigh\"","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"generation_config.thinking_config.thinking_level","description":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"xhigh\""}]}]}}` |
| A11 | `gemini-4-argon` | `{"thinkingLevel":"max"}` | 512 | 400 | `{"error":{"code":400,"message":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"max\"","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"generation_config.thinking_config.thinking_level","description":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"max\""}]}]}}` |
| A12 | `gemini-4-argon` | `{"thinkingBudget":0}` | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":43,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}]}` (`modelVersion` `gemini-4-argon`) |
| A13 | `gemini-4-argon` | `{"thinkingBudget":0}` | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":43,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}]}` (`modelVersion` `gemini-4-argon`) |
| A14 | `gemini-4-argon` | `{"thinkingBudget":0}` | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":43,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}]}` (`modelVersion` `gemini-4-argon`) |
| A15 | `gemini-4-argon` | `{"thinkingBudget":-1}` | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":144,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}],"thoughtsTokenCount":101}` (`modelVersion` `gemini-4-argon`) |
| A16 | `gemini-4-argon` | `{"thinkingBudget":128}` | 512 | 200 | `{"promptTokenCount":38,"candidatesTokenCount":5,"totalTokenCount":43,"promptTokensDetails":[{"modality":"TEXT","tokenCount":38}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}]}` (`modelVersion` `gemini-4-argon`) |

### `gemini-4-argon`, `low` against `thinkingBudget: 0` on prompts that draw thinking

| Probe | model | `thinkingConfig` | `maxOutputTokens` | status | `usageMetadata` or error body |
|---|---|---|---|---|---|
| B01 | `gemini-4-argon` | — | 512 | 200 | `{"promptTokenCount":51,"candidatesTokenCount":12,"totalTokenCount":163,"promptTokensDetails":[{"modality":"TEXT","tokenCount":51}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":12}],"thoughtsTokenCount":100}` (`modelVersion` `gemini-4-argon`) |
| B02 | `gemini-4-argon` | `{"thinkingLevel":"low"}` | 2048 | 200 | `{"promptTokenCount":51,"candidatesTokenCount":12,"totalTokenCount":63,"promptTokensDetails":[{"modality":"TEXT","tokenCount":51}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":12}]}` (`modelVersion` `gemini-4-argon`) |
| B03 | `gemini-4-argon` | `{"thinkingLevel":"low"}` | 2048 | 200 | `{"promptTokenCount":51,"candidatesTokenCount":12,"totalTokenCount":63,"promptTokensDetails":[{"modality":"TEXT","tokenCount":51}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":12}]}` (`modelVersion` `gemini-4-argon`) |
| B04 | `gemini-4-argon` | `{"thinkingBudget":0}` | 2048 | 200 | `{"promptTokenCount":51,"candidatesTokenCount":12,"totalTokenCount":63,"promptTokensDetails":[{"modality":"TEXT","tokenCount":51}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":12}]}` (`modelVersion` `gemini-4-argon`) |
| B05 | `gemini-4-argon` | `{"thinkingBudget":0}` | 2048 | 200 | `{"promptTokenCount":51,"candidatesTokenCount":12,"totalTokenCount":63,"promptTokensDetails":[{"modality":"TEXT","tokenCount":51}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":12}]}` (`modelVersion` `gemini-4-argon`) |
| B06 | `gemini-4-argon` | `{"thinkingLevel":"low"}` | 4096 | 200 | `{"promptTokenCount":56,"candidatesTokenCount":6,"totalTokenCount":62,"promptTokensDetails":[{"modality":"TEXT","tokenCount":56}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":6}]}` (`modelVersion` `gemini-4-argon`) |
| B07 | `gemini-4-argon` | `{"thinkingLevel":"low"}` | 4096 | 200 | `{"promptTokenCount":56,"candidatesTokenCount":6,"totalTokenCount":62,"promptTokensDetails":[{"modality":"TEXT","tokenCount":56}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":6}]}` (`modelVersion` `gemini-4-argon`) |
| B08 | `gemini-4-argon` | `{"thinkingBudget":0}` | 4096 | 200 | `{"promptTokenCount":56,"candidatesTokenCount":6,"totalTokenCount":62,"promptTokensDetails":[{"modality":"TEXT","tokenCount":56}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":6}]}` (`modelVersion` `gemini-4-argon`) |
| B09 | `gemini-4-argon` | `{"thinkingBudget":0}` | 4096 | 200 | `{"promptTokenCount":56,"candidatesTokenCount":6,"totalTokenCount":62,"promptTokensDetails":[{"modality":"TEXT","tokenCount":56}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":6}]}` (`modelVersion` `gemini-4-argon`) |
| B10 | `gemini-4-argon` | `{"thinkingLevel":"low"}` | 8192 | 200 | `{"promptTokenCount":60,"candidatesTokenCount":26,"totalTokenCount":86,"promptTokensDetails":[{"modality":"TEXT","tokenCount":60}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":26}]}` (`modelVersion` `gemini-4-argon`) |
| B11 | `gemini-4-argon` | `{"thinkingLevel":"medium"}` | 8192 | 200 | `{"promptTokenCount":60,"candidatesTokenCount":8,"totalTokenCount":332,"promptTokensDetails":[{"modality":"TEXT","tokenCount":60}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":264}` (`modelVersion` `gemini-4-argon`) |
| B12 | `gemini-4-argon` | `{"thinkingBudget":0}` | 8192 | 200 | `{"promptTokenCount":60,"candidatesTokenCount":24,"totalTokenCount":84,"promptTokensDetails":[{"modality":"TEXT","tokenCount":60}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":24}]}` (`modelVersion` `gemini-4-argon`) |

B10 answered `"2131 nominal ways... wait, the user asked for the number only.\n\n2131"`, B12 `"2131 nominal ways. Let me just output the number. \n\n2131"`, B11 `"2131"`.

### `gemini-3.6-flash`

| Probe | model | `thinkingConfig` | `maxOutputTokens` | status | `usageMetadata` or error body |
|---|---|---|---|---|---|
| F01 | `gemini-3.6-flash` | — | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":84,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":75,"serviceTier":"standard"}` (`modelVersion` `gemini-3.6-flash`) |
| F02 | `gemini-3.6-flash` | `{"thinkingLevel":"minimal"}` | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":9,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"serviceTier":"standard"}` (`modelVersion` `gemini-3.6-flash`) |
| F03 | `gemini-3.6-flash` | `{"thinkingLevel":"low"}` | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":66,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":57,"serviceTier":"standard"}` (`modelVersion` `gemini-3.6-flash`) |
| F04 | `gemini-3.6-flash` | `{"thinkingLevel":"medium"}` | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":89,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":80,"serviceTier":"standard"}` (`modelVersion` `gemini-3.6-flash`) |
| F05 | `gemini-3.6-flash` | `{"thinkingLevel":"high"}` | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":99,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":90,"serviceTier":"standard"}` (`modelVersion` `gemini-3.6-flash`) |
| F06 | `gemini-3.6-flash` | `{"thinkingLevel":"xhigh"}` | 512 | 400 | `{"error":{"code":400,"message":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"xhigh\"","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"generation_config.thinking_config.thinking_level","description":"Invalid value at 'generation_config.thinking_config.thinking_level' (type.googleapis.com/google.ai.generativelanguage.v1beta.ThinkingConfig.ThinkingLevel), \"xhigh\""}]}]}}` |
| F07 | `gemini-3.6-flash` | `{"thinkingBudget":0}` | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":9,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"serviceTier":"standard"}` (`modelVersion` `gemini-3.6-flash`) |

### Legacy and deprecated ids

| Probe | model | `thinkingConfig` | `maxOutputTokens` | status | `usageMetadata` or error body |
|---|---|---|---|---|---|
| L01 | `gemini-2.5-flash` | — | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":27,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":18,"serviceTier":"standard"}` (`modelVersion` `gemini-2.5-flash`) |
| L02 | `gemini-2.5-pro` | — | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":209,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":200,"serviceTier":"standard"}` (`modelVersion` `gemini-2.5-pro`) |
| L03 | `gemini-3.7-flash` | — | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":104,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":95,"serviceTier":"standard"}` (`modelVersion` `gemini-3.8-flash`) |
| L04 | `gemini-3.5-flash` | — | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":80,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":71,"serviceTier":"standard"}` (`modelVersion` `gemini-3.6-flash`) |
| L05 | `gemini-3.8-flash` | — | 512 | 200 | `{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":71,"promptTokensDetails":[{"modality":"TEXT","tokenCount":8}],"thoughtsTokenCount":62,"serviceTier":"standard"}` (`modelVersion` `gemini-3.8-flash`) |

### Models API

`GET /v1beta/models/<model>`, 2026-10-18T17:45:52Z, status 200 for both:

```json
{"name":"models/gemini-4-argon","version":"3.0","displayName":"Gemini 4 Argon","description":"Gemini 4 Argon","inputTokenLimit":1048576,"outputTokenLimit":32768,"supportedGenerationMethods":["generateContent","countTokens","batchGenerateContent"],"temperature":1,"topP":0.95,"topK":64,"maxTemperature":2,"thinking":true}
{"name":"models/gemini-3.6-flash","version":"3.6-flash-07-2026","displayName":"Gemini 3.6 Flash","description":"Gemini 3.6 Flash","inputTokenLimit":1048576,"outputTokenLimit":65536,"supportedGenerationMethods":["generateContent","countTokens","createCachedContent","batchGenerateContent"],"temperature":1,"topP":0.95,"topK":64,"maxTemperature":2,"thinking":true}
```

### What the pages say (read 2026-10-18)

- `gemini-4-argon` model page: context window "1,048,576" (unified input and output); max output "65,536 (default synchronous) / up to 1,048,576 (with long decode)"; caching "Supported (Implicit only) ... Explicit caching is not supported."; thinking "Supported (low, medium, high)". The thinking guide's table gives default "On (medium)", levels "low, medium, high". `latest-model`: default `medium`; implicit caching for prompts of at least 4,096 tokens at a 95% discount. GA 2026-10-13 (changelog).
- `gemini-4-argon` pricing, Standard paid tier, per 1M tokens: input "$2.00 through November 30, 2026. $4.00 starting December 1, 2026."; output "$10.00 through November 30, 2026. $20.00 starting December 1, 2026."; context caching "$0.10 through November 30, 2026. $0.20 starting December 1, 2026. Implicit caching only (explicit caching is not supported)." No long-context tier. Not available on the Free tier.
- `gemini-3.6-flash` model page: input token limit "1,048,576", output "65,536", caching "Supported", thinking "Supported" (no levels listed). Thinking guide: default "On (medium)", levels "minimal, low, medium, high". Pricing, Standard paid: input "$0.75 through December 31, 2026. $1.50 starting January 1, 2027."; output "$3.75 through December 31, 2026. $7.50 starting January 1, 2027."; caching "$0.075 through December 31, 2026. $0.15 starting January 1, 2027." plus storage. Described as "Our previous generation Flash model". GA July 21, 2026 (changelog and deprecations table release date).
- `gemini-3.8-flash` pricing, Standard paid: identical to 3.6 Flash ($0.75 / $0.075 / $3.75 through December 31, 2026; $1.50 / $0.15 / $7.50 from January 1, 2027). Model page: thinking "Supported (low, medium, high) Note: minimal is not supported and returns an error."
- Deprecations page lists both `gemini-3.6-flash` and `gemini-3.8-flash` with "No shutdown date announced" and no replacement. Changelog, October 8, 2026: "gemini-3.7-flash is deprecated and has been replaced by gemini-3.8-flash ... API requests to gemini-3.7-flash are automatically routed to gemini-3.8-flash" and "gemini-3.5-flash is deprecated and has been replaced by gemini-3.6-flash ... routed to gemini-3.6-flash". Neither appears on the pricing page.
- 2.5 models, changelog September 18, 2026 and the deprecations page: "we are limiting access to the 2.5 models to users who have actively used them in the past. These models are not deprecated and will continue to be served until further notice through the API."

Established:

- `gemini-4-argon` accepts `thinkingLevel` `low`, `medium` and `high` (A04–A06) and rejects `minimal` with 400 "Thinking level MINIMAL is not supported for this model." (A07). `none`, `off`, `xhigh` and `max` are not values of the enum on this API and fail field validation (A08–A11).
- `gemini-4-argon` with no `thinkingConfig` sometimes reports `thoughtsTokenCount` and sometimes omits it (A01–A03, B01); its usage block carries `candidatesTokensDetails` and no `serviceTier`, unlike 3.6 Flash.
- `gemini-4-argon` accepts `thinkingBudget: 0` with 200 and reports no thought tokens (A12–A14, B04, B05, B08, B09, B12). `thinkingLevel: low` likewise reported no thought tokens on every prompt tried (A04, B02, B03, B06, B07, B10), while `medium` on the same prompt thought (B11, 264 tokens). On these probes, budget 0 cannot be told apart from `low`, so they do not show whether budget 0 turns thinking off or is mapped to the lowest level. `thinkingBudget: 128` also reported no thoughts (A16), and `-1` thought (A15).
- `gemini-3.6-flash` accepts `minimal`, `low`, `medium` and `high`; `minimal` reports no thought tokens (F02); `xhigh` is rejected (F06). The default thinks (F01). `thinkingBudget: 0` is accepted and reports no thought tokens (F07), the same as `minimal`.
- `gemini-2.5-flash` and `gemini-2.5-pro` still answer with this key (L01, L02): 200, `modelVersion` echoing the id, with thinking. This key is not refused; the pages say access is limited to past users and the models are not deprecated.
- `gemini-3.7-flash` and `gemini-3.5-flash` still answer, as `modelVersion` `gemini-3.8-flash` and `gemini-3.6-flash` respectively (L03, L04): the routing the changelog describes.
- The models API gives `gemini-4-argon` an `outputTokenLimit` of 32768, against 65,536 on its model page, and lists no `createCachedContent` method for it, which fits implicit-only caching; `gemini-3.6-flash` lists `createCachedContent`.

## xAI — `grok-build-0.1` (R5)

Gathered 2026-10-18 between 17:42 and 17:45 UTC with `curl` against the production endpoints; every body below is verbatim from the response. Credentials are redacted: `api_key` means `Authorization: Bearer <XAI_API_KEY>`, `oauth` means `Authorization: Bearer <access_token from ~/.agentkit/x-ai-auth.json>` (scope `openid profile email offline_access grok-cli:access api:access`).

`POST https://api.x.ai/v1/chat/completions` (chat) with `{"model":"grok-build-0.1","messages":[{"role":"user","content":"Reply with the single word OK."}], <cap>}`; `POST https://api.x.ai/v1/responses` (responses) with `{"model":"grok-build-0.1","input":"Reply with the single word OK.","max_output_tokens":1}`. Probe-specific members are named in each table. Non-streaming.

### Default, no reasoning member

A cap of 1 is accepted on every combination (`max_tokens`, `max_completion_tokens`, `max_output_tokens`). Chat answers `"content":"OK"` with `"finish_reason":"length"` and a `reasoning_content` string; Responses answers `"status":"incomplete"`, `"incomplete_details":{"reason":"max_output_tokens"}`, a `reasoning` output item with a summary, then the `OK` message. `reasoning` is the Responses body's echo of the request's reasoning settings.

| Probe | auth | wire | cap | status | `model` | `reasoning` echo | `usage` |
|---|---|---|---|---|---|---|---|
| t01 | api_key | chat | `max_tokens: 1` | 200 | `grok-build-0.1` | `—` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":355,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":161,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":3634000}` |
| t02 | api_key | responses | `max_output_tokens: 1` | 200 | `grok-build-0.1` | `{"effort":"medium","summary":"detailed"}` | `{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":150,"output_tokens_details":{"reasoning_tokens":149},"total_tokens":343,"num_sources_used":0,"num_server_side_tools_used":0,"cost_in_usd_ticks":3394000,"context_details":{"input_tokens":193,"output_tokens":150}}` |
| d_oauth_chat | oauth | chat | `max_tokens: 1` | 200 | `grok-build-0.1` | `—` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":335,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":141,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":3234000}` |
| d_oauth_resp | oauth | responses | `max_output_tokens: 1` | 200 | `grok-build-0.1` | `{"effort":"medium","summary":"detailed"}` | `{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":228,"output_tokens_details":{"reasoning_tokens":227},"total_tokens":421,"num_sources_used":0,"num_server_side_tools_used":0,"cost_in_usd_ticks":4954000,"context_details":{"input_tokens":193,"output_tokens":228}}` |
| x_key_chat_mct | api_key | chat | `max_completion_tokens: 1` | 200 | `grok-build-0.1` | `—` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":447,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":253,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":5474000}` |
| x_oauth_chat_mct | oauth | chat | `max_completion_tokens: 1` | 200 | `grok-build-0.1` | `—` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":311,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":117,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":2754000}` |

Full body of `t01` (api_key, chat):

```json
{"id":"976f0124-7471-9ec3-b6f5-04eabb42f631","object":"chat.completion","created":1792345342,"model":"grok-build-0.1","choices":[{"index":0,"message":{"role":"assistant","content":"OK","reasoning_content":"The user requested a single-word reply: OK.","refusal":null},"finish_reason":"length"}],"usage":{"prompt_tokens":193,"completion_tokens":1,"total_tokens":355,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":161,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":3634000},"system_fingerprint":"fp_1758720567b36303","service_tier":"default"}
```

Full body of `t02` (api_key, responses):

```json
{"created_at":1792345343,"completed_at":null,"id":"5c5f4c36-1a4c-9ccd-bb6e-74b2aed7252c","max_output_tokens":1,"model":"grok-build-0.1","object":"response","output":[{"id":"rs_5c5f4c36-1a4c-9ccd-bb6e-74b2aed7252c","summary":[{"text":"The user requested a single-word reply: OK.","type":"summary_text"}],"type":"reasoning","status":"completed"},{"content":[{"type":"output_text","text":"OK","logprobs":[],"annotations":[]}],"id":"msg_5c5f4c36-1a4c-9ccd-bb6e-74b2aed7252c","role":"assistant","type":"message","status":"completed"}],"parallel_tool_calls":true,"previous_response_id":null,"reasoning":{"effort":"medium","summary":"detailed"},"temperature":0.7,"text":{"format":{"type":"text"}},"tool_choice":"auto","tools":[],"top_p":0.95,"usage":{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":150,"output_tokens_details":{"reasoning_tokens":149},"total_tokens":343,"num_sources_used":0,"num_server_side_tools_used":0,"cost_in_usd_ticks":3394000,"context_details":{"input_tokens":193,"output_tokens":150}},"user":null,"incomplete_details":{"reason":"max_output_tokens"},"status":"incomplete","store":true,"metadata":{"system_fingerprint":"fp_1758720567b36303"},"background":false,"service_tier":"default","truncation":"disabled","top_logprobs":0,"presence_penalty":0.0,"frequency_penalty":0.0,"prompt_cache_key":null,"max_tool_calls":null,"safety_identifier":null,"error":null,"instructions":null}
```

### `reasoning_effort`

Chat probes add `"reasoning_effort":"<e>"` (with `max_tokens: 1`); Responses probes add `"reasoning":{"effort":"<e>"}` (with `max_output_tokens: 1`), plus two Responses probes with top-level `"reasoning_effort":"low"`. Each of `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` was sent on each of the four auth × wire combinations (28 requests), plus the two top-level probes (30 in all).

| Probe | auth | wire | member | values | status | body |
|---|---|---|---|---|---|---|
| e_key_chat_* | api_key | chat | `reasoning_effort` | none, minimal, low, medium, high, xhigh, max | 400 each | `{"code":"invalid-argument","error":"Model grok-build-0.1 does not support parameter reasoningEffort."}` |
| e_oauth_chat_* | oauth | chat | `reasoning_effort` | none, minimal, low, medium, high, xhigh, max | 400 each | identical |
| e_key_resp_* | api_key | responses | `reasoning.effort` | none, minimal, low, medium, high, xhigh, max | 400 each | identical |
| e_oauth_resp_* | oauth | responses | `reasoning.effort` | none, minimal, low, medium, high, xhigh, max | 400 each | identical |
| x_key_resp_toplevel | api_key | responses | top-level `reasoning_effort` | low | 400 | identical |
| x_oauth_resp_toplevel | oauth | responses | top-level `reasoning_effort` | low | 400 | identical |

All 30 bodies are byte-identical. `medium` is rejected too, although it is the value Responses echoes as its default.

### `reasoning.enabled` (the shape agentkit's chat and responses codecs send for `ReasoningOn`)

Chat probes use `max_completion_tokens: 1`, Responses probes `max_output_tokens: 1`.

| Probe | auth | wire | member | status | `reasoning` echo | `usage` |
|---|---|---|---|---|---|---|
| x_key_chat_on | api_key | chat | `reasoning: {"enabled": true}` | 200 | `—` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":362,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":168,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":3774000}` |
| x_oauth_chat_on | oauth | chat | `reasoning: {"enabled": true}` | 200 | `—` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":393,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":199,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":4394000}` |
| x_key_resp_on | api_key | responses | `reasoning: {"enabled": true}` | 200 | `{"effort":"medium","summary":"detailed"}` | `{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":248,"output_tokens_details":{"reasoning_tokens":247},"total_tokens":441,"num_sources_used":0,"num_server_side_tools_used":0,"cost_in_usd_ticks":5354000,"context_details":{"input_tokens":193,"output_tokens":248}}` |
| x_oauth_resp_on | oauth | responses | `reasoning: {"enabled": true}` | 200 | `{"effort":"medium","summary":"detailed"}` | `{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":163,"output_tokens_details":{"reasoning_tokens":162},"total_tokens":356,"num_sources_used":0,"num_server_side_tools_used":0,"cost_in_usd_ticks":3654000,"context_details":{"input_tokens":193,"output_tokens":163}}` |
| y_key_chat_off | api_key | chat | `reasoning: {"enabled": false}` | 200 | `—` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":391,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":197,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":4354000}` |
| y_key_resp_off | api_key | responses | `reasoning: {"enabled": false}` | 200 | `{"effort":"medium","summary":"detailed"}` | `{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":212,"output_tokens_details":{"reasoning_tokens":211},"total_tokens":405,"num_sources_used":0,"num_server_side_tools_used":0,"cost_in_usd_ticks":4634000,"context_details":{"input_tokens":193,"output_tokens":212}}` |

`enabled` is accepted and has no visible effect: `enabled: false` still reasons (197 and 211 reasoning tokens) and still returns `reasoning_content` / a reasoning item.

### Alias `grok-code-fast-1`

As the defaults, with `"model":"grok-code-fast-1"` (chat cap `max_completion_tokens: 1`).

| Probe | auth | wire | status | `model` in response | `usage` |
|---|---|---|---|---|---|
| al_key_chat_codefast | api_key | chat | 200 | `grok-build-0.1` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":265,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":71,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":1834000}` |
| al_key_resp_codefast | api_key | responses | 200 | `grok-build-0.1` | `{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":159,"output_tokens_details":{"reasoning_tokens":158},"total_tokens":352,"num_sources_used":0,"num_server_side_tools_used":0,"cost_in_usd_ticks":3574000,"context_details":{"input_tokens":193,"output_tokens":159}}` |
| al_oauth_chat_codefast | oauth | chat | 200 | `grok-build-0.1` | `{"prompt_tokens":193,"completion_tokens":1,"total_tokens":354,"prompt_tokens_details":{"text_tokens":193,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},"completion_tokens_details":{"reasoning_tokens":160,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0},"num_sources_used":0,"cost_in_usd_ticks":3614000}` |

The reverse direction is the defaults table: a request naming `grok-build-0.1` answers `"model":"grok-build-0.1"`.

`GET https://api.x.ai/v1/models/grok-code-fast-1` (api_key), status 200, returns the `grok-build-0.1` record:

```json
{"id":"grok-build-0.1","aliases":["grok-code-fast-1","grok-code-fast","grok-code-fast-1-0825"],"context_length":256000,"created":1776297600,"object":"model","owned_by":"xai","prompt_text_token_price":10000,"cached_prompt_text_token_price":2000,"prompt_image_token_price":10000,"completion_text_token_price":20000,"prompt_text_token_price_long_context":20000,"cached_prompt_text_token_price_long_context":4000,"completion_text_token_price_long_context":40000,"long_context_threshold":200000}
```

`GET https://api.x.ai/v1/language-models/grok-build-0.1` (api_key), status 200:

```json
{"id":"grok-build-0.1","fingerprint":"fp_dedee3c72d","created":1776297600,"object":"model","owned_by":"xai","version":"1.0","input_modalities":["text","image"],"output_modalities":["text"],"prompt_text_token_price":10000,"cached_prompt_text_token_price":2000,"prompt_image_token_price":10000,"completion_text_token_price":20000,"search_price":0,"prompt_text_token_price_long_context":20000,"cached_prompt_text_token_price_long_context":4000,"completion_text_token_price_long_context":40000,"long_context_threshold":200000,"aliases":["grok-code-fast-1","grok-code-fast","grok-code-fast-1-0825"]}
```

`GET https://api.x.ai/v1/language-models/grok-4.7` (api_key), status 200, for contrast; it carries a `capabilities` block that `grok-build-0.1` lacks:

```json
{"id":"grok-4.7","fingerprint":"fp_86b7148262","created":1788307200,"object":"model","owned_by":"xai","version":"1.0","input_modalities":["text","image"],"output_modalities":["text"],"prompt_text_token_price":20000,"cached_prompt_text_token_price":5000,"prompt_image_token_price":20000,"completion_text_token_price":60000,"search_price":0,"prompt_text_token_price_long_context":40000,"cached_prompt_text_token_price_long_context":10000,"completion_text_token_price_long_context":120000,"long_context_threshold":200000,"aliases":[],"capabilities":{"reasoning_effort":["low","medium","high","xhigh"],"default_reasoning_effort":"high"}}
```

### Pricing and context, from the vendor

`https://docs.x.ai/docs/models` redirects to `https://docs.x.ai/developers/models` (status 200, fetched 2026-10-18). The page's embedded model data, verbatim excerpts (escaping removed):

```
{"name":"grok-build-0.1","version":"1.0","inputModalities":["TEXT","IMAGE"],"outputModalities":["TEXT"],"promptTextTokenPrice":"10000","promptImageTokenPrice":"10000","promptTextTokenPriceLongContext":"20000","cachedPromptTokenPrice":"2000","cachedPromptTokenPriceLongContext":"4000","completionTextTokenPrice":"20000","completionTokenPriceLongContext":"40000",...,"maxPromptLength":256000,"aliases":["grok-code-fast-1","grok-code-fast","grok-code-fast-1-0825"],"features":{"functionCalling":true,"structuredOutputs":true,"reasoning":true},...,"longContextThreshold":"200000",...,"batchDiscountPercent":20,...}
{"name":"grok-4.7","version":"1.0",...,"promptTextTokenPrice":"20000","promptImageTokenPrice":"20000","promptTextTokenPriceLongContext":"40000","cachedPromptTokenPrice":"5000","cachedPromptTokenPriceLongContext":"10000","completionTextTokenPrice":"60000","completionTokenPriceLongContext":"120000",...,"maxPromptLength":500000,"features":{"functionCalling":true,"structuredOutputs":true,"reasoning":true,"reasoningEffortOptions":{"supportedEfforts":["low","medium","high","xhigh"],"defaultEffort":"high"}},...,"longContextThreshold":"200000",...}
```

The price unit is 1/10,000 USD per million tokens (10000 = $1/M), consistent across the page and both API model endpoints. The page does not say whether a prompt of exactly 200,000 tokens is billed at the base or the long-context rate.

The billed cost agrees with $1 / $0.20 / $2 at the base tier: `cost_in_usd_ticks` is 1e-10 USD, and `t01` (1 uncached + 192 cached input, 1 + 161 reasoning output) is 1×1000 + 192×200 + 162×2000 = 363,400 nano-USD = 3,634,000 ticks, exactly as reported. On chat, `completion_tokens` excludes `reasoning_tokens`; both are billed at the output rate. On Responses, `output_tokens` includes them (`t02`: 1×1000 + 192×200 + 150×2000 = 339,400 nano-USD = 3,394,000 ticks).

Established:

- `grok-build-0.1` answers on both `api.x.ai` protocols with the API key and with the xAI OAuth bearer at the same URLs. A cap of 1 is accepted on every combination.
- It always reasons: every successful request reports a non-zero `reasoning_tokens`. Responses echoes `"reasoning":{"effort":"medium","summary":"detailed"}` by default.
- No reasoning-effort value is accepted. Every one of `none`, `minimal`, `low`, `medium`, `high`, `xhigh` and `max`, as `reasoning_effort` on chat and as `reasoning.effort` or top-level `reasoning_effort` on Responses, is rejected 400 `invalid-argument`, `Model grok-build-0.1 does not support parameter reasoningEffort.`, under either credential.
- `reasoning: {"enabled": true|false}` is accepted on both protocols and changes nothing observable; reasoning cannot be turned off.
- `grok-code-fast-1` is an alias. It answers on both protocols, the response's `model` is `grok-build-0.1`, and `/v1/models/grok-code-fast-1` returns the `grok-build-0.1` record with aliases `grok-code-fast-1`, `grok-code-fast` and `grok-code-fast-1-0825`.
- Context window 256,000 (`maxPromptLength`, `context_length`). Long-context threshold 200,000. Rates are $1 input, $0.20 cached input and $2 output, all three doubling past the threshold ($2 / $0.40 / $4).
- `grok-4.7`: $2 / $0.50 / $6, doubling past the 200,000 threshold ($4 / $1 / $12), with 500,000 context and efforts `low` through `xhigh`, default `high`. This is unchanged from the catalog's xAI-host rows.

## OpenRouter (R6)

Gathered 2026-10-18 between 17:42 and 17:46 UTC with `curl` against the production endpoints. The key went in `Authorization: Bearer <redacted>`. Every body below is verbatim from the response, except that the leading whitespace OpenRouter streams as keep-alive before a JSON body is stripped.

### The models API snapshot

`GET https://openrouter.ai/api/v1/models`, one request, status 200, response `date: Sun, 18 Oct 2026 17:42:34 GMT`, 471 models. Saved whole as `openrouter-models-2026-10-18.json` beside this note. Every OpenRouter price below comes from that one snapshot: `pricing.prompt`, `pricing.input_cache_read`, `pricing.input_cache_write`, `pricing.input_cache_write_1h` and `pricing.completion` are USD per token, multiplied by 10^9 and rounded half-up to the nearest nano-USD. A `pricing.overrides` entry with `min_prompt_tokens` is a long-context tier. Each entry also carries a `reasoning` object (`mandatory`, `default_enabled`, `supported_efforts`, `default_effort`), quoted where it matters.

Tiers below are written `[min_input, uncached, cache_read, write_5m, write_1h, output]` in nano-USD per token. "Old" is `specs/_data/catalog_table.go` as of b1d97656. The snapshot writes tier boundaries as `min_prompt_tokens` 100000, 200000 or 272000; the catalog keys most of them `+1` (`200_001`, `272_001`), and the table gives the snapshot's number.

#### Every OpenRouter offering the catalog carries

| Catalog model | OpenRouter id | context old → new | old tiers | new tiers (snapshot) |
|---|---|---|---|---|
| claude-opus-5 | `anthropic/claude-opus-5` | 1000000 → 1000000 | [0,5000,500,6250,10000,25000] | [0,5000,500,6250,10000,25000] |
| claude-opus-4-8 | `anthropic/claude-opus-4.8` | 1000000 → 1000000 | [0,5000,500,6250,10000,25000] | [0,5000,500,6250,10000,25000] |
| claude-sonnet-4-6 | `anthropic/claude-sonnet-4.6` | 1000000 → 1000000 | [0,3000,300,3750,6000,15000] | [0,3000,300,3750,6000,15000] |
| claude-haiku-4-5 | `anthropic/claude-haiku-4.5` | 200000 → 200000 | [0,1000,100,1250,2000,5000] | [0,1000,100,1250,2000,5000] |
| claude-fable-5 | `anthropic/claude-fable-5` | 1000000 → 1000000 | [0,10000,1000,12500,20000,50000] | [0,10000,1000,12500,20000,50000] |
| claude-fable-5-1 | `anthropic/claude-fable-5.1` | 1000000 → 1000000 | [0,10000,250,12500,20000,50000] | [0,10000,250,12500,20000,50000] |
| claude-sonnet-5 | `anthropic/claude-sonnet-5` | 1000000 → 1000000 | [0,3000,300,3750,6000,15000] | [0,2000,200,2500,4000,10000] |
| gemini-2.5-flash | `google/gemini-2.5-flash` | 1048576 → 1048576 | [0,300,30,0,0,2500] | [0,300,30,83,0,2500] |
| gemini-2.5-pro | `google/gemini-2.5-pro` | 1048576 → 1048576 | [0,1250,125,0,0,10000] + [200001,2500,250,0,0,15000] | [0,1250,125,375,0,10000] + [200000,2500,250,0,0,15000] |
| gemini-3.5-flash | `google/gemini-3.5-flash` | 1048576 → 1048576 | [0,1500,150,0,0,9000] | [0,1500,150,83,0,9000] |
| gemini-3.7-flash | `google/gemini-3.7-flash` | 1048576 → 1048576 | [0,375,38,0,0,1875] | [0,750,75,42,0,3750] |
| gemini-3.1-flash-lite | `google/gemini-3.1-flash-lite` | 1048576 → 1048576 | [0,250,25,0,0,1500] | [0,250,25,83,0,1500] |
| gemini-3.1-pro-preview | `google/gemini-3.1-pro-preview` | 1048576 → 1048576 | [0,2000,200,0,0,12000] + [200001,4000,400,0,0,18000] | [0,2000,200,375,0,12000] + [200000,4000,400,0,0,18000] |
| gemini-3.8-flash | `google/gemini-3.8-flash` | 1048576 → 1048576 | [0,1500,150,0,0,7500] | [0,825,83,42,0,4125] |
| gemini-3.5-flash-lite | `google/gemini-3.5-flash-lite` | 1048576 → 1048576 | [0,300,30,0,0,2500] | [0,300,30,83,0,2500] |
| gpt-5.5-pro | `openai/gpt-5.5-pro` | 1050000 → 1050000 | [0,30000,30000,0,0,180000] | [0,30000,0,0,0,180000] + [272000,60000,0,0,0,270000] |
| gpt-5.5 | `openai/gpt-5.5` | 1050000 → 1050000 | [0,5000,500,0,0,30000] + [272001,10000,1000,0,0,45000] | [0,5000,500,0,0,30000] + [272000,10000,1000,0,0,45000] |
| gpt-5.4 | `openai/gpt-5.4` | 1050000 → 1050000 | [0,2500,250,0,0,15000] + [272001,5000,500,0,0,22500] | [0,2500,250,0,0,15000] + [272000,5000,500,0,0,22500] |
| gpt-5.4-mini | `openai/gpt-5.4-mini` | 400000 → 400000 | [0,750,75,0,0,4500] | [0,750,75,0,0,4500] |
| gpt-5.4-nano | `openai/gpt-5.4-nano` | 400000 → 400000 | [0,200,20,0,0,1250] | [0,200,20,0,0,1250] |
| gpt-5.6-sol | `openai/gpt-5.6-sol` | 1050000 → 1050000 | [0,5000,500,0,0,30000] | [0,4000,400,5000,0,20000] + [272000,8000,800,10000,0,30000] |
| gpt-5.6-terra | `openai/gpt-5.6-terra` | 1050000 → 1050000 | [0,2500,250,0,0,15000] | [0,2000,200,2500,0,12000] + [272000,4000,400,5000,0,18000] |
| gpt-5.6-luna | `openai/gpt-5.6-luna` | 400000 → 1050000 | [0,1000,100,0,0,6000] | [0,200,20,250,0,1200] + [272000,400,40,500,0,1800] |
| gpt-6-astra | `openai/gpt-6-astra` | 1050000 → 1050000 | [0,10000,1000,0,0,50000] + [272001,20000,2000,0,0,75000] | [0,10000,1000,12500,0,50000] + [272000,20000,2000,25000,0,75000] |
| grok-4.5 | `x-ai/grok-4.5` | 500000 → 500000 | [0,2000,300,0,0,6000] + [200001,4000,600,0,0,12000] | [0,2000,300,0,0,6000] + [200000,4000,600,0,0,12000] |
| grok-4.6 | `x-ai/grok-4.6` | 500000 → 500000 | [0,2000,500,0,0,6000] + [200001,4000,1000,0,0,12000] | [0,2000,500,0,0,6000] + [200000,4000,1000,0,0,12000] |
| grok-4.7 | `x-ai/grok-4.7` | 500000 → 500000 | [0,1600,400,0,0,4800] + [200000,3200,800,0,0,9600] | [0,2000,500,0,0,6000] + [200000,4000,1000,0,0,12000] |
| grok-4.3 | `x-ai/grok-4.3` | 256000 → 1000000 | [0,3000,0,0,0,15000] | [0,1250,200,0,0,2500] + [200000,2500,400,0,0,5000] |
| grok-4.20 | `x-ai/grok-4.20` | 2000000 → 2000000 | [0,3000,0,0,0,15000] + [200001,6000,0,0,0,30000] | [0,1250,200,0,0,2500] + [200000,2500,400,0,0,5000] |
| grok-4.20-multi-agent | `x-ai/grok-4.20-multi-agent` | 1000000 → 2000000 | [0,1250,200,0,0,2500] + [200001,2500,400,0,0,5000] | [0,1250,200,0,0,2500] + [200000,2500,400,0,0,5000] |
| deepseek-v4-flash | `deepseek/deepseek-v4-flash` | 128000 → 1048576 | [0,300,30,0,0,1200] | [0,91,18,0,0,182] |
| deepseek-v4.1-flash | `deepseek/deepseek-v4.1-flash` | 1048576 → 1048576 | [0,150,3,0,0,600] | [0,40,2,0,0,144] |
| deepseek-v4-pro | `deepseek/deepseek-v4-pro` | 128000 → 1048576 | [0,600,60,0,0,2400] | [0,209,17,0,0,418] |
| nemotron-3.5-lightning | `nvidia/nemotron-3.5-lightning` | 1000000 → 262144 | [0,80,40,0,0,200] | [0,70,40,0,0,200] |
| qwen3.8-max | `qwen/qwen3.8-max` | absent | | absent from the snapshot |
| qwen3.8-27b | `qwen/qwen3.8-27b` | 262144 → 1000000 | [0,450,0,0,0,3200] | [0,240,50,0,0,2200] |
| kimi-k3 | `moonshotai/kimi-k3` | 256000 → 1048576 | [0,600,60,0,0,2500] | [0,304,210,0,0,11936] |
| kimi-k2.7-code | `moonshotai/kimi-k2.7-code` | 256000 → 262144 | [0,600,60,0,0,2500] | [0,671,180,0,0,3350] |
| kimi-k2.6 | `moonshotai/kimi-k2.6` | 256000 → 262144 | [0,600,60,0,0,2500] | [0,650,150,0,0,3410] |
| glm-5.2 | `z-ai/glm-5.2` | 202752 → 1048576 | [0,1400,260,0,0,4400] | [0,620,140,0,0,2420] |
| glm-5.1 | `z-ai/glm-5.1` | 202752 → 204800 | [0,1400,260,0,0,4400] | [0,966,179,0,0,3036] |
| glm-4.7 | `z-ai/glm-4.7` | 202752 → 204800 | [0,600,110,0,0,2200] | [0,540,99,0,0,1980] |
| glm-4.6 | `z-ai/glm-4.6` | 202752 → 204800 | [0,600,110,0,0,2200] | [0,500,100,0,0,2000] |
| glm-5.3 | `z-ai/glm-5.3` | 1310720 → 1048576 | [0,1400,260,0,0,4400] | [0,79,78,0,0,1400] |
| glm-5.3-flash | `z-ai/glm-5.3-flash` | 1310720 → 1048576 | [0,150,30,0,0,500] | [0,48,6,0,0,200] |
| glm-5.3-flashx | `z-ai/glm-5.3-flashx` | 1048576 → 1048576 | [0,370,75,0,0,1250] | [0,370,90,0,0,1250] |
| qwen3.8-flash | `qwen/qwen3.8-flash` | 1000000 → 1000000 | [0,150,16,0,0,470] | [0,150,16,0,0,470] |
| hunyuan-4-preview | `tencent/hy4-preview` | 1000000 → 1048576 | [0,890,45,0,0,2680] | [0,751,38,0,0,2251] (also time-of-day overrides, see below) |
| minimax-m3 | `minimax/minimax-m3` | 1048576 → 1048576 | [0,300,60,0,0,1200] + [512001,600,120,0,0,2400] | [0,300,60,0,0,1200] |
| nemotron-3-ultra | `nvidia/nemotron-3-ultra-550b-a55b` | 262144 → 262144 | [0,500,100,0,0,2200] | [0,600,120,0,0,2400] |
| muse-spark-1.3 | `meta/muse-spark-1.3` | 1048576 → 1048576 | [0,1250,150,0,0,4250] | [0,1250,150,0,0,4250] |

Notes on the snapshot's pricing that the tiers alone do not show:

- `qwen/qwen3.8-max` is absent. The only Qwen3.8 Max ids are `qwen/qwen3.8-max-0902` (a dated snapshot, `[0,2000,250,0,0,6000]`, context 1000000), `qwen/qwen3.8-max-prime` and `qwen/qwen3.8-2.4t-a95b`. The 0902 description: "Qwen3.8 Max 0902 is an updated snapshot of Qwen3.8 Max from Alibaba's Qwen team. It is a 2.4-trillion-parameter mixture-of-experts model ...".
- `tencent/hy4-preview` has time-of-day pricing: the top-level price is `{"prompt":"0.0000007506","completion":"0.0000022509","input_cache_read":"0.0000000378"}`, with overrides `{"utc_start":0,"utc_end":1600,"prompt":"0.000000834","completion":"0.000002501","input_cache_read":"0.000000042"}` and `{"utc_start":1600,"utc_end":0,...}` equal to the top level. The table gives the top level.
- `openai/gpt-5.5-pro` has no `input_cache_read` key, and now has a tier: `"overrides":[{"min_prompt_tokens":272000,"prompt":"0.00006","completion":"0.00027"}]`. The catalog carries cache read 30000, the uncached price.
- `minimax/minimax-m3` no longer lists a long-context override. The catalog's `512_001` tier has nothing behind it in the snapshot.
- `google/*` entries list `input_cache_write` (83, 42, 375 or 188 nano-USD), but no `input_cache_write_1h`. `openai/gpt-5.6-*` and `openai/gpt-6-*` list `input_cache_write` too. The catalog carries zero writes on every Gemini and OpenAI row.
- `google/gemini-2.5-flash` and `google/gemini-2.5-pro` carry `"expiration_date":"2026-10-20"`. No other entry this note names has an expiration date, including the Gemini entries at introductory prices.

#### The offerings the new rows need

| Proposed catalog model | OpenRouter id | context | tiers (snapshot) | `reasoning` object | `supported_parameters` has `reasoning_effort` |
|---|---|---|---|---|---|
| qwen3.8-max-prime | `qwen/qwen3.8-max-prime` | 1000000 | [0,4000,500,0,0,12000] | `{"mandatory":true,"default_enabled":true,"supported_efforts":["xhigh","high","medium","low","minimal"],"default_effort":"xhigh"}` | yes |
| qwen3.8-2.4t-a95b | `qwen/qwen3.8-2.4t-a95b` | 1048576 | [0,2000,250,0,0,6000] | `{"mandatory":true,"default_enabled":true,"supported_efforts":["xhigh","medium","low"],"default_effort":"xhigh"}` | yes |
| glm-5.3-prime | `z-ai/glm-5.3-prime` | 1000000 | [0,2800,560,0,0,8800] | `{"mandatory":true,"default_enabled":true,"supported_efforts":["max","high","low"],"default_effort":"max"}` | yes |
| nemotron-3.5-super-vl | `nvidia/nemotron-3.5-super-vl` | 1048576 | [0,100,80,0,0,400] | `{"mandatory":false}` | no |
| mimo-v2.6-pro | `xiaomi/mimo-v2.6-pro` | 1050000 | [0,435,4,0,0,870] | `{"mandatory":false}` | no |
| mimo-v2.6-flash | `xiaomi/mimo-v2.6-flash` | 1050000 | [0,140,3,0,0,280] | `{"mandatory":false}` | no |
| gpt-6-luna | `openai/gpt-6-luna` | 1050000 | [0,100,10,125,0,500] + [272000,200,20,250,0,750] | `{"mandatory":false,"default_enabled":true,"supported_efforts":["max","xhigh","high","medium","low","none"],"default_effort":"medium"}` | yes |
| gpt-6-sol | `openai/gpt-6-sol` | 1050000 | [0,2000,200,2500,0,10000] + [272000,4000,400,5000,0,15000] | `{"mandatory":false,"default_enabled":true,"supported_efforts":["max","xhigh","high","medium","low","none"],"default_effort":"medium"}` | yes |
| gpt-6.1-sol | `openai/gpt-6.1-sol` | 1050000 | [0,2000,100,2500,0,10000] + [272000,4000,200,5000,0,15000] | `{"mandatory":true,"default_enabled":true,"supported_efforts":["max","xhigh","high","medium","low"],"default_effort":"medium"}` | yes |
| claude-opus-5-5 | `anthropic/claude-opus-5.5` | 1000000 | [0,4000,200,5000,8000,20000] | `{"mandatory":true,"supported_efforts":["max","xhigh","high","medium","low"],"default_effort":"high"}` | yes |
| claude-sonnet-5-5 | `anthropic/claude-sonnet-5.5` | 1000000 | [0,2000,100,2500,4000,10000] | `{"mandatory":true,"supported_efforts":["max","xhigh","high","medium","low"],"default_effort":"high"}` | yes |
| claude-haiku-5-5 | `anthropic/claude-haiku-5.5` | 1000000 | [0,100,10,125,200,500] + [100000,500,50,625,1000,2500] | `{"mandatory":false,"supported_efforts":["max","xhigh","high","medium","low","none"],"default_effort":"medium"}` | yes |
| gemini-4-argon | `google/gemini-4-argon` | 1048576 | [0,2000,100,188,0,10000] | `{"mandatory":true,"supported_efforts":["high","medium","low"],"default_effort":"medium"}` | yes |
| gemini-3.6-flash | `google/gemini-3.6-flash` | 1048576 | [0,750,75,42,0,3750] | `{"mandatory":true,"default_enabled":true,"supported_efforts":["high","medium","low","minimal"],"default_effort":"medium"}` | yes |
| grok-build-0.1 | `x-ai/grok-build-0.1` | 256000 | [0,1000,200,0,0,2000] + [200000,2000,400,0,0,4000] | `{"mandatory":true}` | no |

Every id the decisions name exists under exactly that string. The snapshot also lists `xiaomi/mimo-v2.6-pro-ultraspeed`, `xiaomi/mimo-v2.5-pro`, `xiaomi/mimo-v2.5`, `nvidia/nemotron-3.5-super-vl:free`, and `:batch` and `-pro` variants of the OpenAI and Anthropic ids. This note probes none of them.

### Reasoning probes on the new offerings

`POST https://openrouter.ai/api/v1/chat/completions` with `{"model":…,"messages":[{"role":"user","content":"Reply with the single word OK."}],"max_tokens":16}`, and `POST https://openrouter.ai/api/v1/responses` with `{"model":…,"input":"Reply with the single word OK.","max_output_tokens":16}`. The probes below add a `reasoning` object to that body, or omit it for the default. An offering whose `supported_parameters` lists `reasoning_effort` got one request with no `reasoning`, one at each `supported_efforts` value, and `{"effort":"none"}` where that value is listed, else `{"enabled":false}`. A toggle-only offering got the default, `{"enabled":true}` and `{"enabled":false}`. Every request ran once on each wire.

#### What OpenRouter checks

These three Chat Completions probes show what a 200 can prove:

| Model | `reasoning` sent | status | `usage` or error body |
|---|---|---|---|
| `qwen/qwen3.8-2.4t-a95b` | `{"effort":"high"}` | 200 | `{"prompt_tokens":30,"completion_tokens":21,"total_tokens":51,"cost":0.00018414,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_prompt_cost":0.00006,"upstream_inference_completions_cost":0.000126},"completion_tokens_details":{"reasoning_tokens":18,"image_tokens":0,"audio_tokens":0}}` |
| `z-ai/glm-5.3-prime` | `{"effort":"medium"}` | 200 | `{"prompt_tokens":19,"completion_tokens":2,"total_tokens":21,"cost":0.000070092,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000708,"upstream_inference_prompt_cost":0.0000532,"upstream_inference_completions_cost":0.0000176},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| `qwen/qwen3.8-2.4t-a95b` | `{"effort":"bogus"}` | 400 | `{"error":{"message":"reasoning.effort: Invalid option: expected one of \"max\"\|\"xhigh\"\|\"high\"\|\"medium\"\|\"low\"\|\"minimal\"\|\"none\"","code":400},"user_id":"user_3GeRaAkUjjopxLgeH7c2NZ2GmaA"}` |

`high` is not in `qwen/qwen3.8-2.4t-a95b`'s `supported_efforts`, and `medium` is not in `z-ai/glm-5.3-prime`'s, yet both return 200. A value outside the shared vocabulary is a 400 that lists the vocabulary. So OpenRouter validates `effort` against its own vocabulary, not the model's, and a 200 at a level proves only that the request is not refused. Which levels a model actually has comes from `supported_efforts`. Refusing a disable is different: `{"enabled":false}`, or `{"effort":"none"}` on a mandatory-reasoning model, returns the 400 below.

#### `qwen/qwen3.8-max-prime`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `qwen/qwen3.8-max-prime` | `{"prompt_tokens":68,"completion_tokens":16,"total_tokens":84,"cost":0.00045936,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000464,"upstream_inference_prompt_cost":0.000272,"upstream_inference_completions_cost":0.000192},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `qwen/qwen3.8-max-prime` | `{"input_tokens":68,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":84,"cost":0.00045936,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000464,"upstream_inference_input_cost":0.000272,"upstream_inference_output_cost":0.000192}}` |
| xhigh | chat | `{"effort":"xhigh"}` | 200 | `qwen/qwen3.8-max-prime` | `{"prompt_tokens":68,"completion_tokens":16,"total_tokens":84,"cost":0.00045936,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000464,"upstream_inference_prompt_cost":0.000272,"upstream_inference_completions_cost":0.000192},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| xhigh | responses | `{"effort":"xhigh"}` | 200 | `qwen/qwen3.8-max-prime` | `{"input_tokens":68,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":84,"cost":0.00045936,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000464,"upstream_inference_input_cost":0.000272,"upstream_inference_output_cost":0.000192}}` |
| high | chat | `{"effort":"high"}` | 200 | `qwen/qwen3.8-max-prime` | `{"prompt_tokens":68,"completion_tokens":16,"total_tokens":84,"cost":0.00045936,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000464,"upstream_inference_prompt_cost":0.000272,"upstream_inference_completions_cost":0.000192},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `qwen/qwen3.8-max-prime` | `{"input_tokens":68,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":84,"cost":0.00045936,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000464,"upstream_inference_input_cost":0.000272,"upstream_inference_output_cost":0.000192}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `qwen/qwen3.8-max-prime` | `{"prompt_tokens":30,"completion_tokens":16,"total_tokens":46,"cost":0.00030888,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000312,"upstream_inference_prompt_cost":0.00012,"upstream_inference_completions_cost":0.000192},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `qwen/qwen3.8-max-prime` | `{"input_tokens":30,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":46,"cost":0.00030888,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000312,"upstream_inference_input_cost":0.00012,"upstream_inference_output_cost":0.000192}}` |
| low | chat | `{"effort":"low"}` | 200 | `qwen/qwen3.8-max-prime` | `{"prompt_tokens":56,"completion_tokens":16,"total_tokens":72,"cost":0.00041184,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000416,"upstream_inference_prompt_cost":0.000224,"upstream_inference_completions_cost":0.000192},"completion_tokens_details":{"reasoning_tokens":14,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `qwen/qwen3.8-max-prime` | `{"input_tokens":56,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":14},"total_tokens":72,"cost":0.00041184,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000416,"upstream_inference_input_cost":0.000224,"upstream_inference_output_cost":0.000192}}` |
| minimal | chat | `{"effort":"minimal"}` | 200 | `qwen/qwen3.8-max-prime` | `{"prompt_tokens":56,"completion_tokens":16,"total_tokens":72,"cost":0.00041184,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000416,"upstream_inference_prompt_cost":0.000224,"upstream_inference_completions_cost":0.000192},"completion_tokens_details":{"reasoning_tokens":14,"image_tokens":0,"audio_tokens":0}}` |
| minimal | responses | `{"effort":"minimal"}` | 200 | `qwen/qwen3.8-max-prime` | `{"input_tokens":56,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":72,"cost":0.00041184,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000416,"upstream_inference_input_cost":0.000224,"upstream_inference_output_cost":0.000192}}` |
| disabled | chat | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| disabled | responses | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |

#### `qwen/qwen3.8-2.4t-a95b`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `qwen/qwen3.8-2.4t-a95b` | `{"prompt_tokens":68,"completion_tokens":23,"total_tokens":91,"cost":0.00027126,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000274,"upstream_inference_prompt_cost":0.000136,"upstream_inference_completions_cost":0.000138},"completion_tokens_details":{"reasoning_tokens":19,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `qwen/qwen3.8-2.4t-a95b` | `{"input_tokens":68,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":28,"output_tokens_details":{"reasoning_tokens":24},"total_tokens":96,"cost":0.00030096,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000304,"upstream_inference_input_cost":0.000136,"upstream_inference_output_cost":0.000168}}` |
| xhigh | chat | `{"effort":"xhigh"}` | 200 | `qwen/qwen3.8-2.4t-a95b` | `{"prompt_tokens":68,"completion_tokens":26,"total_tokens":94,"cost":0.00028908,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000292,"upstream_inference_prompt_cost":0.000136,"upstream_inference_completions_cost":0.000156},"completion_tokens_details":{"reasoning_tokens":22,"image_tokens":0,"audio_tokens":0}}` |
| xhigh | responses | `{"effort":"xhigh"}` | 200 | `qwen/qwen3.8-2.4t-a95b` | `{"input_tokens":68,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":24,"output_tokens_details":{"reasoning_tokens":20},"total_tokens":92,"cost":0.0002772,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00028,"upstream_inference_input_cost":0.000136,"upstream_inference_output_cost":0.000144}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `qwen/qwen3.8-2.4t-a95b` | `{"prompt_tokens":30,"completion_tokens":20,"total_tokens":50,"cost":0.0001782,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00018,"upstream_inference_prompt_cost":0.00006,"upstream_inference_completions_cost":0.00012},"completion_tokens_details":{"reasoning_tokens":17,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `qwen/qwen3.8-2.4t-a95b` | `{"input_tokens":30,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":21,"output_tokens_details":{"reasoning_tokens":18},"total_tokens":51,"cost":0.00018414,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_input_cost":0.00006,"upstream_inference_output_cost":0.000126}}` |
| low | chat | `{"effort":"low"}` | 200 | `qwen/qwen3.8-2.4t-a95b` | `{"prompt_tokens":56,"completion_tokens":19,"total_tokens":75,"cost":0.00022374,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000226,"upstream_inference_prompt_cost":0.000112,"upstream_inference_completions_cost":0.000114},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `qwen/qwen3.8-2.4t-a95b` | `{"input_tokens":56,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":17,"output_tokens_details":{"reasoning_tokens":14},"total_tokens":73,"cost":0.00021186,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000214,"upstream_inference_input_cost":0.000112,"upstream_inference_output_cost":0.000102}}` |
| disabled | chat | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| disabled | responses | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |

Both wires ignore `max_tokens`/`max_output_tokens` 16 on this model (completions of 17 to 28 tokens).

#### `z-ai/glm-5.3-prime`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `z-ai/glm-5.3-prime` | `{"prompt_tokens":19,"completion_tokens":16,"total_tokens":35,"cost":0.00019206,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000194,"upstream_inference_prompt_cost":0.0000532,"upstream_inference_completions_cost":0.0001408},"completion_tokens_details":{"reasoning_tokens":14,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `z-ai/glm-5.3-prime` | `{"input_tokens":19,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":14},"total_tokens":35,"cost":0.00019206,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000194,"upstream_inference_input_cost":0.0000532,"upstream_inference_output_cost":0.0001408}}` |
| max | chat | `{"effort":"max"}` | 200 | `z-ai/glm-5.3-prime` | `{"prompt_tokens":19,"completion_tokens":2,"total_tokens":21,"cost":0.000070092,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000708,"upstream_inference_prompt_cost":0.0000532,"upstream_inference_completions_cost":0.0000176},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| max | responses | `{"effort":"max"}` | 200 | `z-ai/glm-5.3-prime` | `{"input_tokens":19,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":21,"cost":0.000070092,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000708,"upstream_inference_input_cost":0.0000532,"upstream_inference_output_cost":0.0000176}}` |
| high | chat | `{"effort":"high"}` | 200 | `z-ai/glm-5.3-prime` | `{"prompt_tokens":19,"completion_tokens":15,"total_tokens":34,"cost":0.000183348,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0001852,"upstream_inference_prompt_cost":0.0000532,"upstream_inference_completions_cost":0.000132},"completion_tokens_details":{"reasoning_tokens":13,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `z-ai/glm-5.3-prime` | `{"input_tokens":19,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":21,"cost":0.000070092,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000708,"upstream_inference_input_cost":0.0000532,"upstream_inference_output_cost":0.0000176}}` |
| low | chat | `{"effort":"low"}` | 200 | `z-ai/glm-5.3-prime` | `{"prompt_tokens":19,"completion_tokens":15,"total_tokens":34,"cost":0.000183348,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0001852,"upstream_inference_prompt_cost":0.0000532,"upstream_inference_completions_cost":0.000132},"completion_tokens_details":{"reasoning_tokens":13,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `z-ai/glm-5.3-prime` | `{"input_tokens":19,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":21,"cost":0.000070092,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000708,"upstream_inference_input_cost":0.0000532,"upstream_inference_output_cost":0.0000176}}` |
| disabled | chat | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| disabled | responses | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |

#### `nvidia/nemotron-3.5-super-vl`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `nvidia/nemotron-3.5-super-vl` | `{"prompt_tokens":23,"completion_tokens":16,"total_tokens":39,"cost":0.0000113355,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00001145,"upstream_inference_prompt_cost":0.00000345,"upstream_inference_completions_cost":0.000008},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `nvidia/nemotron-3.5-super-vl` | `{"input_tokens":23,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":39,"cost":0.000008613,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000087,"upstream_inference_input_cost":0.0000023,"upstream_inference_output_cost":0.0000064}}` |
| enabled | chat | `{"enabled":true}` | 200 | `nvidia/nemotron-3.5-super-vl` | `{"prompt_tokens":23,"completion_tokens":16,"total_tokens":39,"cost":0.0000113355,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00001145,"upstream_inference_prompt_cost":0.00000345,"upstream_inference_completions_cost":0.000008},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| enabled | responses | `{"enabled":true}` | 200 | `nvidia/nemotron-3.5-super-vl` | `{"input_tokens":23,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":39,"cost":0.0000113355,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00001145,"upstream_inference_input_cost":0.00000345,"upstream_inference_output_cost":0.000008}}` |
| disabled | chat | `{"enabled":false}` | 200 | `nvidia/nemotron-3.5-super-vl` | `{"prompt_tokens":23,"completion_tokens":2,"total_tokens":25,"cost":0.0000044055,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00000445,"upstream_inference_prompt_cost":0.00000345,"upstream_inference_completions_cost":0.000001},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| disabled | responses | `{"enabled":false}` | 200 | `nvidia/nemotron-3.5-super-vl` | `{"input_tokens":23,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":25,"cost":0.000003069,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000031,"upstream_inference_input_cost":0.0000023,"upstream_inference_output_cost":8e-7}}` |

#### `xiaomi/mimo-v2.6-pro`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `xiaomi/mimo-v2.6-pro` | `{"prompt_tokens":14,"completion_tokens":16,"total_tokens":30,"cost":0.0000198099,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00002001,"upstream_inference_prompt_cost":0.00000609,"upstream_inference_completions_cost":0.00001392},"completion_tokens_details":{"reasoning_tokens":14,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `xiaomi/mimo-v2.6-pro` | `{"input_tokens":14,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":14},"total_tokens":30,"cost":0.0000198099,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00002001,"upstream_inference_input_cost":0.00000609,"upstream_inference_output_cost":0.00001392}}` |
| enabled | chat | `{"enabled":true}` | 200 | `xiaomi/mimo-v2.6-pro` | `{"prompt_tokens":14,"completion_tokens":16,"total_tokens":30,"cost":0.0000198099,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00002001,"upstream_inference_prompt_cost":0.00000609,"upstream_inference_completions_cost":0.00001392},"completion_tokens_details":{"reasoning_tokens":14,"image_tokens":0,"audio_tokens":0}}` |
| enabled | responses | `{"enabled":true}` | 200 | `xiaomi/mimo-v2.6-pro` | `{"input_tokens":14,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":14},"total_tokens":30,"cost":0.0000198099,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00002001,"upstream_inference_input_cost":0.00000609,"upstream_inference_output_cost":0.00001392}}` |
| disabled | chat | `{"enabled":false}` | 200 | `xiaomi/mimo-v2.6-pro` | `{"prompt_tokens":16,"completion_tokens":2,"total_tokens":18,"cost":0.000008613,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000087,"upstream_inference_prompt_cost":0.00000696,"upstream_inference_completions_cost":0.00000174},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| disabled | responses | `{"enabled":false}` | 200 | `xiaomi/mimo-v2.6-pro` | `{"input_tokens":16,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.000008613,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000087,"upstream_inference_input_cost":0.00000696,"upstream_inference_output_cost":0.00000174}}` |

#### `xiaomi/mimo-v2.6-flash`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `xiaomi/mimo-v2.6-flash` | `{"prompt_tokens":14,"completion_tokens":16,"total_tokens":30,"cost":0.0000079695,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00000805,"upstream_inference_prompt_cost":0.00000245,"upstream_inference_completions_cost":0.0000056},"completion_tokens_details":{"reasoning_tokens":13,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `xiaomi/mimo-v2.6-flash` | `{"input_tokens":14,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":15},"total_tokens":30,"cost":0.0000079695,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00000805,"upstream_inference_input_cost":0.00000245,"upstream_inference_output_cost":0.0000056}}` |
| enabled | chat | `{"enabled":true}` | 200 | `xiaomi/mimo-v2.6-flash` | `{"prompt_tokens":14,"completion_tokens":16,"total_tokens":30,"cost":0.0000079695,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00000805,"upstream_inference_prompt_cost":0.00000245,"upstream_inference_completions_cost":0.0000056},"completion_tokens_details":{"reasoning_tokens":15,"image_tokens":0,"audio_tokens":0}}` |
| enabled | responses | `{"enabled":true}` | 200 | `xiaomi/mimo-v2.6-flash` | `{"input_tokens":14,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":13},"total_tokens":30,"cost":0.0000079695,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00000805,"upstream_inference_input_cost":0.00000245,"upstream_inference_output_cost":0.0000056}}` |
| disabled | chat | `{"enabled":false}` | 200 | `xiaomi/mimo-v2.6-flash` | `{"prompt_tokens":16,"completion_tokens":2,"total_tokens":18,"cost":0.000003465,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000035,"upstream_inference_prompt_cost":0.0000028,"upstream_inference_completions_cost":7e-7},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| disabled | responses | `{"enabled":false}` | 200 | `xiaomi/mimo-v2.6-flash` | `{"input_tokens":16,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.000003465,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000035,"upstream_inference_input_cost":0.0000028,"upstream_inference_output_cost":7e-7}}` |

#### `openai/gpt-6-luna`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `openai/gpt-6-luna` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000013,"upstream_inference_completions_cost":0.0000025},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `openai/gpt-6-luna` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000013,"upstream_inference_output_cost":0.0000025}}` |
| none | chat | `{"effort":"none"}` | 200 | `openai/gpt-6-luna` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000013,"upstream_inference_completions_cost":0.0000025},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| none | responses | `{"effort":"none"}` | 200 | `openai/gpt-6-luna` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000013,"upstream_inference_output_cost":0.0000025}}` |
| low | chat | `{"effort":"low"}` | 200 | `openai/gpt-6-luna` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000013,"upstream_inference_completions_cost":0.0000025},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `openai/gpt-6-luna` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000013,"upstream_inference_output_cost":0.0000025}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `openai/gpt-6-luna` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.000009207,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000093,"upstream_inference_prompt_cost":0.0000013,"upstream_inference_completions_cost":0.000008},"completion_tokens_details":{"reasoning_tokens":9,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `openai/gpt-6-luna` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":11},"total_tokens":29,"cost":0.000009207,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000093,"upstream_inference_input_cost":0.0000013,"upstream_inference_output_cost":0.000008}}` |
| high | chat | `{"effort":"high"}` | 200 | `openai/gpt-6-luna` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.000009207,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000093,"upstream_inference_prompt_cost":0.0000013,"upstream_inference_completions_cost":0.000008},"completion_tokens_details":{"reasoning_tokens":11,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `openai/gpt-6-luna` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":10},"total_tokens":29,"cost":0.000009207,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000093,"upstream_inference_input_cost":0.0000013,"upstream_inference_output_cost":0.000008}}` |
| xhigh | chat | `{"effort":"xhigh"}` | 200 | `openai/gpt-6-luna` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.000009207,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000093,"upstream_inference_prompt_cost":0.0000013,"upstream_inference_completions_cost":0.000008},"completion_tokens_details":{"reasoning_tokens":10,"image_tokens":0,"audio_tokens":0}}` |
| xhigh | responses | `{"effort":"xhigh"}` | 200 | `openai/gpt-6-luna` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":10},"total_tokens":29,"cost":0.000009207,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000093,"upstream_inference_input_cost":0.0000013,"upstream_inference_output_cost":0.000008}}` |
| max | chat | `{"effort":"max"}` | 200 | `openai/gpt-6-luna` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000013,"upstream_inference_completions_cost":0.0000025},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| max | responses | `{"effort":"max"}` | 200 | `openai/gpt-6-luna` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000013,"upstream_inference_output_cost":0.0000025}}` |

#### `openai/gpt-6-sol`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `openai/gpt-6-sol` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.00018414,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00016},"completion_tokens_details":{"reasoning_tokens":10,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `openai/gpt-6-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00005}}` |
| none | chat | `{"effort":"none"}` | 200 | `openai/gpt-6-sol` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| none | responses | `{"effort":"none"}` | 200 | `openai/gpt-6-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00005}}` |
| low | chat | `{"effort":"low"}` | 200 | `openai/gpt-6-sol` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `openai/gpt-6-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00005}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `openai/gpt-6-sol` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.00018414,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00016},"completion_tokens_details":{"reasoning_tokens":11,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `openai/gpt-6-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00005}}` |
| high | chat | `{"effort":"high"}` | 200 | `openai/gpt-6-sol` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.00018414,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00016},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `openai/gpt-6-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":29,"cost":0.00018414,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00016}}` |
| xhigh | chat | `{"effort":"xhigh"}` | 200 | `openai/gpt-6-sol` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.00018414,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00016},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| xhigh | responses | `{"effort":"xhigh"}` | 200 | `openai/gpt-6-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":9},"total_tokens":29,"cost":0.00018414,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00016}}` |
| max | chat | `{"effort":"max"}` | 200 | `openai/gpt-6-sol` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.00018414,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00016},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| max | responses | `{"effort":"max"}` | 200 | `openai/gpt-6-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":29,"cost":0.00018414,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00016}}` |

#### `openai/gpt-6.1-sol`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `openai/gpt-6.1-sol` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `openai/gpt-6.1-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00005}}` |
| none | chat | `{"effort":"none"}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| none | responses | `{"effort":"none"}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| low | chat | `{"effort":"low"}` | 200 | `openai/gpt-6.1-sol` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `openai/gpt-6.1-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00005}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `openai/gpt-6.1-sol` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `openai/gpt-6.1-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00005}}` |
| high | chat | `{"effort":"high"}` | 200 | `openai/gpt-6.1-sol` | `{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `openai/gpt-6.1-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":10},"total_tokens":29,"cost":0.00018414,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00016}}` |
| xhigh | chat | `{"effort":"xhigh"}` | 200 | `openai/gpt-6.1-sol` | `{"prompt_tokens":13,"completion_tokens":16,"total_tokens":29,"cost":0.00018414,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_prompt_cost":0.000026,"upstream_inference_completions_cost":0.00016},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| xhigh | responses | `{"effort":"xhigh"}` | 200 | `openai/gpt-6.1-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":29,"cost":0.00018414,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00016}}` |
| max | chat | `{"effort":"max"}` | 200 | `openai/gpt-6.1-sol` | `{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"cost":0,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0,"upstream_inference_prompt_cost":0,"upstream_inference_completions_cost":0},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| max | responses | `{"effort":"max"}` | 200 | `openai/gpt-6.1-sol` | `{"input_tokens":13,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":29,"cost":0.00018414,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000186,"upstream_inference_input_cost":0.000026,"upstream_inference_output_cost":0.00016}}` |

#### `anthropic/claude-opus-5.5`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `anthropic/claude-opus-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00015048,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_prompt_cost":0.000072,"upstream_inference_completions_cost":0.00008},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `anthropic/claude-opus-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00015048,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_input_cost":0.000072,"upstream_inference_output_cost":0.00008}}` |
| low | chat | `{"effort":"low"}` | 200 | `anthropic/claude-opus-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00015048,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_prompt_cost":0.000072,"upstream_inference_completions_cost":0.00008},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `anthropic/claude-opus-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00015048,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_input_cost":0.000072,"upstream_inference_output_cost":0.00008}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `anthropic/claude-opus-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00015048,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_prompt_cost":0.000072,"upstream_inference_completions_cost":0.00008},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `anthropic/claude-opus-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00015048,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_input_cost":0.000072,"upstream_inference_output_cost":0.00008}}` |
| high | chat | `{"effort":"high"}` | 200 | `anthropic/claude-opus-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00015048,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_prompt_cost":0.000072,"upstream_inference_completions_cost":0.00008},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `anthropic/claude-opus-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00015048,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_input_cost":0.000072,"upstream_inference_output_cost":0.00008}}` |
| xhigh | chat | `{"effort":"xhigh"}` | 200 | `anthropic/claude-opus-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00015048,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_prompt_cost":0.000072,"upstream_inference_completions_cost":0.00008},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| xhigh | responses | `{"effort":"xhigh"}` | 200 | `anthropic/claude-opus-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00015048,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000152,"upstream_inference_input_cost":0.000072,"upstream_inference_output_cost":0.00008}}` |
| max | chat | `{"effort":"max"}` | 200 | `anthropic/claude-opus-5.5` | `{"prompt_tokens":18,"completion_tokens":16,"total_tokens":34,"cost":0.00038808,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000392,"upstream_inference_prompt_cost":0.000072,"upstream_inference_completions_cost":0.00032},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| max | responses | `{"effort":"max"}` | 200 | `anthropic/claude-opus-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":34,"cost":0.00038808,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000392,"upstream_inference_input_cost":0.000072,"upstream_inference_output_cost":0.00032}}` |
| disabled | chat | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| disabled | responses | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |

#### `anthropic/claude-sonnet-5.5`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `anthropic/claude-sonnet-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000036,"upstream_inference_completions_cost":0.00004},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `anthropic/claude-sonnet-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000036,"upstream_inference_output_cost":0.00004}}` |
| low | chat | `{"effort":"low"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000036,"upstream_inference_completions_cost":0.00004},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000036,"upstream_inference_output_cost":0.00004}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000036,"upstream_inference_completions_cost":0.00004},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000036,"upstream_inference_output_cost":0.00004}}` |
| high | chat | `{"effort":"high"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000036,"upstream_inference_completions_cost":0.00004},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000036,"upstream_inference_output_cost":0.00004}}` |
| xhigh | chat | `{"effort":"xhigh"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.00007524,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_prompt_cost":0.000036,"upstream_inference_completions_cost":0.00004},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| xhigh | responses | `{"effort":"xhigh"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.00007524,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000076,"upstream_inference_input_cost":0.000036,"upstream_inference_output_cost":0.00004}}` |
| max | chat | `{"effort":"max"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"prompt_tokens":18,"completion_tokens":16,"total_tokens":34,"cost":0.00019404,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000196,"upstream_inference_prompt_cost":0.000036,"upstream_inference_completions_cost":0.00016},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| max | responses | `{"effort":"max"}` | 200 | `anthropic/claude-sonnet-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":34,"cost":0.00019404,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000196,"upstream_inference_input_cost":0.000036,"upstream_inference_output_cost":0.00016}}` |
| disabled | chat | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| disabled | responses | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |

#### `anthropic/claude-haiku-5.5`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `anthropic/claude-haiku-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000018,"upstream_inference_completions_cost":0.000002},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `anthropic/claude-haiku-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000018,"upstream_inference_output_cost":0.000002}}` |
| none | chat | `{"effort":"none"}` | 200 | `anthropic/claude-haiku-5.5` | `{"prompt_tokens":16,"completion_tokens":4,"total_tokens":20,"cost":0.000003564,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000036,"upstream_inference_prompt_cost":0.0000016,"upstream_inference_completions_cost":0.000002},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| none | responses | `{"effort":"none"}` | 200 | `anthropic/claude-haiku-5.5` | `{"input_tokens":16,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":20,"cost":0.000003564,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000036,"upstream_inference_input_cost":0.0000016,"upstream_inference_output_cost":0.000002}}` |
| low | chat | `{"effort":"low"}` | 200 | `anthropic/claude-haiku-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000018,"upstream_inference_completions_cost":0.000002},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `anthropic/claude-haiku-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000018,"upstream_inference_output_cost":0.000002}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `anthropic/claude-haiku-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000018,"upstream_inference_completions_cost":0.000002},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `anthropic/claude-haiku-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000018,"upstream_inference_output_cost":0.000002}}` |
| high | chat | `{"effort":"high"}` | 200 | `anthropic/claude-haiku-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000018,"upstream_inference_completions_cost":0.000002},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `anthropic/claude-haiku-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000018,"upstream_inference_output_cost":0.000002}}` |
| xhigh | chat | `{"effort":"xhigh"}` | 200 | `anthropic/claude-haiku-5.5` | `{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"cost":0.000003762,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_prompt_cost":0.0000018,"upstream_inference_completions_cost":0.000002},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| xhigh | responses | `{"effort":"xhigh"}` | 200 | `anthropic/claude-haiku-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":22,"cost":0.000003762,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000038,"upstream_inference_input_cost":0.0000018,"upstream_inference_output_cost":0.000002}}` |
| max | chat | `{"effort":"max"}` | 200 | `anthropic/claude-haiku-5.5` | `{"prompt_tokens":18,"completion_tokens":16,"total_tokens":34,"cost":0.000009702,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0000098,"upstream_inference_prompt_cost":0.0000018,"upstream_inference_completions_cost":0.000008},"completion_tokens_details":{"reasoning_tokens":15,"image_tokens":0,"audio_tokens":0}}` |
| max | responses | `{"effort":"max"}` | 200 | `anthropic/claude-haiku-5.5` | `{"input_tokens":18,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":15},"total_tokens":34,"cost":0.000009702,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0000098,"upstream_inference_input_cost":0.0000018,"upstream_inference_output_cost":0.000008}}` |

#### `google/gemini-4-argon`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `google/gemini-4-argon` | `{"prompt_tokens":38,"completion_tokens":5,"total_tokens":43,"cost":0.00012474,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000126,"upstream_inference_prompt_cost":0.000076,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `google/gemini-4-argon` | `{"input_tokens":38,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":43,"cost":0.00012474,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000126,"upstream_inference_input_cost":0.000076,"upstream_inference_output_cost":0.00005}}` |
| low | chat | `{"effort":"low"}` | 200 | `google/gemini-4-argon` | `{"prompt_tokens":38,"completion_tokens":5,"total_tokens":43,"cost":0.00012474,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000126,"upstream_inference_prompt_cost":0.000076,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `google/gemini-4-argon` | `{"input_tokens":38,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":43,"cost":0.00012474,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000126,"upstream_inference_input_cost":0.000076,"upstream_inference_output_cost":0.00005}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `google/gemini-4-argon` | `{"prompt_tokens":38,"completion_tokens":5,"total_tokens":43,"cost":0.00012474,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000126,"upstream_inference_prompt_cost":0.000076,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `google/gemini-4-argon` | `{"input_tokens":38,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":13},"total_tokens":54,"cost":0.00023364,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000236,"upstream_inference_input_cost":0.000076,"upstream_inference_output_cost":0.00016}}` |
| high | chat | `{"effort":"high"}` | 200 | `google/gemini-4-argon` | `{"prompt_tokens":38,"completion_tokens":16,"total_tokens":54,"cost":0.00023364,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000236,"upstream_inference_prompt_cost":0.000076,"upstream_inference_completions_cost":0.00016},"completion_tokens_details":{"reasoning_tokens":13,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `google/gemini-4-argon` | `{"input_tokens":38,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":13},"total_tokens":54,"cost":0.00023364,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000236,"upstream_inference_input_cost":0.000076,"upstream_inference_output_cost":0.00016}}` |
| minimal | chat | `{"effort":"minimal"}` | 200 | `google/gemini-4-argon` | `{"prompt_tokens":38,"completion_tokens":5,"total_tokens":43,"cost":0.00012474,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000126,"upstream_inference_prompt_cost":0.000076,"upstream_inference_completions_cost":0.00005},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| minimal | responses | `{"effort":"minimal"}` | 200 | `google/gemini-4-argon` | `{"input_tokens":38,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":43,"cost":0.00012474,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000126,"upstream_inference_input_cost":0.000076,"upstream_inference_output_cost":0.00005}}` |
| disabled | chat | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| disabled | responses | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |

`minimal` is not in this offering's `supported_efforts` (`["high","medium","low"]`); it was sent to see whether the host refuses it, and it returned 200.

#### `google/gemini-3.6-flash`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `google/gemini-3.6-flash` | `{"prompt_tokens":7,"completion_tokens":12,"total_tokens":19,"cost":0.0000497475,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00005025,"upstream_inference_prompt_cost":0.00000525,"upstream_inference_completions_cost":0.000045},"completion_tokens_details":{"reasoning_tokens":12,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `google/gemini-3.6-flash` | `{"input_tokens":7,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":12,"output_tokens_details":{"reasoning_tokens":11},"total_tokens":19,"cost":0.0000497475,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00005025,"upstream_inference_input_cost":0.00000525,"upstream_inference_output_cost":0.000045}}` |
| minimal | chat | `{"effort":"minimal"}` | 200 | `google/gemini-3.6-flash` | `{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8,"cost":0.00000891,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.000009,"upstream_inference_prompt_cost":0.00000525,"upstream_inference_completions_cost":0.00000375},"completion_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}` |
| minimal | responses | `{"effort":"minimal"}` | 200 | `google/gemini-3.6-flash` | `{"input_tokens":7,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":8,"cost":0.00000891,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000009,"upstream_inference_input_cost":0.00000525,"upstream_inference_output_cost":0.00000375}}` |
| low | chat | `{"effort":"low"}` | 200 | `google/gemini-3.6-flash` | `{"prompt_tokens":7,"completion_tokens":12,"total_tokens":19,"cost":0.0000497475,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00005025,"upstream_inference_prompt_cost":0.00000525,"upstream_inference_completions_cost":0.000045},"completion_tokens_details":{"reasoning_tokens":12,"image_tokens":0,"audio_tokens":0}}` |
| low | responses | `{"effort":"low"}` | 200 | `google/gemini-3.6-flash` | `{"input_tokens":7,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":13,"output_tokens_details":{"reasoning_tokens":13},"total_tokens":20,"cost":0.00005346,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000054,"upstream_inference_input_cost":0.00000525,"upstream_inference_output_cost":0.00004875}}` |
| medium | chat | `{"effort":"medium"}` | 200 | `google/gemini-3.6-flash` | `{"prompt_tokens":7,"completion_tokens":12,"total_tokens":19,"cost":0.0000497475,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00005025,"upstream_inference_prompt_cost":0.00000525,"upstream_inference_completions_cost":0.000045},"completion_tokens_details":{"reasoning_tokens":12,"image_tokens":0,"audio_tokens":0}}` |
| medium | responses | `{"effort":"medium"}` | 200 | `google/gemini-3.6-flash` | `{"input_tokens":7,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":12,"output_tokens_details":{"reasoning_tokens":12},"total_tokens":19,"cost":0.0000497475,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00005025,"upstream_inference_input_cost":0.00000525,"upstream_inference_output_cost":0.000045}}` |
| high | chat | `{"effort":"high"}` | 200 | `google/gemini-3.6-flash` | `{"prompt_tokens":7,"completion_tokens":12,"total_tokens":19,"cost":0.0000497475,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.00005025,"upstream_inference_prompt_cost":0.00000525,"upstream_inference_completions_cost":0.000045},"completion_tokens_details":{"reasoning_tokens":12,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `google/gemini-3.6-flash` | `{"input_tokens":7,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":12,"output_tokens_details":{"reasoning_tokens":12},"total_tokens":19,"cost":0.0000497475,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00005025,"upstream_inference_input_cost":0.00000525,"upstream_inference_output_cost":0.000045}}` |
| disabled | chat | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| disabled | responses | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |

#### `x-ai/grok-build-0.1`

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `x-ai/grok-build-0.1` | `{"prompt_tokens":193,"completion_tokens":120,"total_tokens":313,"cost":0.000276606,"is_byok":false,"prompt_tokens_details":{"cached_tokens":192,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0002794,"upstream_inference_prompt_cost":0.0000394,"upstream_inference_completions_cost":0.00024},"completion_tokens_details":{"reasoning_tokens":119,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `x-ai/grok-build-0.1` | `{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":106,"output_tokens_details":{"reasoning_tokens":105},"total_tokens":299,"cost":0.000248886,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0002514,"upstream_inference_input_cost":0.0000394,"upstream_inference_output_cost":0.000212}}` |
| high | chat | `{"effort":"high"}` | 200 | `x-ai/grok-build-0.1` | `{"prompt_tokens":193,"completion_tokens":124,"total_tokens":317,"cost":0.000284526,"is_byok":false,"prompt_tokens_details":{"cached_tokens":192,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":0.0002874,"upstream_inference_prompt_cost":0.0000394,"upstream_inference_completions_cost":0.000248},"completion_tokens_details":{"reasoning_tokens":123,"image_tokens":0,"audio_tokens":0}}` |
| high | responses | `{"effort":"high"}` | 200 | `x-ai/grok-build-0.1` | `{"input_tokens":193,"input_tokens_details":{"cached_tokens":192},"output_tokens":144,"output_tokens_details":{"reasoning_tokens":143},"total_tokens":337,"cost":0.000324126,"is_byok":false,"cost_details":{"upstream_inference_cost":0.0003274,"upstream_inference_input_cost":0.0000394,"upstream_inference_output_cost":0.000288}}` |
| disabled | chat | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |
| disabled | responses | `{"enabled":false}` | 400 | `-` | `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400,"metadata":{"provider_name":null}}}` |

`supported_parameters` has no `reasoning_effort`. The `{"effort":"high"}` probe tests whether the host refuses one; it returns 200. Both wires ignore the output cap of 16 (completions of 106 to 144 tokens, nearly all reasoning).

### `deepseek/deepseek-v4-flash`

The same request shape, with no `reasoning`:

| Probe | wire | `reasoning` sent | status | `model` | `usage` or error body |
|---|---|---|---|---|---|
| default | chat | `"(omitted)"` | 200 | `deepseek/deepseek-v4-flash` | `{"prompt_tokens":11,"completion_tokens":16,"total_tokens":27,"cost":6.019398e-7,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":6.0802e-7,"upstream_inference_prompt_cost":1.5554e-7,"upstream_inference_completions_cost":4.5248e-7},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}` |
| default | responses | `"(omitted)"` | 200 | `deepseek/deepseek-v4-flash` | `{"input_tokens":11,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":16},"total_tokens":27,"cost":6.019398e-7,"is_byok":false,"cost_details":{"upstream_inference_cost":6.0802e-7,"upstream_inference_input_cost":1.5554e-7,"upstream_inference_output_cost":4.5248e-7}}` |

Chat body, verbatim:

```json
{"id":"gen-1792345444-6ETTLQ2QYOUzepY60Emg","object":"chat.completion","created":1792345444,"model":"deepseek/deepseek-v4-flash","provider":"Baidu","system_fingerprint":null,"service_tier":null,"choices":[{"index":0,"logprobs":null,"finish_reason":"length","native_finish_reason":"length","message":{"role":"assistant","content":null,"refusal":null,"reasoning":"We are asked to reply with the single word OK. So just output \"OK","reasoning_details":[{"type":"reasoning.text","text":"We are asked to reply with the single word OK. So just output \"OK","format":"unknown","index":0}]}}],"usage":{"prompt_tokens":11,"completion_tokens":16,"total_tokens":27,"cost":6.019398e-7,"is_byok":false,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"audio_tokens":0,"video_tokens":0},"cost_details":{"upstream_inference_cost":6.0802e-7,"upstream_inference_prompt_cost":1.5554e-7,"upstream_inference_completions_cost":4.5248e-7},"completion_tokens_details":{"reasoning_tokens":16,"image_tokens":0,"audio_tokens":0}}}
```

The snapshot entry: `"canonical_slug":"deepseek/deepseek-v4-flash-20260423"`, `"name":"DeepSeek: DeepSeek V4 Flash 0423"`, `"hugging_face_id":"deepseek-ai/DeepSeek-V4-Flash"`, `"context_length":1048576`, `"reasoning":{"mandatory":false,"supported_efforts":["xhigh","high","none"],"default_effort":"high"}`. `GET https://openrouter.ai/api/v1/models/deepseek/deepseek-v4-flash/endpoints` (status 200) lists 16 providers: Relace, OpenInference, Baidu, StreamLake, DeepInfra, GMICloud, Venice, DigitalOcean, SiliconFlow, AtlasCloud, Parasail, Alibaba, Mancer 2, Azure, Cloudflare and Wafer. Every one has `"model_name":"DeepSeek: DeepSeek V4 Flash 0423"`, and none is DeepSeek. The related ids in the snapshot are `deepseek/deepseek-v4.1-flash` (canonical `-20260910`), `deepseek/deepseek-v4.1-flash-1017` (described as "served by DeepSeek under the `deepseek-flash` API model name", with weekday and time-of-day pricing), `~deepseek/deepseek-v4-flash-latest` (`"alias_target":{"slug":"deepseek/deepseek-v4-flash-0731"}`), `~deepseek/deepseek-flash-latest`, `deepseek/deepseek-v4-flash-0731` and `deepseek/deepseek-v4-flash-vision-exp`.

### Established (2026-10-18)

- One snapshot prices every OpenRouter offering; the tables above give the old and new tier for each. `qwen/qwen3.8-max` is the only id the catalog carries that is absent from the snapshot.
- Decision 5's repricings hold on OpenRouter. `x-ai/grok-4.7` is `[0,2000,500,0,0,6000]` + `[200000,4000,1000,0,0,12000]`, the vendor's rate. `google/gemini-3.8-flash` is `[0,825,83,…,4125]`; the read is 82.5 rounded half-up. GPT-5.6 Sol is `[0,4000,400,…,20000]` + `[272000,8000,800,…,30000]`, Terra `[0,2000,200,…,12000]` + `[272000,4000,400,…,18000]`, and Luna `[0,200,20,…,1200]` + `[272000,400,40,…,1800]`, with context 1050000 for all three.
- The decisions' list prices for the new rows match the snapshot, with one exception: `nvidia/nemotron-3.5-super-vl` is $0.10 / $0.08 / $0.40, not the $0.15 / $0.02 / $0.50 decision 4 gives. `claude-haiku-5.5`'s second tier starts at `min_prompt_tokens` 100000.
- On both wires the disable probe returns 400 `Reasoning is mandatory for this endpoint and cannot be disabled.` on every offering whose `reasoning.mandatory` is true: Qwen3.8 Max Prime, Qwen3.8 2.4T, GLM-5.3 Prime, GPT-6.1 Sol (`effort: none`), Opus 5.5, Sonnet 5.5, Gemini 4 Argon, Gemini 3.6 Flash and Grok Build 0.1. On the non-mandatory offerings it returns 200 with `reasoning_tokens: 0`: GPT-6 Luna and GPT-6 Sol (`effort: none`), Haiku 5.5 (`effort: none`), Nemotron 3.5 Super VL, MiMo V2.6 Pro and MiMo V2.6 Flash (`enabled: false`).
- Every advertised effort level returns 200 on both wires for every effort offering. Because OpenRouter also accepts levels a model does not list, the 200s do not distinguish the levels; the levels each row can carry are its `supported_efforts`.
- On every toggle-only offering (Nemotron 3.5 Super VL, both MiMo V2.6) the default produces reasoning tokens: the default is on.
- `deepseek/deepseek-v4-flash` answers as itself: `"model":"deepseek/deepseek-v4-flash"`, served by third parties hosting the 0423 weights. No DeepSeek-run endpoint sits behind it, and nothing in the body or the snapshot shows V4.1 Flash.
- Total spend for the 171 probe requests: about $0.018, summed from the `usage.cost` fields.

## R7, OpenAI cache writes

Whether an OpenAI usage block reports a cache-write count distinct from `cached_tokens`. Gathered 2026-10-18 between 17:42 and 17:45 UTC with `curl` against the production endpoints, key in `Authorization: Bearer <redacted>`. Every usage body below is verbatim from the response; the filler prose is elided from requests.

`POST https://api.openai.com/v1/responses` and `POST https://api.openai.com/v1/chat/completions`, model `gpt-6-luna` unless the table says otherwise.

Every request carried 14,163 chars of filler prose (2,461 cached tokens once written) beginning with a per-pair nonce, so no pair could hit an earlier pair's cache, and the user message "Reply with the single word OK.". On Responses the filler is `instructions`, with `max_output_tokens: 16` and `reasoning: {"effort":"none"}`; on Chat Completions it is a `developer` message, with `max_completion_tokens: 16` and `reasoning_effort: "none"`. Each pair is the same body sent twice about three seconds apart. Probe 13 is a control below the minimum: `instructions` is "You are a terse assistant." alone.

| Probe | wire | model | cache parameters | status | `usage` |
|---|---|---|---|---|---|
| 01 | responses | gpt-6-luna | none | 200 | `{"input_tokens":2464,"input_tokens_details":{"cache_write_tokens":2461,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2469}` |
| 02 | responses | gpt-6-luna | none | 200 | `{"input_tokens":2464,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":2461},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2469}` |
| 03 | chat | gpt-6-luna | none | 200 | `{"prompt_tokens":2464,"completion_tokens":4,"total_tokens":2468,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":2461,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| 04 | chat | gpt-6-luna | none | 200 | `{"prompt_tokens":2464,"completion_tokens":4,"total_tokens":2468,"prompt_tokens_details":{"cached_tokens":2461,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| 05 | responses | gpt-6-luna | `prompt_cache_key: "r7-probe"`, `prompt_cache_retention: "24h"` | 200 | `{"input_tokens":2464,"input_tokens_details":{"cache_write_tokens":2461,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2469}` |
| 06 | responses | gpt-6-luna | as 05 | 200 | `{"input_tokens":2464,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":2461},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2469}` |
| 07 | chat | gpt-6-luna | as 05 | 200 | `{"prompt_tokens":2464,"completion_tokens":4,"total_tokens":2468,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":2461,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| 08 | chat | gpt-6-luna | as 05 | 200 | `{"prompt_tokens":2464,"completion_tokens":4,"total_tokens":2468,"prompt_tokens_details":{"cached_tokens":2461,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| 09 | responses | gpt-6-luna | `prompt_cache_options: {"mode":"implicit","ttl":"30m"}` | 200 | `{"input_tokens":2464,"input_tokens_details":{"cache_write_tokens":2461,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2469}` |
| 10 | responses | gpt-6-luna | as 09 | 200 | `{"input_tokens":2464,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":2461},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2469}` |
| 11 | chat | gpt-6-luna | as 09 | 200 | `{"prompt_tokens":2464,"completion_tokens":4,"total_tokens":2468,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":2461,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| 12 | chat | gpt-6-luna | as 09 | 200 | `{"prompt_tokens":2464,"completion_tokens":4,"total_tokens":2468,"prompt_tokens_details":{"cached_tokens":2461,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| 13 | responses | gpt-6-luna | none, 23-token prompt | 200 | `{"input_tokens":23,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":28}` |
| 14 | responses | gpt-5.6-luna | none | 200 | `{"input_tokens":2464,"input_tokens_details":{"cache_write_tokens":2461,"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2469}` |
| 15 | responses | gpt-5.6-luna | none | 200 | `{"input_tokens":2464,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":2461},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2469}` |
| 16 | chat | gpt-5.6-luna | none | 200 | `{"prompt_tokens":2464,"completion_tokens":4,"total_tokens":2468,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":2461,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |
| 17 | chat | gpt-5.6-luna | none | 200 | `{"prompt_tokens":2464,"completion_tokens":4,"total_tokens":2468,"prompt_tokens_details":{"cached_tokens":2461,"cache_write_tokens":0,"audio_tokens":0},"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}` |

Every Responses body, whatever was sent, echoed `"prompt_cache_retention": "24h"`; probes 09 and 10 also echoed `"prompt_cache_options": {"mode": "implicit", "ttl": "30m"}`. The Chat Completions bodies echo neither.

### 18 — `prompt_cache_retention: "in_memory"`

Responses, as 01 with `prompt_cache_retention: "in_memory"`, on `gpt-6-luna` and again on `gpt-5.6-luna`. Status 400 on both, body identical:

```json
{"error":{"message":"This model is compatible only with 24h extended prompt caching","type":"invalid_request_error","param":"prompt_cache_retention","code":"invalid_parameter"}}
```

### Documentation, read 2026-10-18

`https://platform.openai.com/docs/guides/prompt-caching`, verbatim:

> For GPT-5.6 and later, cache writes cost 1.25× the standard, uncached input-token rate. Subsequent reads cost 0.1× that rate on most of these models and 0.05× on GPT-6.1 Sol .

> Track usage.input_tokens_details.cached_tokens , usage.input_tokens_details.cache_write_tokens , input-token counts, latency, and realized cost.

> const ordinaryInputTokens = inputTokens - cachedTokens - cacheWriteTokens; const weightedInputTokens = ordinaryInputTokens + cachedTokens * cacheInputMultiplier + cacheWriteTokens * cacheWriteMultiplier;

From its summary table (columns GPT-5.6 and later / GPT-5.5 and GPT-5.5 Pro / Other earlier models):

> Cache write charge 1.25× the uncached input-token rate No additional cache-write charge No additional cache-write charge Cache lifetime control prompt_cache_options.ttl prompt_cache_retention prompt_cache_retention Supported retention values "30m" "24h" only "in_memory" or "24h"

`https://platform.openai.com/docs/api-reference/responses/object` answered 403 to `curl`. The same reference at `https://developers.openai.com/api/reference/resources/responses`, verbatim:

> input_tokens_details : object { cache_write_tokens , cached_tokens } A detailed breakdown of the input tokens.

> cache_write_tokens : number The number of input tokens that were written to the cache.

The Chat Completions reference at `https://developers.openai.com/api/reference/resources/chat/subresources/completions` was fetched and does not mention `cache_write_tokens` or `prompt_tokens_details` in its served HTML; the field on that wire is established by probes 03, 07, 11 and 16 only.

`https://platform.openai.com/docs/pricing`, Standard tier, "All models" table, $ per 1M tokens, columns Input / Cached input / Cache writes / Output (short context):

| model | input | cached input | cache writes | output |
|---|---|---|---|---|
| gpt-6-astra | 10 | 1 | 12.5 | 50 |
| gpt-6.1-sol | 2 | 0.1 | 2.5 | 10 |
| gpt-6-luna | 0.1 | 0.01 | 0.125 | 0.5 |
| gpt-6-sol | 2 | 0.2 | 2.5 | 10 |
| gpt-5.6-sol | 4 | 0.4 | 5 | 20 |
| gpt-5.6-terra | 2 | 0.2 | 2.5 | 12 |
| gpt-5.6-luna | 0.2 | 0.02 | 0.25 | 1.2 |
| gpt-5.5 (<272K context length) | 5 | 0.5 | - | 30 |
| gpt-5.4 (<272K context length) | 2.5 | 0.25 | - | 15 |

Long context, verbatim from the flagship table: `gpt-6.1-sol $2.00 $0.10 $2.50 $10.00 $4.00 $0.20 $5.00 $15.00` and `gpt-6-luna $0.10 $0.01 $0.125 $0.50 $0.20 $0.02 $0.25 $0.75` (short then long, each Input / Cached input / Cache writes / Output).

Established:

- Both OpenAI wires report a cache-write count distinct from `cached_tokens`: `input_tokens_details.cache_write_tokens` on Responses, `prompt_tokens_details.cache_write_tokens` on Chat Completions. A first request writes (probes 01, 03: 2461 written, 0 read); an identical repeat reads (probes 02, 04: 0 written, 2461 read).
- The field is present without any cache parameter, and is present as 0 below the minimum (probe 13), so it does not depend on extended retention or a cache key; `prompt_cache_key`, `prompt_cache_retention: "24h"` and `prompt_cache_options` change nothing in it.
- It is not GPT-6 only: `gpt-5.6-luna` reports it identically (probes 14–17), and the pricing page lists a cache-write rate for the whole GPT-5.6 line as well as GPT-6.
- Per the guide's formula, `cache_write_tokens` is a subset of `input_tokens`, disjoint from `cached_tokens`: ordinary input = `input_tokens - cached_tokens - cache_write_tokens`. In every probe written plus read never exceeded 2461 and input stayed 2464.
- There is one write rate per model and context tier, with no TTL buckets; documented as 1.25× uncached input for GPT-5.6 and later, and 1.25× on every rate in the table.
- `prompt_cache_retention: "in_memory"` is refused on `gpt-6-luna` and `gpt-5.6-luna` (probe 18); Responses echoes `"24h"` whatever is sent.

## What was established

The values written to `specs/_data/catalog_table.go` on 2026-10-18, per row. Prices are nano-USD per token as `[min_input, uncached, cache_read, write_5m, write_1h, output]`; "OR" is both OpenRouter wires, which always share a row's values.

Added:

- `gpt-6-luna`: OpenAI Responses (with `codex()`) and Chat, OR `openai/gpt-6-luna`. Context 1,050,000. `[0,100,10,0,0,500]` + `[272001,200,20,0,0,750]` everywhere. OpenAI wires effort `none`–`xhigh`, OR `none`–`max`; default `medium`; can disable (`none`).
- `gpt-6-sol`: as Luna, `[0,2000,200,0,0,10000]` + `[272001,4000,400,0,0,15000]`; `codex()` on the probe (the Codex page is silent on it).
- `gpt-6.1-sol`: as Luna, `[0,2000,100,0,0,10000]` + `[272001,4000,200,0,0,15000]`. OpenAI wires effort `low`–`xhigh`, OR `low`–`max`; default `medium`; cannot disable (`none` rejected everywhere).
- `claude-opus-5-5`: Anthropic (max output 128,000) and OR `anthropic/claude-opus-5.5`. Context 1,000,000. `[0,4000,200,5000,8000,20000]`. Effort `low`–`max`, default `medium` (page), cannot disable.
- `claude-sonnet-5-5`: as Opus 5.5, `[0,2000,100,2500,4000,10000]` (read $0.10, the page's 0.05x, not the $0.20 the decisions gave); default `high` (page); cannot disable.
- `claude-haiku-5-5`: as Opus 5.5, `[0,100,10,125,200,500]` + `[100001,500,50,625,1000,2500]` (boundary from the page; not observable on the wire); default `medium` (page); can disable, at effort `high` or below only.
- `gemini-4-argon`: Gemini and OR `google/gemini-4-argon`. Context 1,048,576. `[0,2000,100,0,0,10000]`, introductory. `thinking_level` `low`–`high`, default `medium` (page). Can disable on Gemini (`thinkingBudget: 0` accepted, no thought tokens, though indistinguishable from `low`); cannot on OR.
- `gemini-3.6-flash`: Gemini and OR `google/gemini-3.6-flash`. Context 1,048,576. `[0,750,75,0,0,3750]`, introductory. `thinking_level` `minimal`–`high`, default `medium` (page). Can disable on Gemini, not on OR.
- `grok-build-0.1`: xAI Responses and Chat (API key and OAuth), OR `x-ai/grok-build-0.1`. Context 256,000. `[0,1000,200,0,0,2000]` + `[200001,2000,400,0,0,4000]`. Toggle: always on, cannot disable; every `reasoning_effort` value rejected. Alias `grok-code-fast-1`.
- OR-only: `qwen3.8-max-prime` (`qwen/qwen3.8-max-prime`, 1,000,000, `[0,4000,500,0,0,12000]`), `qwen3.8-2.4t-a95b` (`qwen/qwen3.8-2.4t-a95b`, 1,048,576, `[0,2000,250,0,0,6000]`), both effort `low`/`medium`/`xhigh`, default `xhigh`, cannot disable; `glm-5.3-prime` (`z-ai/glm-5.3-prime`, 1,000,000, `[0,2800,560,0,0,8800]`), effort `low`/`medium`/`high`/`max`, default `max`, cannot disable; `nemotron-3.5-super-vl` (`nvidia/nemotron-3.5-super-vl`, 1,048,576, `[0,100,80,0,0,400]`, not the decisions' $0.15 / $0.02 / $0.50), `mimo-v2.6-pro` (`xiaomi/mimo-v2.6-pro`, 1,050,000, `[0,435,4,0,0,870]`), `mimo-v2.6-flash` (`xiaomi/mimo-v2.6-flash`, 1,050,000, `[0,140,3,0,0,280]`), all three a toggle, default on, can disable.

Removed: `gemini-3.7-flash` and `gemini-3.5-flash` (deprecated 2026-10-08, now routed to 3.8 and 3.6 Flash), `gemini-2.5-flash` and `gemini-2.5-pro` (access limited to past users; they still answer this key), `gpt-5.4-nano` (deprecated; Codex rejects it), `qwen3.8-max` (absent from the OpenRouter snapshot).

Repriced: every OpenRouter offering from the snapshot as R6's table gives, with new long-context tiers keyed `+1` (`gpt-5.6-*`, `gpt-5.5-pro`, `grok-4.3`) and `minimax-m3`'s 512K tier dropped; write prices zero on every non-Anthropic row. On the vendor hosts: the GPT-5.6 line (Sol `[0,4000,400,0,0,20000]`, Terra `[0,2000,200,0,0,12000]`, Luna `[0,200,20,0,0,1200]`, each with a `272001` tier at 2x input and read and 1.5x output; Luna's context 1,050,000) and `gemini-3.8-flash` (`[0,750,75,0,0,3750]`, introductory). `grok-4.7` on xAI is unchanged. `deepseek-v4-flash` stays: it still answers as itself.

Codex membership after the refresh: `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-6-astra`, `gpt-6-luna`, `gpt-6-sol`, `gpt-6.1-sol`. `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.5` and `gpt-5.5-pro` are rejected and carry no `codex()`.

Found and deferred:

- OpenAI reports cache writes: `cache_write_tokens` beside `cached_tokens` on both OpenAI wires and on the Codex backend, on the GPT-5.6 line as well as GPT-6, billed at 1.25x uncached input. The catalog carries zero OpenAI write prices until a usage field and a single-rate write price the OpenAI wires can fill exist.
- The Anthropic codec's off body, `{"type":"disabled","budget_tokens":0}`, is rejected by every Claude model probed; only the bare `{"type":"disabled"}` is accepted. Rows marked can-disable fail on the wire until the codec drops `budget_tokens`.
- Sonnet 5.5's off form is `thinking: {"type":"between_tools"}`, which the Anthropic wire does not send; the row says it cannot disable until it does.
- Introductory prices expire: Gemini 4 Argon on 2026-12-01 ($4 / $0.20 / $20), Gemini 3.6 and 3.8 Flash on 2027-01-01 ($1.50 / $0.15 / $7.50). Each row reprices then.
- On Chat Completions, `gpt-6-luna` and `gpt-6-sol` call functions only at effort `none`, and `gpt-6.1-sol` not at all; Responses accepts `max` on all three where Chat rejects it.
