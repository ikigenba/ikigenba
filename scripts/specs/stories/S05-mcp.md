# Stories — mcp endpoint

The scripts offered to models: scripts' MCP interface at `/mcp`, the one route that answers MCP clients rather than browsers. It is the exact path `/mcp`; a path beneath it, `/mcp/tools` say, is a script path whose name is `mcp`, which no script can have (`S06`), and is answered not found, as any path naming none of the caller's scripts (`S12`). `/mcp` takes only POST, each POST carries one JSON-RPC request, and each request is answered with one `application/json` body: there are no sessions and no streams, and scripts keeps nothing from one request to the next. It offers eleven tools and nothing else, in this order: `list`, which lists the caller's scripts (`S07`); `show`, which shows one of them with its repository, its ref, and its last run (`S07`); `create`, which creates a script from one of the caller's repositories and a ref (`S06`); `update`, which changes the ref a script runs from (`S09`); `delete`, which removes a script and every run it has (`S10`); `subscribe`, which makes a script run each time an event of a given name is delivered to scripts (`S26`, `S27`); `unsubscribe`, which undoes that (`S26`); `run`, which starts a run of a script and answers at once, without waiting for it (`S08`); `runs`, which lists a script's runs, newest first (`S11`); `result`, which answers one run whole, its details, its output so far, and the files it wrote (`S11`); and `cancel`, which ends a run that is still running (`S11`). A script's code never passes through a tool: it is a commit of a repository repos holds, pushed with git (repos' `S11-git.md`), and a tool only names which repository and ref a script runs. A run's files are listed by `result` and downloaded from the run's page (`S14`). Each tool's arguments, its result, and its failures are told in its own group; what they share is fixed here.

A script has an id, `scr_` and 16 lowercase hexadecimal digits, which scripts gives it when it is created and which never changes; a name, 1 to 64 characters, each a lowercase ASCII letter, a digit, or `-`, the first a letter or a digit, unique across the space; a repository, the `rep_` id of one of its owner's repositories in repos, kept as given; a ref; and an owner, the `X-User-Id` of the caller who created it. A tool that acts on one script takes it as `name` and looks it up among the caller's own scripts only: to every caller, a script it does not own is indistinguishable from one that does not exist (`no script named '<name>'`), though the name is still taken (`S06`). A run has an id, `run_` and 16 lowercase hexadecimal digits, which scripts gives it when the run is asked for; a tool that acts on one run takes that id as `run` and looks it up among the runs of the caller's own scripts only, so another user's run is indistinguishable from one that does not exist (`no run '<id>'`). Every time in a tool's result is RFC 3339 UTC to the second, `2026-10-05T09:14:02Z` say. A member that does not apply is left out, never sent as `null`.

A script object, as `show`, `create`, `update`, `subscribe`, and `unsubscribe` return it, has these members in this order: `id`, `name`, `repo`, the repository's id, `ref`, `created`, `subscriptions`, and `last_run`, the script's newest run, absent when it has never run. `subscriptions` is always present: the events the script is subscribed to, each with `event`, the event name, and `created`, when the subscription was made, in that order, sorted by event name ascending, `[]` when there is none (`S26`). `last_run` has `id`, `status`, `exit_code`, present only when `status` is `exited`, and `started`, in that order. An entry of `list`'s `scripts` has `id`, `name`, `repo`, `ref`, `subscriptions`, and `last_run`, in that order, under the same rules but for `subscriptions`, which there is the number of the script's subscriptions, `0` when there is none. A script object has no owner member: the owner is always the caller.

A run's `status` is one of five words: `running` while its process lives; `exited`, when the process ended on its own, with its `exit_code`, 128 plus the signal number for a process that died of a signal scripts did not send (`S15`); `killed`, when scripts killed it, on `cancel` (`S11`), on the `delete` of its script (`S10`), at the drain deadline, or when the next start finds it still recorded `running` (`S18`); `timed_out`, when scripts killed it at `SCRIPT_SECONDS` (`S17`); and `failed`, with a `reason`, when the script never started (`S08`). A run, as `result` returns it, has these members in this order: `id`; `script`, the script's id; `sha`, the commit the run resolved, absent when its ref never resolved; `ref`, the ref it was resolved from; `user`, the user id it acts as; `request_id`, the request id it was caused by; `trigger`, `manual` for a run the `run` tool started and `event` for one an event delivered to scripts started (`S27`); `event`, the id of the event that started it, present only when `trigger` is `event`; `status`; `exit_code`, present only when `status` is `exited`; `started`; `finished`, absent while `running`; `stdout_bytes` and `stderr_bytes`, how many bytes of each stream are kept; `truncated`, `true` when either stream was cut at `OUTPUT_MAX_BYTES` (`S17`); and `reason`, present only when `status` is `failed`. `result` follows them with `stdout` and `stderr`, the kept text of each stream so far, `""` when there is none, and `files`, each regular file under the run's `out/` as `path` and `size`; when the run's files are gone those three are absent and `files_gone`, `true`, comes last (`S11`). An entry of `runs`' `runs` has `id`, `sha`, `ref`, `trigger`, `event`, `status`, `exit_code`, `started`, `finished`, `truncated`, and `reason`, in that order, under the same rules, and `cancel` answers the run it ended in that same shape (`S11`). `run` answers `id`, `status`, `sha`, and `reason`, in that order, under the same rules (`S08`).

On a host, a model does not reach `/mcp` directly: it reaches scripts' tools through the mcp gateway, naming the service `scripts` and the tool, and the gateway calls scripts at its socket on the model's behalf (below, and `S24`). scripts' manifest marks it an MCP service (`S01`), so the host's services file carries `"mcp": true` for it and the gateway's `describe` lists its eleven tools with their kinds: `list`, `show`, `runs`, and `result` are of kind `read` and run with the gateway's `call`; `create`, `update`, `subscribe`, and `run` are `additive`, and `delete`, `unsubscribe`, and `cancel` are `destructive`, and those seven run with `mutate`. The actor in this group is a model working through an MCP client, the client itself, or the gateway standing in for either. Each request is shown as the HTTP request scripts receives on a running scripts (`S02`), started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, the host's services file, which holds the suite's services file (`S03`) unless a story says otherwise, and whose `telemetry` entry names the telemetry service, which takes every event (`S02`). `/mcp` is behind the same identity rule as every route of scripts, which serves nothing to guests: nginx sets `X-User-Id` and `X-User-Email` and, on a host with an authenticator, challenges a request with no credential at `/mcp`, never passing it to scripts (opsctl's `S5-nginx.md`); the gateway forwards both headers (`S02`); and a request without `X-User-Id` is answered 500 before anything else is looked at. The requests below carry both headers by hand, and only the missing-header story carries neither. `/mcp` answers the same whatever `Host` names: the requests below carry `Host: scripts.sbx.ikigenba.dev`, as nginx passes it, but for the gateway's, which sends `Host: backend` to every service.

A client speaking the protocol revision `2026-07-28` sends, on every request, the headers `Content-Type: application/json`, `MCP-Protocol-Version: 2026-07-28`, and `Mcp-Method` equal to the request's `method`, and for a `tools/call` also `Mcp-Name` equal to the tool's name; and its `params` carry `_meta` with `io.modelcontextprotocol/protocolVersion` `2026-07-28` and `io.modelcontextprotocol/clientCapabilities` `{}`. Every successful answer on that revision is status 200, and its `result`, besides what each story fixes, carries `resultType` `"complete"` and `_meta` whose `io.modelcontextprotocol/serverInfo` is `{"name":"scripts","version":"v<semver>"}`, where `v<semver>` is the version `scripts --version` prints (`S01`); a `tools/list` or `server/discover` result also carries `ttlMs` `0` and `cacheScope` `"private"`. Those members are not repeated in this group or in `S06` to `S11`. A client speaking an earlier revision, `2025-11-25` or `2025-06-18`, opens with `initialize` and is served the same tools, and its calls get the same tool results, `isError` results included, without those members; a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200 on the earlier revisions. How the transport answers a request that is not well-formed MCP — a wrong `Content-Type`, a body over 1 MiB, a foreign `Origin`, mismatched headers, an unknown method — is the platform's, the same for every app, and this group does not restate it.

A tool's successful result carries the answer twice: as `structuredContent`, a JSON object, and as one text content block whose text is that same object encoded compactly, with no white space between its tokens. A tool that cannot do what it was asked answers status 200 with a result whose `isError` is `true`, with no `structuredContent` and a `content` array of exactly one text block saying why. The text takes one of three forms:

- Arguments refused as they are read against the tool's input schema — a field missing, of the wrong JSON type, or one the tool does not have — are reported with the platform's wording: the line `invalid arguments:` followed by one line per offence, each `<field>: <reason>` (`name: missing required field`, `name: expected string, got number`, `bogus: unknown field`), separated by LF with no LF after the last; the tool's fields come first, in the order of its input schema, then each unknown field in the order it was sent.
- Arguments that pass that reading but that scripts refuses, or a call it cannot carry out as things stand, are refused with one line in scripts' own words, each told in the group of the tool that meets it: `invalid name '<name>'`, `a script named '<name>' already exists`, `no repository '<repo>'`, `invalid ref '<ref>'`, `no script named '<name>'`, `invalid event '<event>'`, `'<name>' is not subscribed to '<event>'`, `no run '<id>'`, `run '<id>' has already ended`, and `scripts is stopping; try again later` (`S18`). `<name>`, `<ref>`, `<repo>`, `<event>`, and `<id>` are quoted exactly as they were sent. No refusal is more than one line.
- Every tool, when scripts cannot read or write its catalog, refuses with exactly `cannot reach the catalog; try again later`, quoting nothing of the underlying error.

A run that cannot start — its ref names no commit, its repository is missing, its tree is too large, git fails or takes too long, or its process cannot be launched — is not a refusal: it is a run, recorded `failed` with its reason, and `run` answers it as a successful result (`S08`). Nothing is created, changed, deleted, started, or killed by a call that is refused; a refused call makes no run and no run folder, runs no script, and records no `script.*` or `run.*` event.

Every request to `/mcp` is recorded in scripts' trail as every request is, by its `request.started` and `request.finished` (`S02`, `S03`). A `tools/call` that reaches one of the eleven tools and is answered with a `result`, an `isError` result included, also records `tool.called`, after anything the tool recorded and before the request's `request.finished`, under the caller's request id and user. Its attributes are `tool`, the tool's name; `kind`, `read` for `list`, `show`, `runs`, and `result`, `additive` for `create`, `update`, `subscribe`, and `run`, and `destructive` for `delete`, `unsubscribe`, and `cancel`; `outcome`, `ok` for a result with no `isError`, a run that failed to start included, `invalid_arguments` when the arguments were refused as they were read against the input schema, and `error` for every other refusal; and `duration_us`, how long the tool took, in whole microseconds, 0 when the arguments were refused as they were read and the tool never ran. The arguments themselves, a run's input, the text of a refusal, and the name of any script are never recorded. A call answered with a protocol error, such as an unknown tool, reached no tool and records no `tool.called`; nor does any other method. A call that creates, changes, or deletes a script also records `script.created`, `script.updated`, or `script.deleted` before its `tool.called`; a `run` records `run.started` when the script's process starts, or, for a run that could not start, `run.finished`, before its `tool.called`; and a `cancel` records the run's `run.finished` before its `tool.called`; each as its group tells (`S06`, `S08`, `S09`, `S10`, `S11`, `S16`). `list`, `show`, `subscribe`, `unsubscribe`, `runs`, and `result` record nothing of their own (`S26`).

A response body below is laid out for reading: its white space is not fixed. The order of members within a tool and within its schemas, and within a tool's result where a story shows one, is fixed as shown; elsewhere the order of members is not. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed. No answer in this group or in `S06` to `S11` earns a line on stderr, the missing-header 500 included; only an event scripts cannot deliver reaches stderr (`S02`).

## An MCP client lists scripts' tools

A client lists the tools before offering them to a model. What it gets is everything the model is told about each tool: its name, a description written for the model, the shape of its arguments and its answer, and how careful the client must be before calling it. The description's first line is a one-line summary a catalogue can show on its own, the same line the landing page shows beside the tool's name (`S03`). `list`, `show`, `runs`, and `result` change nothing, so they are marked read-only; `create`, `update`, `subscribe`, and `run` add a script, a ref, a subscription, or a run, never remove one, and what `create`, `update`, and `subscribe` set can be changed back by calling them again or `unsubscribe`, so they are marked neither read-only nor destructive; `delete` removes a script and every run it has, with their output and files, for good, `unsubscribe` removes a subscription, so events that would have run the script no longer do, and `cancel` kills a running script and ends its run for good, so all three are marked destructive. None is open-world: each acts on the platform's own data; what a running script reaches is the script's own (`S15`). `input` is any JSON object in `run`'s schema; scripts never looks inside it.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `nextCursor` and whose `tools` is an array of exactly eleven tools, in this order:

```
[
  {
    "name": "list",
    "description": "The scripts you own, by name.\n\nTakes no arguments. Each script has its id, name, repo (the id of the repos repository it runs from), ref (the branch, tag, or commit a run resolves), subscriptions (how many events it is subscribed to), and last_run (its newest run's id, status, exit_code when it exited, and started; absent when it has never run). Use show for one script's created time and the events it is subscribed to, and runs for all of its runs.",
    "inputSchema": {"type": "object", "additionalProperties": false},
    "outputSchema": <the list output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "show",
    "description": "One of your scripts, with its repository, its ref and its last run.\n\nPass name, the script's name. The result has its id, name, repo (the id of the repos repository it runs from), ref (the branch, tag, or commit a run resolves), created, subscriptions (each event it is subscribed to, with event and created, sorted by event; empty when none), and last_run (its newest run's id, status, exit_code when it exited, and started); last_run is absent when it has never run.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The script's name."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the script output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "create",
    "description": "Create a script from one of your repositories and a ref.\n\nname is 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, is not about, mcp, events, or declarations, and must not already name a script in the space: names are shared by every user, because a script's page is at its name. repo is the id of one of your repositories in repos. ref is the branch, tag, or commit a run uses, main unless given; it is not resolved until a run, so it may name nothing yet. A run unpacks that commit and runs python3.12 main.py from the repository's root. The script does not run until you call run. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The new script's name: 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, not about, mcp, events, or declarations, and not already a script's name in the space."},
        "repo": {"type": "string", "description": "The id of one of your repositories in repos (rep_ and 16 hexadecimal digits)."},
        "ref": {"type": "string", "description": "The branch, tag, or commit a run uses; main unless given."}
      },
      "required": ["name", "repo"],
      "additionalProperties": false
    },
    "outputSchema": <the script output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "update",
    "description": "Change the ref one of your scripts runs from.\n\nPass name and ref, the branch, tag, or commit every later run resolves, under the rules of create. A run already started keeps the commit it resolved, and the script keeps its subscriptions, whose later runs use the new ref. A script's name and repository never change. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The script's name."},
        "ref": {"type": "string", "description": "The branch, tag, or commit the script runs from now on."}
      },
      "required": ["name", "ref"],
      "additionalProperties": false
    },
    "outputSchema": <the script output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "delete",
    "description": "Delete one of your scripts and every run it has.\n\nPass name. Every run of the script goes with it, its output and files included, and a run still running is killed. Its subscriptions go too, so no event starts it again. Its name is free for anyone to take. The repository and its history stay in repos. The result is the id of the deleted script.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The script's name."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the delete output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  },
  {
    "name": "subscribe",
    "description": "Run one of your scripts each time an event of a given name is delivered.\n\nPass name, the script's name, and event, an exact event name such as repo.pushed: two words joined by one dot, each a lowercase letter followed by lowercase letters, digits, or single underscores between them. It is not a pattern, and it need not be one any service sends yet. Each time the suite's events app delivers an event of that name to scripts, the script runs as run would run it, as you, at its ref, with the whole event as its input; that run's trigger is event and its event is the event's id. Subscribing a script to an event it is already subscribed to changes nothing. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The script's name."},
        "event": {"type": "string", "description": "The exact event name, such as repo.pushed."}
      },
      "required": ["name", "event"],
      "additionalProperties": false
    },
    "outputSchema": <the script output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "unsubscribe",
    "description": "Stop running one of your scripts on an event it is subscribed to.\n\nPass name and event, as subscribe took them. Events of that name delivered later no longer run the script; a run already started is not touched. A script that is not subscribed to that event is refused. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The script's name."},
        "event": {"type": "string", "description": "The exact event name the script is subscribed to."}
      },
      "required": ["name", "event"],
      "additionalProperties": false
    },
    "outputSchema": <the script output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  },
  {
    "name": "run",
    "description": "Start a run of one of your scripts and return its id, status and commit.\n\nPass name, and ref to run a branch, tag, or commit other than the one the script runs from; a ref given here is used for this run only and does not change the script's ref. Pass input, a JSON object, for the script to read from the file its IKIGENBA_INPUT variable names; {} unless given. The ref is resolved and its commit unpacked before the answer; the script then runs on its own, and run never waits for it. The result is the run's id, its status, running, and sha, the commit it runs. A run that could not start is still a run: its status is failed, with reason (repository_missing, commit_missing, too_large, git_failed, timed_out, or start_failed), and sha when the ref resolved. Follow a run with result until its status is final; end it early with cancel.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The script's name."},
        "ref": {"type": "string", "description": "The branch, tag, or commit to run, for this run only; the script's own ref unless given."},
        "input": {"type": "object", "description": "A JSON object the run's script reads as its input; {} unless given."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the run output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "runs",
    "description": "The runs of one of your scripts, newest first.\n\nPass name, the script's name. Each run has its id, sha (the commit it ran, absent when the ref never resolved), ref, trigger (manual, or event when an event started it), event (the id of the event that started it, when trigger is event), status (running, exited, killed, timed_out, or failed), exit_code (when it exited), started, finished (absent while running), truncated (whether output was cut), and reason (why it never started, when it failed). Use result for one run's output and files.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The script's name."}
      },
      "required": ["name"],
      "additionalProperties": false
    },
    "outputSchema": <the runs output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "result",
    "description": "One run whole: its details, its output so far, and the files it wrote.\n\nPass run, the run's id. The result has its id, script (the script's id), sha, ref, user, request_id, trigger (manual, or event when an event started it), event (the event's id, when trigger is event), status, exit_code (when it exited), started, finished (absent while running), stdout_bytes and stderr_bytes (how much of each is kept), truncated, and reason (when it failed), then stdout and stderr, the output kept so far, and files, each file the script wrote under its out folder, with its path and size. While status is running, call result again until it is final. When the run's files are gone, stdout, stderr, and files are absent and files_gone is true.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "run": {"type": "string", "description": "The run's id (run_ and 16 hexadecimal digits)."}
      },
      "required": ["run"],
      "additionalProperties": false
    },
    "outputSchema": <the result output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "cancel",
    "description": "End one of your runs that is still running.\n\nPass run, the run's id. The script's process group is killed whole, and the run is recorded killed; what it wrote so far is kept. A run that has already ended is refused. The result is the run as runs lists it, with its final status and finished.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "run": {"type": "string", "description": "The run's id (run_ and 16 hexadecimal digits)."}
      },
      "required": ["run"],
      "additionalProperties": false
    },
    "outputSchema": <the run entry output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  }
]
```

The output schemas are not quoted whole; each describes an object closed to other members, with its properties in the order given here, and each object it holds is closed to other members too. The script output schema, which `show`, `create`, `update`, `subscribe`, and `unsubscribe` all carry, describes one script: `id`, a string; `name`, a string; `repo`, a string; `ref`, a string; `created`, a string; `subscriptions`, an array of objects with `event`, a string, and `created`, a string; and `last_run`, an object, not always present, with `id`, a string; `status`, a string; `exit_code`, an integer, not always present; and `started`, a string. `list`'s has one property, `scripts`, an array of objects with `id`, `name`, `repo`, `ref`, `subscriptions`, an integer, and `last_run`, as in the script output schema. `delete`'s has `deleted`, a boolean, and `id`, a string. `run`'s has `id`, a string; `status`, a string; `sha`, a string, not always present; and `reason`, a string, not always present. The run entry output schema, which `cancel` carries, describes one run as `runs` lists it: `id`, a string; `sha`, a string, not always present; `ref`, a string; `trigger`, a string; `event`, a string, not always present; `status`, a string; `exit_code`, an integer, not always present; `started`, a string; `finished`, a string, not always present; `truncated`, a boolean; and `reason`, a string, not always present. `runs`' has one property, `runs`, an array of such run entries. `result`'s has `id`, `script`, `sha`, `ref`, `user`, `request_id`, `trigger`, `event`, `status`, `exit_code`, `started`, `finished`, `stdout_bytes`, `stderr_bytes`, `truncated`, `reason`, `stdout`, `stderr`, `files`, and `files_gone`, where `exit_code`, `stdout_bytes`, and `stderr_bytes` are integers, `truncated` and `files_gone` booleans, `files` an array of objects with `path`, a string, and `size`, an integer, and the rest strings; `sha`, `event`, `exit_code`, `finished`, `reason`, `stdout`, `stderr`, `files`, and `files_gone` are not always present. Which members each output schema marks required, and which carry a description, are not fixed here. The schemas carry no `$schema` member.

Preconditions:

- scripts is serving, and telemetry takes every event.

Postconditions:

- Nothing has changed. No catalog entry and no repository was read, no git ran, and no script ran.
- scripts wrote nothing to stderr. telemetry has received the request's two events, under user `u_7f3a9c21` and the id scripts gave the request, and no `tool.called`, since no tool ran:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `request_bytes` is the length of the request's body and `response_bytes` the length of the response's.

## A client asks scripts what it is for

A client tells its model what each server is for through the server's instructions. scripts' instructions are its own description, as the host's services file gives it, so they are written once, in scripts' manifest (`S01`), and never anywhere else. scripts reads the file afresh for every request that asks, as it does for the launcher (`S03`), so a rewrite of the file shows in the next answer without a restart.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":2,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly these members besides the `resultType`, `_meta`, `ttlMs`, and `cacheScope` every such result carries:

```
{
  "supportedVersions": ["2026-07-28", "2025-11-25", "2025-06-18"],
  "capabilities": {"tools": {}},
  "instructions": "Python scripts run from the suite's repositories"
}
```

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file, whose entry named `scripts` has the description `Python scripts run from the suite's repositories`.

Postconditions:

- Nothing has changed. The services file is as it was.
- scripts wrote nothing to stderr.

## A client asks scripts what it is for on a host with no services file

With no services file there is no description to give, so the answer has no instructions at all rather than empty ones, and is otherwise the same. The answer is the same when the file named is missing, cannot be read, or has no entry named `scripts`. The tools are offered all the same.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":3,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `supportedVersions` `["2026-07-28","2025-11-25","2025-06-18"]`, `capabilities` `{"tools":{}}`, the members every such result carries, and no `instructions` member.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- scripts wrote nothing to stderr about the missing instructions. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as a `scripts: undelivered event: <event>` line, and nothing else for this request.

## A client speaking an earlier revision opens with initialize

Many clients in the field still speak `2025-11-25` or `2025-06-18`, which open with `initialize` and send no `_meta`. scripts serves them as every app does: `initialize` echoes the revision the client asked for when it is one of those two, and answers `2025-11-25` otherwise. The instructions are the ones `server/discover` gives. scripts keeps no session, so the client's following `notifications/initialized` is answered `202` with an empty body, and the client may list or call tools with or without having sent `initialize`.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"example-client","version":"1.0.0"}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has exactly these members:

```
{
  "protocolVersion": "2025-11-25",
  "capabilities": {"tools": {}},
  "serverInfo": {"name": "scripts", "version": "v<semver>"},
  "instructions": "Python scripts run from the suite's repositories"
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- scripts is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.
- scripts wrote nothing to stderr.

## A client speaking an earlier revision lists the tools

A client on an earlier revision is offered the same eleven tools, with the same names, descriptions, schemas, and annotations, and its calls get the same tool results, `isError` results included, as on `2026-07-28`. Only the envelope differs: results carry no `resultType`, `_meta`, `ttlMs`, or `cacheScope`, and a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200, not 400.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-11-25

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-06-18

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly one member, `tools`, the array of `An MCP client lists scripts' tools`, member for member.

Preconditions:

- scripts is serving.

Postconditions:

- Nothing has changed.
- scripts wrote nothing to stderr.

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client hoping for a stream, is refused with an empty body: scripts offers no stream and no page at this address, and its catalog of scripts is at `/` (`S03`). A DELETE, from a client ending a session it believes it has, is refused the same way, since there are no sessions, and so is any other method that is not POST.

Request:

```
GET /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: POST
```

Status 405. The body is empty.

Preconditions:

- scripts is serving.

Postconditions:

- Nothing has changed.
- scripts wrote nothing to stderr.

## A request to /mcp arrives without the identity headers

`/mcp` follows the rule every route of scripts follows (`S03`): nginx sets `X-User-Id` on every request it forwards and the gateway forwards the one it received, so a request without it says nginx or the gateway is misconfigured, a server fault answered 500. The identity check runs before anything about MCP is looked at, so the answer is the same plain text every route gives, not a JSON-RPC response, and no tool runs, whatever the body asked for. Without a caller there is no owner to look a script up for and no user for a run to act as. An `X-User-Id` header whose value is empty is answered the same way.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending with LF. A `GET /mcp` with no identity is answered the same way, not with the 405 of `A browser opens /mcp`.

Preconditions:

- scripts is serving, and telemetry takes every event.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.

Postconditions:

- Nothing has changed. No run was made, nothing was added under `state/runs/`, no repository was read, no git ran, and no script ran.
- scripts wrote nothing to stderr about the 500. telemetry has received the request's two events, with an empty user, under the id scripts gave the request (`S02`); no tool ran, so there is no `tool.called` and no `run.*` event:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A model calls a tool scripts does not have

scripts has eleven tools. A call naming any other — a tool to rename a script, say, which scripts does not offer, since `update` changes only the ref (`S09`) — is not a tool's refusal but a protocol error: there is no tool to answer it.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"rename","arguments":{"name":"nightly-report","new_name":"daily-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 5 and no `result`, whose `error` has `code` `-32602` and `message` `Unknown tool: rename`.

Preconditions:

- scripts is serving, and telemetry takes every event.
- The caller owns the script `nightly-report`, `scr_6d1f4a9b2e8c7035`.

Postconditions:

- Nothing has changed. `nightly-report` is as it was, its runs and their folders under `state/runs/scr_6d1f4a9b2e8c7035/` are untouched, no git ran, and no script ran.
- scripts wrote nothing to stderr. No tool ran, so scripts recorded no `tool.called`: telemetry has received only the request's `request.started` and its `request.finished`, whose `status` is 400, under user `u_7f3a9c21`.

## A model calls a tool while scripts cannot reach its catalog

Every tool answers from the catalog, so a tool that cannot read it has nothing true to say. It does not answer as if the caller had no scripts, which would tell the model its work was gone; it refuses, in the same words whichever of the eleven tools was called, and quotes nothing of the database's own error. The model can tell this from a refusal of its arguments and may try again later. scripts keeps serving; a page opened meanwhile is answered as `S03` tells.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cannot reach the catalog; try again later
```

A `show`, `create`, `update`, `delete`, `subscribe`, `unsubscribe`, `run`, `runs`, `result`, or `cancel` call whose arguments the tool would otherwise act on is refused with the same text, and a `create`, `update`, `delete`, `subscribe`, `unsubscribe`, `run`, or `cancel` so refused has changed nothing, in the catalog or under `state/runs/`: no run is made, no git runs, no script starts, and no running script is killed. A call the tool refuses whatever the catalog holds — arguments refused as they are read against the input schema, a name that breaks the naming rule — is refused as its own group says. A `subscribe` or `unsubscribe` with an `event` that is not an event name is refused with `invalid event '<event>'` only once its script has been found, so with the catalog out of reach it too gets the catalog line.

Preconditions:

- scripts is serving, and telemetry takes every event.
- scripts' database cannot be read: `state/scripts.db` has become unreadable since scripts opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed.
- scripts wrote nothing to stderr. telemetry has received the request's three events, under user `u_7f3a9c21` and request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"list"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- scripts is still serving.

## The mcp gateway calls scripts over its socket

The platform's MCP gateway offers the tools of every service whose entry in the services file is marked for MCP, and scripts' is. A model asks the gateway to `call` the service `scripts` and a read tool, or to `mutate` with any other, and the gateway calls scripts directly on its socket, not through nginx, on behalf of the caller it is serving: it speaks `2026-07-28`, names no service in `Host`, sending `Host: backend` as it does to every service, and forwards that caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`, as any sibling does (`S02`). scripts answers the gateway exactly as it answers a client through nginx; it cannot tell the two apart and does not try, and the caller the gateway forwards is the owner every tool works for. Here a developer on the host, as the `ikigenba` user, stands in for the gateway's `call` of `list`; what the gateway does with the answer is the gateway's, told in its own stories.

Request:

```
POST /mcp HTTP/1.1
Host: backend
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"scripts":[{"id":"scr_e8f2a6c0d4b19357","name":"backfill","repo":"rep_0f6a2d9e8c4b7153","ref":"main","subscriptions":0},{"id":"scr_6d1f4a9b2e8c7035","name":"nightly-report","repo":"rep_9c2e4b7a1d3f8e05","ref":"main","subscriptions":0,"last_run":{"id":"run_8a2c6e1f9b3d5074","status":"running","started":"2026-10-05T09:31:40Z"}},{"id":"scr_5c9b1e3a7f2d4068","name":"rotate-keys","repo":"rep_7b3e9a0c5d1f2846","ref":"release","subscriptions":0,"last_run":{"id":"run_1e9c3a7f5b0d2864","status":"failed","started":"2026-10-04T22:00:00Z"}},{"id":"scr_a2e7c4f9b1d03856","name":"sync-crm","repo":"rep_41d8f0a6b2c97e13","ref":"main","subscriptions":0,"last_run":{"id":"run_6b2d8f4a0c9e1735","status":"running","started":"2026-10-05T09:31:00Z"}}]}
```

and a `content` array of one text block whose text is exactly that line, as `S07` tells. `backfill` has no `last_run`, since it has never run, and its `repo` is given though that repository is gone from repos: `list` reads no repository. `ann@example.com`'s script `digest` is not in it.

Preconditions:

- scripts `v<semver>` is deployed and active on the host, serving on `/run/ikigenba/scripts.sock`.
- The host's services file lists scripts with `"mcp": true`, the `url` `https://scripts.sbx.ikigenba.dev`, and the socket `/run/ikigenba/scripts.sock`, and lists the telemetry service, which takes every event.
- The caller runs as the `ikigenba` user, which can reach the socket.
- The catalog holds `S06`'s shared catalog: `u_7f3a9c21` owns `backfill`, `nightly-report`, `rotate-keys`, and `sync-crm`, and `u_2b8e1d04` owns `digest`; `nightly-report` and `sync-crm` each have a run still `running`; no script is subscribed to any event.

Postconditions:

- Nothing has changed. No repository was read, no git ran, and no script started; the two running runs run on.
- scripts wrote nothing to stderr.
- telemetry has received scripts' three events for the call under the id the developer sent and the user it sent, as the gateway would forward them, so a trace of that id shows the gateway's forward and scripts' execution together:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No script's name is in them.
