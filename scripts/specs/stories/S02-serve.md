# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits: scripts never opens one of its own, and there is no port or address it falls back to. It takes the socket the way systemd socket activation passes it — `LISTEN_PID` names scripts' own process, `LISTEN_FDS` is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*` variables from its environment once it has taken it. On a host, opsctl publishes `ikigenba-scripts.socket`, which holds the Unix socket `/run/ikigenba/scripts.sock`, beside `ikigenba-scripts.service`, which runs `/opt/scripts/bin/scripts` with no arguments as the `ikigenba` user, with `/opt/scripts` as its working directory and `/opt/scripts/etc/env` as its environment file; nginx proxies scripts' public name, `scripts.<space>`, to `http://unix:/run/ikigenba/scripts.sock:`. The service is `Type=notify`: scripts tells systemd it is ready, by sending `READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, scripts drains for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its environment, and 5 when that is unset or empty. On a host, opsctl owns this value and the service unit's stop timeout: both are space-wide settings in opsctl's configuration, opsctl writes the drain into every app's `etc/env` and the stop timeout (10 seconds by default, always longer than the drain) into every service unit, and an app's manifest never sets either. The actor in these stories is the host, whether that is systemd or a developer at a terminal standing in for it.

scripts' environment also carries its seven settings, each the manifest's default (`S01`) when it is unset or empty; the host writes the defaults into `etc/env`, and an operator changes them there. `REPOS_DIR`, `../repos/state/repos`, is the directory holding repos' bare repositories, each as `<repository id>.git`; a relative value is resolved against scripts' working directory, so on a host it is `/opt/scripts/../repos/state/repos`, which is `/opt/repos/state/repos`, and an absolute one is used as it is (`S20`). scripts only reads there, with git, and never writes there. `TREE_MAX_BYTES`, 268435456, a positive whole number of bytes, is the largest a run's unpacked tree may be, counted as the sum of its files' sizes (`S17`). `OUTPUT_MAX_BYTES`, 1048576, a positive whole number of bytes, is how much of a run's standard output, and separately of its standard error, is kept (`S17`). `OPERATION_SECONDS`, 600, a positive whole number of seconds, is the longest one git run may take before scripts kills it (`S17`). `SCRIPT_SECONDS`, 600, a positive whole number of seconds, is the longest a script may run before scripts kills it (`S17`). `RUN_KEEP_DAYS`, 15, a positive whole number of days, is how long a run is kept, and `RUN_KEEP_COUNT`, 10, a positive whole number of runs, is how many of each script's newest runs are kept whatever their age (`S19`). And it carries `IKIGENBA_SERVICES`, the path of the host's services file, normally `/var/lib/ikigenba/services.json`, which opsctl sets in the environment the host gives scripts. The file lists the platform's services: it feeds the launcher in the banner of scripts' pages (`S03`), it holds the description scripts' MCP endpoint gives its clients as instructions (`S05`), its entry named `telemetry` is where scripts sends its trail (below), and its path is handed, as it is, to every script scripts runs, which finds its siblings there (`S15`). scripts reads the variable once, when it starts, and reads the file it names afresh whenever it needs it, so a rewritten file shows without a restart. scripts never fails to start over it: unset, empty, or naming a file that is missing, unreadable, or malformed, scripts starts and serves all the same, treats the file as listing no services, and says nothing about the file itself.

scripts runs the host's own `git` for every read of a repository — reading a repository's owner and name, resolving a ref, unpacking a commit's tree with `git archive` — and has no git of its own, and it runs every script with the host's own `python3.12` (`S15`, `S22`); both are dependencies of the host. scripts checks its environment first — `DRAIN_SECONDS`, then `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, `OPERATION_SECONDS`, `SCRIPT_SECONDS`, `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT`, in that order — then looks for its socket, then checks that an executable named `git` is on its `PATH`, then that one named `python3.12` is (`S22`), and only then opens its SQLite database, the catalog of scripts and the record of every run, at `state/scripts.db`, relative to its working directory, creating `state/` and the database on its first start, and brings the database up to date by applying, in order, every migration it carries that the database has not had (`S01`), and then the directory that holds the run folders, `state/runs/`, creating it when it is absent. It then marks every run its catalog still records as `running` as `killed` (`S18`) and prunes the runs past keeping (`S19`). Then it is ready. `REPOS_DIR` is not checked at start: repos may be installed after scripts, and a repository is looked for only when a tool, a run or a page needs it (`S21`). So a start refused as a usage error, or for want of git or `python3.12`, has touched nothing, not even the database. A database scripts cannot open, or one that records a migration it does not carry, is a start it refuses, with one line on stderr, `scripts: cannot open database state/scripts.db: <reason>`, and exit status 1. scripts is the database's only writer, and the host replicates it as the manifest declares (`S01`, `S21`); the run folders under `state/runs/` are not replicated (`S20`).

scripts keeps a trail: it records what it does as events it sends to the platform's telemetry service, exactly as sites does, where an operator, or an agent working for one, follows what happened from one thing they know — a request id, a user, a script's id, a run's id, or a time. scripts finds telemetry in the services file `IKIGENBA_SERVICES` names, as the entry named `telemetry`, and sends each event to that entry's socket; it looks the entry up afresh for every event, so a telemetry installed after scripts started is found without a restart. What telemetry does with an event is told in telemetry's own stories. The stories here show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"scripts","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when scripts recorded the event, in UTC to the microsecond, as `2026-10-05T09:14:02.123456Z`; `service` is always `scripts`; `<request-id>` and `<user>` are the id of the request that caused the event and the caller's `X-User-Id`, each empty when there is none, as for an event no request caused — a start or a stop; and `attrs` holds the event's attributes, flat, their keys in alphabetical order. Attributes name what happened and the ids of what it touched, never data: no event carries a script's name in an attribute of its own, a repository's name, a run's input, a script's output, a file's content, a caller's email, a credential, a request's query, or a tool's arguments. The place a script's name reaches the trail is the `path` of a request's `request.started`, `/nightly-report/` say, which is the URL path as it arrived, as on every app. A script is named in an attribute by its id, under the key `script`, and a run by its id under `run` (`S16`). scripts sends its events one at a time, in the order it recorded them, and an answer never waits for its events to be sent. Telemetry takes an event by answering `204`. When it cannot be reached — the services file is unset, unreadable, or has no `telemetry` entry, or nothing answers on its socket — or it answers anything other than `204` or a `4xx`, scripts tries the event a few times over a fraction of a second. When it answers `4xx`, it has refused the event itself, and sending it again cannot help, so scripts does not retry it. Either way scripts then writes the event to stderr as one line, `scripts: undelivered event: <event>`, where `<event>` is the JSON telemetry would have received, and carries on serving, so nothing in the trail is lost without trace. A developer whose environment names no services file therefore sees every event on stderr. A developer stands in for telemetry with a services file whose `telemetry` entry names a socket that a listener of their own holds and that takes every event it is sent; a story that says telemetry takes every event means that, or, on a host, the telemetry service itself.

scripts records these events and no others:

- `service.started`, once scripts is serving and has told systemd it is ready, with `version`, the version `scripts --version` prints (`S01`);
- `service.stopping`, when scripts is told to stop and has finished the requests it accepted, with `reason`, the name of the signal that stopped it, `SIGTERM` or `SIGINT`; it is the last event scripts records;
- `request.started`, as each request arrives, with `method` and `path`, the request's URL path without its query;
- `request.finished`, once that request's answer is complete, with `status`, the status of scripts' answer; `duration_us`, how long scripts took to answer, in whole microseconds; `request_bytes`, how many bytes of the request's body scripts read; and `response_bytes`, how many bytes of the response's body scripts wrote; the three vary from request to request, and a story's event JSON shows `duration_us` as `<n>` and the two byte counts as `<bytes>` unless it fixes them;
- `tool.called`, for each call of one of its eleven tools that is answered with a result, with `tool`, `kind`, `outcome`, and `duration_us` (`S05`);
- `script.created`, `script.updated` and `script.deleted`, one for each `create`, `update` and `delete` that changes the catalog, with `script` (`S06`, `S09`, `S10`, `S16`);
- `run.started`, when a run's process starts, with `run`, `script`, `sha`, and `trigger` (`S08`, `S16`);
- `run.finished`, when a run reaches a final status, with `run`, `status`, its exit code when it exited, its duration, whether its output was truncated, and its reason when it failed; a run that could not start records only this one (`S08`, `S11`, `S16`).

The events of a request carry its request id and its caller, and come in this order: `request.started`, then the request's domain events, then `tool.called` for a tool call, then `request.finished`. A run's `run.finished` carries the request id and user of the run, whichever request or moment ends it (`S16`).

stderr holds only trouble: a condition scripts cannot go on from — the start-up refusals below and the requests lost to a drain cut short — and an event scripts could not deliver. A failure scripts handles is not trouble: a tool call refused, a run that could not start, a script that exits non-zero, runs too long or is cancelled, a path answered `404`, a request answered 500, like a request answered any other way, is recorded in the trail, the run or the tool's result and earns no line on stderr. So while telemetry takes every event, a running scripts writes nothing to stdout or stderr, and under systemd the journal holds only trouble. Every line scripts writes to stderr begins `scripts: `. What git itself writes to its stderr while scripts runs it for a run goes into that run's `stderr` (`S08`), or nowhere, and what a script writes goes into its run's `stdout` and `stderr` (`S15`); neither ever reaches scripts' own stderr.

These are the terms every app of the platform serves on, the same as sites'. The socket is the app's only way in. Every app runs as the one `ikigenba` user, so any app can reach any sibling's socket, and nginx reaches them all; nothing else on the host can. The suite is a closed system that only we deploy services into, and an app trusts the suite: it trusts the headers nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated, and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every request and overwrites whatever a client sent — and it trusts a sibling that calls it to have forwarded them. scripts' manifest declares no `guests` (`S01`), so on a host with an authenticator nginx passes scripts no request from a visitor with no credential: it sends one to sign in at a page (`S03`) and challenges one at `/mcp` (`S05`). The mcp gateway calls scripts at its socket (`S05`). A request that reaches scripts with no `X-Request-Id`, or an empty one, as a developer's request does, is given an id of the same shape by scripts, a new one for each such request, so every event about a request names it. scripts calls no sibling while serving a request, and reaches repos only through its bare repositories on disk; a script it runs reaches siblings on its own, as the run's user and under the run's request id (`S15`).

## The host starts scripts

The socket keeps out every process that is not part of the suite or nginx, which a port on loopback would not: any process on the host can connect to a loopback port, and only the `ikigenba` user and nginx can connect to `/run/ikigenba/scripts.sock`. systemd owns the socket, so it exists, and accepts connections into its queue, before scripts starts and while it is stopped; scripts' part is to serve what arrives on it. `systemctl start` returns once scripts has opened its catalog and its runs directory, settled what a previous process left (`S18`, `S19`), and reported that it is ready. At that moment scripts records `service.started`, with the version it is running: a new version in a start event is how a deploy shows in the trail.

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed scripts: `/opt/scripts/bin/scripts` exists, and `ikigenba-scripts.socket` and `ikigenba-scripts.service` are published.
- `/opt/scripts/etc/env` sets `DRAIN_SECONDS` and each of the seven settings to a valid value, and `IKIGENBA_SERVICES` to the host's services file.
- `git` and `python3.12` are installed on the host, on the `PATH` the service runs with.
- `ikigenba-scripts.socket` is active, so `/run/ikigenba/scripts.sock` exists and accepts connections.
- `/opt/scripts/state/scripts.db` exists, from an earlier start, records no migration this scripts does not carry, and holds the scripts `nightly-report`, `sync-crm`, `rotate-keys`, `backfill`, and `digest` and their runs; none of the runs is recorded as `running`, and none is past what `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` keep. `/opt/scripts/state/runs/` exists and holds their run folders.
- `ikigenba-scripts.service` is not running.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `ikigenba-scripts.service` is `active`, and scripts is serving on `/run/ikigenba/scripts.sock`: a connection there, and every connection queued before scripts started, is answered by scripts.
- scripts listens on no other socket and no port.
- `/opt/scripts/state/scripts.db` is the database it opened, now up to date, and every script and run it held is still there, unchanged by the start; every run folder under `/opt/scripts/state/runs/` is as it was. No git and no script ran.
- telemetry has received one event from scripts, with no request id and no user, whose `version` is the version `scripts --version` prints (`S01`):

  ```
  {"time":"<time>","service":"scripts","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
  ```

- scripts has written nothing to the journal.
- It keeps running until it is signalled.

## The host starts scripts for the first time

Nothing of scripts' state exists yet. scripts creates the `state/` directory if it is absent, then creates `state/scripts.db` and applies every migration it carries, then creates `state/runs/`, empty, and serves with a catalog that names no script and no run. The same start succeeds when `state/` already exists and only the database or `state/runs/` is absent. Neither a fresh deployment nor any other first start needs a directory created beforehand. The paths are relative to scripts' working directory.

Command:

```
$ scripts
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`, and so do `git` and `python3.12`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so scripts reports readiness to nobody.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry names a socket a listener holds that takes every event.
- Neither `state/scripts.db` nor `state/runs/` exists.
- Either `state/` is absent and scripts can create it in its working directory, or `state/` is an existing directory in which scripts can create the database and `runs/`.

Postconditions:

- `state/` exists, created by scripts if it was absent.
- `state/scripts.db` now exists, created by this start, and is up to date: `scripts db status` prints `0001 applied <time>` and `0002 applied <time>`, each `<time>` being the moment this start applied that version (`S01`). It names no script and no run: `list` answers `{"scripts":[]}` for every caller (`S07`).
- `state/runs/` now exists, empty, created by this start.
- scripts is serving on the socket it was passed, and on no other.
- telemetry has received exactly one event from scripts, its `service.started` with `version` `v<semver>`, the version `scripts --version` prints, under an empty request id and an empty user.
- It keeps running until it is signalled.

## The host starts scripts over a catalog kept before its migrations

A scripts from before the database carried migrations kept its catalog in `state/scripts.db` with the same schema, but the database records no migration at all. The baseline migration, `0001`, is that same schema, and applying it to such a database changes nothing in it but the record that `0001` has been applied; `0002` follows it as on any database that has `0001` alone. So a deploy of this scripts over that database needs no step of its own: scripts starts, applies `0001` and `0002`, and serves every script and run the old one kept.

Command:

```
$ scripts
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`, and so do `git` and `python3.12`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so scripts reports readiness to nobody.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry names a socket a listener holds that takes every event.
- `state/scripts.db` holds the catalog a scripts from before migrations kept: scripts and their runs, none of which is recorded as `running` and none past what `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` keep, and records no migration; `state/runs/` holds their run folders. `scripts db status` there prints `0001 pending` and `0002 pending` (`S01`).

Postconditions:

- scripts is serving on the socket it was passed, over the same `state/scripts.db`, now up to date: `scripts db status` prints `0001 applied <time>` and `0002 applied <time>`, each `<time>` being the moment this start applied that version (`S01`).
- Every script and run the database held is still there, unchanged: `list`, `show`, `runs` and `result` answer each as they did before the start (`S07`, `S11`), and every run folder under `state/runs/` is as it was.
- telemetry has received exactly one event from scripts, its `service.started` with `version` `v<semver>`, the version `scripts --version` prints, under an empty request id and an empty user.
- It keeps running until it is signalled.

## The host starts scripts with no services file

A developer running scripts at a terminal, or a host whose services file is not there, gives scripts no list of services. It serves all the same, and every page request and tool call is answered as it would be with the file. What the file would have given is gone: the launcher in its pages' banner (`S03`), the instructions its MCP endpoint gives (`S05`), and the services a script finds through `IKIGENBA_SERVICES` (`S15`). And with no `telemetry` entry there is nowhere to send the trail, so every event scripts records goes to stderr as an `undelivered event` line (above), starting with `service.started`. A services file that exists but has no entry named `telemetry` is the same for the trail.

Command:

```
$ scripts
```

Output:

```
scripts: undelivered event: {"time":"<time>","service":"scripts","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
```

Does not exit. The line is on stderr, written once scripts is serving; stdout is empty. Every event scripts records from then on is written the same way, one line each.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`, and so do `git` and `python3.12`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.
- `IKIGENBA_SERVICES` is unset, or names a file that does not exist.
- `state/scripts.db` exists, from an earlier start, or can be created as in `The host starts scripts for the first time`.

Postconditions:

- scripts is serving on the socket it was passed, and answers every request as it would with a services file: its pages have no launcher, and its MCP endpoint gives no instructions.
- Every event scripts records is on stderr as an `undelivered event` line; none was sent anywhere.

## The host starts scripts where its state directory cannot be created

A regular file named `state` occupies the path where scripts needs its state directory. scripts has taken its socket and found git and `python3.12`, but it reports the database setup failure and exits before it serves or reports ready. The failure is not the caller's usage, so it exits 1.

Command:

```
$ scripts
```

Output:

```
scripts: cannot open database state/scripts.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying directory-creation failure.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`, and so do `git` and `python3.12`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.
- `state` is an existing regular file in scripts' working directory.

Postconditions:

- The existing `state` file is unchanged.
- No database was created, and no runs directory either. scripts served nothing, ran no git and no script, told systemd nothing, and sent telemetry nothing.

## The host starts scripts with a database it cannot open

`state/scripts.db` exists but scripts cannot open it — the file is not writable by the service user, or its contents are not a valid SQLite database. scripts writes a diagnostic naming the database problem and exits before it serves or reports ready. A missing file is not this error: an absent `state/scripts.db` is created, empty, as on a first start. scripts does not rebuild a catalog from anywhere, not even from the run folders under `state/runs/`: the scripts and runs a lost database held come back only from its replica (`S21`). Under systemd the start fails, and `systemctl start` reports it; the socket stays up, and requests and the gateway's calls wait in its queue for a scripts that can answer them.

Command:

```
$ scripts
```

Output:

```
scripts: cannot open database state/scripts.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying open failure.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`, and so do `git` and `python3.12`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.
- `state/scripts.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. `state/scripts.db` is as it was, and `state/runs/` is as it was, or still absent. scripts served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts scripts with a database a newer scripts has upgraded

A deploy rolled back to an older binary leaves it over a database a newer scripts has upgraded: the database records a migration this scripts does not carry, so its schema is one this scripts does not understand. Rather than read it, scripts refuses to start, naming the version it does not know, and the rollback fails loudly instead of serving wrong answers. There is no way back down a migration; restoring the database from before the upgrade is the rollback. `scripts db status` shows the version as `unknown` (`S01`).

Command:

```
$ scripts
```

Output:

```
scripts: cannot open database state/scripts.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` names the version this scripts does not carry, zero-padded to four digits: `0003`.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`, carrying only migrations `0001` and `0002`, and so do `git` and `python3.12`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.
- `state/scripts.db` exists and records versions `0001`, `0002` and `0003` as applied.

Postconditions:

- Nothing has changed: the database still records `0001`, `0002` and `0003` and holds the scripts and runs it held, none marked `killed` and none pruned, and `state/runs/` is as it was, or still absent. scripts served nothing, ran no git and no script, told systemd nothing, and sent telemetry nothing.

## The host starts scripts where its runs directory cannot be created

A regular file named `state/runs` occupies the path where scripts keeps its run folders. scripts has opened its database, but with nowhere to put a run's folder it could start no run, so it reports the failure and exits before it serves or reports ready. The failure is not the caller's usage, so it exits 1.

Command:

```
$ scripts
```

Output:

```
scripts: cannot create directory state/runs: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying directory-creation failure.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`, and so do `git` and `python3.12`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.
- `state/scripts.db` exists, or can be created as in `The host starts scripts for the first time`.
- `state/runs` is an existing regular file.

Postconditions:

- The existing `state/runs` file is unchanged.
- `state/scripts.db` exists, created by this start if it was absent, and names what it named before: no run was marked `killed` and none was pruned. scripts served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts scripts where git is not installed

scripts unpacks every run's tree by running git, and checks a repository's owner by running it too, so without git it can run no script and create none, and it says so at start rather than with a failed run later. git is looked for on the `PATH` scripts was started with, in the order of its directories as a shell would look: an executable file named `git` in one of them. Unlike a shell, scripts skips an empty or relative entry, so it never runs a `git` it finds through its working directory. This is not the caller's usage but a host missing a dependency it provides, installed on a space by the space's first-boot script, so it exits 1, and it does so before it opens the database, so a host without git keeps its state as it was. scripts looks for git before `python3.12` (`S22`), so this is reported whether or not `python3.12` is installed.

Command:

```
$ scripts
```

Output:

```
scripts: git not found on PATH
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is run by its path.
- No directory on the `PATH` scripts is started with holds an executable named `git`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.

Postconditions:

- Nothing has changed. scripts opened no database, and an absent `state/scripts.db` is still absent, as is an absent `state/runs/`; no run was marked `killed` and none was pruned; it served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts scripts with REPOS_DIR unset

`REPOS_DIR` unset or empty means the manifest's default, `../repos/state/repos`, the same as writing that value, so the two forms below, `REPOS_DIR` unset and `REPOS_DIR` empty, behave identically: on a host, where repos runs from `/opt/repos`, that is where repos keeps its bare repositories. scripts does not look in the directory at start, so the start is the same whether repos is installed or not (`S21`); the directory is first read when a tool, a run or a page needs a repository.

Command:

```
$ scripts
```

```
$ REPOS_DIR= scripts
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`, and so do `git` and `python3.12`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS`, `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, `OPERATION_SECONDS`, `SCRIPT_SECONDS`, `RUN_KEEP_DAYS`, and `RUN_KEEP_COUNT` are unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry names a socket a listener holds that takes every event.
- scripts' working directory is `/opt/scripts`, and `/opt/repos/state/repos/rep_9c2e4b7a1d3f8e05.git` is repos' bare repository `nightly-report`, owned by `u_7f3a9c21`.

Postconditions:

- scripts is serving, and telemetry has received its `service.started`.
- `create` by `u_7f3a9c21` with `repo` `rep_9c2e4b7a1d3f8e05` finds that repository at `../repos/state/repos/rep_9c2e4b7a1d3f8e05.git`, relative to `/opt/scripts`, and makes the script (`S06`); a run of a script that names it is unpacked from there (`S08`).
- Nothing under `/opt/repos/state/repos/` has changed; scripts read nothing there at start.

## The host stops scripts

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C` sends `SIGINT`, which scripts treats the same way. scripts stops taking new connections, finishes the requests it has already accepted, and exits. A request that runs git is finished when its git is: a `create` reading its repository at the signal goes on until the script is made and the caller has its whole result. What becomes of a run still running at the signal, and of a `run` call scripts reaches after it, is told in `S18`; in this story none is running. scripts closes its own copy of the socket and nothing more: the socket belongs to systemd, which keeps it open, and scripts never removes `/run/ikigenba/scripts.sock`. It waits at most `DRAIN_SECONDS` for requests to finish, which on a host is always less than the time the service unit allows before systemd kills it. Once its requests have finished, and each has recorded its `request.finished`, scripts records `service.stopping` with the reason it is stopping, the name of the signal it received, `SIGTERM` or `SIGINT`. That is the last event of its trail: scripts sends everything it recorded before exiting, within the same drain deadline. Sending takes time, so a stop is silent when the requests leave scripts at least a second of the drain for it; a request that finishes later still gets its whole response, but an event scripts has not sent when the deadline comes goes to stderr as an `undelivered event` line instead. A `service.started` with no `service.stopping` before the next one is how the trail shows a scripts that died rather than stopped.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

scripts exits 0. Nothing is on stdout or stderr.

Preconditions:

- scripts is serving as process `<pid>`, on the socket it was passed, over `S06`'s shared catalog, and no run is running.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `u_7f3a9c21`'s `create` of `crm-weekly` from `rep_41d8f0a6b2c97e13` is running at the signal: its git read of the repository has begun.
- That call, and every other request scripts has accepted, finishes at least a second before the drain deadline, within 4 seconds of the signal, so scripts has that second left to send what it recorded.
- telemetry takes every event as soon as it is sent.

Postconditions:

- Every request accepted before the signal received its full response. The create succeeded: its result is `crm-weekly`'s script object, with `repo` `rep_41d8f0a6b2c97e13` and `ref` `main` (`S06`), and `show` of `crm-weekly` by `u_7f3a9c21` answers it once a scripts is serving again (`S07`).
- telemetry has received every event scripts recorded: the create's `script.created` (`S16`), its `tool.called`, and its `request.finished` with `status` `200`; and last, after the `request.finished` of every request accepted before the signal:

  ```
  {"time":"<time>","service":"scripts","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

  With `SIGINT` the `reason` is `SIGINT`.
- No git process scripts started is still running.
- `/run/ikigenba/scripts.sock` still exists, and connections made to it after scripts exited wait in the socket's queue for the next scripts to answer.

## The host stops scripts while a request outlasts the drain

scripts waits for accepted requests only until its drain deadline, `DRAIN_SECONDS` after the signal, so that it always exits before the service unit's stop timeout and is never killed mid-write by systemd. A request still running at the deadline — a `create` whose git is still reading its repository, say — is cut off: scripts kills its git and closes its connection without the rest of the response. A `create` cut off makes no script. A `run` call cut off at the deadline, and the runs still running there, are told in `S18`; a run killed at the deadline is not a request and is not counted here. Losing a request is trouble, so scripts says how many it lost and exits non-zero.

The drain has used the whole deadline, so scripts has no time left to send `service.stopping`, or any other event not yet sent, to telemetry: each goes to stderr as an `undelivered event` line instead. telemetry never receives a `request.finished` for a request cut off: its `request.started` with no finish is how the trail shows it was cut off.

Command:

```
$ kill -TERM <pid>
```

Output:

```
scripts: undelivered event: {"time":"<time>","service":"scripts","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
scripts: stopped with <n> requests unfinished
```

scripts exits 1, 5 seconds after the signal. The text is on stderr; stdout is empty. `<n>` is the number of requests still running at the deadline. When `<n>` is 1 the line reads `scripts: stopped with 1 request unfinished`. The `service.stopping` line above is always written. Any other event scripts had recorded and not yet delivered when the deadline came is written as an `undelivered event` line too; those lines are in the order the events were recorded, `service.stopping` last of them. The position of the `stopped with` line among them is not fixed. stderr may also hold, for a cut-off request, an `undelivered event` line carrying its `request.finished`, recorded after the deadline and before scripts exited; whether it does, and where that line falls, is not fixed. stderr holds no other line.

Preconditions:

- scripts is serving as process `<pid>`, on the socket it was passed, over `S06`'s shared catalog, and no run is running.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `u_7f3a9c21`'s `create` of `crm-weekly` from `rep_41d8f0a6b2c97e13` is running at the signal, and its git read of the repository is still running 5 seconds after it.
- `<n>` of the requests scripts has accepted, the create among them, are still running 5 seconds after the signal.
- telemetry takes every event.

Postconditions:

- Every request that finished within 5 seconds of the signal received its full response; the `<n>` that did not were cut off. The create's git was killed, and its caller received no result.
- There is no script `crm-weekly`: `show` of it answers `no script named 'crm-weekly'` once a scripts is serving again (`S07`), and the create recorded no `script.created` and no `tool.called`.
- No git process scripts started is still running.
- Each of the `<n>` cut-off requests has its `request.started` recorded (delivered to telemetry, written to stderr as undelivered, or both, when it was being delivered as the deadline came). telemetry has received no `request.finished` for any of them, and no `service.stopping`.
- `/run/ikigenba/scripts.sock` still exists, and connections made to it after scripts exited wait in the socket's queue for the next scripts to answer.

## The host restarts scripts during a deploy

A deploy replaces scripts' binary and restarts `ikigenba-scripts.service` alone; `ikigenba-scripts.socket` stays up throughout. Between the old scripts exiting and the new one being ready, connections wait in the socket's queue instead of being refused, so a user or the gateway never sees scripts missing. That holds because scripts finishes what it accepted before it exits and leaves the socket where systemd put it. The new scripts runs no git and no script at start, so the wait is short; the run folders the old scripts left are still under `state/runs/`, and the new one shows them as they are. The catalog outlives the deploy: the new scripts opens the same `state/scripts.db`, bringing it up to date first when the new binary carries a migration the database has not had, and serves every script and run the old one kept. A deploy while a run is running kills that run; that is told in `S18`, and in this story none is running.

Command:

```
$ sudo systemctl restart ikigenba-scripts.service
```

Output:

```
```

Exits 0, once the new scripts has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- scripts is serving on `/run/ikigenba/scripts.sock` under `ikigenba-scripts.service`, over `S06`'s shared catalog, with no run running and none past keeping.
- Users are loading scripts' pages, and the gateway is calling scripts' tools other than `run`, throughout the restart, and every request the old scripts accepted finishes at least a second before its drain deadline.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- Every request sent was answered, by the old scripts or the new one; none was refused and none was cut off.
- A new scripts process is serving on `/run/ikigenba/scripts.sock`, over the same `state/scripts.db` and the same `state/runs/`; every run and run folder is as the old scripts left it.
- telemetry has received the old scripts' `service.stopping`, with `reason` `SIGTERM`, and after it the new scripts' `service.started`, whose `version` is the version the new binary's `scripts --version` prints. Every request the old scripts answered is recorded before its `service.stopping`, and every request the new one answered after its `service.started`.

## The host starts scripts where telemetry cannot be reached

The trail is not a reason to stop serving. When scripts cannot deliver its events — telemetry is not installed yet, is stopped, or the services file names no `telemetry` entry — scripts starts and serves exactly as it does otherwise, and its events go to the journal as `undelivered event` lines (above). No answer and no run waits on telemetry, so a user or the gateway sees no difference. scripts keeps looking for telemetry with every event, so once telemetry takes events again, scripts' next events go to it without a restart; an event already written to the journal is not sent again.

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed scripts, and `ikigenba-scripts.socket` is active, as in `The host starts scripts`.
- `ikigenba-scripts.service` is not running.
- The host's services file lists no `telemetry` entry, or nothing accepts connections on the socket that entry names.

Postconditions:

- `ikigenba-scripts.service` is `active`, and scripts is serving on `/run/ikigenba/scripts.sock`, as in `The host starts scripts`.
- The journal holds one line from scripts, written after it reported that it was ready:

  ```
  scripts: undelivered event: {"time":"<time>","service":"scripts","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
  ```

- Every event scripts records while telemetry cannot be reached — every `run.started` and `run.finished` among them — is written to the journal the same way, one line each, and every request is answered, and every run started and ended, as it would be with telemetry taking events.

## The host starts scripts without a socket

Run bare, with no socket passed in, scripts has nothing to serve on, and it does not open one of its own: there is no port or address it falls back to. That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or `LISTEN_PID` naming some other process, counts as no socket passed in. The detail says how to run scripts correctly. scripts looks for its socket before it looks for git or `python3.12`, so this is reported whether or not either is installed.

Command:

```
$ scripts
```

Output:

```
scripts: no socket was passed in

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not scripts' process id.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.

Postconditions:

- Nothing has changed. scripts opened no database, created no `state/runs/`, listened on nothing, told systemd nothing, and sent telemetry nothing; an absent `state/scripts.db` is still absent.

## The host passes scripts more than one socket

scripts serves on exactly one socket. A unit that passes it several is misconfigured, and scripts will not guess which one it was meant to serve on.

Command:

```
$ scripts
```

Output:

```
scripts: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `2`: two listening sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` and each of the seven settings are unset, or valid.

Postconditions:

- Nothing has changed. scripts opened no database, served on neither socket, told systemd nothing, and sent telemetry nothing.

## The host gives scripts a drain deadline that is not a number of seconds

scripts reads `DRAIN_SECONDS` before it serves, so a bad value is found at start rather than at the moment scripts is asked to stop. A value that is not a positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and scripts serves nothing. The value is quoted back verbatim. scripts sets no upper limit: keeping the drain inside the service unit's stop timeout is opsctl's to enforce. It checks `DRAIN_SECONDS` before `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, `OPERATION_SECONDS`, `SCRIPT_SECONDS`, `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT`, so it is the one named when several are bad, and before it looks for its socket, for git or for `python3.12`, so it is reported whether or not a socket was passed in or either is installed.

Command:

```
$ DRAIN_SECONDS=abc scripts
```

Output:

```
scripts: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. scripts opened no database, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives scripts a tree size limit that is not a number of bytes

scripts reads `TREE_MAX_BYTES` before it serves, so a bad value is found at start rather than when a run first needs it. A value that is not a positive whole number — `0`, `-1`, `2.5`, `256M`, or `abc` — is the caller's mistake, so it is a usage error and scripts serves nothing: a limit it cannot read is not a limit it can guess, and guessing one could let one run fill the host's disk or refuse every run. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). scripts sets no upper limit. It checks `TREE_MAX_BYTES` after `DRAIN_SECONDS` and before `OUTPUT_MAX_BYTES`, `OPERATION_SECONDS`, `SCRIPT_SECONDS`, `RUN_KEEP_DAYS`, and `RUN_KEEP_COUNT`, so it is the one named when it and any of those are bad, and before it looks for its socket, for git or for `python3.12`, so it is reported whether or not a socket was passed in or either is installed.

Command:

```
$ TREE_MAX_BYTES=abc scripts
```

Output:

```
scripts: TREE_MAX_BYTES is 'abc', not a positive whole number of bytes
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. scripts opened no database, ran no git and no script, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives scripts an output limit that is not a number of bytes

scripts reads `OUTPUT_MAX_BYTES` before it serves, so a bad value is found at start rather than when a run first writes output. A value that is not a positive whole number — `0`, `-1`, `2.5`, `1M`, or `abc` — is the caller's mistake, so it is a usage error and scripts serves nothing: a bound it cannot read could let one run's output fill the host's disk, or keep none of any run's. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). scripts sets no upper limit. It checks `OUTPUT_MAX_BYTES` after `DRAIN_SECONDS` and `TREE_MAX_BYTES` and before `OPERATION_SECONDS`, `SCRIPT_SECONDS`, `RUN_KEEP_DAYS`, and `RUN_KEEP_COUNT`, and before it looks for its socket, for git or for `python3.12`, so it is reported whether or not a socket was passed in or either is installed.

Command:

```
$ OUTPUT_MAX_BYTES=abc scripts
```

Output:

```
scripts: OUTPUT_MAX_BYTES is 'abc', not a positive whole number of bytes
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and `TREE_MAX_BYTES` are unset, or a positive whole number.

Postconditions:

- Nothing has changed. scripts opened no database, ran no git and no script, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives scripts a git time limit that is not a number of seconds

scripts reads `OPERATION_SECONDS` before it serves, so a bad value is found at start rather than when a git run first needs it. A value that is not a positive whole number — `0`, `-1`, `2.5`, `10m`, or `abc` — is the caller's mistake, so it is a usage error and scripts serves nothing: a git run with a deadline scripts cannot read could hold a `run` call for as long as git likes. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). scripts sets no upper limit. It checks `OPERATION_SECONDS` after `DRAIN_SECONDS`, `TREE_MAX_BYTES`, and `OUTPUT_MAX_BYTES` and before `SCRIPT_SECONDS`, `RUN_KEEP_DAYS`, and `RUN_KEEP_COUNT`, and before it looks for its socket, for git or for `python3.12`, so it is reported whether or not a socket was passed in or either is installed.

Command:

```
$ OPERATION_SECONDS=abc scripts
```

Output:

```
scripts: OPERATION_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS`, `TREE_MAX_BYTES`, and `OUTPUT_MAX_BYTES` are unset, or a positive whole number.

Postconditions:

- Nothing has changed. scripts opened no database, ran no git and no script, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives scripts a script time limit that is not a number of seconds

scripts reads `SCRIPT_SECONDS` before it serves, so a bad value is found at start rather than when a run first needs it. A value that is not a positive whole number — `0`, `-1`, `2.5`, `10m`, or `abc` — is the caller's mistake, so it is a usage error and scripts serves nothing: a script with a deadline scripts cannot read could run for as long as it likes. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). scripts sets no upper limit. It checks `SCRIPT_SECONDS` after `DRAIN_SECONDS`, `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, and `OPERATION_SECONDS` and before `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT`, and before it looks for its socket, for git or for `python3.12`, so it is reported whether or not a socket was passed in or either is installed.

Command:

```
$ SCRIPT_SECONDS=abc scripts
```

Output:

```
scripts: SCRIPT_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS`, `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, and `OPERATION_SECONDS` are unset, or a positive whole number.

Postconditions:

- Nothing has changed. scripts opened no database, ran no git and no script, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives scripts a run age limit that is not a number of days

scripts reads `RUN_KEEP_DAYS` before it serves, so a bad value is found at start rather than when it first prunes, which it does at start (`S19`). A value that is not a positive whole number — `0`, `-1`, `2.5`, `15d`, or `abc` — is the caller's mistake, so it is a usage error and scripts serves nothing: a limit it cannot read is not one it can guess, and guessing one could remove runs a user still wants. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). scripts sets no upper limit. It checks `RUN_KEEP_DAYS` after `DRAIN_SECONDS`, `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, `OPERATION_SECONDS`, and `SCRIPT_SECONDS` and before `RUN_KEEP_COUNT`, and before it looks for its socket, for git or for `python3.12`, so it is reported whether or not a socket was passed in or either is installed.

Command:

```
$ RUN_KEEP_DAYS=abc scripts
```

Output:

```
scripts: RUN_KEEP_DAYS is 'abc', not a positive whole number of days
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS`, `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, `OPERATION_SECONDS`, and `SCRIPT_SECONDS` are unset, or a positive whole number.

Postconditions:

- Nothing has changed. scripts opened no database, pruned no run, ran no git and no script, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives scripts a run count to keep that is not a number of runs

scripts reads `RUN_KEEP_COUNT` before it serves, so a bad value is found at start rather than when it first prunes (`S19`). A value that is not a positive whole number — `0`, `-1`, `2.5`, `1e1`, or `abc` — is the caller's mistake, so it is a usage error and scripts serves nothing. `0` is refused like the rest: every script keeps at least its newest run whatever its age. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). scripts sets no upper limit. It checks `RUN_KEEP_COUNT` last of its settings, after `DRAIN_SECONDS`, `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, `OPERATION_SECONDS`, `SCRIPT_SECONDS`, and `RUN_KEEP_DAYS`, and before it looks for its socket, for git or for `python3.12`, so it is reported whether or not a socket was passed in or either is installed.

Command:

```
$ RUN_KEEP_COUNT=abc scripts
```

Output:

```
scripts: RUN_KEEP_COUNT is 'abc', not a positive whole number of runs
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is on the `PATH` as `scripts`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS`, `TREE_MAX_BYTES`, `OUTPUT_MAX_BYTES`, `OPERATION_SECONDS`, `SCRIPT_SECONDS`, and `RUN_KEEP_DAYS` are unset, or a positive whole number.

Postconditions:

- Nothing has changed. scripts opened no database, pruned no run, ran no git and no script, served nothing, told systemd nothing, and sent telemetry nothing.
