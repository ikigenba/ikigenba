# Stories — maintenance

Scheduled housekeeping of the repos' directories. Every push leaves loose objects and small packs behind, and a forced push or a deleted branch leaves commits nothing reaches (`S11-git.md`); left alone, a repo grows slower to serve and larger than its content. So repos runs git's own garbage collection on each repo on a schedule: every `MAINTENANCE_HOURS` hours (24), a positive whole number read from its environment at start (`S02-serve.md`), whose default is the manifest's `[env]` (`S01-bootstrap.md`). The first cycle begins `MAINTENANCE_HOURS` after repos started, not at start, so a restart never sets off a burst of work, and each later cycle begins `MAINTENANCE_HOURS` after the one before began, or when that one ends if it ran longer. A cycle takes the available repos one at a time, in an order not fixed, and skips an unavailable one (`S14-verification.md`). Maintaining a repo is a write like a push (`S12-limits.md`): it takes a write slot and the repo's write lock, waits for them as a push would, counted among the writes waiting, and holds them while git runs, so it never runs beside a push to the same repo, and a push to that repo waits for it. When it cannot have them within `QUEUE_SECONDS`, or the writes' queue is full, the repo is skipped this cycle and tried again the next, and the cycle goes on to the next repo. Once running, it has `OPERATION_SECONDS` (600) like any git operation (`S12-limits.md`): past it repos kills git, records `operation.timed_out` with `operation` `maintenance` and `limit` `operation_seconds` and no `maintenance.finished`, and tries the repo again the next cycle; a killed collection leaves the repo whole. A maintenance whose git fails records nothing and is tried again the next cycle. Reads take no lock, so clones and fetches of a repo go on while it is maintained. Maintenance changes no ref: each ref names the same commit after it as before, and every commit reachable from a ref stays; git's collection packs what is reachable and removes only unreachable objects old enough for git's own default expiry to drop. While a repo is maintained `status` answers its `busy` `true` (`S10-status.md`), so `delete` refuses it (`S09-delete.md`). Maintenance is not a request and has no caller: its events carry an empty request id and an empty user. Each repo maintained records `maintenance.finished`, with `repo`, the repo's id; `duration_us`, how long git ran, in whole microseconds; and `size_before` and `size_after`, the repo's `size_bytes` before git ran and after. A maintenance that waited and then ran records `operation.waited` first, with `operation` `maintenance`; one skipped records `operation.rejected`, with `operation` `maintenance` and the `limit` that skipped it. Maintenance is routine, not trouble, and writes nothing to stdout or stderr. The actor is the host, or a developer at a terminal standing in for it, as in `S02-serve.md`, and a developer using git as in `S11-git.md`. Every story starts repos with `MAINTENANCE_HOURS=1` so a cycle comes within the hour, with the suite's services file, whose `telemetry` entry takes every event; the caller `u_7f3a9c21` owns `notes`, `rep_3f9a0c1d2e4b5a69`, and `site`, `rep_8c21d4e0f7a3b915`, both available, and no other repo exists, unless a story says otherwise.

## The host runs scheduled maintenance

An hour after start the first cycle runs: each repo in turn is collected and recorded, and repos goes on serving throughout.

Command:

```
$ MAINTENANCE_HOURS=1 repos
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/repos` exists and is on the `PATH` as `repos`.
- `LISTEN_PID` is repos' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- repos starts at `2026-10-02T14:00:00Z`.
- Each of `notes` and `site` holds loose objects from earlier pushes, every one reachable from a ref (no forced push or deleted branch has left any behind); no git operation runs on either from `2026-10-02T15:00:00Z` until the cycle ends.

Postconditions:

- The cycle began at `2026-10-02T15:00:00Z`; none ran before it. The next begins at `2026-10-02T16:00:00Z`.
- Each repo's refs name the same commits as before the cycle; a `show` of each answers the same `head`.
- In each repo every object is in one pack and none is loose: `git --git-dir=state/repos/<id>.git count-objects -v` prints `count: 0` and `packs: 1`.
- telemetry has received one `maintenance.finished` for each repo, `notes`' first or `site`'s, with no request id and no user:

  ```
  {"time":"<time>","service":"repos","event":"maintenance.finished","request_id":"","user":"","attrs":{"duration_us":<n>,"repo":"rep_3f9a0c1d2e4b5a69","size_after":<bytes>,"size_before":<bytes>}}
  {"time":"<time>","service":"repos","event":"maintenance.finished","request_id":"","user":"","attrs":{"duration_us":<n>,"repo":"rep_8c21d4e0f7a3b915","size_after":<bytes>,"size_before":<bytes>}}
  ```

  Neither records `operation.waited`.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.

## The host's maintenance waits for a push in progress

When the cycle reaches `notes`, a push to it is running and holds its lock. Maintenance waits for the push to finish, as a second push would, then runs on the repo as the push left it.

Command:

```
$ MAINTENANCE_HOURS=1 repos
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- repos starts as in `The host runs scheduled maintenance`, at `2026-10-02T14:00:00Z`.
- When the cycle reaches `notes`, a push of `main` to `<new>` is running, and finishes about 10 seconds later; `QUEUE_SECONDS` is unset, so 30 seconds.

Postconditions:

- The push took effect: `notes`' `refs/heads/main` is `<new>`, and its `repo.pushed` was recorded before `notes`' `maintenance.finished`.
- telemetry has received, for `notes`, in this order:

  ```
  {"time":"<time>","service":"repos","event":"operation.waited","request_id":"","user":"","attrs":{"operation":"maintenance","repo":"rep_3f9a0c1d2e4b5a69","wait_us":<n>}}
  {"time":"<time>","service":"repos","event":"maintenance.finished","request_id":"","user":"","attrs":{"duration_us":<n>,"repo":"rep_3f9a0c1d2e4b5a69","size_after":<bytes>,"size_before":<bytes>}}
  ```

  `wait_us` is about 10000000. `site` was maintained too, before or after.
- Nothing was written to stdout or stderr.

## The host's maintenance gives up on a repository that stays busy

A push that holds `notes`' lock longer than `QUEUE_SECONDS` keeps maintenance waiting no longer than any other write would wait. Maintenance skips `notes` this cycle, records why, goes on to `site`, and tries `notes` again in the next cycle. The push is not disturbed.

Command:

```
$ MAINTENANCE_HOURS=1 repos
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- repos starts as in `The host runs scheduled maintenance`, at `2026-10-02T14:00:00Z`.
- When the cycle reaches `notes`, a push to it is running, sending its pack slowly, and runs on for more than 30 seconds; `QUEUE_SECONDS` is unset.
- No git operation runs on `notes` in the second cycle, from `2026-10-02T16:00:00Z`.

Postconditions:

- In the first cycle, telemetry has received for `notes`, 30 seconds after the cycle reached it, and no `maintenance.finished`:

  ```
  {"time":"<time>","service":"repos","event":"operation.rejected","request_id":"","user":"","attrs":{"limit":"queue_seconds","operation":"maintenance","repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

  and one `maintenance.finished` for `site`.
- The push ran on to its own end, as it would have with no cycle.
- In the second cycle `notes` was maintained: telemetry has received a `maintenance.finished` for `rep_3f9a0c1d2e4b5a69` after `2026-10-02T16:00:00Z`.
- Nothing was written to stdout or stderr.

## A developer clones a repository while it is maintained

Maintenance holds the repo's write lock, which no read takes, and a write slot, which no read uses. A clone of the repo being maintained runs at once, and gets the repo whole, either as it was before git's collection or as it is after; the commits are the same either way.

Command:

```
$ git clone http://127.0.0.1:8080/notes.git
```

Output:

```
Cloning into 'notes'...
```

Exits 0. The text is on stderr, followed by git's own progress lines; stdout is empty.

Preconditions:

- repos is serving with `MAINTENANCE_HOURS=1`, and the cycle is maintaining `notes` while the clone runs: `status` answers `notes` `busy` `true` and `write` `active` 1.
- The working directory holds no `notes`; the forwarder and identity headers are those of `S11-git.md`.

Postconditions:

- `./notes` is a working tree on `main`, checked out at `notes`' `head`.
- No request of the clone recorded `operation.waited`; its `POST /notes.git/git-upload-pack` recorded its `repo.fetched`.
- telemetry has received `notes`' `maintenance.finished`, with no request id and no user.

## The host's maintenance skips an unavailable repository

An unavailable repo has no directory git can open (`S14-verification.md`), so there is nothing to collect. A cycle passes it by without waiting, without running git, and without a record; the trail already holds its `repo.unavailable` from the start.

Command:

```
$ MAINTENANCE_HOURS=1 repos
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- repos starts as in `The host runs scheduled maintenance`, at `2026-10-02T14:00:00Z`.
- `state/repos/rep_3f9a0c1d2e4b5a69.git` is missing, so startup verification marked `notes` unavailable.

Postconditions:

- In the cycle at `2026-10-02T15:00:00Z`, telemetry has received one `maintenance.finished`, for `rep_8c21d4e0f7a3b915`, and no event naming `rep_3f9a0c1d2e4b5a69`: no `maintenance.finished`, `operation.waited`, or `operation.rejected`.
- `notes` is still unavailable, and `state/repos/rep_3f9a0c1d2e4b5a69.git` still does not exist.
- Nothing was written to stdout or stderr.

## The host stops repos during a maintenance cycle

A stop ends maintenance as it ends everything else: from the signal on, no repo's maintenance starts, and a maintenance already running is drained like a request, finishing within the drain deadline (`S02-serve.md`). One that is still running at the deadline is killed, which leaves the repo consistent, since git's collection changes no ref and removes an object only once nothing needs it; it records no `maintenance.finished` and is not counted among the requests unfinished. A maintenance waiting for its lock or slot when the signal comes is given up, recording `operation.rejected` with `limit` `draining`. The repos not reached are maintained in the next repos' first cycle, `MAINTENANCE_HOURS` after it starts.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

repos exits 0. Nothing is on stdout or stderr.

Preconditions:

- repos is serving as process `<pid>`, started with `MAINTENANCE_HOURS=1`, and its cycle is maintaining `notes`, a maintenance that finishes about 1 second after the signal; `site` has not been reached.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds, and no request outlasts it.

Postconditions:

- `notes`' maintenance ran to its end: telemetry has received its `maintenance.finished`.
- `site` was not maintained: telemetry has received no `maintenance.finished` and no other event naming `rep_8c21d4e0f7a3b915` after the signal.
- The last event telemetry received is `service.stopping`, after `notes`' `maintenance.finished`:

  ```
  {"time":"<time>","service":"repos","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

- Every repo's refs name the same commits as before the signal.
