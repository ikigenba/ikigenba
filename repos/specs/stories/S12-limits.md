# Stories — limits

What repos does so that git can never take it down: how many git operations run at once, how long one may wait or run, and how large a push or a repo may grow. Each limit is a setting repos reads once, at start, from its environment (`S02-serve.md`), whose default is the manifest's `[env]` (`S01-bootstrap.md`), which the host writes into `/opt/repos/etc/env`; an operator changes one there and restarts repos, and a developer sets it on the command line. Every git request of `S11-git.md` is a read, the operation `fetch`, or a write, the operation `push`, and takes a slot of its kind for as long as its git runs: at most `READ_SLOTS` reads (8) and at most `WRITE_SLOTS` writes (2) run at once, and the two kinds never share, so a flood of clones cannot hold up a push. Scheduled maintenance takes a write slot too (`S13-maintenance.md`). A request that finds every slot of its kind taken waits for one, first come first served, and up to `QUEUE_LENGTH` operations of each kind (16) may wait; one more is refused at once. A `POST git-receive-pack` also holds its repo's write lock while git runs, so two pushes to one repo never run together, and a second push to a repo waits for the lock as it would for a slot, counted among the writes waiting. No operation waits longer than `QUEUE_SECONDS` (30) for its slot and its lock together; past that it is refused. A refused operation is answered `503` with `Retry-After` set to `QUEUE_SECONDS`, and the same one line of plain text whether the queue was full or the wait too long; it never ran git, so it changed nothing. Once running, a git operation has `OPERATION_SECONDS` (600) of wall-clock time; past it repos kills git and closes the connection without the rest of its answer. A push may send a pack of at most `PUSH_MAX_BYTES` bytes (104857600, 100 MiB), which git itself enforces as the push arrives, from the setting repos was started with, for every repo whatever its age. A repo whose size on disk, its `size_bytes` (`S07-list-and-show.md`), is at or over `REPO_MAX_BYTES` (1073741824, 1 GiB) takes no push: both push routes answer `507` before any slot is sought, and reads go on as before. The ceiling is checked as a push begins, so a push that starts under it is taken whole even when it carries the repo over. A client that goes away mid-operation has its git killed at once, and its slot and lock are freed for the next.

The actor is a developer, with git through the forwarder of `S11-git.md`, which carries the caller's identity headers, or by hand. repos is started as `S11-git.md` starts it, with every setting of this group unset unless a story sets it, and telemetry takes every event. "A slow client" is a git of the developer's own whose transfer they have throttled, so it holds its slot as long as the story needs. Besides the events of `S11-git.md`, a request on the git routes records these, between its `request.started` and its `request.finished`, under its request id and user: `operation.waited`, with `repo`, `operation`, and `wait_us`, the whole microseconds it waited for its slot and lock, when it waited and then ran, before any domain event of its operation; `operation.rejected`, with `repo`, `operation`, and `limit`, the setting that refused it, `queue_length`, `queue_seconds`, `push_max_bytes`, or `repo_max_bytes`; and `operation.timed_out`, with `repo`, `operation`, and `limit` `operation_seconds`. An operation that ran without waiting records no `operation.waited`. A push refused, killed, or cut off records no `repo.pushed` and emits nothing to the event bus (`S02-serve.md`). Nothing in this group earns a line on stderr: a limit doing its work is a fact of the trail, and the tool `status` (`S10-status.md`) shows the pressure as it stands.

## A developer's fetch waits for a read slot and then runs

Every read slot is taken by a clone of `site` to a slow client. A fetch of `notes` arriving now waits for one of them to finish, rather than being refused, and then runs as it would have; its owner sees only a slower answer.

Request:

```
GET /notes.git/info/refs?service=git-upload-pack HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/x-git-upload-pack-advertisement
```

Status 200, answered once one of the clones of `site` has finished, about 5 seconds after the request arrived. The body is the advertisement of `S11-git.md`'s `A developer reads a repository's refs`.

Preconditions:

- `READ_SLOTS` and `QUEUE_LENGTH` are unset: 8 read slots, 16 may wait.
- Eight clones of `site` are running, each serving a slow client, and no read is waiting; the first of them finishes about 5 seconds after the request arrives.

Postconditions:

- Nothing has changed.
- While it waited, `status` answered `read` `{"slots":8,"active":8,"queued":1}`.
- telemetry has received the request's events, in this order:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/notes.git/info/refs"}}
  {"time":"<time>","service":"repos","event":"operation.waited","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"operation":"fetch","repo":"rep_3f9a0c1d2e4b5a69","wait_us":<n>}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `wait_us` is about 5000000, and no more than `duration_us`.

## A developer's fetch finds the queue full

With every read slot taken and as many reads waiting as the queue holds, one more read is refused at once rather than left to pile up: the client is told to come back when the queue has had time to clear. The answer is the same for a write when the writes' slots and queue are full.

Request:

```
GET /notes.git/info/refs?service=git-upload-pack HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
Retry-After: 30
```

Status 503, answered at once. The body is exactly the one line `too many git operations; try again later`, ending in a newline. `Retry-After` is `QUEUE_SECONDS`, so `QUEUE_SECONDS=10` makes it `10`.

Preconditions:

- `READ_SLOTS`, `QUEUE_LENGTH`, and `QUEUE_SECONDS` are unset.
- Eight reads are running, each serving a slow client, and sixteen more are waiting.

Postconditions:

- Nothing has changed. git did not run for the request, and the eight running and sixteen waiting reads go on as before.
- telemetry has received the request's events, in this order:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/notes.git/info/refs"}}
  {"time":"<time>","service":"repos","event":"operation.rejected","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"limit":"queue_length","operation":"fetch","repo":"rep_3f9a0c1d2e4b5a69"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":41,"status":503}}
  ```

## A developer's fetch waits longer than the queue allows

A place in the queue is not a promise to run. A read that has waited `QUEUE_SECONDS` without a slot coming free is refused, with the same answer as a full queue, so no client waits indefinitely behind long transfers.

Request:

```
GET /notes.git/info/refs?service=git-upload-pack HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
Retry-After: 30
```

Status 503, answered 30 seconds after the request arrived. The body is exactly the one line `too many git operations; try again later`, ending in a newline.

Preconditions:

- `READ_SLOTS`, `QUEUE_LENGTH`, and `QUEUE_SECONDS` are unset.
- Eight reads are running, each serving a slow client, and none finishes within 30 seconds of the request arriving; fewer than sixteen reads are waiting.

Postconditions:

- Nothing has changed. git did not run for the request.
- telemetry has received the request's events, in this order, with no `operation.waited`:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/notes.git/info/refs"}}
  {"time":"<time>","service":"repos","event":"operation.rejected","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"limit":"queue_seconds","operation":"fetch","repo":"rep_3f9a0c1d2e4b5a69"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":41,"status":503}}
  ```

## A developer pushes while clones flood the read slots

Reads and writes have slots of their own. However many clones are running or waiting, a push to a repo nobody else is pushing to runs at once.

Command:

```
$ git -C notes push origin main
```

Output:

```
To http://127.0.0.1:8080/notes.git
```

Exits 0. The line is on stderr, after git's own progress lines and before git's line reporting `main` moving from `<old>` to `<new>`; stdout is empty.

Preconditions:

- `READ_SLOTS`, `WRITE_SLOTS`, and `QUEUE_LENGTH` are unset.
- Eight clones of `site` are running, each serving a slow client, and sixteen more reads are waiting; no write is running or waiting.
- `notes`' `main` is at `<old>`, and `./notes` has commits on `main` after it, ending at `<new>`.

Postconditions:

- `notes`' `refs/heads/main` is `<new>`, as in `S11-git.md`'s `A developer pushes new commits to a branch`.
- No request of the push recorded `operation.waited` or `operation.rejected`; its `POST /notes.git/git-receive-pack` recorded its `repo.pushed`.
- The clones of `site`, running and waiting, go on as before.

## A second push to a repository waits for the first

Two clones of `notes` push to it at once, to different branches. The repo's write lock lets one push run at a time, so the second waits for the first to finish, then runs, as if it had arrived after it; neither push sees the other half-done. A write slot is free throughout: the wait is for the lock. Had the first push held the lock for longer than `QUEUE_SECONDS`, the second would have been refused with the 503 of `A developer's fetch waits longer than the queue allows`, its `operation.rejected` naming `operation` `push` and `limit` `queue_seconds`, and git would have reported the push failed.

Command:

```
$ git -C notes-b push origin draft
```

Output:

```
To http://127.0.0.1:8080/notes.git
```

Exits 0, once the first push has finished. The line is on stderr, after git's own progress lines and before git's line reporting `draft` as a new branch; stdout is empty.

Preconditions:

- `WRITE_SLOTS` and `QUEUE_SECONDS` are unset: 2 write slots, 30 seconds.
- From another clone, `./notes-a`, a push of `main` to `notes` is running, sending its pack slowly; it holds `notes`' lock and finishes about 10 seconds after this push's `POST /notes.git/git-receive-pack` arrives.
- `./notes-b` is a clone of `notes` with a branch `draft` at `<new>`; `notes` has no `draft`.

Postconditions:

- `notes` has `refs/heads/draft` at `<new>`, and `refs/heads/main` at what `./notes-a` pushed: both pushes took effect.
- While it waited, `status` answered `write` `{"slots":2,"active":1,"queued":1}`, and `notes` `busy` `true`.
- This push's `POST /notes.git/git-receive-pack` recorded, in this order:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/notes.git/git-receive-pack"}}
  {"time":"<time>","service":"repos","event":"operation.waited","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"operation":"push","repo":"rep_3f9a0c1d2e4b5a69","wait_us":<n>}}
  {"time":"<time>","service":"repos","event":"repo.pushed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"new":"<new>","old":"0000000000000000000000000000000000000000","ref":"refs/heads/draft","repo":"rep_3f9a0c1d2e4b5a69"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `wait_us` is about 10000000. Its `GET /notes.git/info/refs` took no lock and did not wait.
- The first push's `repo.pushed` was recorded before this push's.

## A developer's push runs past the operation deadline

A git operation that is still running `OPERATION_SECONDS` after it started — a stalled client, a pack trickling in — is killed, so one slow transfer cannot hold a slot and a lock for ever. The connection is closed without the rest of the answer and git on the client fails. Git changes a ref only once the whole push has arrived, so a push killed before then changed nothing. A clone or fetch past the deadline is killed the same way, with `operation` `fetch`. The client is not told which limit it met: the trail and `status` say.

Command:

```
$ git -C notes push origin main
```

Output: git's own progress, then its report that the transfer failed; neither is fixed.

Exits non-zero, about 60 seconds after the `POST /notes.git/git-receive-pack` began to run. The text is on stderr; stdout is empty.

Preconditions:

- `OPERATION_SECONDS=60` is set in repos' environment.
- `notes`' `main` is at `<old>`; `./notes` has one commit after it, `<new>`, which adds a file of 52428800 random bytes.
- The developer's git sends its pack at 64 KiB a second, so the pack would take over 13 minutes to arrive.

Postconditions:

- `notes`' `refs/heads/main` is still `<old>`, and `<new>` is not in the repo: `git --git-dir=state/repos/rep_3f9a0c1d2e4b5a69.git cat-file -e <new>` exits non-zero.
- The push's slot and `notes`' lock are free: a push started now runs without waiting.
- The `POST /notes.git/git-receive-pack` recorded, in this order, its `request.started`, then:

  ```
  {"time":"<time>","service":"repos","event":"operation.timed_out","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"limit":"operation_seconds","operation":"push","repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

  then its `request.finished`, whose `request_bytes` is what had arrived and whose `status` is not fixed. It recorded no `repo.pushed`.

## A developer pushes more than a push may carry

The pack a push sends is capped at `PUSH_MAX_BYTES`. git enforces it as the pack arrives and stops reading once it is over, so the over-size pack is never stored; the refusal is git's own and reaches the developer in git's words. No ref moves.

Command:

```
$ git -C notes push origin main
```

Output: git's own lines, among them one from the remote containing `pack exceeds maximum allowed size`, and git's report that the push failed.

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `PUSH_MAX_BYTES=1048576` is set in repos' environment.
- `notes`' `main` is at `<old>`; `./notes` has one commit after it, `<new>`, which adds a file of 5242880 random bytes, so its pack is over 1 MiB.

Postconditions:

- `notes`' `refs/heads/main` is still `<old>`, and `<new>` is not in the repo.
- The push's slot and `notes`' lock are free.
- The `POST /notes.git/git-receive-pack` recorded, in this order, its `request.started`, then:

  ```
  {"time":"<time>","service":"repos","event":"operation.rejected","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"limit":"push_max_bytes","operation":"push","repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

  then its `request.finished`. It recorded no `repo.pushed`.

## A developer pushes to a repository at its size limit

A repo whose size on disk has reached `REPO_MAX_BYTES` takes no more pushes: repos refuses both push routes before seeking a slot or running git, so the first request of any push is refused and nothing is sent. The answer is `507`, not `503`: waiting will not help, so there is no `Retry-After`. Its owner can still clone and fetch it.

Request:

```
GET /notes.git/info/refs?service=git-receive-pack HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
POST /notes.git/git-receive-pack HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/x-git-receive-pack-request

<a push request>
```

Response:

```
HTTP/1.1 507 Insufficient Storage
Content-Type: text/plain; charset=utf-8
```

Status 507. The body is exactly the one line `repository is at its size limit of 10485760 bytes`, ending in a newline, the number being `REPO_MAX_BYTES`. There is no `Retry-After` header.

Preconditions:

- `REPO_MAX_BYTES=10485760` is set in repos' environment.
- `notes`' `size_bytes` is 12582912, over the limit.

Postconditions:

- Nothing has changed. git did not run, and no slot was taken.
- `status` answers `notes` with `size_bytes` 12582912 and `limit_bytes` 10485760.
- A clone of `notes` succeeds as in `S11-git.md`'s `A developer clones a repository with content`.
- telemetry has received the request's events, in this order (for the `POST`, with `method` `POST` and `path` `/notes.git/git-receive-pack`):

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/notes.git/info/refs"}}
  {"time":"<time>","service":"repos","event":"operation.rejected","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"limit":"repo_max_bytes","operation":"push","repo":"rep_3f9a0c1d2e4b5a69"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":50,"status":507}}
  ```

## A developer's push is cut off by the client

The developer interrupts a push while its pack is still on the way. repos sees the connection go and kills git at once rather than letting it wait for bytes that will never come; git changes no ref for a pack it never finished receiving, and the slot and the lock are free for the next operation straight away. A clone or fetch whose client goes away is ended the same way.

Command:

```
$ git -C notes push origin main
```

Output: git's own progress lines, up to the interrupt; not fixed.

The developer presses `Ctrl-C` while git is sending the pack. Exits 130. The text is on stderr; stdout is empty.

Preconditions:

- `notes`' `main` is at `<old>`; `./notes` has one commit after it, `<new>`, which adds a file of 52428800 random bytes, sent slowly enough to interrupt.

Postconditions:

- `notes`' `refs/heads/main` is still `<old>`, and `<new>` is not in the repo.
- No git runs for `notes`: `status` answers `write` `active` 0 and `notes` `busy` `false`, and a push started at once runs without recording `operation.waited`.
- The `POST /notes.git/git-receive-pack` recorded its `request.started` and its `request.finished`, whose `status` is not fixed, and between them no `repo.pushed` and no `operation.timed_out`.
