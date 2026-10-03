# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits: repos never opens one of its own, and there is no port or address it falls back to. It takes the socket the way systemd socket activation passes it — `LISTEN_PID` names repos' own process, `LISTEN_FDS` is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*` variables from its environment once it has taken it. On a host, opsctl publishes `ikigenba-repos.socket`, which holds the Unix socket `/run/ikigenba/repos.sock`, beside `ikigenba-repos.service`, which runs `/opt/repos/bin/repos` with no arguments as the `ikigenba` user, with `/opt/repos` as its working directory and `/opt/repos/etc/env` as its environment file; nginx proxies repos' public name, git's requests included, to `http://unix:/run/ikigenba/repos.sock:`. The service is `Type=notify`: repos tells systemd it is ready, by sending `READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, repos drains for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its environment, and 5 when that is unset or empty. On a host, opsctl owns this value and the service unit's stop timeout: both are space-wide settings in opsctl's configuration, opsctl writes the drain into every app's `etc/env` and the stop timeout (10 seconds by default, always longer than the drain) into every service unit, and an app's manifest never sets either. The actor in these stories is the host, whether that is systemd or a developer at a terminal standing in for it.

repos' environment also carries its eight settings, each a positive whole number, and each the manifest's default (`S01`) when it is unset or empty; the host writes the defaults into `etc/env`, and an operator changes them there. `READ_SLOTS`, 8, is how many git reads — a clone or a fetch — run at once, and `WRITE_SLOTS`, 2, how many git writes — a push, or a repository's maintenance (`S13`) — run at once; the two are counted apart, so a flood of clones never keeps a push waiting. `QUEUE_LENGTH`, 16, is how many operations of each kind may wait for a slot, and `QUEUE_SECONDS`, 30, the longest one waits for a slot or for its repository's lock before it is refused (`S12`). `OPERATION_SECONDS`, 600, is the longest one git operation may run before git is killed (`S12`). `PUSH_MAX_BYTES`, 104857600, is the largest pack one push may send, and `REPO_MAX_BYTES`, 1073741824, the size on disk at which a repository takes no more pushes (`S12`). `MAINTENANCE_HOURS`, 24, is how often each repository is tidied (`S13`). And it carries `IKIGENBA_SERVICES`, the path of the host's services file, normally `/var/lib/ikigenba/services.json`, which opsctl sets in the environment the host gives repos. The file lists the platform's services: it feeds the launcher in the banner of repos' pages (`S03`), it holds the description repos' MCP endpoint gives its clients as instructions (`S05`), its entry named `repos` gives the public address repos puts in every clone URL (`S07`), and its entry named `telemetry` is where repos sends its trail (below). repos reads the variable once, when it starts, and reads the file it names afresh whenever it needs it, so a rewritten file shows without a restart. repos never fails to start over it: unset, empty, or naming a file that is missing, unreadable, or malformed, repos starts and serves all the same, treats the file as listing no services, and says nothing about the file itself.

repos runs the host's own `git` for every repository operation: it has no git of its own, and `git` is a dependency of the host that opsctl provisions. repos checks its environment first — `DRAIN_SECONDS`, then the eight settings in the manifest's order — then looks for its socket, then checks that an executable named `git` is on its `PATH`, and only then opens its SQLite database, the catalog of repositories, at `state/repos.db`, relative to its working directory, creating `state/` and the database on its first start, and then the directory that holds the repositories themselves, `state/repos/`, creating it on its first start too. Each repository is a bare git repository in `state/repos/`, named by its id (`S15`). Before it tells systemd it is ready, repos verifies every repository the catalog names (`S14`). So a start refused as a usage error, or for want of git, has touched nothing, not even the database. repos is the database's only writer, and the host replicates it as the manifest declares (`S01`, `S17`).

repos keeps a trail: it records what it does as events it sends to the platform's telemetry service, exactly as dummy does, where an operator, or an agent working for one, follows what happened from one thing they know — a request id, a user, a repository's id, or a time. repos finds telemetry in the services file `IKIGENBA_SERVICES` names, as the entry named `telemetry`, and sends each event to that entry's socket; it looks the entry up afresh for every event, so a telemetry installed after repos started is found without a restart. What telemetry does with an event is told in telemetry's own stories. The stories here show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"repos","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when repos recorded the event, in UTC to the microsecond, as `2026-10-02T14:03:07.123456Z`; `service` is always `repos`; `<request-id>` and `<user>` are the id of the request that caused the event and the caller's `X-User-Id`, each empty when there is none, as for an event no request caused — a start, a stop, a repository's maintenance (`S13`), or its verification (`S14`); and `attrs` holds the event's attributes, flat. Attributes name what happened and the ids of what it touched, never data: no event carries a repository's name in an attribute of its own, a commit message, a path within a repository, a diff, an author, a caller's email, a credential, a request's query, or a tool's arguments. The one place a name reaches the trail is the `path` of a git request's `request.started`, `/notes.git/info/refs` say, which is the URL path as it arrived, as on every app. A repository is named in an attribute by its id, under the key `repo`, so the trail of a repository survives a rename. repos sends its events one at a time, in the order it recorded them, and an answer never waits for its events to be sent. Telemetry takes an event by answering `204`. When it cannot be reached — the services file is unset, unreadable, or has no `telemetry` entry, or nothing answers on its socket — or it answers anything other than `204` or a `4xx`, repos tries the event a few times over a fraction of a second. When it answers `4xx`, it has refused the event itself, and sending it again cannot help, so repos does not retry it. Either way repos then writes the event to stderr as one line, `repos: undelivered event: <event>`, where `<event>` is the JSON telemetry would have received, and carries on serving, so nothing in the trail is lost without trace. A developer whose environment names no services file therefore sees every event on stderr. A developer stands in for telemetry with a services file whose `telemetry` entry names a socket that a listener of their own holds and that takes every event it is sent; a story that says telemetry takes every event means that, or, on a host, the telemetry service itself.

repos records these events and no others:

- `service.started`, once repos is serving and has told systemd it is ready, with `version`, the version `repos --version` prints (`S01`);
- `service.stopping`, when repos is told to stop and has finished the requests it accepted, with `reason`, the name of the signal that stopped it, `SIGTERM` or `SIGINT`; it is the last event repos records;
- `request.started`, as each request arrives, with `method` and `path`, the request's URL path without its query;
- `request.finished`, once that request's answer is complete, with `status`, the status of repos' answer; `duration_us`, how long repos took to answer, in whole microseconds; `request_bytes`, how many bytes of the request's body repos read; and `response_bytes`, how many bytes of the response's body repos wrote; the three vary from request to request, and a story's event JSON shows `duration_us` as `<n>` and the two byte counts as `<bytes>` unless it fixes them;
- `tool.called`, for each call of one of its six tools that is answered with a result (`S05`), with `tool`, `kind`, `outcome`, and `duration_us`;
- `repo.created`, `repo.renamed`, and `repo.deleted`, when a tool creates, renames, or deletes a repository (`S06`, `S08`, `S09`), each with `repo` and `owner`, the owner's user id;
- `repo.pushed`, once git has accepted a push, one for each ref the push updated, with `repo`; `ref`, the ref's full name, `refs/heads/main` say; and `old` and `new`, the 40-hexadecimal-digit shas the ref named before and after, 40 zeros for a ref the push created or deleted (`S11`);
- `repo.fetched`, once git has served a clone or a fetch, with `repo` and `bytes`, how many bytes of pack it served (`S11`);
- `operation.rejected`, when repos refuses a git operation or a maintenance run because of a limit, with `repo`; `operation`, `fetch`, `push`, or `maintenance`; and `limit`, the limit that applied: `queue_length`, `queue_seconds`, `push_max_bytes`, `repo_max_bytes`, or `draining` (`S12`, `S13`, and below);
- `operation.timed_out`, when git is killed for running past `OPERATION_SECONDS`, with `repo`, `operation`, and `limit`, `operation_seconds` (`S12`);
- `operation.waited`, when an operation waited for a slot or for its repository's lock and then ran, with `repo`, `operation`, and `wait_us`, how long it waited in whole microseconds (`S12`);
- `maintenance.finished`, after each repository's maintenance, with `repo`; `duration_us`; and `size_before` and `size_after`, the repository's size on disk in bytes (`S13`);
- `repo.unavailable`, when startup verification finds a repository broken, with `repo` (`S14`).

The events of a git request carry its request id and its caller, and come in this order: `request.started`, then `operation.waited` if it waited, then the request's domain events, then `request.finished`.

stderr holds only trouble: a condition repos cannot go on from — the start-up refusals below and the requests lost to a drain cut short — and an event repos could not deliver. A failure repos handles is not trouble: a git request refused for a limit, a git operation killed for its deadline or its client's going away, a repository found broken at start, a request answered 500, like a request answered any other way, is recorded in the trail and earns no line on stderr. So while telemetry takes every event, a running repos writes nothing to stdout or stderr, and under systemd the journal holds only trouble. Every line repos writes to stderr begins `repos: `. What git itself writes to its stderr while repos runs it goes back to the git client that asked, as git's smart HTTP carries it, or nowhere; it never reaches repos' own stderr.

These are the terms every app of the platform serves on, the same as dummy's. The socket is the app's only way in. Every app runs as the one `ikigenba` user, so any app can reach any sibling's socket, and nginx reaches them all; nothing else on the host can. The suite is a closed system that only we deploy services into, and an app trusts the suite: it trusts the headers nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated, and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every request and overwrites whatever a client sent — and it trusts a sibling that calls it to have forwarded them. The mcp gateway calls repos that way, at its socket (`S05`); git reaches repos through nginx, whose gate authenticates git's Basic credential like any other (`S11`, `S17`). A request that reaches repos with no `X-Request-Id`, or an empty one, as a developer's request does, is given an id of the same shape by repos, a new one for each such request, so every event about a request names it. repos calls no sibling while serving a request.

## The host starts repos

The socket keeps out every process that is not part of the suite or nginx, which a port on loopback would not: any process on the host can connect to a loopback port, and only the `ikigenba` user and nginx can connect to `/run/ikigenba/repos.sock`. systemd owns the socket, so it exists, and accepts connections into its queue, before repos starts and while it is stopped; repos' part is to serve what arrives on it. `systemctl start` returns once repos has verified its repositories (`S14`) and reported that it is ready. At that moment repos records `service.started`, with the version it is running: a new version in a start event is how a deploy shows in the trail.

Command:

```
$ sudo systemctl start ikigenba-repos.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed repos: `/opt/repos/bin/repos` exists, and `ikigenba-repos.socket` and `ikigenba-repos.service` are published.
- `/opt/repos/etc/env` sets `DRAIN_SECONDS` and each of the eight settings to a valid value, and `IKIGENBA_SERVICES` to the host's services file.
- `git` is installed on the host, on the `PATH` the service runs with.
- `ikigenba-repos.socket` is active, so `/run/ikigenba/repos.sock` exists and accepts connections.
- `/opt/repos/state/repos.db` exists, from an earlier start, and every repository it names has its directory in `/opt/repos/state/repos/`, sound.
- `ikigenba-repos.service` is not running.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `ikigenba-repos.service` is `active`, and repos is serving on `/run/ikigenba/repos.sock`: a connection there, and every connection queued before repos started, is answered by repos.
- repos listens on no other socket and no port.
- `/opt/repos/state/repos.db` is the database it opened, and every repository it names is available, its directory unchanged by the start (`S14`).
- telemetry has received one event from repos, with no request id and no user, whose `version` is the version `repos --version` prints (`S01`):

  ```
  {"time":"<time>","service":"repos","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
  ```

- repos has written nothing to the journal.
- It keeps running until it is signalled.

## The host starts repos for the first time

Nothing of repos' state exists yet. repos creates the `state/` directory if it is absent, then creates `state/repos.db` and its schema, then creates `state/repos/`, empty, and serves with a catalog that names no repository. The same start succeeds when `state/` already exists and only the database and `state/repos/` are absent. Neither a fresh deployment nor any other first start needs a directory created beforehand. The paths are relative to repos' working directory.

Command:

```
$ repos
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`, and so does `git`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so repos reports readiness to nobody.
- `DRAIN_SECONDS` and each of the eight settings are unset, or a positive whole number.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry names a socket a listener holds that takes every event.
- Neither `state/repos.db` nor `state/repos` exists.
- Either `state/` is absent and repos can create it in its working directory, or `state/` is an existing directory in which repos can create the database and `state/repos/`.

Postconditions:

- `state/` exists, created by repos if it was absent.
- `state/repos.db` now exists, with its schema, created by this start, and names no repository: `list` answers `{"repos":[]}` for every caller (`S07`).
- `state/repos/` now exists, empty, created by this start.
- repos is serving on the socket it was passed, and on no other.
- telemetry has received exactly one event from repos, its `service.started` with `version` `v<semver>`, the version `repos --version` prints, under an empty request id and an empty user.
- It keeps running until it is signalled.

## The host starts repos with no services file

A developer running repos at a terminal, or a host whose services file is not there, gives repos no list of services. It serves all the same, and every git request and tool call is answered as it would be with the file. What the file would have given is gone: the launcher in its pages' banner (`S03`), the instructions its MCP endpoint gives (`S05`), and the `repos` entry's address, so every clone URL is built from the request's own `Host` instead (`S07`). And with no `telemetry` entry there is nowhere to send the trail, so every event repos records goes to stderr as an `undelivered event` line (above), starting with `service.started`. A services file that exists but has no entry named `telemetry` is the same for the trail.

Command:

```
$ repos
```

Output:

```
repos: undelivered event: {"time":"<time>","service":"repos","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
```

Does not exit. The line is on stderr, written once repos is serving; stdout is empty. Every event repos records from then on is written the same way, one line each.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`, and so does `git`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the eight settings are unset, or a positive whole number.
- `IKIGENBA_SERVICES` is unset, or names a file that does not exist.
- `state/repos.db` exists, from an earlier start, or can be created as in `The host starts repos for the first time`.

Postconditions:

- repos is serving on the socket it was passed, and answers every request as it would with a services file: its pages have no launcher, its MCP endpoint gives no instructions, and its clone URLs take their address from each request's `Host` (`S07`).
- Every event repos records is on stderr as an `undelivered event` line; none was sent anywhere.

## The host starts repos where its state directory cannot be created

A regular file named `state` occupies the path where repos needs its state directory. repos has taken its socket and found git, but it reports the database setup failure and exits before it serves or reports ready. The failure is not the caller's usage, so it exits 1. When `state/` and the database are sound but a regular file named `state/repos` occupies the path where repos keeps its repositories, the start fails the same way, at that point, with `repos: cannot create directory state/repos: <reason>`.

Command:

```
$ repos
```

Output:

```
repos: cannot open database state/repos.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying directory-creation failure.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`, and so does `git`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the eight settings are unset, or a positive whole number.
- `state` is an existing regular file in repos' working directory.

Postconditions:

- The existing `state` file is unchanged.
- No database and no repository directory was created. repos served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts repos with a database it cannot open

`state/repos.db` exists but repos cannot open it — the file is not writable by the service user, or its contents are not a valid SQLite database. repos writes a diagnostic naming the database problem and exits before it serves or reports ready. A missing file is not this error: an absent `state/repos.db` is created, on a first start empty and otherwise rebuilt from the repositories on disk (`S14`). Under systemd the start fails, and `systemctl start` reports it; the socket stays up, and git's requests and the gateway's calls wait in its queue for a repos that can answer them.

Command:

```
$ repos
```

Output:

```
repos: cannot open database state/repos.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying open failure.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`, and so does `git`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the eight settings are unset, or a positive whole number.
- `state/repos.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. Every directory in `state/repos/` is as it was. repos served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts repos where git is not installed

repos serves every repository by running git, so without git it can serve none of them, and it says so at start rather than with a failed request later. git is looked for on the `PATH` repos was started with, as a shell would find it: an executable file named `git` in one of its directories. This is not the caller's usage but a host missing a dependency opsctl provisions, so it exits 1, and it does so before it opens the database, so a host without git keeps its state as it was.

Command:

```
$ repos
```

Output:

```
repos: git not found on PATH
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `bin/repos` exists and is run by its path.
- No directory on the `PATH` repos is started with holds an executable named `git`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the eight settings are unset, or a positive whole number.

Postconditions:

- Nothing has changed. repos opened no database, and an absent `state/repos.db` is still absent; it served nothing, told systemd nothing, and sent telemetry nothing.

## The host stops repos

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C` sends `SIGINT`, which repos treats the same way. repos stops taking new connections, finishes the requests it has already accepted, and exits. A git request is finished when git is: a clone or a push running at the signal goes on, with its git, until it completes and its client has its whole answer. No repository's maintenance starts after the signal (`S13`); one already running goes on to its end. An operation still waiting for a slot or its repository's lock at the signal does not run: it is answered at once, as told in `The host stops repos while a git operation outlasts the drain`. repos closes its own copy of the socket and nothing more: the socket belongs to systemd, which keeps it open, and repos never removes `/run/ikigenba/repos.sock`. It waits at most `DRAIN_SECONDS` for requests to finish, which on a host is always less than the time the service unit allows before systemd kills it. Once its requests have finished, and each has recorded its `request.finished`, repos records `service.stopping` with the reason it is stopping, the name of the signal it received, `SIGTERM` or `SIGINT`. That is the last event of its trail: repos sends everything it recorded before exiting, within the same drain deadline. Sending takes time, so a stop is silent when the requests leave repos at least a second of the drain for it; a request that finishes later still gets its whole response, but an event repos has not sent when the deadline comes goes to stderr as an `undelivered event` line instead. A `service.started` with no `service.stopping` before the next one is how the trail shows a repos that died rather than stopped.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

repos exits 0. Nothing is on stdout or stderr.

Preconditions:

- repos is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- A client is in the middle of a push of one commit to `main` of the caller's `notes` (`rep_3f9a0c1d2e4b5a69`) at the signal: git has begun receiving its pack.
- That push, and every other request repos has accepted, finishes at least a second before the drain deadline, within 4 seconds of the signal, so repos has that second left to send what it recorded; no operation is waiting for a slot or a lock.
- telemetry takes every event as soon as it is sent.

Postconditions:

- Every request accepted before the signal received its full response. The push succeeded: the client's `git push` exited 0, and `refs/heads/main` of `notes` names the pushed commit.
- telemetry has received every event repos recorded: the push's `repo.pushed`, with `repo` `rep_3f9a0c1d2e4b5a69`, `ref` `refs/heads/main`, and its `old` and `new` shas, and the push's `request.finished` with `status` `200`; and last, after the `request.finished` of every request accepted before the signal:

  ```
  {"time":"<time>","service":"repos","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

  With `SIGINT` the `reason` is `SIGINT`.
- No git process repos started is still running.
- `/run/ikigenba/repos.sock` still exists, and connections made to it after repos exited — git's next request among them — wait in the socket's queue for the next repos to answer.

## The host stops repos while a git operation outlasts the drain

repos waits for accepted requests only until its drain deadline, `DRAIN_SECONDS` after the signal, so that it always exits before the service unit's stop timeout and is never killed mid-write by systemd. A git request still running at the deadline — a large push over a slow link, say — is cut off: repos kills its git and closes its connection without the rest of the response, and the client's git fails. Git changes a ref only once it has the whole pack, and it is killed before that, so a push cut off leaves its repository exactly as it was: every ref names what it named before, and the next push or clone sees a sound repository. A maintenance run still going at the deadline is killed too; it is not a request, so it is not counted below, earns no line of its own, and records no `maintenance.finished`, and the repository is as sound as maintenance leaves it at any moment (`S13`). Losing a request is trouble, so repos says how many it lost and exits non-zero.

An operation that had not started when the signal came — a second push waiting for the repository's lock the first one holds, or a clone waiting for a read slot — will not get its turn before the stop, so repos answers it at once, the moment the signal arrives, rather than leave it to be cut off: `503`, with a `Retry-After` of `QUEUE_SECONDS`, and the one line of plain text `repos is stopping; try again later`, which git shows its user. It records `operation.rejected` with `limit` `draining` for it. That answer is complete, so the request is finished, not cut off, and is not counted.

The drain has used the whole deadline, so repos has no time left to send `service.stopping`, or any other event not yet sent, to telemetry: each goes to stderr as an `undelivered event` line instead. telemetry never receives a `request.finished` for a request cut off: its `request.started` with no finish is how the trail shows it was cut off.

Command:

```
$ kill -TERM <pid>
```

Output:

```
repos: undelivered event: {"time":"<time>","service":"repos","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
repos: stopped with <n> requests unfinished
```

repos exits 1, 5 seconds after the signal. The text is on stderr; stdout is empty. `<n>` is the number of requests still running at the deadline. When `<n>` is 1 the line reads `repos: stopped with 1 request unfinished`. The `service.stopping` line above is always written. Any other event repos had recorded and not yet delivered when the deadline came is written as an `undelivered event` line too; those lines are in the order the events were recorded, `service.stopping` last of them. The position of the `stopped with` line among them is not fixed. stderr may also hold, for a cut-off request, an `undelivered event` line carrying its `request.finished`, recorded after the deadline and before repos exited; whether it does, and where that line falls, is not fixed. stderr holds no other line.

Preconditions:

- repos is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds, and `QUEUE_SECONDS` is unset, so it is 30.
- A client's push to `notes` (`rep_3f9a0c1d2e4b5a69`) is running at the signal and is still running 5 seconds after it; `refs/heads/main` of `notes` names `<old>`.
- A second client's push to `notes` is waiting for the repository's lock at the signal.
- `<n>` of the requests repos has accepted, the first push among them, are still running 5 seconds after the signal.
- telemetry takes every event.

Postconditions:

- The second push was answered at once, at the signal:

  ```
  HTTP/1.1 503 Service Unavailable
  Retry-After: 30
  ```

  with the body `repos is stopping; try again later`; its `git push` failed and changed nothing. telemetry has received its `request.started`, then `operation.rejected` with `repo` `rep_3f9a0c1d2e4b5a69`, `operation` `push`, and `limit` `draining`, then its `request.finished` with `status` `503`.
- Every request that finished within 5 seconds of the signal received its full response; the `<n>` that did not were cut off. The first push's git was killed and its client's `git push` failed.
- `refs/heads/main` of `notes` still names `<old>`, and every other ref of it is as it was; the push recorded no `repo.pushed`. `git fsck` in the repository's directory finds nothing wrong.
- No git process repos started is still running.
- Each of the `<n>` cut-off requests has its `request.started` recorded (delivered to telemetry, written to stderr as undelivered, or both, when it was being delivered as the deadline came). telemetry has received no `request.finished` for any of them, and no `service.stopping`.
- `/run/ikigenba/repos.sock` still exists, and connections made to it after repos exited wait in the socket's queue for the next repos to answer.

## The host restarts repos during a deploy

A deploy replaces repos' binary and restarts `ikigenba-repos.service` alone; `ikigenba-repos.socket` stays up throughout. Between the old repos exiting and the new one being ready, connections wait in the socket's queue instead of being refused, so a git client or the gateway never sees repos missing. That holds because repos finishes what it accepted before it exits and leaves the socket where systemd put it. The new repos verifies its repositories (`S14`) before it is ready, so the wait lasts as long as that takes.

Command:

```
$ sudo systemctl restart ikigenba-repos.service
```

Output:

```
```

Exits 0, once the new repos has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- repos is serving on `/run/ikigenba/repos.sock` under `ikigenba-repos.service`.
- Clients are cloning from and pushing to the caller's repositories, and the gateway is calling repos' tools, throughout the restart, and every request the old repos accepted finishes at least a second before its drain deadline.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- Every request sent was answered, by the old repos or the new one; none was refused and none was cut off. Every clone has the repository as it stood when it was served, and every push that succeeded is in its repository.
- A new repos process is serving on `/run/ikigenba/repos.sock`, over the same `state/repos.db` and the same `state/repos/`.
- telemetry has received the old repos' `service.stopping`, with `reason` `SIGTERM`, and after it the new repos' `service.started`, whose `version` is the version the new binary's `repos --version` prints. Every request the old repos answered is recorded before its `service.stopping`, and every request the new one answered after its `service.started`.

## The host starts repos where telemetry cannot be reached

The trail is not a reason to stop serving. When repos cannot deliver its events — telemetry is not installed yet, is stopped, or the services file names no `telemetry` entry — repos starts and serves exactly as it does otherwise, and its events go to the journal as `undelivered event` lines (above). No answer waits on telemetry, so a git client or the gateway sees no difference. repos keeps looking for telemetry with every event, so once telemetry takes events again, repos' next events go to it without a restart; an event already written to the journal is not sent again.

Command:

```
$ sudo systemctl start ikigenba-repos.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed repos, and `ikigenba-repos.socket` is active, as in `The host starts repos`.
- `ikigenba-repos.service` is not running.
- The host's services file lists no `telemetry` entry, or nothing accepts connections on the socket that entry names.

Postconditions:

- `ikigenba-repos.service` is `active`, and repos is serving on `/run/ikigenba/repos.sock`, as in `The host starts repos`.
- The journal holds one line from repos, written after it reported that it was ready:

  ```
  repos: undelivered event: {"time":"<time>","service":"repos","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
  ```

- Every event repos records while telemetry cannot be reached is written to the journal the same way, one line each, and every request is answered as it would be with telemetry taking events.

## The host starts repos without a socket

Run bare, with no socket passed in, repos has nothing to serve on, and it does not open one of its own: there is no port or address it falls back to. That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or `LISTEN_PID` naming some other process, counts as no socket passed in. The detail says how to run repos correctly. repos looks for its socket before it looks for git, so this is reported whether or not git is installed.

Command:

```
$ repos
```

Output:

```
repos: no socket was passed in

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not repos' process id.
- `DRAIN_SECONDS` and each of the eight settings are unset, or a positive whole number.

Postconditions:

- Nothing has changed. repos opened no database, listened on nothing, told systemd nothing, and sent telemetry nothing; an absent `state/repos.db` is still absent.

## The host passes repos more than one socket

repos serves on exactly one socket. A unit that passes it several is misconfigured, and repos will not guess which one it was meant to serve on.

Command:

```
$ repos
```

Output:

```
repos: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `2`: two listening sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` and each of the eight settings are unset, or a positive whole number.

Postconditions:

- Nothing has changed. repos opened no database, served on neither socket, told systemd nothing, and sent telemetry nothing.

## The host gives repos a drain deadline that is not a number of seconds

repos reads `DRAIN_SECONDS` before it serves, so a bad value is found at start rather than at the moment repos is asked to stop. A value that is not a positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and repos serves nothing. The value is quoted back verbatim. repos sets no upper limit: keeping the drain inside the service unit's stop timeout is opsctl's to enforce. It checks `DRAIN_SECONDS` before any of its eight settings, so it is the one named when several are bad, and before it looks for its socket or for git, so it is reported whether or not a socket was passed in or git is installed.

Command:

```
$ DRAIN_SECONDS=abc repos
```

Output:

```
repos: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. repos opened no database, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives repos a setting that is not a positive whole number

repos reads its eight settings before it serves, so a bad value is found at start rather than when a git request or a maintenance run first needs it. A value that is not a positive whole number — `0`, `-1`, `2.5`, `8x`, or `abc` — is the caller's mistake, so it is a usage error and repos serves nothing: a limit it cannot read is not a limit it can guess, and guessing one could let a flood of git operations take the host or refuse every push. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). repos sets no upper limit on any of them. It checks them after `DRAIN_SECONDS`, one at a time in the manifest's order, and names only the first bad one, and before it looks for its socket or for git, so it is reported whether or not a socket was passed in or git is installed. The story shows `READ_SLOTS`; each of the others fails the same way, with its own name and the unit it counts:

- `repos: WRITE_SLOTS is 'abc', not a positive whole number of operations`
- `repos: QUEUE_LENGTH is 'abc', not a positive whole number of operations`
- `repos: QUEUE_SECONDS is 'abc', not a positive whole number of seconds`
- `repos: OPERATION_SECONDS is 'abc', not a positive whole number of seconds`
- `repos: PUSH_MAX_BYTES is 'abc', not a positive whole number of bytes`
- `repos: REPO_MAX_BYTES is 'abc', not a positive whole number of bytes`
- `repos: MAINTENANCE_HOURS is 'abc', not a positive whole number of hours`

Command:

```
$ READ_SLOTS=abc repos
```

Output:

```
repos: READ_SLOTS is 'abc', not a positive whole number of operations
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. repos opened no database, ran no git, served nothing, told systemd nothing, and sent telemetry nothing.
