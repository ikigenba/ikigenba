# prompts — Product

**Authority: intent.** This document owns *why* this change exists, *for whom*, what is in and out of scope, and what we *promise* the user. It does not state mechanism, exact formats, exit codes, or test assertions — those belong to `project/design/README.md`. Where the two could overlap on behavior, this doc states the *promise*; design states the *exact, checkable proof of that promise*.

## Problem

The prompts service is hardcoded to a single provider (Anthropic) with a narrow set of generation controls (effort, max_tokens, temperature). Users cannot choose a different provider or model per prompt, and cannot tune the full set of generation and retry knobs that the underlying agentkit supports. The local agentkit package that backs the service is also a dead end — it is a private fork, not the shared library.

Inference is also scattered across the suite: other services execute their own LLM calls with their own provider keys, their own logging, and their own controls, so there is no single place where the owner can see every inference workload, what caused it, and what it cost — and every new inference-using service reinvents that plumbing again.

## Purpose

Replace the local `./agentkit` dependency with the published `github.com/ikigenba/agentkit` package, and expose its full provider and configuration surface through the prompts MCP API. A prompt now declares which provider and model it runs on, and optionally tunes any generation or retry setting the agentkit supports. The change is mechanical at the library boundary and additive at the API surface — existing behavior is preserved under a clean migration.

prompts' primary surface stays **MCP** (the owner works through an agent and tools, not screens), but it also serves a **human web surface** at its mount root (`…/srv/prompts/`): a browse UI where a logged-in person can look through the box's prompts and runs, narrow either list down, and open a run to read the full story of what it did — every model exchange it made, in order. It is gated by the dashboard browser session like any web page in the suite, and it is a whole-box view: any signed-in user sees all of it, matching how prompts accounts for inference. Agents are unaffected — they keep working through MCP.

prompts is also the suite's **inference service**: the one place on the box that executes LLM work, accounts for it, and answers for its spend. Beside agent sessions it runs two further workload classes on behalf of sibling services — one-shot **completions** and **embeddings** — so a service that needs inference asks prompts instead of holding provider keys and building its own execution, logging, and reporting. Every unit of inference, whatever its class and whoever caused it, lands in one durable record the owner can inspect and total.

## Users

The box owner, operating through an agent connected to the prompts MCP service. The owner states intent in natural language ("run this on OpenAI with low temperature"); the agent maps that to the structured MCP parameters. The owner never writes raw JSON — that is the agent's job.

Sibling services on the box are the second consumer: their daemons hand prompts their completion and embedding work and get the result back synchronously, without ever touching a provider key.

## Scope

**In scope:**

- **Multi-provider support over a curated model catalog.** Any prompt may target any of the five providers the published agentkit supports: `anthropic`, `openai`, `google`, `zai`, `openrouter` — the last opening up the OpenRouter-hosted vendors (Grok, DeepSeek, Kimi). The model is required on every prompt and must be one the agentkit model catalog lists; the provider is optional — when omitted it is the model's catalog default — but stays settable to deliberately reach a model through an alternate provider the catalog supports.
- **Full generation and retry tuning via a config object.** Alongside provider and model, a prompt may carry an optional structured config object with any of these keys: `temperature`, `top_p`, `max_tokens`, `effort`, `thinking_budget`, `thinking_level`, `thinking`, `max_attempts`, `base_delay`, `max_delay`, `max_elapsed`, `ignore_retry_after`, `tool_loop_limit`, `base_url`, `auth`. Unset keys use the agentkit's defaults; keys that do not apply to the chosen model are silently ignored by the agentkit.
- **OpenAI work can run on the ChatGPT subscription.** Any OpenAI-backed prompt or service completion may set `auth: "sub"` to authenticate with the account's ChatGPT subscription instead of the metered API key; the default (`auth` unset or `"key"`) remains the API key, and nothing changes for existing configs. The subscription credential is a file the operator provisions once by logging in; producing that file (the login flow) is out of scope — prompts consumes it, keeps it fresh, and rejects subscription-configured work up front with a clear error while the file is absent. Subscription auth applies to sessions and completions only; embeddings always use the API key.
- **Config update with full-replacement semantics.** Updating a prompt's config replaces the entire config object. To keep a value, re-specify it; omitting a key removes it and reverts it to the agentkit default.
- **Validation at create/update time.** A model outside the catalog, a provider that cannot serve the chosen model, or a reasoning setting (effort, thinking level, thinking budget, thinking on/off) the chosen model does not accept is rejected immediately with a clear error naming what the model does accept. A run is never started with a config that cannot be resolved.
- **Discoverable inventory.** The service's own documentation surface lists every available model — per provider, with each model's reasoning options — generated from the same catalog that validation enforces, so what the doc offers and what validation accepts can never disagree.
- **Edits take effect on the next run.** The runner pins its execution inputs at spawn time, so in-flight runs are unaffected. Any edit to provider, model, or config is reflected starting with the next run.
- **Backfill migration for existing prompts.** Existing rows with no provider set are migrated to `anthropic` so they continue to work without user intervention.
- **Serve a browse UI for humans.** At the mount root, a **logged-in person** (gated by the dashboard browser session, like any web page in the suite) lands in a styled two-tab browse surface — **prompts** (the default) and **runs**. Each tab shows the fields you'd scan and narrow by (a prompt's name, owner, and freshness; a run's prompt, status, owner, start time, duration, and what triggered it), newest first, in pages, with narrowing done by the service so even very large histories browse quickly. Clicking a prompt shows its full text, configuration, and triggers, and leads to that prompt's runs; clicking a run shows its remaining details and **replays it**: every model call the run made, in order, with what was sent and what came back — readable top to bottom as the run's log. Very large exchanges stay fully retrievable even where the page shows only their beginning. The view is whole-box (any signed-in user sees everyone's rows, consistent with the inference accounting), and it is an inspection surface, not a live monitor — refresh to see a running run's progress. One-shot completions and embeddings made by sibling services are not browsed here; they remain inspectable through the MCP reporting tools.
- **Loopback ports resolve from the shared registry, not from literals.** prompts learns its own loopback port and every peer's loopback address by **service name** from the suite's single authoritative `registry` table, instead of those port numbers being written into prompts's own source. The addresses prompts uses are unchanged (this is behavior-preserving); the only shift is *where the number is written down*: one authoritative table, so a renumber can no longer drift silently out of sync with prompts and surface only at deploy. The existing env overrides (`PROMPTS_<SRC>_FEED_URL`, `DROPBOX_BASE_URL`) still win where set; the registry only supplies the **default** the override falls back to.
- **Runs work against the account's file share.** A run can list, read, and
  write the account's shared files directly: pull a shared file into its own
  folder to work on it, and place results back in the share where the owner —
  and everything else watching the account's files — sees them. The whole
  share is reachable; nothing is fenced off per prompt. Moving a file between
  the share and the run's folder never routes the file's contents through the
  agent's working context, so a run can process files of any size. A file a
  run places in the share behaves exactly like a file the owner placed there:
  it is durable, it syncs wherever the account's files sync, and it sets off
  whatever workflows watch that location.
- **Suite tools reach the in-run agent on demand, not front-loaded.** The in-run agent can still use every other suite service on the owner's behalf, exactly as before — but it no longer carries every service tool's full definition in its working context from the first moment of every run. It starts with a compact catalog of what the account's services offer and pulls in the specific tools a task actually needs, as it needs them. The observable outcome: runs behave the same, reach the same services, and produce the same kinds of results, while a run's context carries only the catalog plus the tools it actually used — so runs over a fully-populated box stay focused and spend less on tool definitions that were never touched.

- **Completions and embeddings for sibling services.** A service on the box can hand prompts a one-shot completion (its own full prompt text, model choice, and tuning — including a multi-turn retry history) or a batch embedding request, and get the result back synchronously. The same curated model catalog that governs prompts' own runs governs these calls: an unknown model, an unroutable provider, or a rejected reasoning setting fails immediately with a clear error. All provider credentials live with prompts alone.
- **One account of every inference workload.** Every unit of inference prompts executes — an agent-session run, a completion, an embedding — is durably recorded with who caused it (a user, an event trigger, or a service), a stable workload name for grouping (every wiki compile call is recognizable as a group), what model ran it, tokens in and out, cost, timing, and any error. Unnamed or unattributed work is refused. The metrics are kept forever; the full request/response text of recent work stays inspectable for a retention window (default 30 days) and then ages out, leaving the totals intact.
- **Inspection and spend reporting.** Through prompts' own MCP tools the owner can list and filter recorded inference (by workload, cause, group, time, errors), open one call's full request and response while retained, and total tokens, cost, and error counts by workload, cause, model, or day — "what did wiki ingest cost this month" is one question, one answer. These views cover the whole box's inference, whoever caused it.
- **Bounded concurrency.** prompts caps how much inference it fires at once — concurrent agent-session runs, and concurrent in-flight service calls per provider — so a burst of triggered work degrades into orderly progress instead of a provider-quota incident, and long-running sessions cannot starve services' synchronous calls.

**Out of scope (nothing else):** Converting any sibling service to use these capabilities is that service's own change (wiki is the first planned consumer, specified in wiki's own project). The repos service's inference remains as it is, pending its own redesign. No spend budgets or alerting yet — reporting first. No streaming completion responses. No renumbering of any service and no ownership of the registry table itself (prompts only *reads* it by name); the `registry` module and the repo-root wiring that publishes it (`go.work`) are provided from outside prompts. No free-form model strings — models outside the agentkit catalog are not accepted, and widening the catalog (including which models OpenRouter can route) is agentkit's change, not prompts'. No change to *which* services and tools a run can reach (only to how they are surfaced). No changes to the trigger or event model. No new run management capabilities. The `system_prompt` field remains a dedicated top-level field and is not part of the config object. The on-demand loading mechanism itself is the agentkit's (owned and proven in its own project); prompts only adopts it.

## Contractual constants

The five valid provider names, exactly as the agentkit recognises them:

```
anthropic   openai   google   zai   openrouter
```

The fifteen optional config keys, exactly as named:

```
temperature   top_p   max_tokens   effort
thinking_budget   thinking_level   thinking
max_attempts   base_delay   max_delay   max_elapsed
ignore_retry_after   tool_loop_limit   base_url   auth
```

The two authentication mode names the `auth` key accepts, exactly as named:

```
key   sub
```

These names are promises — the design must use them verbatim in the MCP tool schema and in stored JSON.

The three inference workload class names, exactly as recorded and reported:

```
session   completion   embedding
```

## What we promise (user-facing behavior)

**Creating a prompt** requires a model from the catalog; the provider and all config keys are optional:

> "Create a prompt that runs on gpt-5.5 with temperature 0.3 and a max of 2000 output tokens."

The MCP tool accepts `model: "gpt-5.5"` and a config object `{"temperature": 0.3, "max_tokens": 2000}`; the provider defaults to the model's home (here OpenAI) and may be given explicitly to pick an alternate route the catalog supports. If the model is not in the catalog, the provider cannot serve it, or a reasoning setting is not one the model accepts, the create call fails immediately with a descriptive error — no prompt is stored.

**Running a prompt** uses exactly the provider, model, and config stored at run-start time. A run triggered by an event and a manual run go through the same path. Provider selection, model selection, and all config values are applied; keys left at default have no effect.

**Updating a prompt's config** replaces the whole config object. An owner can change provider, model, or any config key and the next run will reflect it. The current run, if any, is unaffected.

**Existing prompts** continue to run without any owner action. They are silently migrated to `provider: "anthropic"` and behave identically to before.

**Opening prompts in a browser lands in the browse UI.** A logged-in dashboard user who navigates to the prompts mount root arrives at the prompts tab of an on-brand browse surface naming the service and its version; someone without a valid session is turned away to log in. From there they can switch to the runs tab, narrow either list, and drill into any prompt or run. A run's page replays its model calls in order — what was sent, what came back, what it cost — and says so plainly when an old call's text has aged out of retention or when a linked prompt has since been deleted, rather than showing an empty page. Agents are unaffected — they keep working through MCP.

**Reasoning settings are checked against the chosen model up front.** An effort level, thinking level, thinking budget, or thinking-off request the model does not accept is rejected at create/update with an error naming the model's actual options — it never becomes a mid-run surprise. Non-reasoning keys a model happens to ignore (e.g. `temperature` on a model that fixes it) are still passed through and silently ignored — the run proceeds normally.

**A run reads and writes the account's file share.** A run triggered by a new
file arriving in the share can pull that file into its own folder, work on it,
and save its results back beside the original — and after the run the owner
finds those results in the shared folder like any other file. What a run
writes to the share is as real as what the owner puts there: it persists, it
syncs, and it can set the next workflow in motion.

**A run discovers suite tools as it needs them.** The in-run agent starts each run knowing what the account's services offer — a compact, per-service catalog — and brings the specific tools a task needs into play on demand. An event-triggered run still follows its event's identifiers to the right service's tools; a run that touches no suite service carries no suite tool definitions at all. Which services a run may reach is unchanged (all of them except prompts itself, on the owner's behalf), and a service that is down simply doesn't appear, exactly as before.

**OpenAI inference can bill the subscription instead of the meter.** An owner marks an OpenAI-backed prompt — or a service marks its completion — with `auth: "sub"` and it runs against the account's ChatGPT subscription; leaving `auth` unset (or `"key"`) keeps today's API-key behavior exactly. If the operator has not yet provisioned the subscription credential, creating or running subscription-configured work fails immediately with an error saying what is missing — never a silent fallback to the key. Embedding work is unaffected by `auth` and always uses the API key.

**A sibling service gets its inference from prompts.** A service daemon on the box submits a completion — its own prompt text, a catalog model, its tuning, optionally a prior exchange to continue — and receives the model's reply, token usage, and cost in the same request. Embeddings work the same way: a batch of texts in, vectors and cost out. The service holds no provider key and builds no LLM plumbing; a bad model choice or malformed request is refused immediately with an error that names the problem.

**Every inference workload is on the record, attributably.** After any inference — an owner-run prompt, an event-triggered run, a service's completion or embedding — the owner can find its record: which class it was, who caused it, its workload name and group, model, tokens, cost, duration, and error if any. Work arriving without a valid cause or workload name is refused, so the record is never partial by omission.

**Spend questions have one answer.** The owner asks prompts — not each service — what inference cost: totals by workload name, by cause, by model, or by day, over any time window, covering sessions, completions, and embeddings alike.

**Recent work is fully inspectable; history never loses its totals.** Within the retention window the owner can open any recorded call and read exactly what was sent and what came back. Older records keep every metric forever; only the bodies age out, and a pruned record says so.

## Success criteria (outcomes)

- A prompt created with `provider: "openai"` and a catalog OpenAI model runs successfully against the OpenAI API.
- A prompt created with only a model (no provider) is stored with that model's default provider and runs on it — including an OpenRouter-hosted model like a Grok, DeepSeek, or Kimi model running through OpenRouter.
- A prompt created with an unknown provider name is rejected at create time with a clear error message; no prompt row is created.
- A prompt created with a model outside the catalog, or with a provider the catalog cannot serve that model through, is rejected at create time with a clear error message.
- A prompt created with a reasoning setting the model does not accept (a level outside its list, a budget outside its range, or thinking off where it cannot be turned off) is rejected at create time with an error naming the model's accepted options.
- Asking the service to describe itself lists every catalog model with its provider and reasoning options; a model the description lists is accepted by create, and a model it does not list is rejected.
- A prompt created with `{"temperature": 0.5, "max_tokens": 500}` in its config runs with those values applied; a subsequent update omitting `temperature` causes the next run to use the agentkit default temperature.
- An existing prompt with no provider set in the database continues to run successfully after the migration, executing against Anthropic as before.
- A prompt running on the `zai` provider with `base_url` set in its config targets that URL.
- A non-reasoning config value the chosen model happens to ignore does not cause the run to fail.
- Editing a prompt's provider, model, or config while a run is in flight does not affect that run; the change is reflected only in the next run.
- As a logged-in dashboard user I open the prompts mount root in a browser and land in a styled browse surface (naming the service and its version) listing the box's prompts, newest first; without a valid session I am sent to log in, and the agent-facing MCP surface is unchanged.
- I switch to the runs tab, narrow it to one status or one prompt, and page through the matches — the narrowing happens on the server, so a history far larger than one screen still answers quickly.
- I open a run and read its log: every model call it made, in order, with the full request and response text while retention holds it, an explicit note where a body has aged out, and the complete text of an oversized exchange still retrievable in full.
- I follow a run's link to a prompt that was deleted and get a clear "this prompt no longer exists" page, not a bare error.
- prompts serves on the same loopback port and reaches the same peer `/feed` and dropbox addresses as before, with every one of those addresses obtained from the shared registry by service name; no loopback port number appears as a literal anywhere in prompts's own (non-test) source.
- A run whose task needs a suite service pulls that service's tools in on demand and completes the task, end to end, the same as before.
- A run's working context carries a compact per-service catalog plus only the suite tools the run actually used — never the full definitions of every tool on the box.
- A suite service that is unreachable at run start is absent from the catalog and the run proceeds unaffected, exactly as discovery behaves today.
- A run set off by a new file in a shared folder pulls that file into its workspace, and the report it saves back to the share is sitting in that folder — durable and synced — when the owner looks.
- A run can pull in, transform, and save back a shared file far larger than any message the agent could carry, and the run completes normally.
- A file a run saves into a watched shared folder triggers the workflows watching that folder, exactly as if the owner had put it there.
- A sibling service on the box submits a completion with its own prompt text and a catalog model and receives the reply, token usage, and cost in the same request; a request naming a model outside the catalog, or missing its cause or workload name, is refused with an error naming the problem and leaves no record.
- A service submits a completion that continues a prior exchange (its earlier request, the model's earlier reply, a corrective follow-up) and the model's answer reflects that full history.
- A service submits a batch of texts for embedding against a catalog embedding model and receives one vector per text, in order, with usage and cost.
- After a run, a service completion, and an embedding have each executed, the owner can list all three from prompts, each carrying its class, its cause (the user, the trigger, or the service), its workload name, model, tokens, cost, and timing.
- The owner asks for inference totals grouped by workload name over a time window and gets counts, tokens, and cost that add up to the individual records — with every wiki-style service workload recognizable as its own group.
- Within the retention window the owner opens one recorded completion and reads the exact request and response text; after the window the same record still shows every metric but reports its bodies as pruned.
- With concurrency caps in place, a burst of simultaneously triggered runs executes without exceeding the configured limits, every run still completes, and a service's synchronous completion still gets through while sessions are saturated.
- A prompt or completion configured with `auth: "sub"` on an OpenAI model executes against the ChatGPT subscription when the operator-provisioned credential is present, and is rejected with an error naming the missing credential when it is not; the same config with `auth` unset behaves exactly as today.
- A config combining `auth: "sub"` with a non-OpenAI provider, an unknown `auth` value, or a custom `base_url` is rejected at create/update with a clear error.
