# Stories — in a sandbox

repos reached through a sandbox: the local runner a developer brings up from a worktree, which serves every app of the checkout in plain HTTP behind its own nginx on a loopback port, each at `http://<app>.<name>.localhost:<port>`. The stories share one sandbox: `wip`, on port `7400`, holding `auth`, `dummy`, `mcp`, `repos`, and `telemetry`, all active, with repos at `http://repos.wip.localhost:7400` and the gateway at `http://mcp.wip.localhost:7400`. Browsers, curl, and git resolve every name under `localhost` to the loopback address. The sandbox gives repos the environment of a host, `DRAIN_SECONDS`, `IKIGENBA_SERVICES`, and the eight settings of its manifest's `[env]` at their manifest values, and variables a host never sets, none of which repos reads; repos runs the developer's own `git`, found on the `PATH` the sandbox gives it. Its working directory is the sandbox's own for the app, so its database is `<sandbox data>/apps/repos/state/repos.db` and its repositories are under `<sandbox data>/apps/repos/state/repos/` (`S15-disk.md`), created on the first `up` and kept across `down` and `up` until `wipe`. The sandbox's services file lists repos with `url` `http://repos.wip.localhost:7400`, so every clone URL repos gives in the sandbox is `http://repos.wip.localhost:7400/<name>.git`, and the credential guidance names `credential.http://*.wip.localhost:7400.helper` (`S06-create.md`). The sandbox's nginx includes repos' `etc/nginx.conf` from the checkout in repos' server, as a host's does, so a large push streams through to repos (`S16-package.md`). Every request to an app other than `auth` is first put to auth's `/check`; an agent sends the sandbox's token, the one `sandbox token` prints, as `Authorization: Bearer <token>`, and git sends it as the Basic password after the sandbox's nginx challenges it on a git path, as a host's nginx does (sandbox's `S4-routing.md`). The sandbox's nginx, its routing through `/check`, its challenge, and the token are the sandbox's own (a separate sub-project), named here only by their observable effect. Every request below is made to the sandbox; responses pass through the sandbox's nginx over HTTP/1.1. The developer's shell has the token in `IKIGENBA_TOKEN` and the helper the guidance gives installed once:

```
$ export IKIGENBA_TOKEN="$(sandbox token)"
$ git config --global credential.http://*.wip.localhost:7400.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'
```

The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05-mcp.md` fixes; the members every result carries on that revision are not repeated. As on a space (`S17-on-a-space.md`), the token is in no record any story below leaves behind: not in the trail, not in a clone's `.git/config`, and not in the developer's global git config.

## An agent reaches repos in a sandbox

An agent working in the worktree opens repos' own name in the sandbox with the stored token and gets the landing page, as on a space.

Request:

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://repos.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03-landing.md`): an HTML page whose title is `repos`, whose visible text carries the heading `repos`, the heading `MCP tools` and the six tool names `list`, `show`, `status`, `create`, `rename`, and `delete`, the heading `Clone with git` and under it the credential guidance naming `credential.http://*.wip.localhost:7400.helper`, and the link `About repos` to `/about`, whose banner's profile link is titled with the email of the token's owner and leads to `http://auth.wip.localhost:7400/`, and whose footer reads `repos v<semver>`. Its banner carries the launcher button `Services`, since the sandbox's services file lists repos with its icon.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token a human created at `http://auth.wip.localhost:7400/` and stored with `sandbox token set`.
- auth's `/check` answers 200 for that token, naming its owner.

Postconditions:

- Nothing has changed but the trail: repos records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id the sandbox's nginx gave the request and the token's owner; auth records `check.allowed` for the same id with `host=repos.wip.localhost` and `path=/` (auth's `S4-check.md`).

## git in a sandbox asks for a repository without a credential

As on a space, git's first request sends no credential, and the sandbox's nginx answers auth's 401 on a git path with the Basic challenge rather than a redirect to sign in.

Request:

```
$ curl -si 'http://repos.wip.localhost:7400/notes.git/info/refs?service=git-upload-pack'
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Basic realm="ikigenba"
```

Status 401. The body is the sandbox's nginx's one line `authentication required: send your token as the password` (sandbox's `S4-routing.md`).

Preconditions:

- The sandbox above is up.
- The request carries no cookie and no `Authorization` header.

Postconditions:

- Nothing has changed. Nothing reached repos' socket, and repos recorded no event.

## An agent creates a repository in a sandbox and pushes to it

The whole loop an agent runs in a sandbox: create a repo through the sandbox's gateway, clone it from the URL the result gives, commit, and push with plain git, the helper handing git the token. `create` is additive, so the agent calls it with the gateway's `mutate` tool (mcp's `S09-mutate.md`).

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"repos","tool":"create","args":{"name":"notes"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is repos' answer to `create` (`S06-create.md`), relayed by the gateway: no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"notes","default_branch":"main","size_bytes":<n>,"available":true,"created":"<created>","clone_url":"http://repos.wip.localhost:7400/notes.git","credentials":"<guidance>"}
```

and a `content` array of one text block holding that same object encoded compactly, where `<guidance>` is the credential guidance naming `credential.http://*.wip.localhost:7400.helper`. Then, in the developer's shell:

Command:

```
$ git clone -q http://repos.wip.localhost:7400/notes.git
$ echo hello > notes/README.md
$ git -C notes add README.md
$ git -C notes commit -q -m 'First commit'
$ git -C notes push -q origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `git clone` may warn on stderr that the repository is empty; the other commands write nothing to stderr. `<new>` is the sha `git -C notes rev-parse HEAD` prints.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token auth accepts, whose owner is the user `<user-id>` and has no repo named `notes` in this sandbox.
- The developer's shell is as the opening paragraph says, and the current directory holds nothing named `notes`.

Postconditions:

- `<user-id>` owns `notes` with id `<id>`; `<sandbox data>/apps/repos/state/repos/<id>.git` is its bare repository, and its `refs/heads/main` is `<new>`. `show` gives `<new>` as `head`.
- `git -C notes remote get-url origin` prints `http://repos.wip.localhost:7400/notes.git`, holding no credential.
- The trail holds repos' `repo.created` with `repo=<id>` and `owner=<user-id>` under the gateway request's id, and `repo.pushed` with `repo=<id>`, `ref=refs/heads/main`, `old` of 40 zeros, and `new=<new>` under the id the sandbox's nginx gave the `POST /notes.git/git-receive-pack`, both under user `<user-id>`; auth's check events for the git requests carry `credential=basic`.
- `sandbox logs repos` shows no line from repos: it wrote nothing to stderr.

## A developer pushes a large commit through a sandbox

As on a space, a commit far larger than git's 1 MiB buffer goes in a chunked request body, and the sandbox's nginx, carrying repos' fragment, passes it through without a size limit, so a 50 MiB push succeeds.

Command:

```
$ head -c 52428800 /dev/urandom > notes/blob.bin
$ git -C notes add blob.bin
$ git -C notes commit -q -m 'Add a large file'
$ git -C notes push -q origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `ls-remote` writes its one line to stdout; nothing is on stderr. `<new>` is the sha `git -C notes rev-parse HEAD` prints.

Preconditions:

- The clone of `An agent creates a repository in a sandbox and pushes to it` exists, and its `main` is `<old>`, the sha `notes`' `main` points at in repos.
- The sandbox's nginx was configured by an `up` run with `repos/etc/nginx.conf` in the checkout.

Postconditions:

- `notes`' `refs/heads/main` in repos is `<new>`; no `413` or other refusal reached git.
- The trail holds repos' `repo.pushed` with `ref=refs/heads/main`, `old=<old>`, and `new=<new>`, and `request.finished` with `status=200` and `request_bytes` of at least 52428800, under the id the sandbox's nginx gave the push and the token's owner.
