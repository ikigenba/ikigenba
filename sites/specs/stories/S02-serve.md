# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits: sites never opens one of its own, and there is no port or address it falls back to. It takes the socket the way systemd socket activation passes it — `LISTEN_PID` names sites' own process, `LISTEN_FDS` is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*` variables from its environment once it has taken it. On a host, opsctl publishes `ikigenba-sites.socket`, which holds the Unix socket `/run/ikigenba/sites.sock`, beside `ikigenba-sites.service`, which runs `/opt/sites/bin/sites` with no arguments as the `ikigenba` user, with `/opt/sites` as its working directory and `/opt/sites/etc/env` as its environment file; nginx proxies sites' public name, `sites.<space>`, and the apex host when it is routed to sites (`S13`), to `http://unix:/run/ikigenba/sites.sock:`. The service is `Type=notify`: sites tells systemd it is ready, by sending `READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, sites drains for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its environment, and 5 when that is unset or empty. On a host, opsctl owns this value and the service unit's stop timeout: both are space-wide settings in opsctl's configuration, opsctl writes the drain into every app's `etc/env` and the stop timeout (10 seconds by default, always longer than the drain) into every service unit, and an app's manifest never sets either. The actor in these stories is the host, whether that is systemd or a developer at a terminal standing in for it.

sites' environment also carries its three settings, each the manifest's default (`S01`) when it is unset or empty; the host writes the defaults into `etc/env`, and an operator changes them there. `REPOS_DIR`, `../repos/state/repos`, is the directory holding repos' bare repositories, each as `<repository id>.git`; a relative value is resolved against sites' working directory, so on a host it is `/opt/sites/../repos/state/repos`, which is `/opt/repos/state/repos`, and an absolute one is used as it is (`S18`). sites only reads there, with git, and never writes there. `SITE_MAX_BYTES`, 268435456, a positive whole number of bytes, is the largest a site's tree may be, counted as the sum of its files' sizes (`S17`). `OPERATION_SECONDS`, 600, a positive whole number of seconds, is the longest one git run may take before sites kills it (`S17`). And it carries `IKIGENBA_SERVICES`, the path of the host's services file, normally `/var/lib/ikigenba/services.json`, which opsctl sets in the environment the host gives sites. The file lists the platform's services: it feeds the launcher in the banner of sites' landing and about pages (`S03`), it holds the description sites' MCP endpoint gives its clients as instructions (`S05`), its entry named `sites` gives the public address sites puts in every site's `url` and on its landing page (`S03`, `S07`), and its entry named `telemetry` is where sites sends its trail (below). sites reads the variable once, when it starts, and reads the file it names afresh whenever it needs it, so a rewritten file shows without a restart. sites never fails to start over it: unset, empty, or naming a file that is missing, unreadable, or malformed, sites starts and serves all the same, treats the file as listing no services, and says nothing about the file itself.

sites runs the host's own `git` for every read of a repository — resolving a ref, reading a repository's owner, unpacking a commit's tree with `git archive` — and has no git of its own; `git` is a dependency of the host that opsctl provisions. sites checks its environment first — `DRAIN_SECONDS`, then `SITE_MAX_BYTES`, then `OPERATION_SECONDS` — then looks for its socket, then checks that an executable named `git` is on its `PATH`, and only then opens its SQLite database, the catalog of sites and the apex setting, at `state/sites.db`, relative to its working directory, creating `state/` and the database on its first start and bringing the database up to date by applying, in order, every migration it carries that the database has not had (`S01`), and then the directory that holds the unpacked trees, `cache/sites/`, creating `cache/` and `cache/sites/` when they are absent. Then it is ready. `REPOS_DIR` is not checked at start: repos may be installed after sites, and a repository is looked for only when a tool or a site request needs it (`S19`). No tree is unpacked at start: a published site whose tree is missing from `cache/` is rebuilt by the first request that needs it (`S16`). A database it cannot open is a start it refuses, with one line on stderr, `sites: cannot open database state/sites.db: <reason>`, and exit status 1. A database that records a migration it does not carry is one a newer sites has upgraded; sites applies nothing to it, warns on stderr that it is ahead, and serves it as usual (below). So a start refused as a usage error, or for want of git, has touched nothing, not even the database. sites is the database's only writer, and the host replicates it as the manifest declares (`S01`, `S21`); `cache/` is never backed up.

sites' environment may also carry `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`, from which it builds `<display>`, the display string `sites --version` prints under the same environment (`S01`). sites reads them once, when it starts, and shows that string as its version wherever it shows one: in its `service.started` event (below), its pages' footer and its about screen (`S03`), and its MCP `serverInfo` (`S05`). With neither set the string is empty, and sites starts and serves all the same.

sites keeps a trail: it records what it does as events it sends to the platform's telemetry service, exactly as repos does, where an operator, or an agent working for one, follows what happened from one thing they know — a request id, a user, a site's id, a visitor's id, or a time. sites finds telemetry in the services file `IKIGENBA_SERVICES` names, as the entry named `telemetry`, and sends each event to that entry's socket; it looks the entry up afresh for every event, so a telemetry installed after sites started is found without a restart. What telemetry does with an event is told in telemetry's own stories. The stories here show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"sites","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when sites recorded the event, in UTC to the microsecond, as `2026-10-02T14:03:07.123456Z`; `service` is always `sites`; `<request-id>` and `<user>` are the id of the request that caused the event and the caller's `X-User-Id`, each empty when there is none, as for a guest's request, which has a request id and no user, or for an event no request caused — a start or a stop; and `attrs` holds the event's attributes, flat, their keys in alphabetical order. Attributes name what happened and the ids of what it touched, never data: no event carries a site's name or slug in an attribute of its own, a repository's name, a file's content, a caller's email, a credential, a request's query, a `Referer` beyond its host, or a tool's arguments. The places a slug reaches the trail are the `path` of a request's `request.started` and of a site's `site.viewed`, `/blog/` say, which is the URL path as it arrived, as on every app. A site is named in an attribute by its id, under the key `site`, a repository by its id under `repo`, and a visitor by its id under `visitor`. sites sends its events one at a time, in the order it recorded them, and an answer never waits for its events to be sent. Telemetry takes an event by answering `204`. When it cannot be reached — the services file is unset, unreadable, or has no `telemetry` entry, or nothing answers on its socket — or it answers anything other than `204` or a `4xx`, sites tries the event a few times over a fraction of a second. When it answers `4xx`, it has refused the event itself, and sending it again cannot help, so sites does not retry it. Either way sites then writes the event to stderr as one line, `sites: undelivered event: <event>`, where `<event>` is the JSON telemetry would have received, and carries on serving, so nothing in the trail is lost without trace. A developer whose environment names no services file therefore sees every event on stderr. A developer stands in for telemetry with a services file whose `telemetry` entry names a socket that a listener of their own holds and that takes every event it is sent; a story that says telemetry takes every event means that, or, on a host, the telemetry service itself.

sites records these events and no others:

- `service.started`, once sites is serving and has told systemd it is ready, with `version`, `<display>`, the string `sites --version` prints (`S01`);
- `service.stopping`, when sites is told to stop and has finished the requests it accepted, with `reason`, the name of the signal that stopped it, `SIGTERM` or `SIGINT`; it is the last event sites records;
- `request.started`, as each request arrives, with `method` and `path`, the request's URL path without its query;
- `request.finished`, once that request's answer is complete, with `status`, the status of sites' answer; `duration_us`, how long sites took to answer, in whole microseconds; `request_bytes`, how many bytes of the request's body sites read; and `response_bytes`, how many bytes of the response's body sites wrote; the three vary from request to request, and a story's event JSON shows `duration_us` as `<n>` and the two byte counts as `<bytes>` unless it fixes them;
- `tool.called`, for each call of one of its seven tools that is answered with a result, with `tool`, `kind`, `outcome`, and `duration_us` (`S05`);
- `site.created`, when `create` makes a site, with `site`, `repo`, `visibility`, and `listed` (`S06`);
- `site.published`, when `publish` points a site at a commit, with `site`, `commit`, and `ref` (`S08`);
- `site.updated`, once for each field `update` changes, with `site`, `field`, and `value` (`S09`);
- `site.deleted`, when `delete` removes a site, with `site` (`S10`);
- `site.apex`, when the apex site is set or cleared, by `apex` or by deleting the apex site, with `site`, empty when cleared (`S13`, `S10`);
- `site.viewed`, for each answer sites gives a request for a known site's path, with `site`, `visitor`, `path`, `status`, `referrer_host`, and `commit` (`S11`, `S12`, `S14`, `S15`);
- `site.unavailable`, when a site's published tree cannot be rebuilt for a request, with `site`, `commit`, and `reason` (`S16`, `S17`).

The events of a request carry its request id and its caller, and come in this order: `request.started`, then the request's domain events, then `tool.called` for a tool call, then `request.finished`.

stderr holds only trouble: a condition sites cannot go on from — the start-up refusals below and the requests lost to a drain cut short — and an event sites could not deliver. A failure sites handles is not trouble: a tool call refused, a publish whose git failed or ran too long, a site that cannot be rebuilt and is answered `503`, a path answered `404`, a request answered 500, like a request answered any other way, is recorded in the trail or the tool's result and earns no line on stderr. So while telemetry takes every event, a running sites writes nothing to stdout or stderr, and under systemd the journal holds only trouble. Every line sites writes to stderr begins `sites: `. What git itself writes to its stderr while sites runs it goes into the tool error of the publish that ran it (`S08`), or nowhere; it never reaches sites' own stderr.

These are the terms every app of the platform serves on, the same as repos'. The socket is the app's only way in. Every app runs as the one `ikigenba` user, so any app can reach any sibling's socket, and nginx reaches them all; nothing else on the host can. The suite is a closed system that only we deploy services into, and an app trusts the suite: it trusts the headers nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated, and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every request and overwrites whatever a client sent — and it trusts a sibling that calls it to have forwarded them. sites' manifest declares `guests = true` (`S01`), so nginx also passes a visitor with no credential to sites, outside `/mcp`, `/api`, and the paths under them, with no `X-User-Id` and no `X-User-Email` and with its `X-Request-Id`, having dropped any identity header the client sent (opsctl's `S5-nginx.md`); such a request is a guest's. The mcp gateway calls sites at its socket (`S05`). A request that reaches sites with no `X-Request-Id`, or an empty one, as a developer's request does, is given an id of the same shape by sites, a new one for each such request, so every event about a request names it. sites calls no sibling while serving a request, and reaches repos only through its bare repositories on disk.

## The host starts sites

The socket keeps out every process that is not part of the suite or nginx, which a port on loopback would not: any process on the host can connect to a loopback port, and only the `ikigenba` user and nginx can connect to `/run/ikigenba/sites.sock`. systemd owns the socket, so it exists, and accepts connections into its queue, before sites starts and while it is stopped; sites' part is to serve what arrives on it. `systemctl start` returns once sites has opened its catalog and its cache directory and reported that it is ready. At that moment sites records `service.started`, with `<display>` as its `version`: a new value there is how a deploy shows in the trail.

Command:

```
$ sudo systemctl start ikigenba-sites.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed sites: `/opt/sites/bin/sites` exists, and `ikigenba-sites.socket` and `ikigenba-sites.service` are published.
- `/opt/sites/etc/env` sets `DRAIN_SECONDS` and each of the three settings to a valid value, and `IKIGENBA_SERVICES` to the host's services file.
- `git` is installed on the host, on the `PATH` the service runs with.
- `ikigenba-sites.socket` is active, so `/run/ikigenba/sites.sock` exists and accepts connections.
- `/opt/sites/state/sites.db` exists, from an earlier start, records no migration this sites does not carry, and holds the sites `blog`, `handbook`, `scratch`, and `recipes`; `/opt/sites/cache/sites/` exists.
- `ikigenba-sites.service` is not running.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `ikigenba-sites.service` is `active`, and sites is serving on `/run/ikigenba/sites.sock`: a connection there, and every connection queued before sites started, is answered by sites.
- sites listens on no other socket and no port.
- `/opt/sites/state/sites.db` is the database it opened, now up to date, and holds the same sites and apex as before the start, each site unchanged; every tree under `/opt/sites/cache/sites/` is as it was, and none was unpacked by the start (`S19`).
- telemetry has received one event from sites, with no request id and no user, whose `version` is `<display>`, the string `sites --version` prints under the environment the host gives sites (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`:

  ```
  {"time":"<time>","service":"sites","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

- sites has written nothing to the journal.
- It keeps running until it is signalled.

## The host starts sites for the first time

Nothing of sites' state exists yet. sites creates the `state/` directory if it is absent, then creates `state/sites.db` and applies every migration it carries, then creates `cache/` and `cache/sites/`, empty, and serves with a catalog that names no site and no apex. The same start succeeds when `state/` or `cache/` already exists and only the database or `cache/sites/` is absent. Neither a fresh deployment nor any other first start needs a directory created beforehand. The paths are relative to sites' working directory.

Command:

```
$ sites
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`, and so does `git`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so sites reports readiness to nobody.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry names a socket a listener holds that takes every event.
- Neither `state/sites.db` nor `cache/` exists.
- Either `state/` is absent and sites can create it in its working directory, or `state/` is an existing directory in which sites can create the database; sites can create `cache/` in its working directory.

Postconditions:

- `state/` exists, created by sites if it was absent.
- `state/sites.db` now exists, created by this start, and is up to date: `sites db status` prints `0001 applied <time>`, `<time>` being the moment this start applied it (`S01`). It names no site and no apex: `list` answers `{"sites":[]}` for every caller (`S07`), and `apex` with no arguments answers `{"apex":null}` (`S13`).
- `cache/sites/` now exists, empty, created by this start.
- sites is serving on the socket it was passed, and on no other.
- telemetry has received exactly one event from sites, its `service.started` with `version` `<display>`, under an empty request id and an empty user.
- It keeps running until it is signalled.

## The host starts sites with no services file

A developer running sites at a terminal, or a host whose services file is not there, gives sites no list of services. It serves all the same, and every site request and tool call is answered as it would be with the file. What the file would have given is gone: the launcher in its pages' banner (`S03`), the instructions its MCP endpoint gives (`S05`), and the `sites` entry's address, so every site's `url` and the landing page's addresses are built from the request's own `Host` and `X-Forwarded-Proto` instead (`S03`, `S07`). A guest's sign-in redirect is built from the request's `Host` either way, so it is unchanged (`S12`). And with no `telemetry` entry there is nowhere to send the trail, so every event sites records goes to stderr as an `undelivered event` line (above), starting with `service.started`. A services file that exists but has no entry named `telemetry` is the same for the trail.

Command:

```
$ sites
```

Output:

```
sites: undelivered event: {"time":"<time>","service":"sites","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
```

Does not exit. The line is on stderr, written once sites is serving; stdout is empty. Every event sites records from then on is written the same way, one line each.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`, and so does `git`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.
- `IKIGENBA_SERVICES` is unset, or names a file that does not exist.
- `state/sites.db` exists, from an earlier start, or can be created as in `The host starts sites for the first time`.

Postconditions:

- sites is serving on the socket it was passed, and answers every request as it would with a services file: its pages have no launcher, its MCP endpoint gives no instructions, and its site URLs take their address from each request's `Host` (`S07`).
- Every event sites records is on stderr as an `undelivered event` line; none was sent anywhere.

## The host starts sites where its state directory cannot be created

A regular file named `state` occupies the path where sites needs its state directory. sites has taken its socket and found git, but it reports the database setup failure and exits before it serves or reports ready. The failure is not the caller's usage, so it exits 1.

Command:

```
$ sites
```

Output:

```
sites: cannot open database state/sites.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying directory-creation failure.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`, and so does `git`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.
- `state` is an existing regular file in sites' working directory.

Postconditions:

- The existing `state` file is unchanged.
- No database was created, and no `cache/` either. sites served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts sites with a database it cannot open

`state/sites.db` exists but sites cannot open it — the file is not writable by the service user, or its contents are not a valid SQLite database. sites writes a diagnostic naming the database problem and exits before it serves or reports ready. A missing file is not this error: an absent `state/sites.db` is created, empty, as on a first start. sites does not rebuild a catalog from anywhere: the sites a lost database held come back only from its replica (`S19`, `S21`). Under systemd the start fails, and `systemctl start` reports it; the socket stays up, and visitors' requests and the gateway's calls wait in its queue for a sites that can answer them.

Command:

```
$ sites
```

Output:

```
sites: cannot open database state/sites.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying open failure.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`, and so does `git`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.
- `state/sites.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. `state/sites.db` is as it was, and `cache/` is as it was, or still absent. sites served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts sites with a database a newer sites has upgraded

A deploy rolled back to an older binary leaves it over a database a newer sites has upgraded: the database records a migration this sites does not carry. Data never rolls back, so older code must run on newer data, and this sites serves the database as it stands. It applies nothing, not even a migration it carries that the database lacks, and writes one line to stderr naming the lowest version it does not carry, zero-padded to four digits as `sites db status` prints it; on a host that line goes to the journal. Then it opens its cache directory, serves, and tells systemd it is ready, as in any start. The warning is a line on stderr only, not an event in the trail. `sites db status` lists every version the database records, the unknown ones as `unknown` (`S01`).

Command:

```
$ sites
```

Output:

```
sites: unknown migration version 0002: database is ahead of this binary
```

Does not exit. The line is on stderr, written before sites serves; stdout is empty.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`, carrying only migration `0001`; `git` is on the `PATH` too.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry takes every event.
- `state/sites.db` exists and records versions `0001` and `0002` as applied. It holds `S06`'s shared catalog, with `blog` as the apex site.

Postconditions:

- The database still records `0001` and `0002`, and no other version; this start applied nothing.
- sites is serving on the socket it was passed, over that `state/sites.db`.
- `list` for each caller answers what that catalog holds (`S07`), and `apex` with no arguments answers `blog`'s site object (`S13`).
- telemetry has received sites' `service.started`, whose `version` is `<display>`, and no event about the warning.
- It keeps running until it is signalled.

## The host starts sites over a catalog from before it recorded its migrations

The first release of sites that records its migrations is deployed over the catalog an earlier sites kept, which records none. sites applies its first migration, `0001`, which finds the catalog's schema already there and changes nothing in it, records `0001` as applied, and serves every site and the apex the catalog held. No step of the deploy prepares the database beforehand.

Command:

```
$ sudo systemctl restart ikigenba-sites.service
```

Output:

```
```

Exits 0, once the new sites has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed a sites that carries migration `0001` over one that recorded no migrations.
- `/opt/sites/state/sites.db` is the catalog the earlier sites kept: it holds `S06`'s shared catalog, with `blog` as the apex site, and records no migration (`sites db status` prints `0001 pending`, `S01`).
- `git` is installed on the host, on the `PATH` the service runs with.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- sites is serving on `/run/ikigenba/sites.sock`, over the same `state/sites.db`.
- `sites db status` prints `0001 applied <time>`, `<time>` being the moment this start applied it (`S01`).
- `list` for each caller answers what it answered before the deploy (`S07`), and `apex` with no arguments answers `blog`'s site object (`S13`): no site, field, or setting changed.
- telemetry has received the new sites' `service.started`, whose `version` is `<display>`.

## The host starts sites where its cache directory cannot be created

A regular file named `cache`, or `cache/sites`, occupies the path where sites keeps its unpacked trees. sites has opened its database, but with nowhere to unpack a tree it could serve no site, so it reports the failure and exits before it serves or reports ready. The failure is not the caller's usage, so it exits 1. The path in the message is `cache/sites` whichever of the two is in the way.

Command:

```
$ sites
```

Output:

```
sites: cannot create directory cache/sites: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying directory-creation failure.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`, and so does `git`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.
- `state/sites.db` exists, or can be created as in `The host starts sites for the first time`.
- `cache` is an existing regular file in sites' working directory.

Postconditions:

- The existing `cache` file is unchanged.
- `state/sites.db` exists, created by this start if it was absent, and names what it named before. sites served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts sites where git is not installed

sites unpacks every site it serves by running git, and creates and publishes sites by running it too, so without git it can do neither, and it says so at start rather than with a failed request later. git is looked for on the `PATH` sites was started with, in the order of its directories as a shell would look: an executable file named `git` in one of them. Unlike a shell, sites skips an empty or relative entry, so it never runs a `git` it finds through its working directory. This is not the caller's usage but a host missing a dependency opsctl provisions, so it exits 1, and it does so before it opens the database, so a host without git keeps its state as it was.

Command:

```
$ sites
```

Output:

```
sites: git not found on PATH
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists and is run by its path.
- No directory on the `PATH` sites is started with holds an executable named `git`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.

Postconditions:

- Nothing has changed. sites opened no database, and an absent `state/sites.db` is still absent, as is an absent `cache/`; it served nothing, told systemd nothing, and sent telemetry nothing.

## The host starts sites with REPOS_DIR unset

`REPOS_DIR` unset or empty means the manifest's default, `../repos/state/repos`, the same as writing that value, so the two forms below, `REPOS_DIR` unset and `REPOS_DIR` empty, behave identically: on a host, where repos runs from `/opt/repos`, that is where repos keeps its bare repositories. sites does not look in the directory at start, so the start is the same whether repos is installed or not (`S19`); the directory is first read when a tool or a site request needs a repository.

Command:

```
$ sites
```

```
$ REPOS_DIR= sites
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`, and so does `git`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS`, `SITE_MAX_BYTES`, and `OPERATION_SECONDS` are unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry names a socket a listener holds that takes every event.
- sites' working directory is `/opt/sites`, and `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` is repos' bare repository `site`, owned by `u_7f3a9c21`.

Postconditions:

- sites is serving, and telemetry has received its `service.started`.
- `create` by `u_7f3a9c21` with `repo` `rep_8c21d4e0f7a3b915` finds that repository at `../repos/state/repos/rep_8c21d4e0f7a3b915.git`, relative to `/opt/sites`, and makes the site (`S06`); a site that names it is unpacked from there (`S08`, `S16`).
- Nothing under `/opt/repos/state/repos/` has changed; sites read nothing there at start.

## The host stops sites

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C` sends `SIGINT`, which sites treats the same way. sites stops taking new connections, finishes the requests it has already accepted, and exits. A request that runs git is finished when its git is: a publish unpacking its tree at the signal goes on until the tree is in place, the site points at it, and the caller has its whole result; a site request whose tree is being rebuilt goes on until the tree is rebuilt and served, and every request sharing that rebuild with it is served too (`S16`). sites closes its own copy of the socket and nothing more: the socket belongs to systemd, which keeps it open, and sites never removes `/run/ikigenba/sites.sock`. It waits at most `DRAIN_SECONDS` for requests to finish, which on a host is always less than the time the service unit allows before systemd kills it. Once its requests have finished, and each has recorded its `request.finished`, sites records `service.stopping` with the reason it is stopping, the name of the signal it received, `SIGTERM` or `SIGINT`. That is the last event of its trail: sites sends everything it recorded before exiting, within the same drain deadline. Sending takes time, so a stop is silent when the requests leave sites at least a second of the drain for it; a request that finishes later still gets its whole response, but an event sites has not sent when the deadline comes goes to stderr as an `undelivered event` line instead. A `service.started` with no `service.stopping` before the next one is how the trail shows a sites that died rather than stopped.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

sites exits 0. Nothing is on stdout or stderr.

Preconditions:

- sites is serving as process `<pid>`, on the socket it was passed, over `S06`'s shared catalog.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `u_7f3a9c21`'s `publish` of `scratch` (`sit_2d6f8a0c4e1b3957`), which tracks `preview` of `rep_8c21d4e0f7a3b915`, is running at the signal: its `git archive` of `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170` has begun.
- That publish, and every other request sites has accepted, finishes at least a second before the drain deadline, within 4 seconds of the signal, so sites has that second left to send what it recorded.
- telemetry takes every event as soon as it is sent.

Postconditions:

- Every request accepted before the signal received its full response. The publish succeeded: its result is `scratch`'s site object with `commit` `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170` (`S08`), and `https://sites.sbx.ikigenba.dev/scratch-7c1e9a4f/` serves that commit's tree once a sites is serving again.
- telemetry has received every event sites recorded: the publish's `site.published`, `{"commit":"9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170","ref":"preview","site":"sit_2d6f8a0c4e1b3957"}`, its `tool.called`, and its `request.finished` with `status` `200`; and last, after the `request.finished` of every request accepted before the signal:

  ```
  {"time":"<time>","service":"sites","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

  With `SIGINT` the `reason` is `SIGINT`.
- No git process sites started is still running.
- `/run/ikigenba/sites.sock` still exists, and connections made to it after sites exited wait in the socket's queue for the next sites to answer.

## The host stops sites while a request outlasts the drain

sites waits for accepted requests only until its drain deadline, `DRAIN_SECONDS` after the signal, so that it always exits before the service unit's stop timeout and is never killed mid-write by systemd. A request still running at the deadline — a publish whose `git archive` of a large tree is still unpacking, or a site request waiting on the rebuild of its site's tree — is cut off: sites kills its git and closes its connection without the rest of the response. A tree is servable only once it is wholly unpacked, so a cut-off unpack never becomes one: a publish cut off leaves its site pointing where it pointed before, and a rebuild cut off leaves the tree missing, to be rebuilt by the next request that needs it (`S16`). Losing a request is trouble, so sites says how many it lost and exits non-zero.

A site request that sites reaches after the signal and that would have to start a rebuild — its site's tree is missing, and no rebuild of it is already running — will not get its rebuild before the stop, so sites starts no git for it and answers it at once rather than leave it to be cut off: `503`, with a `Retry-After` of 30, and the one line of plain text `sites is stopping; try again later`. It is an answer for a known site's path, so it sets the visitor cookie when the request carried none and records `site.viewed` with `status` `503`, like any other (`S14`); it records no `site.unavailable`, since nothing is wrong with the site. That answer is complete, so the request is finished, not cut off, and is not counted. A request that joins a rebuild already running at the signal shares its fate: it is served if the rebuild finishes within the drain, and cut off with it if not.

The drain has used the whole deadline, so sites has no time left to send `service.stopping`, or any other event not yet sent, to telemetry: each goes to stderr as an `undelivered event` line instead. telemetry never receives a `request.finished` for a request cut off: its `request.started` with no finish is how the trail shows it was cut off.

Command:

```
$ kill -TERM <pid>
```

Output:

```
sites: undelivered event: {"time":"<time>","service":"sites","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
sites: stopped with <n> requests unfinished
```

sites exits 1, 5 seconds after the signal. The text is on stderr; stdout is empty. `<n>` is the number of requests still running at the deadline. When `<n>` is 1 the line reads `sites: stopped with 1 request unfinished`. The `service.stopping` line above is always written. Any other event sites had recorded and not yet delivered when the deadline came is written as an `undelivered event` line too; those lines are in the order the events were recorded, `service.stopping` last of them. The position of the `stopped with` line among them is not fixed. stderr may also hold, for a cut-off request, an `undelivered event` line carrying its `request.finished`, recorded after the deadline and before sites exited; whether it does, and where that line falls, is not fixed. stderr holds no other line.

Preconditions:

- sites is serving as process `<pid>`, on the socket it was passed, over `S06`'s shared catalog.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `u_7f3a9c21`'s `publish` of `scratch` (`sit_2d6f8a0c4e1b3957`) at `preview` is running at the signal, and its `git archive` of `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170` is still unpacking 5 seconds after it; `scratch` is unpublished.
- `blog` (`sit_4e7a1c9b0d2f8635`) is published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree, so its next request must rebuild it. sites reaches a guest's `GET /blog/`, carrying `Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081` and `X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`, after the signal, with no rebuild of `blog` running.
- `<n>` of the requests sites has accepted, the publish among them, are still running 5 seconds after the signal.
- telemetry takes every event.

Postconditions:

- The guest's `GET /blog/` was answered at once:

  ```
  HTTP/1.1 503 Service Unavailable
  Content-Type: text/plain; charset=utf-8
  Retry-After: 30
  Cache-Control: public, no-cache
  ```

  with the body exactly the one line `sites is stopping; try again later`, ending in a newline. sites ran no git for it. telemetry has received its `request.started`, then:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":503,"visitor":"vis_1a2b3c4d5e6f7081"}}
  ```

  then its `request.finished` with `status` `503`, and no `site.unavailable`.
- Every request that finished within 5 seconds of the signal received its full response; the `<n>` that did not were cut off. The publish's git was killed, and its caller received no result.
- `scratch` is still unpublished: `show` of it answers with no `commit` (`S07`), its path is still answered with the not-found page (`S11`), and the publish recorded no `site.published` and no `tool.called`. No tree under `cache/sites/sit_2d6f8a0c4e1b3957/` is served by any later request; a later `publish` of `scratch` unpacks afresh (`S08`).
- No git process sites started is still running.
- Each of the `<n>` cut-off requests has its `request.started` recorded (delivered to telemetry, written to stderr as undelivered, or both, when it was being delivered as the deadline came). telemetry has received no `request.finished` for any of them, and no `service.stopping`.
- `/run/ikigenba/sites.sock` still exists, and connections made to it after sites exited wait in the socket's queue for the next sites to answer.

## The host restarts sites during a deploy

A deploy replaces sites' binary and restarts `ikigenba-sites.service` alone; `ikigenba-sites.socket` stays up throughout. Between the old sites exiting and the new one being ready, connections wait in the socket's queue instead of being refused, so a visitor or the gateway never sees sites missing. That holds because sites finishes what it accepted before it exits and leaves the socket where systemd put it. The catalog outlives the deploy: the new sites opens the same `state/sites.db`, bringing it up to date first when the new binary carries a migration the database has not had, and serves every site the old one kept. The new sites unpacks nothing at start, so the wait is short; the trees the old sites unpacked are still under `cache/sites/`, and the new one serves them as they are.

Command:

```
$ sudo systemctl restart ikigenba-sites.service
```

Output:

```
```

Exits 0, once the new sites has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- sites is serving on `/run/ikigenba/sites.sock` under `ikigenba-sites.service`, over `S06`'s shared catalog, with the published trees of `blog`, `handbook`, and `recipes` under `cache/sites/`.
- Visitors are requesting pages of `blog` and `recipes`, and the gateway is calling sites' tools, throughout the restart, and every request the old sites accepted finishes at least a second before its drain deadline.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- Every request sent was answered, by the old sites or the new one; none was refused and none was cut off. Every page served is the published commit's file, before and after.
- A new sites process is serving on `/run/ikigenba/sites.sock`, over the same `state/sites.db`, now up to date, and the same `cache/sites/`; every site and the apex are as the old sites left them, and the trees there are the ones the old sites left, and the new one rebuilt none of them.
- telemetry has received the old sites' `service.stopping`, with `reason` `SIGTERM`, and after it the new sites' `service.started`, whose `version` is the `<display>` of the new sites' environment. Every request the old sites answered is recorded before its `service.stopping`, and every request the new one answered after its `service.started`.

## The host starts sites where telemetry cannot be reached

The trail is not a reason to stop serving. When sites cannot deliver its events — telemetry is not installed yet, is stopped, or the services file names no `telemetry` entry — sites starts and serves exactly as it does otherwise, and its events go to the journal as `undelivered event` lines (above). No answer waits on telemetry, so a visitor or the gateway sees no difference. sites keeps looking for telemetry with every event, so once telemetry takes events again, sites' next events go to it without a restart; an event already written to the journal is not sent again.

Command:

```
$ sudo systemctl start ikigenba-sites.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed sites, and `ikigenba-sites.socket` is active, as in `The host starts sites`.
- `ikigenba-sites.service` is not running.
- The host's services file lists no `telemetry` entry, or nothing accepts connections on the socket that entry names.

Postconditions:

- `ikigenba-sites.service` is `active`, and sites is serving on `/run/ikigenba/sites.sock`, as in `The host starts sites`.
- The journal holds one line from sites, written after it reported that it was ready:

  ```
  sites: undelivered event: {"time":"<time>","service":"sites","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

- Every event sites records while telemetry cannot be reached — every `site.viewed` among them — is written to the journal the same way, one line each, and every request is answered as it would be with telemetry taking events.

## The host starts sites without a socket

Run bare, with no socket passed in, sites has nothing to serve on, and it does not open one of its own: there is no port or address it falls back to. That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or `LISTEN_PID` naming some other process, counts as no socket passed in. The detail says how to run sites correctly. sites looks for its socket before it looks for git, so this is reported whether or not git is installed.

Command:

```
$ sites
```

Output:

```
sites: no socket was passed in

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not sites' process id.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.

Postconditions:

- Nothing has changed. sites opened no database, created no `cache/`, listened on nothing, told systemd nothing, and sent telemetry nothing; an absent `state/sites.db` is still absent.

## The host passes sites more than one socket

sites serves on exactly one socket. A unit that passes it several is misconfigured, and sites will not guess which one it was meant to serve on.

Command:

```
$ sites
```

Output:

```
sites: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `2`: two listening sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` and each of the three settings are unset, or valid.

Postconditions:

- Nothing has changed. sites opened no database, served on neither socket, told systemd nothing, and sent telemetry nothing.

## The host gives sites a drain deadline that is not a number of seconds

sites reads `DRAIN_SECONDS` before it serves, so a bad value is found at start rather than at the moment sites is asked to stop. A value that is not a positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and sites serves nothing. The value is quoted back verbatim. sites sets no upper limit: keeping the drain inside the service unit's stop timeout is opsctl's to enforce. It checks `DRAIN_SECONDS` before `SITE_MAX_BYTES` and `OPERATION_SECONDS`, so it is the one named when several are bad, and before it looks for its socket or for git, so it is reported whether or not a socket was passed in or git is installed.

Command:

```
$ DRAIN_SECONDS=abc sites
```

Output:

```
sites: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. sites opened no database, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives sites a site size limit that is not a number of bytes

sites reads `SITE_MAX_BYTES` before it serves, so a bad value is found at start rather than when a publish or a rebuild first needs it. A value that is not a positive whole number — `0`, `-1`, `2.5`, `256M`, or `abc` — is the caller's mistake, so it is a usage error and sites serves nothing: a limit it cannot read is not a limit it can guess, and guessing one could let one site fill the host's disk or refuse every site. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). sites sets no upper limit. It checks `SITE_MAX_BYTES` after `DRAIN_SECONDS` and before `OPERATION_SECONDS`, so it is the one named when it and `OPERATION_SECONDS` are both bad, and before it looks for its socket or for git, so it is reported whether or not a socket was passed in or git is installed.

Command:

```
$ SITE_MAX_BYTES=abc sites
```

Output:

```
sites: SITE_MAX_BYTES is 'abc', not a positive whole number of bytes
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. sites opened no database, ran no git, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives sites a git time limit that is not a number of seconds

sites reads `OPERATION_SECONDS` before it serves, so a bad value is found at start rather than when a git run first needs it. A value that is not a positive whole number — `0`, `-1`, `2.5`, `10m`, or `abc` — is the caller's mistake, so it is a usage error and sites serves nothing: a git run with a deadline sites cannot read could hold a request, or a rebuild every visitor of a site waits on, for as long as git likes. The value is quoted back verbatim. Only an unset or empty variable means the manifest's default (`S01`). sites sets no upper limit. It checks `OPERATION_SECONDS` after `DRAIN_SECONDS` and `SITE_MAX_BYTES`, and before it looks for its socket or for git, so it is reported whether or not a socket was passed in or git is installed.

Command:

```
$ OPERATION_SECONDS=abc sites
```

Output:

```
sites: OPERATION_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists and is on the `PATH` as `sites`.
- `LISTEN_PID` is sites' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and `SITE_MAX_BYTES` are unset, or a positive whole number.

Postconditions:

- Nothing has changed. sites opened no database, ran no git, served nothing, told systemd nothing, and sent telemetry nothing.
