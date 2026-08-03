# sites — Research

Collected external ground truth the design references. Non-contractual: the
build loop never reads this; design cites these facts instead of re-deriving
them.

## chromedp (browser automation from Go)

`github.com/chromedp/chromedp` is a pure-Go library that drives a Chrome/Chromium
browser over the **Chrome DevTools Protocol** (the same wire protocol Chrome's
own devtools use). No node, no npm, no driver server, no cgo — a Go test talks
TCP/websocket to a Chrome process it spawns itself.

**The API footprint the design uses** (all of it — the library is much larger):

- `chromedp.NewExecAllocator(ctx, opts...)` — launches and owns a Chrome
  process. `chromedp.DefaultExecAllocatorOptions` includes `headless` (the
  modern `--headless=new` engine — the real browser minus the window), a
  **fresh temporary `--user-data-dir`** (no profile, no cookies, no history,
  fully isolated from any desktop Chrome), and sandbox/GPU flags suitable for
  unattended runs. It finds the browser binary by looking up well-known names
  (`google-chrome`, `chromium`, …) on `PATH` unless `chromedp.ExecPath` pins
  one.
- `chromedp.NewContext(allocCtx)` — one browser tab. Everything hangs off
  `context.Context`: cancelling the context kills the tab/browser (cleanup is
  the `defer cancel()` stack), and `context.WithTimeout` bounds any scenario so
  a hung page fails instead of hanging `go test`.
- `chromedp.Run(ctx, actions...)` — executes actions sequentially:
  - `chromedp.Navigate(url)` — load a page.
  - `chromedp.WaitVisible(sel)` — poll until the CSS-selected element exists
    **and is visible**. The idiomatic no-sleep way to wait for JS to act; doubles
    as an assertion that it did.
  - `chromedp.SendKeys(sel, text)` — dispatch genuine trusted key events
    character-by-character; the page's real `input`/`keydown` listeners fire.
    Requires the target to be visible/interactable — typing into a `hidden`
    element fails.
  - `chromedp.Click(sel)` — a real click on the selected element.
  - `chromedp.Evaluate(js, &out)` — run a JS expression in the page and marshal
    its JSON result into a Go value (the DOM read-back channel).

**Costs and characteristics:**

- The Chrome launch is the expensive step (~300–800 ms once per allocator);
  each action afterward is milliseconds. Multi-step scenarios amortize the
  launch by sharing one session.
- The dominant flake mode is the **launch**, not the scenario; a single launch
  retry distinguishes "Chrome hiccuped" from "Chrome broken/absent".
- Transitive deps: `chromedp/cdproto` (large machine-generated DevTools
  protocol bindings — a chunky `go.sum` diff, build-cache absorbed),
  `gobwas/ws` (websocket), small utilities. All pure Go.
- The browser binary itself is an **environment assumption** `go.mod` cannot
  express — like a C compiler. It must be documented as part of the suite's
  green definition.
- Debug escape hatch: dropping the `headless` flag runs the same test headful
  (a visible window) for diagnosis. Never the default.

## Environment facts (verified on this box, 2026-07-10)

- `/usr/bin/google-chrome` is installed (the binary chromedp finds on `PATH`).
- `node` v24 / `npx` exist but nothing in this repo uses them; no Playwright
  package is installed (npm/pip/CLI all absent). A stale `~/.cache/ms-playwright`
  browser cache exists but is unused.
- The ralph build loop runs on this box; the deploy box never runs the test
  suite; there is no CI. Every environment that runs `go test ./...` has Chrome.

## The suite copy-button pattern (prior art to replicate)

The dashboard's logged-in page already ships a **copy-to-clipboard button** for
each MCP service's URL. sites replicates this pattern for its per-row copy-URL
control (D6/D22). It is captured here as ground truth so the design cites the
*pattern*, not a `dashboard/` file — the scope boundary forbids sites depending
on a sibling module, so sites owns its own byte-for-byte copy (exactly as
`tokens.css` is a per-service vendored copy, not a shared runtime dep). The
pattern, observed on the dashboard (`2026-07-10`), is:

- **Markup.** `<button type="button" class="copy-btn" aria-label="Copy … URL">`
  containing an inline copy-icon `<svg class="icon" …>` (two overlapping
  rounded rectangles — the conventional "copy" glyph, `viewBox="0 0 24 24"`,
  `stroke="currentColor"`, `fill="none"`) followed by
  `<span class="copy-label">Copy</span>`. The URL to copy sits in the same row —
  on the dashboard a sibling `<code>`; sites, having no `<code>`, exposes the
  URL on the button itself (a data attribute) since its rows are rebuilt by the
  controller.
- **Behaviour (JS).** On click: copy the URL text via
  `navigator.clipboard.writeText(text)` when `navigator.clipboard` exists **and**
  `window.isSecureContext`; otherwise fall back to a hidden `<textarea>` +
  `document.execCommand("copy")` (covers plain-http localhost without a secure
  context). On success, add `is-copied` to the button and swap the label to
  `Copied`, then revert both after ~1600 ms. A denied/unavailable clipboard is
  swallowed (the user can select manually).
- **CSS.** `.copy-btn` (icon-plus-label affordance, `--icon-sm` icon, hover /
  focus-visible / `.is-copied` accent states) and `.copy-label`, all built from
  the shared Carbon token custom properties — no bespoke values. sites rebuilds
  these rules in its own `share/www` from its own `tokens.css`.
- **Secure-context / clipboard-permission facts.** `navigator.clipboard` is
  available on `http://127.0.0.1` and `http://localhost` (both are secure
  contexts by spec), so the async path — not the `execCommand` fallback — is the
  one exercised by an `httptest` server (which listens on `127.0.0.1`). Reading
  the clipboard back in headless Chrome (chromedp) requires granting the browser
  context clipboard permission via the DevTools `Browser.grantPermissions`
  (`clipboardReadWrite`) before `navigator.clipboard.readText()` will resolve.

## Suite telemetry — the external contracts sites consumes

Facts collected for D28/D29 so those Decisions cite rather than re-derive them.
None of this is sites' to define: the header, the record, and the shared client
are the suite's, and appkit owns the client's design.

- **The header.** `X-Correlation-Id`, whose value is a bare 26-character
  Crockford-base32 ULID. One id per causal chain, propagated verbatim on every
  hop — never re-minted mid-chain.
- **Who mints it.** The dashboard's introspection endpoint mints the id while
  answering an `auth_request` subrequest for a gated route and returns it as a
  response header. For a request that arrives with no id at all (an ungated
  public route), appkit's inbound middleware mints one — the universal
  read-or-mint point. Loopback callers are inside the trust boundary, so an id
  arriving on the loopback interface is trusted as-is.
- **The instrumented outbound client (appkit's, handed out by the Router as `rt.HTTPClient(…)`).** An `*http.Client`
  whose transport records each call as a telemetry record of kind `outbound`
  (operation, elapsed, status class, response size + SHA-256 digest — never the
  response bytes) and attaches `X-Correlation-Id` **only** when the destination
  host is `127.0.0.1`, so nothing leaks to a third party. It reads the id off
  the outgoing request's `Context()`, which is why the caller must thread its
  handler context down to the call instead of detaching it — Go's
  `http.Request.Context()` is exactly the channel a `RoundTripper` sees.
  Recording is best-effort: the telemetry service being down never blocks or
  fails an outbound call.
- **dropbox is a loopback peer.** sites reaches the dropbox mirror through the
  registry-resolved `http://127.0.0.1:<port>` base URL, so mirror traffic is
  inside the propagation rule. `net/http/httptest.NewServer` also binds
  `127.0.0.1`, so a test server is the *real* substrate for that rule rather
  than a stand-in for it.
- **sites emits no events.** The event-plane change that adds `correlation_id`
  to the outbox and the wire envelope (and the `Append` signature change that
  makes it compile-caught) touches producers only. sites has no producer and no
  consumer, so it sees neither.

## nginx facts the fragment change relies on

Verified against the nginx `ngx_http_auth_request_module` and
`ngx_http_proxy_module` documentation:

- `auth_request_set $var $upstream_http_<header_name>;` copies a **response**
  header of the auth subrequest into a variable, with the header name
  lowercased and `-` replaced by `_` (so `X-Correlation-Id` is read as
  `$upstream_http_x_correlation_id`). The binding is **per location** — the same
  variable name used in several `location` blocks is set independently in each,
  which is why the fragment can reuse one naming scheme across gates.
- `proxy_set_header <Name> <value>;` **replaces** any inbound client header of
  that name on the upstream request; the last directive for a name in a location
  wins. This is the identity-hygiene property the owner headers already rely on,
  applied to the correlation header.
- `proxy_set_header <Name> "";` — an **empty value means the field is not passed
  to the proxied server at all**. That is the strip: the upstream sees no such
  header rather than seeing an empty one, so appkit's read-or-mint middleware
  takes the mint branch.
- `auth_request` only understands 2xx (allow) and 401/403 (deny); the fragment's
  existing `@sites_authn_500` re-emit handles the collapsed-status case and is
  unaffected by adding header captures.

## Alternatives evaluated and not chosen (browser testing)

- **Playwright (node).** Would drag a second-language toolchain into a pure-Go
  repo: `package.json`, `node_modules`, a version-churning driver. Everything it
  offers that this design needs, chromedp does over the same DevTools protocol
  with zero node dependency. Rejected.
- **A goja DOM shim.** Hand-rolling a fake `document`/event system to test the
  controller in goja is a mock that passes whatever it is taught to pass — it
  cannot falsify real browser wiring. Rejected on verification-substrate
  grounds.
- **`t.Skip` when Chrome is absent.** Keeps the suite pure-Go-green anywhere but
  makes the gate soft: an environment misconfiguration silently un-proves the
  wiring, and a skipped test reads as green to the verify step. Rejected in
  favor of a hard requirement.
