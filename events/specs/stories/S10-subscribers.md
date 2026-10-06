# Stories — subscribers

`subscribers`, the read tool that tells an agent where each subscriber is in the log. A subscriber is a service whose declaration (`S06`) accepts at least one event name; events keeps one place in the log for each, and delivers to it from there (`S11`). The tool takes no arguments, and its result is `{"subscribers":[...]}`, one entry per subscriber, sorted by service name, each with these members in this order: `service`, the service's name as the services file gives it; `status`, exactly one of `ok`, `paused`, and `gone`; `cursor`, the `seq` of the last event it finished; `lag`, the number of accepted events it has not finished, the head of the log's `seq` minus its `cursor`; `since`, the moment its current status began; and, only while it is `paused`, `reason`, `{"event":"<id>","name":"<event name>","seq":<n>,"error":"<text>"}`: the id, name and `seq` of the event it is stuck on and the error text of that event's last attempt. A subscriber's `cursor` moves past every event it does not accept as well as every event it finishes, so its `lag` counts only events still ahead of it, accepted by it or not. A subscriber appears when events first sees its declaration accept something, and starts at the head of the log: its `cursor` is then the head's `seq` and its `lag` 0, and no event accepted before it appeared is ever delivered to it. A subscriber is `ok` while events delivers to it, `paused` when one event has failed every attempt (`S11`) until an agent skips or resumes it (`S12`), and `gone` once its service is no longer enabled in the services file or its declaration accepts nothing; a `gone` subscriber is delivered nothing and keeps its `cursor`. When the service of a `gone` subscriber is enabled again and accepts something again, the subscriber is `ok` again and resumes after its `cursor` if every event after it is still retained (`S13`), and from the head of the log otherwise. `subscribers` changes nothing, and it is of kind `read`.

The actor is an agent working through an MCP client, reaching events through the MCP gateway's `call` with `service` `events`, `tool` `subscribers`, and the tool's own arguments as `args`. Each request is the HTTP request the client sends to a running mcp on `sbx.ikigenba.dev`, on revision `2026-07-28`, with `Mcp-Name: call` and the caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`; the gateway forwards it to events on its socket and relays events' result verbatim (`S05`). A successful result carries its answer as `structuredContent` and as one text block holding that object encoded compactly; a refusal is status 200 with `isError` `true`, no `structuredContent`, and one text block (`S05`). A `since` is shown as a placeholder naming the moment it is. events is serving (`S02`) with every setting at its default, and it is `2026-10-05T12:00:00Z`. This group's log and its subscribers' state are its own scenario, distinct from the logs `S07`, `S09`, `S11`, `S12` and `S13` tell. The services file enables `repos`, `scripts`, and `sites`, among others; `repos` declares that it emits `repo.pushed`; `scripts` declares `"accepts":["*"]`; and `sites`, a service the stories suppose, declares that it emits `site.published` and `"accepts":["repo.pushed"]`; no other service accepts anything unless a story says so. The log holds these eight `repo.pushed` events from `repos`, `seq` 5001 to 5008, each received a few milliseconds after its `time` and on the same day; its head is `seq` 5008, `evt_f947ed7ef1586466`:

```
{"id":"evt_90e67ae2bce4a7ad","time":"2026-10-04T08:00:00.667135Z","service":"repos","event":"repo.pushed","request_id":"4a3703c990fb485ad48ca386d55d0866","user":"u_1e9b4d07","attrs":{"new":"2d8d55f5bb340ded4bdb3be8595407f4d7f53eb7","old":"2050f99e9d5aee4b4dae9c36406b4b00c6c260eb","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":5001,"received":"2026-10-04T08:00:00.671346Z"}
{"id":"evt_7804dbe32e23df0b","time":"2026-10-04T09:15:00.868679Z","service":"repos","event":"repo.pushed","request_id":"d3e91432fe248a7c0f919d15db5267c9","user":"u_7f3a9c21","attrs":{"new":"728791d4e0db09c70a1bbcc756d2ab586d145008","old":"434a94e079b658b6a31a47bc6ac2559e987cc0af","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":5002,"received":"2026-10-04T09:15:00.872890Z"}
{"id":"evt_4483f0cd16f78c51","time":"2026-10-04T11:40:00.784624Z","service":"repos","event":"repo.pushed","request_id":"c57c46a082bff2cae60775f0152a6157","user":"u_2b8e1d04","attrs":{"new":"0d944cd427bfcc0fdd30e440b01a7a7ac1020b6d","old":"e4d8f0a417bc2192d553bd67a36ab153804dd1c0","ref":"refs/heads/main","repo":"rep_d41c7a9e05b28f63"},"cause":"","depth":0,"seq":5003,"received":"2026-10-04T11:40:00.788835Z"}
{"id":"evt_b66c42e1b673550e","time":"2026-10-04T14:05:00.674456Z","service":"repos","event":"repo.pushed","request_id":"b2211d08e3f4afc246140d1fae9dc9f3","user":"u_1e9b4d07","attrs":{"new":"af6663972f1582074203ccfe6c986b2dc55bcf58","old":"5ad5c3d00cb73b3cfb52e1d4d8339dd8a0f47eb5","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":5004,"received":"2026-10-04T14:05:00.678667Z"}
{"id":"evt_f3f6109c360aa3b4","time":"2026-10-05T08:30:00.635679Z","service":"repos","event":"repo.pushed","request_id":"b08248af837d2135e70c5862c683f513","user":"u_7f3a9c21","attrs":{"new":"05f678f2e610435b33d111de58931b93b253fdda","old":"5afeba4cf41629135e2a39c984ccfe9311491053","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":5005,"received":"2026-10-05T08:30:00.639890Z"}
{"id":"evt_675feee938e53d79","time":"2026-10-05T09:10:00.116602Z","service":"repos","event":"repo.pushed","request_id":"5469a103062df940f89b0a1674183465","user":"u_7f3a9c21","attrs":{"new":"c02464c50f15b70dff2761d42edb3db531c5f7ee","old":"c12d8f1d35dec018af1c8a0a5304a40ca15ba165","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":5006,"received":"2026-10-05T09:10:00.120813Z"}
{"id":"evt_a8d3f24c27f48736","time":"2026-10-05T10:20:00.672467Z","service":"repos","event":"repo.pushed","request_id":"fa9677d4d85e21f076e722a4a9369570","user":"u_2b8e1d04","attrs":{"new":"226904c2b37b834afe4c507cc0815c8ec0c7d821","old":"fbbd2c795b846aa0079735cb1272b63f1eb48c42","ref":"refs/heads/main","repo":"rep_d41c7a9e05b28f63"},"cause":"","depth":0,"seq":5007,"received":"2026-10-05T10:20:00.676678Z"}
{"id":"evt_f947ed7ef1586466","time":"2026-10-05T11:45:00.568824Z","service":"repos","event":"repo.pushed","request_id":"59c9922b3eee214a1883f51146488b9e","user":"u_7f3a9c21","attrs":{"new":"e42483f9594af6a786a3fdfa4f36a862c42e1076","old":"b1888f32a151dc6abfce6d0d65655df3c6157d15","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":5008,"received":"2026-10-05T11:45:00.573035Z"}
```

`scripts` has finished every event up to the head, and has been `ok` since `<scripts-since>`. `sites` has finished every event up to `seq` 5001 and has been `paused` since `<sites-since>` on `seq` 5002, `evt_7804dbe32e23df0b`, every attempt of which it answered with the error `publish failed: commit not found` (`S11`). No event is accepted while a story's calls are made unless the story says so.

## An agent lists the subscribers

The ordinary case: the agent wants to know whether every service that reacts to events is keeping up. `scripts` is current; `sites` is stuck, and the agent learns which event it is stuck on and why, so it can decide whether to skip or resume it (`S12`). No other service is answered, since no other accepts anything.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 02a559b5e40733a91fb0f8bb462f08dd
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":5008,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"paused","cursor":5001,"lag":7,"since":"<sites-since>","reason":{"event":"evt_7804dbe32e23df0b","name":"repo.pushed","seq":5002,"error":"publish failed: commit not found"}}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No delivery was made, retried, or skipped by the call.

## An agent lists the subscribers before any service accepts events

On a space where no enabled service's declaration accepts anything, there is no subscriber; the answer says so rather than refusing.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 0a6ff5977212ad441f050a128fb0f149
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- events has never seen a declaration that accepts anything: `scripts` and `sites` are not in the services file, and every enabled service declares `"accepts":[]` or answers no declaration.

Postconditions:

- Nothing has changed.

## A service that begins to accept events appears at the head of the log

`sites` is deployed for the first time, accepting `repo.pushed`. events sees its declaration at its next refresh (`S06`) and from then on `sites` is a subscriber, starting at the head: the `repo.pushed` events already in the log were emitted before it existed and are not delivered to it. Only events accepted after it appeared reach it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a17edfcaab71ba55c41d6989e928edbb
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":5008,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":5008,"lag":0,"since":"<appeared>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<appeared>` is the moment events first saw `sites`' declaration.

Preconditions:

- The preamble's, but `sites` was not in the services file and was never a subscriber.
- `sites` was then added to the services file, enabled, declaring `"accepts":["repo.pushed"]`, and events has refreshed its declarations since. No event has been accepted since.

Postconditions:

- Nothing has changed by the call.
- events delivered no event to `sites` when it appeared: not `evt_f947ed7ef1586466`, nor any earlier one. The next `repo.pushed` accepted is the first delivered to it (`S11`).

## A subscriber's cursor moves past an event it does not accept

`sites` emits `site.published` but does not accept it. When one is accepted, `scripts`, which accepts every name, is handed it, and `sites` is not; `sites`' `cursor` moves past it all the same, so it is not counted as behind on an event it will never be handed. The event:

```
{"id":"evt_28a3d7f340b0c863","time":"2026-10-05T11:45:01.642441Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_7b3e9a0c5d1f2846","sha":"7a81a64cbce64f9d0560ed3d4c5fe7c0647eb21b"},"cause":"evt_f947ed7ef1586466","depth":1,"seq":5009,"received":"2026-10-05T11:45:01.646652Z"}
```

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a16e8be76893fb18c8f98628c8f832d1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":5009,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":5009,"lag":0,"since":"<appeared>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, but `sites` is `ok` at `cursor` 5008, as in `A service that begins to accept events appears at the head of the log`.
- events then accepted `evt_28a3d7f340b0c863`, the `site.published` event above, as `seq` 5009, and `scripts` answered its delivery ok. This call comes after that answer.

Postconditions:

- Nothing has changed by the call.
- `sites` received no delivery of `evt_28a3d7f340b0c863`.

## An agent finds a subscriber whose service is no longer enabled

An operator disabled `sites` in the services file while it was `ok` and seven events behind. Its subscriber is `gone`: events delivers nothing to it, and its `cursor` stays where it stopped, while its `lag` still counts what it has not finished.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: fd9d0adde0630c2cb7faef3e2d79da6e
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":5008,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"gone","cursor":5001,"lag":7,"since":"<gone>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<gone>` is the moment events found `sites` no longer enabled.

Preconditions:

- The preamble's, but `sites` was not paused: it was `ok` at `cursor` 5001 when its entry in the services file was set to `"enabled": false`. events has refreshed its declarations since the change.

Postconditions:

- Nothing has changed by the call.
- events makes no delivery to `sites` while it is `gone`.

## An agent finds a subscriber whose service accepts nothing any more

`sites` is still enabled, but a new release of it declares `"accepts":[]`. A service that accepts nothing is not a subscriber, so `sites` is `gone`, exactly as if it had been disabled.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3870f3da2c2458da1c9251f8f67bf844
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":5008,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"gone","cursor":5001,"lag":7,"since":"<gone>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<gone>` is the moment events found `sites` accepting nothing.

Preconditions:

- The preamble's, but `sites` was not paused: it was `ok` at `cursor` 5001 when a new release of it, still enabled, began to declare `"accepts":[]`. events has refreshed its declarations since.

Postconditions:

- Nothing has changed by the call.
- events makes no delivery to `sites` while it is `gone`.

## A gone subscriber returns while its events are still retained

`sites` was `gone` and is enabled again, accepting `repo.pushed`. Every event after its `cursor` is still in the log, so it picks up where it stopped: it is `ok` again, and events delivers to it, in order (`S11`), each event after its `cursor`, `evt_7804dbe32e23df0b` first.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 0ae7e392ce37577e72ff3fe8cf37091a
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":5008,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":5008,"lag":0,"since":"<returned>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<returned>` is the moment events found `sites` enabled again.

Preconditions:

- `sites` was `gone` with `cursor` 5001, as in `An agent finds a subscriber whose service is no longer enabled`. Every event from `seq` 5002 to 5008 is still retained.
- `sites`' entry was then set to `"enabled": true`, it declares `"accepts":["repo.pushed"]`, and events has refreshed its declarations since. `sites` answered every delivery ok, and this call comes after it answered `seq` 5008.

Postconditions:

- Nothing has changed by the call.
- events delivered to `sites` `seq` 5002 to 5008, in that order, 5002 (`evt_7804dbe32e23df0b`) first, each once it had answered the one before.

## A gone subscriber returns after its events were swept

`sites` was `gone` long enough for the sweep (`S13`) to remove events after its `cursor`: a `gone` subscriber holds nothing in place. It cannot pick up where it stopped, so it starts again at the head of the log, as a new subscriber does, and the events it missed are never delivered to it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a2dbb8c9a8d1be9554cadd4ac97ba4f7
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":5008,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":5008,"lag":0,"since":"<returned>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<returned>` is the moment events found `sites` enabled again.

Preconditions:

- `sites` was `gone` with `cursor` 5001, as in `An agent finds a subscriber whose service is no longer enabled`, and stayed `gone` until shortly before `2026-10-06T17:00:00Z`, the time of this call. While it was `gone`, `scripts` stayed `ok` at `cursor` 5008 and a sweep ran after `2026-10-06T14:05:01Z`, when even `seq` 5004, received on `2026-10-04` before `14:05:01`, had been received more than 2 days before, removing `seq` 5001 to 5004; 5005 to 5008 are still retained.
- `sites`' entry was then set to `"enabled": true`, it declares `"accepts":["repo.pushed"]`, and events has refreshed its declarations since. No event has been accepted since.

Postconditions:

- Nothing has changed by the call.
- events delivered to `sites` none of the events from `seq` 5005 to 5008, though they are still retained; the next `repo.pushed` accepted is the first delivered to it.

## An agent passes subscribers an argument it does not have

`subscribers` takes no arguments. One it does not have is refused as every tool of the suite refuses it (`S05`); events answers nothing about its subscribers.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9e5acf677f65e15a157ab6844115b912
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{"bogus":"x"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
bogus: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
