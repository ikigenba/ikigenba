# Stories — retention

How long the trail keeps a record. telemetry keeps a record for `RETENTION_DAYS` days, a positive whole number read from its environment, and 15 when that is unset or empty; a value that is not a positive whole number of days is a usage error found at start (`S02-serve.md`). On a host the value comes from the manifest's `[env]` table (`S01-bootstrap.md`), which opsctl writes into `/etc/opt/ikigenba/telemetry/env` as it writes auth's `WORKSPACE_DOMAIN`; a developer sets it in the environment they run the binary with. A sweep deletes every record whose `time` is more than `RETENTION_DAYS` days before the moment of the sweep, whatever service it came from, telemetry's own records included (`S12-own-events.md`). telemetry sweeps once when it starts, before it reports ready, and then once every hour for as long as it runs; nothing else deletes a record, and nothing a sibling posts is refused for its age (`S06-ingest.md`). A swept record is gone from every tool at once: `search` and `trace` no longer answer it, `count` no longer counts it, and `catalog` no longer lists its service, event, or attribute keys unless a kept record carries them. Sweeping writes nothing to stdout or stderr: it is routine, not trouble. The actor is the host, or a developer at a terminal standing in for it, and an agent that reads the trail afterwards. The tool calls named in postconditions are made to telemetry's `/mcp` as `S09-search.md`, `S10-count.md`, and `S08-catalog.md` make them; the one shown as a request carries the identity headers and the `2026-07-28` headers and `_meta` `S05-mcp.md` fixes, and the members every result carries on that revision are not repeated.

## The host starts telemetry and old records are swept

The database holds records from before the window; telemetry's first act on starting is to sweep them, so a telemetry that was stopped for a while never serves a stale trail.

Command:

```
$ telemetry
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `RETENTION_DAYS` is unset, so the window is 15 days.
- Today is `2026-10-02`. `state/telemetry.db` holds records of several services; those from `dummy` are exactly 40 whose `time` is in August 2026 and 25 whose `time` is on or after `2026-09-20`, and `dummy` posts no other before the tool calls below.

Postconditions:

- telemetry is serving on the socket it was passed.
- The 40 August records are gone and the 25 later ones remain: `count` with `services` `["dummy"]` answers `{"total":25}`, `search` with `services` `["dummy"]` and `until` `"2026-09-01T00:00:00Z"` answers `{"records":[]}`, and `catalog` lists for `dummy` only the events and attribute keys the 25 kept records carry.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.

## The host gives telemetry a shorter window

telemetry reads `RETENTION_DAYS` from its environment (`S02-serve.md`). The manifest's `[env]` declares its default, `15` (`S01-bootstrap.md`), which the host writes into `/etc/opt/ikigenba/telemetry/env`; an operator who wants a shorter trail changes the value there and restarts telemetry, and a developer sets it on the command line. The sweep at start and every hourly sweep then use that window.

Command:

```
$ RETENTION_DAYS=2 telemetry
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- telemetry starts at `2026-10-02T14:00:00Z`, so the window reaches back to `2026-09-30T14:00:00Z`. `state/telemetry.db` holds records of several services; those from `dummy` are exactly 10 whose `time` is on `2026-09-29` and 5 whose `time` is on `2026-10-01`, and `dummy` posts no other before the `count` below.
- The `count` below is made before `2026-10-02T15:00:00Z`, within the hour of the start.

Postconditions:

- telemetry is serving on the socket it was passed.
- The 10 records of `2026-09-29` are gone and the 5 of `2026-10-01` remain: `count` with `services` `["dummy"]` answers `{"total":5}`.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.

## A record ages out while telemetry runs

A record that was inside the window when telemetry started crosses it later. The hourly sweep removes it, so within an hour of its aging out it is gone from every tool, without a restart and without a line on stderr. The agent here searches for it after that hour.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: search

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"request_id":"8c4d1e2f3a5b4c6d9e0f1a2b3c4d5e6f"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of `{"records":[]}`, and a `content` array of one text block whose text is exactly that line (`S09-search.md`).

Preconditions:

- telemetry has been serving since `2026-10-02T14:00:00Z` with `RETENTION_DAYS` unset, so the window is 15 days.
- The trail held one record with request id `8c4d1e2f3a5b4c6d9e0f1a2b3c4d5e6f`, whose `time` is `2026-09-17T15:30:00.000000Z`: inside the window at start, and outside it from `2026-10-02T15:30:00Z`.
- It is after `2026-10-02T16:30:00Z`: at least one hourly sweep has run since the record aged out.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's own records of the request (`S12`). The record was removed by the sweep, not by the search: a `trace` of `8c4d1e2f3a5b4c6d9e0f1a2b3c4d5e6f` (`S11-trace.md`) answers `{"records":[]}` too, and `count` with that `request_id` answers `{"total":0}`.
- telemetry has written nothing to stdout or stderr since it started.
