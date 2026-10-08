# Stories — declarations

How events knows what each service emits and accepts. Every service on the bus serves its declaration at `GET /declarations` on its own socket: a JSON object whose `emits` lists each event name the service emits with the attribute names it declares for it, and whose `accepts` lists the event names it takes deliveries of, `*` standing for every event. Each `emits` element's `event`, and each `accepts` element but a bare `*`, is an event name, in the shape `S07` gives, or a pattern: a name of two or more words in which one or more words are exactly `*`, a `*` word matching exactly one word, so that `cron.*.fired` matches `cron.hourly.fired` but neither `cron.fired` nor `cron.hourly.daily.fired`. A bare `*` in `accepts` keeps its meaning, every event, and is not a pattern. A word that holds `*` beside other characters, as `cron.h*.fired` does, makes neither a name nor a pattern, so an answer holding one is not a declaration. A service declares that it emits a name when an `emits` element's `event` equals the name or is a pattern that matches it. A pattern in `accepts` is held with the rest of the declaration, and `catalog` lists the service under that pattern's entry when some service declares that it emits that pattern (`S08`). events reads the services file `IKIGENBA_SERVICES` names, afresh each time, and asks every service the file marks `"enabled": true` for its declaration, on the socket the file gives for it; a service the file marks `"enabled": false` is not asked. It asks them all when it starts, before it tells systemd it is ready (`S02`), and again every `EVENTS_DECLARATIONS_SECONDS` seconds, 60 by default. It keeps each answer as that service's declaration, in its database, so a declaration outlives a restart of events. A service the file no longer enables, or no longer lists, is dropped at the next ask, the one at start included: from then it declares nothing, as if events had never heard from it. A services file that cannot be read is another matter: at start, with no services file, nothing is declared; at a later refresh it counts as no answer, so every held declaration stays and no subscriber becomes gone because of it. It also asks a service at once when an event arrives at `/emit` (`S07`) whose `service` is that service and whose `event` is a name the declaration events holds for it does not declare that it emits, and judges the event by the answer, so a producer deployed with a new event since events last asked has its first event of that name accepted, not refused. A service that answers nothing — nothing answers on its socket, it answers that it has no such path, it answers with an error, or what it answers is not a declaration — keeps the declaration events last had from it; a service events has never had a declaration from declares nothing, so it emits nothing events accepts and accepts nothing. What events holds is what `catalog` (`S08`) answers, what `/emit` judges an event by (`S07`), and whom events delivers to (`S10`, `S11`). Each ask is a call to a sibling, which events' trail records as `sibling.called` (`S14`). Asking writes nothing to stdout or stderr and changes nothing in events' log.

events is serving on the host (`S02`) with every setting at its default, and it is `2026-10-05T09:32:00Z`. Unless a story says otherwise, the services file, `/run/ikigenba/services.json`, enables `repos`, on `/run/ikigenba/repos.sock`, `scripts`, on `/run/ikigenba/scripts.sock`, and `sites`, a service the stories suppose, on `/run/ikigenba/sites.sock`, beside services that declare nothing; events has asked all three since it started and holds their answers, `repos`':

```
{"emits":[{"event":"repo.pushed","attrs":["repo","ref","old","new"]}],"accepts":[]}
```

`scripts`':

```
{"emits":[],"accepts":["*"]}
```

and `sites`':

```
{"emits":[{"event":"site.published","attrs":["repo","sha"]}],"accepts":["repo.pushed"]}
```

events' log holds `S09`'s eight events, `seq` 4175 to 4182. The emits below are made on events' socket as `S07` makes them, by a developer, as the `ikigenba` user, standing in for the service the event names.

## events asks each enabled service what it declares

events learns the bus's shape from the services themselves: what each emits and accepts is what the service answers with, never configured in events. Here it asks `repos`, as it asks every enabled service, and `repos` answers with its declaration.

Request:

```
GET /declarations HTTP/1.1
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is `repos`' declaration, `{"emits":[{"event":"repo.pushed","attrs":["repo","ref","old","new"]}],"accepts":[]}`.

Preconditions:

- The preamble's.
- `repos` answers `GET /declarations` on `/run/ikigenba/repos.sock` with the declaration above.

Postconditions:

- events holds `repos`' declaration: `catalog` (`S08`) lists `repo.pushed` as emitted by `repos` with the attribute names `repo`, `ref`, `old`, and `new`, and an event `repos` emits named `repo.pushed` is accepted (`S07`).
- events asks `repos` again `EVENTS_DECLARATIONS_SECONDS` seconds later, and every `EVENTS_DECLARATIONS_SECONDS` seconds after that while it serves.
- Nothing has changed in events' log.

## events holds a declaration whose names are patterns

`cron`, a service the story supposes, emits an event each time one of its triggers is created, paused, resumed, deleted, or fires, the event's name holding the trigger's slug, `cron.hourly.fired` when the trigger `hourly` fires, and cannot list in advance every trigger a user will make. It declares the names it emits as five patterns, `cron.*.created`, `cron.*.paused`, `cron.*.resumed`, `cron.*.deleted`, and `cron.*.fired`, and events holds the patterns as it holds any declaration.

Request:

```
GET /declarations HTTP/1.1
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is `cron`'s declaration, `{"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]}],"accepts":[]}`.

Preconditions:

- The preamble's, and the services file also enables `cron`, on `/run/ikigenba/cron.sock`, which answers `GET /declarations` with the declaration above. The log holds no event from `cron`.

Postconditions:

- events holds `cron`'s declaration: a `catalog` call (`S08`) with `{"service":"cron"}` answers

  ```
  {"events":[
    {"event":"cron.*.created","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
    {"event":"cron.*.deleted","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
    {"event":"cron.*.fired","emits":[{"service":"cron","attrs":["trigger","when","scheduled"]}],"accepts":["scripts"],"count":0},
    {"event":"cron.*.paused","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
    {"event":"cron.*.resumed","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0}]}
  ```

  one entry per pattern, in byte order, `scripts`, which accepts every event, listed under each; and an event `cron` emits named `cron.hourly.fired` is accepted (`S07`).
- Nothing has changed in events' log.

## events learns a changed declaration at its next refresh

A new release of `sites` also emits `site.unpublished`, with the attribute name `repo`. events does not hear of the deploy; it learns of the new name the next time it asks, within `EVENTS_DECLARATIONS_SECONDS` seconds, and from then the catalog lists it, with a count of 0, before any `site.unpublished` has been emitted.

Request:

```
GET /declarations HTTP/1.1
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is `sites`' new declaration, `{"emits":[{"event":"site.published","attrs":["repo","sha"]},{"event":"site.unpublished","attrs":["repo"]}],"accepts":["repo.pushed"]}`.

Preconditions:

- The preamble's, but `sites` has since been deployed again and now answers `GET /declarations` with the new declaration above. No `site.unpublished` has been emitted.

Postconditions:

- Within `EVENTS_DECLARATIONS_SECONDS` seconds of the deploy, 60 here, events has asked `sites` and holds its new declaration. A `catalog` call (`S08`) made after that lists `site.unpublished`, emitted by `sites` alone with the attribute name `repo`, accepted by `scripts`, which accepts every event, with a count of 0 and no `last_seen`; `site.published` and `repo.pushed` are listed as before. With `EVENTS_DECLARATIONS_SECONDS=10` in events' environment, the same is true within 10 seconds.
- Nothing has changed in events' log.

## A restarted events still knows what a silent service declared

events asks every enabled service before it reports ready, and keeps what it learns in its database. When the host restarts events while `repos` is down, `repos` answers nothing at that first ask, and events still holds what `repos` declared before the restart; the push `repos` emits once it is back, from the queue it kept meanwhile, is accepted, not refused as undeclared.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_5b9e3d7a1f4c2068","time":"2026-10-05T09:36:21.604187Z","service":"repos","event":"repo.pushed","request_id":"8e2c6a0f4b9d1735c7e1a5f9d3b0c286","user":"u_7f3a9c21","attrs":{"new":"656cf2d133187c8df95247f2866028de71159b42","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's, and then the host restarted events at `2026-10-05T09:35:00Z` while nothing answered on `/run/ikigenba/repos.sock`; `repos` has answered nothing since.

Postconditions:

- At its start, before it told systemd it was ready, events asked `repos`, `scripts`, and `sites` for their declarations, and had no answer from `repos`.
- events' log holds the event with `seq` 4183, as `S07` tells for an event it accepts, and events still holds `repos`' declaration from before the restart: `catalog` (`S08`) lists `repo.pushed` as emitted by `repos`, with a count of 8.
- events wrote nothing to stderr.

## A freshly deployed producer emits its first event of a new name

`sites` has just been deployed with `site.unpublished`, and a user takes a site down before events' next refresh. The declaration events holds for `sites` does not list `site.unpublished`, so events asks `sites` at once, before it answers the emit, finds the name declared, and accepts the event.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_7ff4adfa36d08401","time":"2026-10-05T09:33:10.418226Z","service":"sites","event":"site.unpublished","request_id":"aef7ac86d66c7a644fb12853ef86dcfa","user":"u_7f3a9c21","attrs":{"repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's, but `sites` has since been deployed again and answers `GET /declarations` with `{"emits":[{"event":"site.published","attrs":["repo","sha"]},{"event":"site.unpublished","attrs":["repo"]}],"accepts":["repo.pushed"]}`; events has not asked it since the deploy.

Postconditions:

- Between the post and the answer, events asked `sites` for its declaration, once, and now holds the new one.
- events' log holds the event with `seq` 4183, as `S07` tells for an event it accepts; `catalog` (`S08`) lists `site.unpublished` with a count of 1.

## A service that does not answer keeps what it last declared

`repos` is stopped for a while, and nothing answers on its socket when events next asks. events does not forget what `repos` declared: a service that is down is not a service that has stopped emitting, and its events, queued while it was down or emitted once it is back, are judged by what it last declared.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is the catalog of `S08`'s `An agent reads the catalog of the retained log`, unchanged: `repo.pushed` is still emitted by `repos`, with the attribute names `repo`, `ref`, `old`, and `new`, with a count of 7.

Preconditions:

- The preamble's, but nothing answers on `/run/ikigenba/repos.sock`, and events has asked `repos` at least once since then and had no answer.
- The call is made by an agent through the MCP gateway, as `S08` makes it.

Postconditions:

- Nothing has changed. events still holds the declaration it last had from `repos`, and goes on asking `repos` every `EVENTS_DECLARATIONS_SECONDS` seconds; once `repos` answers again, events holds what it answers.

## A service that has no declarations path keeps what it last declared

`repos` has been rolled back to a release that serves no declaration, and answers `GET /declarations` with 404. That is no answer, so events keeps what `repos` last declared, and a push `repos` emits is accepted. events does not ask `repos` again before answering, since the declaration it holds lists `repo.pushed`.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_1f4a8c2e6b9d0357","time":"2026-10-05T09:35:44.870115Z","service":"repos","event":"repo.pushed","request_id":"d5b9e3a17c0f4628b4e8a2c6f0d3b719","user":"u_1e9b4d07","attrs":{"new":"023c39c200661fccd268a29a0d347301ef56e64d","old":"3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's, but `repos` now answers `GET /declarations` with `404 Not Found`, and events has asked it at least once since then.

Postconditions:

- events' log holds the event with `seq` 4183, as `S07` tells for an event it accepts.
- events still holds the declaration it last had from `repos`: `catalog` (`S08`) lists `repo.pushed` as emitted by `repos`, as before, with a count of 8.

## A service that answers with an error keeps what it last declared

`repos` is in trouble and answers `GET /declarations` with 500. An error is no declaration, so events keeps what `repos` last declared, and asks again at the next refresh. An answer of 200 whose body is not a declaration — not JSON, `{"emits":"repo.pushed"}`, an `emits` naming `Repo.Pushed`, or one naming `cron.h*.fired` — is no declaration either, and is treated the same way.

Request:

```
GET /declarations HTTP/1.1
```

Response:

```
HTTP/1.1 500 Internal Server Error
```

Status 500. The body is not fixed.

Preconditions:

- The preamble's, but `repos` now answers `GET /declarations` with 500.

Postconditions:

- events still holds the declaration it last had from `repos`: `catalog` (`S08`) lists `repo.pushed` as emitted by `repos`, as before, and an event `repos` emits named `repo.pushed` is accepted (`S07`).
- Nothing has changed in events' log.

## A service events has never heard from declares nothing

`mail`, a service the story supposes, has just been added to the services file, but nothing has answered on its socket since: events has never had a declaration from it. `mail` therefore emits nothing events accepts, and accepts nothing. An event it emits is refused, after events has asked it once more and again had no answer.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_0e7c3a9f5b1d2864","time":"2026-10-05T09:34:02.551873Z","service":"mail","event":"mail.sent","request_id":"ea21818b904ce6087413e3b266f850f1","user":"u_7f3a9c21","attrs":{},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 422 Unprocessable Entity
```

Status 422. The body is empty.

Preconditions:

- The preamble's, but `mail` was added to the services file, enabled, on `/run/ikigenba/mail.sock`, after events started, and nothing has answered on that socket since; events holds no declaration from `mail`, and its log holds no event from `mail`.

Postconditions:

- Between the post and the answer, events tried to ask `mail` for its declaration, once.
- Nothing was stored: events' log still ends at `seq` 4182, and a `search` (`S09`) with `{"services":["mail"]}` answers `{"records":[]}`. events recorded no `event.accepted` (`S14`).
- `catalog` (`S08`) names `mail` nowhere, neither as emitting nor as accepting any event.
- events wrote nothing to stderr.

## A service the services file does not list emits an event

The producer of an event is the service its `service` names. A name the services file does not list is a service events has never heard from: it declares nothing, there is no socket to ask, and the event is refused.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_9c1e5a3f7d2b0846","time":"2026-10-05T09:34:30.120044Z","service":"widgets","event":"widget.created","request_id":"","user":"","attrs":{},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 422 Unprocessable Entity
```

Status 422. The body is empty.

Preconditions:

- The preamble's: the services file has no entry named `widgets`.

Postconditions:

- Nothing was stored: events' log still ends at `seq` 4182. events asked no service for a declaration, and recorded no `event.accepted` (`S14`).
- events wrote nothing to stderr.

## A service that is switched off emits an event

The operator switches `sites` off: its entry in the services file now says `"enabled": false`. At its next ask, whether a refresh or the one at its start, events does not ask `sites`, and drops what `sites` declared, so `sites` declares nothing: what it emits is refused, and it accepts nothing. An entry taken out of the services file altogether is dropped the same way.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_8d4a0e6c2f9b1357","time":"2026-10-05T09:37:15.902433Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_7b3e9a0c5d1f2846","sha":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4"},"cause":"evt_8c3f1a6e2d9b4075","depth":1}
```

Response:

```
HTTP/1.1 422 Unprocessable Entity
```

Status 422. The body is empty.

Preconditions:

- The preamble's, but `sites`' entry in the services file has since been set to `"enabled": false`, and events has asked for declarations since the change, at a refresh or at its start.

Postconditions:

- events did not ask `sites` for its declaration, before or after the post. Nothing was stored: the log still ends at `seq` 4182, and events recorded no `event.accepted` (`S14`).
- `catalog` (`S08`) lists `repo.pushed` as accepted by `scripts` alone, and `site.published`, which no enabled service declares but the log still holds as 4177, with an empty `emits`.
- `sites`, no longer enabled, is a `gone` subscriber (`S10`).
- events wrote nothing to stderr.
