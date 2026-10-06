# Stories — git

Git over HTTP: how a user's git client clones, fetches, and pushes a repo. repos speaks git's smart HTTP protocol, and only it, at `/<name>.git/`, where `<name>` is the name of one of the caller's repos, resolved among the repos whose owner is the request's `X-User-Id` (`S06-create.md`). Four routes are git's: `GET /<name>.git/info/refs?service=git-upload-pack` and `POST /<name>.git/git-upload-pack` are a read, the operation `fetch` (a clone and a fetch are the same operation to repos); `GET /<name>.git/info/refs?service=git-receive-pack` and `POST /<name>.git/git-receive-pack` are a write, the operation `push`. repos answers each by running the host's own `git http-backend` against the repo's directory, `state/repos/<id>.git` (`S15-disk.md`), so what passes on these routes is git's protocol, exactly as git writes it, and the repo is changed only by git. Request and response bodies stream through repos in both directions: repos never holds a whole body, and a request body sent chunked, with no `Content-Length`, as git sends any push larger than its 1 MiB `http.postBuffer`, is read like any other. Every other path under `/<name>.git/` is git's dumb HTTP protocol, or nothing, and is answered `404`. A name that is not one of the caller's repos is answered `404` whatever follows it, and another user's repo is indistinguishable from one that does not exist. A request without `X-User-Id` is answered `500`, as every route is (`S03-landing.md`). repos never authenticates: on a host the caller's credential is the host nginx's and auth's to check, and git's request reaches repos with the identity headers set (`S17-on-a-space.md`). How many git operations run at once, how long one may wait or run, and how large a push or a repo may grow are `S12-limits.md`'s; the stories here meet none of those limits.

The actor is a developer, with git or by hand. Each story runs against a repos started as `S02-serve.md` starts it, from its working directory, with the suite's services file of `S03-landing.md`, whose `telemetry` entry takes every event, and with the event bus, the `events` app, taking every event repos emits unless a story says otherwise. A request shown as HTTP is the request repos receives on its socket, with the headers the story depends on. Where a git client is the actor, the developer stands in for nginx with a forwarder of their own: a listener at `127.0.0.1:8080` that passes every connection, byte for byte and unbuffered, to the socket repos was passed. Their git adds the identity headers nginx would set to every request to that address, once, with:

```
$ git config --global --add http.http://127.0.0.1:8080/.extraHeader 'X-User-Id: u_7f3a9c21'
$ git config --global --add http.http://127.0.0.1:8080/.extraHeader 'X-User-Email: mg@example.com'
```

so the stand-in URL of `notes` is `http://127.0.0.1:8080/notes.git`; on a host it is the repo's `clone_url` (`S07-list-and-show.md`). git's own progress and report go to its stderr and are not fixed beyond what a story quotes. The caller `u_7f3a9c21` owns `notes`, `rep_3f9a0c1d2e4b5a69`, and `site`, `rep_8c21d4e0f7a3b915`, both available, unless a story says otherwise. `<head>` is the 40-hex sha `refs/heads/main` of `notes` points at.

Every request on these routes is recorded in repos' trail (`S02-serve.md`), shown as the JSON object telemetry receives, by its `request.started`, with `method` and `path`, the URL path without its query, and its `request.finished`, with `status`, `duration_us`, `request_bytes`, the bytes of request body repos read, and `response_bytes`, the bytes of response body it wrote; both counts are of the body as sent, after any chunked encoding is removed. A developer's git sends no `X-Request-Id`, so each request git makes is given an id of its own (`S02-serve.md`), and one clone or push is several requests. Between a request's two events come the domain events of the operation, in this order: any `operation.waited` (`S12-limits.md`), then the operation's own. A `POST git-upload-pack` that completes records `repo.fetched`, with `repo`, the repo's id, and `bytes`, the bytes of pack it served, `0` when it served none, as for a client asking only for the refs. A `POST git-receive-pack` whose push git accepts records one `repo.pushed` per ref it changed, with `repo`; `ref`, the ref's full name, `refs/heads/main` or `refs/tags/draft` say; `old` and `new`, the 40-hex shas the ref pointed at before and after, `0000000000000000000000000000000000000000` for a ref the push created or deleted. When one push changes several refs their order is not fixed. Each such `repo.pushed` also goes to the event bus once git has moved its ref, with the same attributes, request id, and user (`S02-serve.md`): the stories here show the trail's record, and the bus has received the same event for each. A push that changes no ref, fails, or is refused emits nothing to the bus. A ref advertisement, the two `GET` routes, records no domain event. No event carries a repo's name except as part of a request's `path`, and none carries a commit message, a path within the repo, an author, or a credential. No story in this group earns a line on stderr but the one where the event bus stays away too long, which earns the one line `S02-serve.md` gives a dropped bus event.

## A developer reads a repository's refs

The first request of every clone, fetch, and push asks for the repo's refs. repos answers with git's advertisement, uncached, and runs nothing else.

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
Cache-Control: no-cache, max-age=0, must-revalidate
```

Status 200. The body is git's ref advertisement in pkt-lines: it begins `001e# service=git-upload-pack`, a newline, and the flush `0000`; it then names `HEAD` and `refs/heads/main`, each with `<head>`; and it ends with the flush `0000`. The advertisement for `service=git-receive-pack` is the same in kind, with `Content-Type: application/x-git-receive-pack-advertisement` and a first line `001f# service=git-receive-pack`.

Preconditions:

- `notes` has commits on `main`, its only branch, and no tags.

Postconditions:

- Nothing has changed.
- telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/notes.git/info/refs"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `response_bytes` is the length of the advertisement in bytes.

## A developer clones an empty repository

A repo just made by `create` (`S06-create.md`) has no commits. It clones all the same, and git says so; the clone's `origin` is the URL it was cloned from.

Command:

```
$ git clone http://127.0.0.1:8080/notes.git
```

Output:

```
Cloning into 'notes'...
warning: You appear to have cloned an empty repository.
```

Exits 0. The text is on stderr; stdout is empty.

Preconditions:

- `notes` was created and nothing has been pushed to it: its `head` is `null`.
- The working directory holds no `notes`.

Postconditions:

- `./notes` is a git working tree with no commits, whose `origin` is `http://127.0.0.1:8080/notes.git`.
- The repo is unchanged.
- telemetry has received, for each request git made, its `request.started` and `request.finished` with status 200, and, for each `POST /notes.git/git-upload-pack` among them, a `repo.fetched` between the two, with `bytes` 0:

  ```
  {"time":"<time>","service":"repos","event":"repo.fetched","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"bytes":0,"repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

## A developer clones a repository with content

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

- `notes` has commits on `main`, its only branch.
- The working directory holds no `notes`.

Postconditions:

- `./notes` is a working tree on `main`, checked out at `<head>`, whose `origin/main` is `<head>`.
- The repo is unchanged.
- telemetry has received, for each request git made, its `request.started` and `request.finished` with status 200. The `POST /notes.git/git-upload-pack` that served the pack recorded, between its two, a `repo.fetched` whose `bytes` is the size of that pack, more than 0 and no more than its request's `response_bytes`:

  ```
  {"time":"<time>","service":"repos","event":"repo.fetched","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"bytes":<bytes>,"repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

## A developer fetches new commits

A clone made earlier catches up with commits pushed since, from another clone or another agent. To repos a fetch is the same operation as a clone.

Command:

```
$ git -C notes fetch origin
```

Output:

```
From http://127.0.0.1:8080/notes
```

Exits 0. The line is on stderr, followed by git's own line for `main` naming the old and new commits; stdout is empty.

Preconditions:

- `./notes` was cloned from `http://127.0.0.1:8080/notes.git` when `main` was at `<old>`; commits have been pushed since, so `main` is now at `<head>`.

Postconditions:

- `origin/main` in `./notes` is `<head>`; the working tree is unchanged.
- The repo is unchanged.
- telemetry has received each request's two events, and a `repo.fetched` for the `POST /notes.git/git-upload-pack` that served the new commits, with `bytes` more than 0, as in `A developer clones a repository with content`.

## A developer pushes the first commit to an empty repository

The first push creates `main`, the branch `HEAD` names, so from then on a clone checks it out.

Command:

```
$ git -C notes push origin main
```

Output:

```
To http://127.0.0.1:8080/notes.git
```

Exits 0. The line is on stderr, after git's own progress lines and before git's line reporting `main` as a new branch; stdout is empty.

Preconditions:

- `./notes` is the clone of `A developer clones an empty repository`, with one commit `<new>` on `main`.
- `notes` has no refs.

Postconditions:

- `notes`' `refs/heads/main` is `<new>`: `git --git-dir=state/repos/rep_3f9a0c1d2e4b5a69.git rev-parse refs/heads/main` prints it, and `show` answers `head` `<new>` (`S07-list-and-show.md`).
- telemetry has received, for each request git made, its `request.started` and `request.finished` with status 200. The `POST /notes.git/git-receive-pack` that carried the push recorded, between its two, one `repo.pushed`:

  ```
  {"time":"<time>","service":"repos","event":"repo.pushed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"new":"<new>","old":"0000000000000000000000000000000000000000","ref":"refs/heads/main","repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

## A developer pushes a new branch

Command:

```
$ git -C notes push origin draft
```

Output:

```
To http://127.0.0.1:8080/notes.git
```

Exits 0. The line is on stderr, after git's own progress lines and before git's line reporting `draft` as a new branch; stdout is empty.

Preconditions:

- `./notes` is a clone of `notes` with a local branch `draft` at `<new>`; `notes` has no `draft`.

Postconditions:

- `notes` has `refs/heads/draft` at `<new>`; `refs/heads/main`, and so `head`, is unchanged.
- The push's `POST /notes.git/git-receive-pack` recorded one `repo.pushed`, with `ref` `refs/heads/draft`, `old` `0000000000000000000000000000000000000000`, and `new` `<new>`, between its `request.started` and its `request.finished` with status 200; every other request git made recorded its two events and nothing else.

## A developer pushes new commits to a branch

The everyday push: `main` moves forward.

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

- `notes`' `main` is at `<old>`; `./notes` is a clone of it with commits on `main` after `<old>`, ending at `<new>`.

Postconditions:

- `notes`' `refs/heads/main` is `<new>`, and `show` answers `head` `<new>`.
- The push's `POST /notes.git/git-receive-pack` recorded one `repo.pushed`:

  ```
  {"time":"<time>","service":"repos","event":"repo.pushed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"new":"<new>","old":"<old>","ref":"refs/heads/main","repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

## A developer force-pushes a rewritten branch

repos sets no rule against rewriting history: a forced push that moves a branch to a commit that does not descend from it is accepted, as git accepts it into a bare repository by default. The commits it leaves behind stay in the repo until maintenance (`S13-maintenance.md`) finds them unreachable, so a consumer's pin to one of them keeps resolving until then (`S15-disk.md`).

Command:

```
$ git -C notes push --force origin main
```

Output:

```
To http://127.0.0.1:8080/notes.git
```

Exits 0. The line is on stderr, after git's own progress lines and before git's line reporting `main` as forced from `<old>` to `<new>`; stdout is empty.

Preconditions:

- `notes`' `main` is at `<old>`; `./notes`' `main` was rewritten to `<new>`, which does not descend from `<old>`.

Postconditions:

- `notes`' `refs/heads/main` is `<new>`.
- The push's `POST /notes.git/git-receive-pack` recorded one `repo.pushed` with `ref` `refs/heads/main`, `old` `<old>`, and `new` `<new>`, as in `A developer pushes new commits to a branch`; nothing in the trail marks it forced.

## A developer deletes a branch

A push that deletes a ref is recorded like any other change to it, with `new` all zeros.

Command:

```
$ git -C notes push origin --delete draft
```

Output:

```
To http://127.0.0.1:8080/notes.git
```

Exits 0. The line is on stderr, before git's line reporting `draft` deleted; stdout is empty.

Preconditions:

- `notes` has `refs/heads/draft` at `<old>`, beside `main`.

Postconditions:

- `notes` has no `refs/heads/draft`; `main` is unchanged.
- The push's `POST /notes.git/git-receive-pack` recorded one `repo.pushed`:

  ```
  {"time":"<time>","service":"repos","event":"repo.pushed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"new":"0000000000000000000000000000000000000000","old":"<old>","ref":"refs/heads/draft","repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

## A developer pushes several refs at once

One push can change several refs; each change is its own `repo.pushed`, all under the one request, so a consumer watching a ref sees exactly the change to it.

Command:

```
$ git -C notes push origin main refs/tags/draft
```

Output:

```
To http://127.0.0.1:8080/notes.git
```

Exits 0. The line is on stderr, after git's own progress lines and before git's two lines reporting `main` and the new tag `draft`; stdout is empty.

Preconditions:

- `notes`' `main` is at `<old>`; `./notes`' `main` is at `<new>`, descending from it, and its tag `draft` points at `<tag>`; `notes` has no tags.

Postconditions:

- `notes`' `refs/heads/main` is `<new>` and its `refs/tags/draft` is `<tag>`.
- The push's `POST /notes.git/git-receive-pack` recorded, between its `request.started` and its `request.finished`, exactly two `repo.pushed`, in either order, under its request id:

  ```
  {"time":"<time>","service":"repos","event":"repo.pushed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"new":"<new>","old":"<old>","ref":"refs/heads/main","repo":"rep_3f9a0c1d2e4b5a69"}}
  {"time":"<time>","service":"repos","event":"repo.pushed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"new":"<tag>","old":"0000000000000000000000000000000000000000","ref":"refs/tags/draft","repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

## A developer pushes a large commit, sent chunked

A push whose pack is larger than git's 1 MiB `http.postBuffer` goes as a chunked request body, with no `Content-Length`, whose size neither git nor repos knows until it ends. repos streams it into git as it arrives, holding only a fixed buffer of it at a time, so a push far larger than repos' own memory use succeeds, and repos' memory does not grow with it. 50 MiB is well inside `PUSH_MAX_BYTES` and `REPO_MAX_BYTES` (`S12-limits.md`).

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

- `notes`' `main` is at `<old>`; `./notes` has one commit after it, `<new>`, which adds a file of 52428800 random bytes, so the pack git sends is more than 50 MiB and does not compress.
- `PUSH_MAX_BYTES` and `REPO_MAX_BYTES` are unset, so their defaults apply.

Postconditions:

- git sent the pack as the body of `POST /notes.git/git-receive-pack` with `Transfer-Encoding: chunked` and no `Content-Length` (`GIT_TRACE_CURL=1` on the push shows the header).
- `notes`' `refs/heads/main` is `<new>`, and `git --git-dir=state/repos/rep_3f9a0c1d2e4b5a69.git cat-file -s <new>:<file>` prints `52428800`, where `<file>` is the added file's path.
- The `request.finished` of that `POST` has status 200 and `request_bytes` more than 52428800, and a `repo.pushed` with `ref` `refs/heads/main`, `old` `<old>`, and `new` `<new>` precedes it.
- repos' own resident memory, not counting the git it ran, rose during the push by far less than the pack's size: it never held the body whole.

## A developer pushes while the event bus is away

The bus is not a reason to refuse or slow a push. With the events app stopped, the push is accepted and answered exactly as it would be, and the trail records its `repo.pushed` as ever; repos keeps the bus event and delivers it once the events app is back, within a few minutes of the push.

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

- `notes`' `main` is at `<old>`; `./notes` is a clone of it with commits on `main` after `<old>`, ending at `<new>`.
- The events app is stopped, and is started again well within a few minutes of the push.

Postconditions:

- `notes`' `refs/heads/main` is `<new>`, and the push's requests recorded in the trail exactly what they record in `A developer pushes new commits to a branch`, its `repo.pushed` included.
- Once the events app is back, it has received the push's `repo.pushed`, with `id` beginning `evt_`, `service` `repos`, `event` `repo.pushed`, `attrs` `repo` `rep_3f9a0c1d2e4b5a69`, `ref` `refs/heads/main`, `old` `<old>`, and `new` `<new>`, the push's `request_id`, `user` `u_7f3a9c21`, an empty `cause`, and `depth` `0`.
- The trail holds no `event.lost`.

## A developer pushes while the event bus stays away too long

repos keeps a bus event only a few minutes. When the events app is still away after that, the event is dropped: the push itself is untouched, and the trail says what was lost.

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

- `notes`' `main` is at `<old>`; `./notes` is a clone of it with commits on `main` after `<old>`, ending at `<new>`.
- The events app is stopped, and stays stopped well past a few minutes after the push.

Postconditions:

- `notes`' `refs/heads/main` is `<new>`, and the push's requests recorded in the trail exactly what they record in `A developer pushes new commits to a branch`, its `repo.pushed` included.
- The events app never receives the push's `repo.pushed`, not even once it is started again.
- The trail holds an `event.lost` from `repos`, recorded after the push's `repo.pushed`, carrying among its attributes the `id` repos gave the dropped bus event.

## A developer asks for another user's repository

ann has no repo named `notes`. The caller's identity decides which repos a name can mean, so ann asking for `notes` reaches nothing, and she learns only that there is no such repo of hers, not that someone else has one. Had she a `notes` of her own, the same request would reach hers. Every route under `/notes.git/` answers her this way, and nothing runs git.

Request:

```
GET /notes.git/info/refs?service=git-upload-pack HTTP/1.1
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The body is exactly the one line `repository not found`, ending in a newline.

Preconditions:

- `u_7f3a9c21` owns `notes`; `u_2b8e1d04` owns no repo named `notes`.

Postconditions:

- Nothing has changed. git did not run.
- telemetry has received the request's two events, under the id repos gave it, with the user `u_2b8e1d04`:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_2b8e1d04","attrs":{"method":"GET","path":"/notes.git/info/refs"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_2b8e1d04","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":21,"status":404}}
  ```

## A developer asks for a repository that does not exist

A name none of the caller's repos has, a mistyped one or one renamed away (`S08-rename.md`), is answered exactly as another user's repo is. An id in the path, `/rep_3f9a0c1d2e4b5a69.git/...`, is a name like any other and matches none, since a name never holds `_`. git, given the 404, tells the developer the repository was not found.

Command:

```
$ git clone http://127.0.0.1:8080/ghost.git
```

Output:

```
Cloning into 'ghost'...
fatal: repository 'http://127.0.0.1:8080/ghost.git/' not found
```

Exits 128. The text is on stderr; stdout is empty.

Preconditions:

- `u_7f3a9c21` owns no repo named `ghost`.

Postconditions:

- Nothing has changed; no `./ghost` was left behind.
- telemetry has received the two events of `GET /ghost.git/info/refs`, the `request.finished` with status 404, `request_bytes` 0, and `response_bytes` 21, and no domain event.

## A developer's client tries git's dumb protocol

repos serves only the smart protocol. A client that falls back to the dumb one, asking for `info/refs` without a `service`, or for files of the repo by path — `HEAD`, `objects/info/packs`, a loose object — is answered `404`, so no file of the repo's directory is ever served as a file. So is any other path under a repo's name, and either git route asked with the other method, `GET /notes.git/git-upload-pack` say.

Request:

```
GET /notes.git/info/refs HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /notes.git/HEAD HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /notes.git/objects/info/packs HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The body is exactly the one line `not found`, ending in a newline.

Preconditions:

- `notes` exists and has commits.

Postconditions:

- Nothing has changed. git did not run.
- telemetry has received the request's two events, the `request.finished` with status 404, `request_bytes` 0, and `response_bytes` 10, and no domain event.

## A developer asks for an unavailable repository

A repo that startup verification found missing or broken is unavailable (`S14-verification.md`). Its name still resolves, so its owner learns that the repo is there but cannot be served, rather than that it does not exist; there is nothing to retry soon, so the answer carries no `Retry-After`. Every route under its name answers this way, and nothing runs git.

Request:

```
GET /notes.git/info/refs?service=git-upload-pack HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
```

Status 503. The body is exactly the one line `repository unavailable`, ending in a newline. The response has no `Retry-After` header.

Preconditions:

- At its last start repos found `state/repos/rep_3f9a0c1d2e4b5a69.git` missing, and marked `notes` unavailable: `show` answers its `available` `false`.

Postconditions:

- Nothing has changed. git did not run.
- telemetry has received the request's two events, the `request.finished` with status 503, `request_bytes` 0, and `response_bytes` 23, and no domain event.

## A request for a repository arrives without the identity headers

Without `X-User-Id` there is no owner to resolve a name among, and its absence means the gate or a sibling is misconfigured: repos answers as every route does (`S03-landing.md`), before it looks at the path or the method, and runs nothing.

Request:

```
POST /notes.git/git-receive-pack HTTP/1.1
Content-Type: application/x-git-receive-pack-request

<a push request>
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the one line `identity header missing`, ending in a newline. An `X-User-Id` whose value is empty is answered the same way.

Preconditions:

- The request carries no `X-User-Id` header, or one whose value is empty.

Postconditions:

- Nothing has changed. git did not run, and no ref of any repo moved.
- telemetry has received the request's two events with an empty user:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"POST","path":"/notes.git/git-receive-pack"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":24,"status":500}}
  ```

  `request_bytes` is however much of the body repos read before answering, which is not fixed.
