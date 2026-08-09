# repos — Research

Collected external ground truth the design leans on, so no Decision has to
re-derive it. **Non-contractual**: nothing here is a requirement, and the build
loop never reads it. Everything below is about the one external dependency repos
actually has — the `git` binary — plus the two nginx directives its fragment
relies on.

## 1. Bare repositories

- `git init --bare --initial-branch=main <dir>` creates a repository whose
  working tree is the repository directory itself; `git rev-parse
  --is-bare-repository` prints `true`. `HEAD` is written as a symbolic ref to
  `refs/heads/main` **before that branch exists** — a valid, normal state. A
  clone of such a repository succeeds and leaves the client on `main` with no
  commits.
- `git ls-remote <dir>` lists zero refs for a repository with no commits, and
  `git rev-parse main` fails there. Code must treat "no commits yet" as a state,
  not an error.
- Renaming a bare repository's directory moves the entire object database and
  ref store with it: nothing inside a bare repository stores its own path
  (there is no `core.worktree`, no absolute path in `config` for a plain
  `init --bare`), so a directory rename preserves history exactly.

## 2. Committing to a bare repository without a worktree

The plumbing sequence, all of it supported and stable, using a temporary index
via the `GIT_INDEX_FILE` environment variable:

```
GIT_INDEX_FILE=<tmp>/index git read-tree <commit-ish>        # seed from the current head
GIT_INDEX_FILE=<tmp>/index git update-index --add --cacheinfo <mode>,<blob>,<path>
GIT_INDEX_FILE=<tmp>/index git update-index --force-remove <path>
GIT_INDEX_FILE=<tmp>/index git write-tree                    # → tree sha
git hash-object -w --stdin                                   # → blob sha (stdin = the bytes)
git commit-tree <tree> [-p <parent>] -m <message>            # → commit sha
git update-ref refs/heads/main <new> <old>                   # compare-and-swap
```

- `read-tree` is skipped when there is no parent commit; `write-tree` over an
  empty index yields the empty tree.
- `update-index --cacheinfo` takes `<mode>,<sha>,<path>` in a single argument in
  modern git. Regular file modes are `100644` and `100755`.
- `commit-tree` reads author/committer identity and dates from
  `GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL`/`GIT_AUTHOR_DATE` and the
  `GIT_COMMITTER_*` trio. Fixing all six makes a commit sha fully deterministic
  given the tree and parents — which is what lets tests assert exact shas.
- `git update-ref <ref> <new> <old>` fails if the ref is not currently `<old>`;
  passing the 40-zero sha as `<old>` means "must not exist". This is the
  compare-and-swap.
- `git archive --format=tar <ref>` writes a tar of the tree to stdout with no
  VCS metadata; it fails for a ref that does not resolve.
- `git ls-tree -r -l -t <ref> [<path>]` lists blobs (with size) and, with `-t`,
  the tree entries too.
- `git merge-tree --write-tree <branch1> <branch2>` (git ≥ 2.38) performs a real
  three-way merge **entirely in the object database**: it prints the resulting
  tree sha on success and exits non-zero with a conflict report when the merge
  conflicts. No index, no worktree, and nothing is written to any ref.
- `git merge-base --is-ancestor <a> <b>` exits 0 when `a` is an ancestor of `b`
  — the fast-forward test.
- `git cat-file -e <sha>^{commit}` exits 0 only when the sha names a commit that
  exists in that repository.

## 3. Serving the smart-HTTP protocol: `git http-backend`

`git http-backend` ships with git and is a **CGI program** implementing the
whole smart-HTTP protocol (ref advertisement, upload-pack, receive-pack).

- Environment it reads: `GIT_PROJECT_ROOT` (the directory repositories live
  under), `GIT_HTTP_EXPORT_ALL` (serve repositories without a
  `git-daemon-export-ok` marker), `PATH_INFO` (the repository path plus the
  service path), `REQUEST_METHOD`, `QUERY_STRING`, `CONTENT_TYPE`,
  `CONTENT_LENGTH`, `HTTP_CONTENT_ENCODING`, and `REMOTE_USER` (used as the
  default identity for reflog entries).
- It reads the request body from **stdin** and writes a CGI response to
  **stdout**: `Header: value` lines, a blank line, then the body. A caller must
  parse those headers and copy them onto the HTTP response.
- The two request shapes are `GET  <repo>/info/refs?service=git-upload-pack|git-receive-pack`
  (response `Content-Type: application/x-git-<service>-advertisement`, body
  beginning with the pkt-line `# service=<service>`) and
  `POST <repo>/git-upload-pack|git-receive-pack`.
- Any environment variable set for the CGI process is inherited by the hooks
  `receive-pack` runs, which is how a per-request policy value reaches
  `pre-receive`.

## 4. Hooks

- `pre-receive` runs **once** per push, before any ref is updated, reading lines
  of `<old-sha> <new-sha> <ref>` on stdin. A non-zero exit **rejects the entire
  push** — no ref is updated — and its stderr is relayed to the client's
  terminal. The all-zeros sha means "ref does not exist" as `<old>`, or "delete
  this ref" as `<new>`.
- Hooks must be executable (`0755`); a non-executable hook is silently skipped,
  which is why the design re-materializes it rather than assuming it survives a
  copy or a restore.
- `receive.denyNonFastForwards` is a repository-wide config that refuses any
  non-fast-forward update on **every** ref; there is no per-ref form, which is
  why a hook is used instead.
- A hook inherits the CGI environment (above) and runs with the bare repository
  as its working directory, so plain `git` commands inside it operate on the
  right repository with no `-C`.

## 5. Client-side facts the door tests rely on

- `git clone http://user:pass@host/path` sends HTTP Basic credentials; git only
  offers them after a `401` carrying `WWW-Authenticate: Basic realm="…"`, so a
  door that wants credentials must challenge.
- `GIT_TERMINAL_PROMPT=0` makes a credential prompt a hard failure instead of a
  hang — required for any test that expects an unauthenticated clone to fail.
- `git push` reports a `pre-receive` rejection as a non-zero exit with the
  hook's stderr prefixed `remote:`.
- `git -c protocol.version=2` is the default in current git; `http-backend`
  handles both v0 and v2 without the caller doing anything.

## 6. nginx facts the fragment relies on

From `ngx_http_auth_request_module` and `ngx_http_proxy_module`:

- `auth_request_set $var $upstream_http_<header_name>;` copies a **response**
  header of the auth subrequest into a variable, the name lowercased with `-`
  replaced by `_` (so `X-Correlation-Id` reads as
  `$upstream_http_x_correlation_id`). The binding is **per location**, so the
  same variable name used in several blocks is set independently in each.
- `proxy_set_header <Name> <value>;` **replaces** any inbound client header of
  that name on the upstream request; the last directive for a name in a location
  wins.
- `proxy_set_header <Name> "";` — an **empty value means the field is not passed
  to the proxied server at all**, so the upstream sees no such header rather
  than an empty one.
- `auth_request` understands only 2xx (allow) and 401/403 (deny); every other
  subrequest status collapses to a synthetic 500, which the `@repos_authn_500`
  re-emit location converts back into a faithful 429.
- nginx selects the **longest matching prefix** location regardless of order in
  the file, so `/srv/repos/git/` wins over `/srv/repos/` without reordering.
- By default nginx **buffers the whole request body** before proxying
  (`proxy_request_buffering on`) and caps it at `client_max_body_size` (1m).
  Both must be changed for a git push, which streams a pack of arbitrary size.
  `proxy_buffering off` likewise keeps a long clone streaming rather than
  accumulating in nginx.

## 7. chromedp (browser automation from Go)

`github.com/chromedp/chromedp` is a pure-Go library that drives a
Chrome/Chromium browser over the **Chrome DevTools Protocol**. No node, no
npm, no driver server, no cgo — a Go test talks websocket to a Chrome process
it spawns itself.

**The API footprint the design (D26) uses:**

- `chromedp.NewExecAllocator(ctx, opts...)` — launches and owns a Chrome
  process. `chromedp.DefaultExecAllocatorOptions` includes `headless` (the
  modern `--headless=new` engine), a fresh temporary `--user-data-dir` (no
  profile, fully isolated from any desktop Chrome), and sandbox/GPU flags for
  unattended runs. It finds the browser by well-known names (`google-chrome`,
  `chromium`, …) on `PATH` unless `chromedp.ExecPath` pins one.
- `chromedp.NewContext(allocCtx)` — one browser tab; cancelling the context
  kills the tab/browser, and `context.WithTimeout` bounds a scenario so a hung
  page fails instead of hanging `go test`.
- `chromedp.Run(ctx, actions...)` — sequential actions: `Navigate(url)`,
  `WaitVisible(sel)` (poll until the element exists **and** is visible — the
  idiomatic no-sleep wait that doubles as an assertion), `SendKeys(sel, text)`
  (genuine trusted key events; the page's real listeners fire; the target must
  be visible), `Click(sel)` (a real click), and `Evaluate(js, &out)` (run JS
  in the page and marshal the result back — the DOM read-back channel).

**Costs and characteristics:** the Chrome launch is the expensive step
(~300–800 ms per allocator); actions afterward are milliseconds, so multi-step
scenarios amortize one launch. The dominant flake mode is the launch, not the
scenario — one launch retry distinguishes "Chrome hiccuped" from
"Chrome broken/absent". Transitive deps (`chromedp/cdproto`, `gobwas/ws`) are
all pure Go. The browser binary itself is an environment assumption `go.mod`
cannot express — like a C compiler — and must be documented as part of the
suite's green definition. Dropping the `headless` flag runs the same test
headful for diagnosis; never the default.

**Environment facts (verified on this box, 2026-08-09):**
`/usr/bin/google-chrome` is installed. The ralph build loop runs on this box;
the deploy box never runs the test suite; there is no CI — every environment
that runs `go test ./...` has Chrome. `github.com/dop251/goja` (pure-Go ES
engine) is the sibling precedent already proven in the suite for testing
shipped browser JS's pure functions inside `go test` with no node toolchain.

**Alternatives evaluated and not chosen:** Playwright (drags a node toolchain
— `package.json`, `node_modules`, driver churn — into a pure-Go repo for
nothing chromedp can't do over the same protocol); a goja DOM shim (a
hand-rolled fake `document` passes whatever it is taught and cannot falsify
real event wiring); `t.Skip` when Chrome is absent (a soft gate that silently
un-proves the wiring and reads as green).

## 8. The suite copy-button pattern (prior art to replicate)

The dashboard's logged-in page ships a copy-to-clipboard button for each MCP
service's URL, and sites replicated it for its landing listing. repos
replicates the same pattern for its per-row clone-address control (D24/D25).
Captured here as ground truth so the design cites the *pattern*, not a
sibling module's files — the scope boundary forbids repos depending on
`dashboard/` or `sites/`; repos owns its own byte-for-byte copy. The pattern:

- **Markup.** `<button type="button" class="copy-btn" aria-label="Copy … URL">`
  containing an inline copy-icon `<svg class="icon">` (two overlapping rounded
  rectangles, `viewBox="0 0 24 24"`, `stroke="currentColor"`, `fill="none"`)
  followed by `<span class="copy-label">Copy</span>`. The value to copy is
  exposed on the button itself (a data attribute) since the rows are rebuilt
  by the controller.
- **Behaviour (JS).** On click: copy via
  `navigator.clipboard.writeText(text)` when `navigator.clipboard` exists
  **and** `window.isSecureContext`; otherwise a hidden `<textarea>` +
  `document.execCommand("copy")` fallback. On success, add `is-copied` and
  swap the label to `Copied`, reverting both after ~1600 ms. A denied
  clipboard is swallowed.
- **CSS.** `.copy-btn` (icon-plus-label affordance, hover / focus-visible /
  `.is-copied` accent states) and `.copy-label`, built entirely from the
  shared Carbon token custom properties — no bespoke values. repos rebuilds
  these rules in its own `share/www` from its own `tokens.css`.
- **Secure-context / clipboard-permission facts.** `navigator.clipboard` is
  available on `http://127.0.0.1` and `http://localhost` (secure contexts by
  spec), so the async path — not the fallback — is what an `httptest` server
  exercises. Reading the clipboard back in headless Chrome requires granting
  the browser context clipboard permission via the DevTools
  `Browser.grantPermissions` (`clipboardReadWrite`) before
  `navigator.clipboard.readText()` resolves.
