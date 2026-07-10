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

## Alternatives evaluated and not chosen

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
