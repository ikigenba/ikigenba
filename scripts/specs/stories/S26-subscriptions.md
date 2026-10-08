# Stories — subscriptions

`subscribe` and `unsubscribe`, the two tools that wire one of the caller's scripts to an event, so that the script runs whenever the suite's events app delivers to scripts an event whose name the subscription's pattern matches, and stops doing so. A subscription is a pair: one of the caller's scripts and one event pattern, kept as the exact string given, with `created`, the time of the `subscribe` call that made it, RFC 3339 UTC to the second. Both tools take, in this order, `name`, a string, required, the name of one of the caller's scripts, looked up among the caller's scripts only, and `event`, a string, required, an event pattern: two or more words joined by single `.`s, each word either a lowercase ASCII letter followed by lowercase letters and digits, with single `_` allowed between them, or exactly `*`, so that the whole matches `^([a-z][a-z0-9]*(_[a-z0-9]+)*|\*)(\.([a-z][a-z0-9]*(_[a-z0-9]+)*|\*))+$`, `repo.pushed` or `cron.*.fired` say, taken as given, never trimmed or folded. A pattern matches an event whose `event` has exactly as many words as the pattern and, at each place where the pattern's word is not `*`, the same word: a `*` stands for exactly one word, never none and never two. So `cron.*.fired` matches `cron.hourly.fired` and `cron.tick.fired`, and not `cron.fired` or `cron.a.b.fired`; `*.*` matches every two-word name; and a pattern with no `*`, which is an event name, matches only itself, so a subscription to `repo.pushed` matches only events whose `event` is exactly `repo.pushed`. A bare `*` is one word, not two, and so is not a pattern. A subscription is kept, compared and removed as the exact string sent: `subscribe` with a string the script already holds adds nothing, `subscribe` with another string adds a subscription even when one the script holds matches the same events, and `unsubscribe` removes only the subscription whose pattern is the string sent, never one that matches it or that it matches. scripts does not ask whether any service sends an event the pattern matches; a valid pattern that matches nothing any service sends yet is accepted, and the script runs once a matching event comes. The checks run in this order — the script; the pattern; for `unsubscribe`, whether the script holds a subscription to exactly that pattern — and the first that fails is the whole answer, one line naming it: `no script named '<name>'`, `invalid event '<event>'`, and `'<name>' is not subscribed to '<event>'`, each quoting the values as sent. Both answer the script object `show` answers (`S07`), as it is after the call, whose `subscriptions` lists the script's subscriptions, each `{"event":"<event>","created":"<time>"}`, `<event>` the pattern as it was sent, sorted by `event` ascending, `[]` when there are none. Neither tool runs anything, reads any repository, runs git, or touches a run: what a subscription does when an event it matches is delivered, a run of the script with trigger `event` and the event as its input, at most one per script however many of its subscriptions match, is `S27`'s. `subscribe` is of kind `additive` and `unsubscribe` of kind `destructive`, both reached through the gateway's `mutate` (`S05`). Neither records an event of its own: a call records only the request's `request.started`, its `tool.called`, and its `request.finished`, and neither the script's name or id nor the pattern is in any of them. A refusal changes nothing. scripts writes nothing to stderr for any answer in this group.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: `S06`'s shared catalog, it being `2026-10-05T09:32:00Z`, in which the caller `u_7f3a9c21` owns `nightly-report` (`scr_6d1f4a9b2e8c7035`, repository `rep_9c2e4b7a1d3f8e05`, ref `main`, created `2026-09-18T16:40:00Z`, its newest run `run_8a2c6e1f9b3d5074` still `running` since `2026-10-05T09:31:40Z`), `sync-crm` (`scr_a2e7c4f9b1d03856`, repository `rep_41d8f0a6b2c97e13`, ref `main`, created `2026-09-25T10:00:00Z`, its newest run `run_6b2d8f4a0c9e1735` still `running` since `2026-10-05T09:31:00Z`), `rotate-keys`, and `backfill`, and `u_2b8e1d04` owns `digest` (`scr_3b7f9d1c5e0a2846`). To that catalog this group adds one subscription, which every story here assumes unless it says otherwise: `sync-crm` is subscribed to `repo.pushed`, created `2026-10-04T10:15:00Z`. No other script has a subscription, and no event has been delivered to scripts since that subscription was made, so every run in the catalog has the trigger `manual`.

## A model subscribes a script to an event

The ordinary case: the model wants `nightly-report` to run each time someone pushes to a repository, so it subscribes the script to `repo.pushed`. Nothing runs now; the answer is the script as `show` gives it, with its one subscription, made now. The script's repository is not read: a script whose repository is gone, `backfill` say, is subscribed the same way, and each run an event starts for it fails as a `run` of it would (`S27`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: subscribe

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"subscribe","arguments":{"name":"nightly-report","event":"repo.pushed"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_6d1f4a9b2e8c7035","name":"nightly-report","repo":"rep_9c2e4b7a1d3f8e05","ref":"main","created":"2026-09-18T16:40:00Z","subscriptions":[{"event":"repo.pushed","created":"2026-10-05T09:32:00Z"}],"last_run":{"id":"run_8a2c6e1f9b3d5074","status":"running","started":"2026-10-05T09:31:40Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly. `id`, `name`, `repo`, `ref`, `created`, and `last_run` are as they were.

Preconditions:

- The preamble's: `nightly-report` has no subscription, and the call is made at `2026-10-05T09:32:00Z`.

Postconditions:

- The catalog holds the subscription of `nightly-report` to `repo.pushed`, created `2026-10-05T09:32:00Z`. `show` with `nightly-report` answers what `subscribe` answered, and `list` gives `nightly-report`'s entry `"subscriptions":1` (`S07`).
- Nothing ran and no run was made: `nightly-report`'s seven runs are as they were, and `run_8a2c6e1f9b3d5074` runs on. No git ran and no repository was read.
- From now on, each event named `repo.pushed` that the events app delivers to scripts starts a run of `nightly-report`, as `S27` tells; `sync-crm`, subscribed to the same name, gets a run of its own from each.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"subscribe"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  scripts recorded no `script.*` or `run.*` event, and the name `nightly-report`, its id, and `repo.pushed` are in none of them.

## A model subscribes a script to an event it is already subscribed to

`sync-crm` is already subscribed to `repo.pushed`, so there is nothing to add. That is not a refusal: a model retrying a subscribe that already happened gets the answer it would have got the first time, and the subscription keeps the time it was made. A script never holds one pattern twice, and an event starts at most one run of a script however many of its subscriptions match it (`S27`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: subscribe

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"subscribe","arguments":{"name":"sync-crm","event":"repo.pushed"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_a2e7c4f9b1d03856","name":"sync-crm","repo":"rep_41d8f0a6b2c97e13","ref":"main","created":"2026-09-25T10:00:00Z","subscriptions":[{"event":"repo.pushed","created":"2026-10-04T10:15:00Z"}],"last_run":{"id":"run_6b2d8f4a0c9e1735","status":"running","started":"2026-10-05T09:31:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `sync-crm` is subscribed to `repo.pushed`, created `2026-10-04T10:15:00Z`.

Postconditions:

- Nothing has changed. `sync-crm` has the one subscription it had, still created `2026-10-04T10:15:00Z`, and `run_6b2d8f4a0c9e1735` runs on.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"subscribe"}}
  ```

## A model subscribes a script to a second event

A script may be subscribed to any number of patterns, and an event any one of them matches starts it. `sync-crm`, already subscribed to `repo.pushed`, is now subscribed to `crm.contact_added` too. The answer lists both, sorted by `event`, so the new one comes first, whatever the order they were made in.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: subscribe

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"subscribe","arguments":{"name":"sync-crm","event":"crm.contact_added"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_a2e7c4f9b1d03856","name":"sync-crm","repo":"rep_41d8f0a6b2c97e13","ref":"main","created":"2026-09-25T10:00:00Z","subscriptions":[{"event":"crm.contact_added","created":"2026-10-05T09:32:00Z"},{"event":"repo.pushed","created":"2026-10-04T10:15:00Z"}],"last_run":{"id":"run_6b2d8f4a0c9e1735","status":"running","started":"2026-10-05T09:31:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `sync-crm` is subscribed to `repo.pushed` only, and the call is made at `2026-10-05T09:32:00Z`.

Postconditions:

- The catalog holds `sync-crm`'s two subscriptions, to `crm.contact_added`, created `2026-10-05T09:32:00Z`, and to `repo.pushed`, created `2026-10-04T10:15:00Z`; `list` gives `sync-crm`'s entry `"subscriptions":2` (`S07`).
- Nothing ran, no git ran, no repository was read, and `run_6b2d8f4a0c9e1735` runs on.
- An event named `crm.contact_added` delivered to scripts starts a run of `sync-crm`, and so does one named `repo.pushed`, each its own run (`S27`).
- The request's `tool.called` has `tool` `subscribe`, `kind` `additive`, and `outcome` `ok`; scripts recorded no `script.*` or `run.*` event.

## A model subscribes a script to a pattern

The model wants `nightly-report` to run each time any of cron's schedules fires, whichever schedule it is, so it subscribes the script to `cron.*.fired` rather than to each schedule's name. The pattern is kept as sent, `*` and all, and the answer lists it as it lists any subscription. Nothing runs now, and scripts does not ask whether cron, or any service, sends an event the pattern matches.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: subscribe

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"subscribe","arguments":{"name":"nightly-report","event":"cron.*.fired"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_6d1f4a9b2e8c7035","name":"nightly-report","repo":"rep_9c2e4b7a1d3f8e05","ref":"main","created":"2026-09-18T16:40:00Z","subscriptions":[{"event":"cron.*.fired","created":"2026-10-05T09:32:00Z"}],"last_run":{"id":"run_8a2c6e1f9b3d5074","status":"running","started":"2026-10-05T09:31:40Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly. `id`, `name`, `repo`, `ref`, `created`, and `last_run` are as they were.

Preconditions:

- The preamble's: `nightly-report` has no subscription, and the call is made at `2026-10-05T09:32:00Z`.

Postconditions:

- The catalog holds the subscription of `nightly-report` to `cron.*.fired`, created `2026-10-05T09:32:00Z`. `show` with `nightly-report` answers what `subscribe` answered, and `list` gives `nightly-report`'s entry `"subscriptions":1` (`S07`).
- Nothing ran and no run was made: `nightly-report`'s seven runs are as they were, and `run_8a2c6e1f9b3d5074` runs on. No git ran and no repository was read.
- From now on, each event named `cron.hourly.fired`, `cron.tick.fired`, or any other name of three words that begins with the word `cron` and ends with the word `fired`, that the events app delivers to scripts starts a run of `nightly-report`, as `S27` tells; an event named `cron.fired` or `cron.a.b.fired` starts none. Subscribing `nightly-report` to `cron.hourly.fired` as well would add a second subscription, and a `cron.hourly.fired` event would still start one run of it, not two (`S27`).
- The request's `tool.called` has `tool` `subscribe`, `kind` `additive`, and `outcome` `ok`; scripts recorded no `script.*` or `run.*` event, and the name `nightly-report`, its id, and `cron.*.fired` are in none of the request's events.

## A model unsubscribes a script from an event

The model no longer wants `sync-crm` to run on every push. `unsubscribe` removes the subscription and answers the script as it is after the call, here with no subscription left. Only what later deliveries start changes: a run an event already started is a run like any other and is not touched, and nothing is killed, here or ever, by `unsubscribe`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: unsubscribe

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"unsubscribe","arguments":{"name":"sync-crm","event":"repo.pushed"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_a2e7c4f9b1d03856","name":"sync-crm","repo":"rep_41d8f0a6b2c97e13","ref":"main","created":"2026-09-25T10:00:00Z","subscriptions":[],"last_run":{"id":"run_6b2d8f4a0c9e1735","status":"running","started":"2026-10-05T09:31:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `sync-crm` is subscribed to `repo.pushed`.

Postconditions:

- The catalog holds no subscription of `sync-crm`; `list` gives its entry `"subscriptions":0` (`S07`).
- An event named `repo.pushed` delivered to scripts from now on starts no run of `sync-crm` (`S27`). Subscribing it again makes a new subscription, created at the time of that call.
- `run_6b2d8f4a0c9e1735` runs on, and `sync-crm`'s runs and their folders are as they were. No git ran and no repository was read.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"unsubscribe"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  scripts recorded no `script.*` or `run.*` event, and the name `sync-crm`, its id, and `repo.pushed` are in none of them.

## A model unsubscribes a script from an event it is not subscribed to

Unlike a repeated `subscribe`, an `unsubscribe` of a pair that does not exist is refused, so a model that mistyped the event name, or named the wrong script, learns that nothing was removed rather than believing the script will no longer run. `nightly-report` is subscribed to nothing. The same line comes for a script subscribed to other patterns only: `sync-crm` and `crm.contact_added` get `'sync-crm' is not subscribed to 'crm.contact_added'`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: unsubscribe

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"unsubscribe","arguments":{"name":"nightly-report","event":"repo.pushed"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
'nightly-report' is not subscribed to 'repo.pushed'
```

Preconditions:

- The preamble's: `nightly-report` has no subscription.

Postconditions:

- Nothing has changed. `sync-crm`'s subscription to `repo.pushed` is untouched.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"unsubscribe"}}
  ```

## A model unsubscribes a script from a pattern

A pattern is removed as it was made, by sending the same string: `unsubscribe` with `cron.*.fired` removes `nightly-report`'s subscription to `cron.*.fired`, and the answer is the script with no subscription left. As for any `unsubscribe`, a run an event already started is not touched.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: unsubscribe

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"unsubscribe","arguments":{"name":"nightly-report","event":"cron.*.fired"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_6d1f4a9b2e8c7035","name":"nightly-report","repo":"rep_9c2e4b7a1d3f8e05","ref":"main","created":"2026-09-18T16:40:00Z","subscriptions":[],"last_run":{"id":"run_8a2c6e1f9b3d5074","status":"running","started":"2026-10-05T09:31:40Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, and `nightly-report` is subscribed to `cron.*.fired`, created `2026-10-05T09:00:00Z`, and to nothing else.

Postconditions:

- The catalog holds no subscription of `nightly-report`; `list` gives its entry `"subscriptions":0` (`S07`).
- An event named `cron.hourly.fired`, or any other the pattern matched, delivered to scripts from now on starts no run of `nightly-report` (`S27`).
- `run_8a2c6e1f9b3d5074` runs on, and `nightly-report`'s runs and their folders are as they were. No git ran and no repository was read.
- The request's `tool.called` has `tool` `unsubscribe`, `kind` `destructive`, and `outcome` `ok`; scripts recorded no `script.*` or `run.*` event, and the name `nightly-report`, its id, and `cron.*.fired` are in none of the request's events.

## A model unsubscribes a script from a name its pattern matches

A model that wants `nightly-report` to stop running on the hourly schedule might send `cron.hourly.fired`, a name the script's subscription `cron.*.fired` matches. That is not a subscription the script holds, and `unsubscribe` does not narrow a pattern or remove one that matches the name sent, so it is refused with the line any pattern the script does not hold gets, and the model learns that nothing was removed. The converse is refused too: `unsubscribe` with `cron.*.fired` of a script that holds only `cron.hourly.fired` gets `'<name>' is not subscribed to 'cron.*.fired'`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: unsubscribe

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"unsubscribe","arguments":{"name":"nightly-report","event":"cron.hourly.fired"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
'nightly-report' is not subscribed to 'cron.hourly.fired'
```

Preconditions:

- The preamble's, and `nightly-report` is subscribed to `cron.*.fired`, created `2026-10-05T09:00:00Z`, and to nothing else.

Postconditions:

- Nothing has changed. `nightly-report` is still subscribed to `cron.*.fired`, created `2026-10-05T09:00:00Z`, and an event named `cron.hourly.fired` delivered to scripts still starts a run of it (`S27`).
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"unsubscribe"}}
  ```

## A model subscribes another user's script to an event

A subscription runs its script as the script's owner, so only the owner may make one. Another user's script does not exist for the caller: asking for it gets exactly the answer a script that does not exist gets, and `event` is not looked at. `unsubscribe` with `digest` gets the same line, whatever subscriptions `digest` has, and removes none of them.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: subscribe

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"subscribe","arguments":{"name":"digest","event":"repo.pushed"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'digest'
```

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s.

Postconditions:

- Nothing has changed. `digest` has no subscription, and an event named `repo.pushed` starts no run of it.
- The request's `tool.called` has `tool` `subscribe`, `kind` `additive`, and `outcome` `error`, as for a script that does not exist.

## A model unsubscribes a script it does not have

A name among none of the caller's scripts is refused, quoting it as sent, before `event` is looked at, so the answer is the same whether or not `event` is a valid pattern. A name that is not a valid script name, or a script's id in place of its name, gets the same line, and `subscribe` answers each of them the same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: unsubscribe

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"unsubscribe","arguments":{"name":"cleanup","event":"repo.pushed"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'cleanup'
```

Preconditions:

- The preamble's: no script is named `cleanup`.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"unsubscribe"}}
  ```

## A model subscribes a script to a string that is not a pattern

A pattern is held to one rule, and every way of breaking it gets the same line, quoting the value as sent: an upper-case letter, as here; one word only, as in `pushed`, or a bare `*`; a word that is empty or begins with a digit or `_`, as in `.pushed`, `repo..pushed`, `repo.pushed.`, `repo.2fa` or `repo._pushed`; a `_` doubled or at the end of a word, as in `repo.pushed_`; a `*` that is not a whole word, as in `re*po.pushed`, `repo.push*` or `repo.**`; `-`, a space, or any other character outside `a`-`z`, `0`-`9`, `_`, `*` and `.`; and the empty string. Any number of words above one is allowed, so `repo.git.pushed` and `repo.*` are patterns and are accepted. Nothing is trimmed or folded, so ` repo.pushed` and `Repo.Pushed` are refused rather than read as `repo.pushed`. `unsubscribe` holds `event` to the same rule, after the script and before it looks for the subscription, so `unsubscribe` of `sync-crm` and `Repo.Pushed` gets `invalid event 'Repo.Pushed'`, not the not-subscribed line.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: subscribe

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"subscribe","arguments":{"name":"nightly-report","event":"Repo.Pushed"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid event 'Repo.Pushed'
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. `nightly-report` has no subscription.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"subscribe"}}
  ```

## A model calls subscribe without saying which event

`name` and `event` are both required, by `subscribe` and `unsubscribe` alike. A call that leaves one out is refused as its arguments are read, every offence in one answer, before any rule of scripts' is looked at (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: subscribe

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"subscribe","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
event: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`); the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"subscribe"}}
  ```

## A model sends arguments unsubscribe does not take

A field of the wrong JSON type, or one the tool does not have, is refused as the arguments are read, every offence in one answer: the tool's fields first, in the order of its input schema, then each unknown field in the order it was sent (`S05`). Here the model sent `event` as a list of names, which neither tool takes, since each call names exactly one pattern, and tried to name a ref, which a subscription does not have: a run an event starts uses the script's ref (`S27`). `subscribe` refuses the same arguments with the same text.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: unsubscribe

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"unsubscribe","arguments":{"name":"sync-crm","event":["repo.pushed"],"ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
event: expected string, got array
ref: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. `sync-crm` is still subscribed to `repo.pushed`.
- The request's `tool.called` has `tool` `unsubscribe`, `kind` `destructive`, `outcome` `invalid_arguments`, and `duration_us` 0.
