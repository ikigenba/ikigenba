# design

The signature visual style for every Ikigenba app, service, and page. This is
a reference that sits beside the spec-managed sub-projects. Open `index.html` in
a browser.

## Layout

```
design/
  index.html        links into ikigenba/
  ikigenba/
    theme.css       the entire style — tokens, elements, components
    feedback.js     button press and copy feedback, the toast
    lab.js          the page-switcher toolbar
    favicon.svg     the suite's favicon, linked from every page
    icons/tabler/   the Tabler SVGs in use, with LICENSE and VERSION
    specimen.html   the parts: tokens, type, controls, table, alerts, states
    app.html        dummy's panel: banner, widgets table, add form, states
    banner.html     the banner: product, service, launcher, profile, sign out,
                    with and without the launcher, and at 375px
    launcher.html   the banner's service launcher, open, filtered, empty, disabled
    login.html      auth's sign-in, its error state, the signed-in profile
    profile.html    account, sessions, API tokens
    scripts.html    scripts' catalog: your scripts and their last runs
    scripts-script.html
                    one script and its runs, under a breadcrumb
    scripts-run.html
                    one run: details, input, output, files, other states
    events.html     events' landing: its subscribers and its MCP tools
    cron.html       cron's landing: the space's triggers and its MCP tools
    icons.html      every icon shipped code emits, and what it says
    landing.html    marketing: hero, features, call to action
    prose.html      long-form docs / blog / legal
```

## Rules

- All styling lives in `theme.css`; pages carry only semantic markup.
- App markup stays close to what the Go services emit: `header`, `main`,
  `table`, `form`, `label` + `input` + an error `span` wired with
  `aria-describedby`. Style elements first; add a class only when an element
  alone can't say it. A service should get most of the look by linking the
  stylesheet.
- Typography is exact. Every face is the one we would ship, loaded from Google
  Fonts (a service self-hosts the same files in production), and every weight,
  style, and optical size a page uses is loaded — variable fonts with their
  full `wght`/`opsz` ranges. `font-synthesis: none` everywhere, so every
  weight and italic on screen is the real cut. The design faces render every
  page; system stacks exist only as fallbacks. Google Fonts is the only
  external request.
- Fonts load without a flash. Every `@font-face` uses `font-display:
  fallback`. In production a service serves each font file at a
  content-addressed URL (the file's hash in its name), cached `public,
  max-age=31536000, immutable`, and every page preloads upright Inter; italic
  and JetBrains Mono are not preloaded.
- Light grounds. Colors are custom properties on `:root`.
- Every page fits a 375px-wide screen; a wide table scrolls inside its own
  container.
- Visible focus states. Errors are stated in words, with an icon, as well as
  in color.
- A page below the root of a hierarchy opens with a breadcrumb: a
  `nav.crumbs` labelled `Breadcrumb`, holding an ordered list, root first,
  each level a link except the last, the current page, which carries
  `aria-current="page"` and no link. The root page has none; its `h1` is the
  root. The `h1` beneath repeats the current level's name.
- Every page ends with the feedback script (`ikigenba/feedback.js`), then the
  lab toolbar script (`ikigenba/lab.js`).

## Shared content

The audience is technical: these are tools for people who build and operate
systems. The product name is **Ikigenba**. The example space is
`acme.ikigenba.com`, its workspace domain `acme.dev`, the user
`ada@acme.dev`.

**app.html** — dummy's control panel.
- Banner: as banner.html has it, the service `dummy`, without the launcher;
  the profile links to `https://auth.acme.ikigenba.com/`, sign out posts to
  `https://auth.acme.ikigenba.com/logout`.
- Footer: the service name and its version, `dummy v0.8.0`, muted and small.
  Banner and footer appear only on signed-in pages.
- `h1` Widgets; a table of Name / Count / Status: `alpha` 3 active, `beta` 0
  paused, `gamma` 12 retired, `delta` 128 active, `epsilon` 7 paused,
  `zeta` 1024 active.
- The Add widget form (Name, Count, Status select, `Add widget` button) shown
  mid-error: Name `alpha` → "that name is already taken", Count `-2` → "the
  count cannot be negative", Status `active` accepted.
- A states section: the table with no widgets; the message page
  ("Widget created." + `Back to widgets`).

**banner.html** — the banner every app page carries, in this order:
- `strong.mark[data-service=<service>]`: the favicon as an image (`img` of
  `favicon.svg`, `alt=""`, 18px; the glyph is hidden when the mark holds
  one), the text **Ikigenba**, capitalised, then after a hairline
  `span.service` holding the app's own icon and its name. The icon is the
  app's `share/icon.svg` inserted verbatim, no class, 16px, styled as
  `.mark > .service > svg`. `data-service` stays as a hook; the visible name
  is the span's.
- At the right edge three quiet 36px icon buttons: the service launcher's
  grid (`button.launcher[popovertarget=services]`, labelled and titled
  `Services`, only when there are services), the profile (`a.profile`, a
  `user-circle`, labelled `Profile`, titled with the email) and sign out
  (`button.signout`, the `logout` icon, labelled and titled `Sign out`, in
  `form.inline` posting to the logout URL). With no launcher the other two
  keep their places.
- Below 640px the product name hides and the favicon stays, so a long
  service name (`telemetry`) fits beside three icons at 375px. The text stays
  bare in the mark: the rule sets the mark's `font-size` to 0 and the
  service restores it.
- The separator is the hairline; a `|` glyph was drawn and dropped.
- Shown with the launcher (its popover opens from the page's banner),
  without it, with `telemetry`, and in 375px frames. The lab frames a banner
  in `.frame`, which shares `body > header`'s rules (`:is(body, .frame) >
  header`); `.frame.narrow` takes the below-640px rules.

**launcher.html** — the service launcher, shown from dummy.
- Banner and footer as in app.html, with the launcher's grid button first of
  the icons at the right edge; the panel open on load, hanging under it, its
  right edge on the banner's.
- The panel: a `Find a service` search field over a 4-column grid of 30
  services, A to Z, each a Tabler icon over its name, linking to
  `https://<name>.acme.ikigenba.com/`; `dummy` marked current. The services
  known today — `auth`, `crm`, `cron`, `dummy`, `events`, `files`,
  `invoices`, `ledger`, `prompts`, `repos`, `scripts`, `sites`, `webhooks`,
  `wiki` — with plausible others to fill the grid to scale.
- `repos` is disabled (`opsctl disable`): its tile stays in place, faint and
  unlinked, titled "repos is unavailable".
- States: filtered by `cr` (cron, scripts, secrets); no match for `kafka`;
  filtered by `re` with `repos` unavailable beside `secrets`.
- Below 640px the panel is a full-width sheet under the banner.
- Each tile's icon is the app's own `share/icon.svg`, inserted verbatim, so
  it carries no class: the launcher styles `nav.services a > svg`, never
  `svg.ico`, and leaves the icon without `aria-hidden` (the link text names
  the service).

**scripts.html, scripts-script.html, scripts-run.html** — scripts at
`scripts.acme.ikigenba.com`, three levels deep: the catalog, one script, one
run. Banner and footer as in app.html, the service `scripts`, `scripts v0.1.0`.
- The catalog: `h1` scripts, a lede, the user's own scripts (never another
  user's) as a table of Script / Repository / Ref / Last run / When:
  `nightly-report` (exited 0), `sync-crm` (running), `rotate-keys` (failed),
  `backfill` (never run). The catalog stores a repository as its `rep_` id;
  a page resolves the name from the repository's directory when it renders,
  the id in the cell's `title`, and shows the id, muted, when the directory
  is gone (`backfill`). An MCP
  tools list naming `list`, `show`, `create`, `update`, `delete`, `run`,
  `runs`, `result`, `cancel`. State: no scripts.
- One script, `nightly-report`: breadcrumb scripts / nightly-report; a key/
  value card (id, repository, ref, created, runs kept); the runs table, newest
  first, Run / Status / Commit / Started / Duration / Exit, with one row per
  status. The repository shows its name over its id as secondary text.
  `section#subscriptions` (h2 Subscriptions) sits between the card and the
  runs: `ul#subscription-list`, one `li[data-event=<name>] > code` per
  subscribed event, sorted by name. States: no subscriptions
  (`div#no-subscriptions.empty`, "This script is subscribed to no events."),
  no runs.
- One run: breadcrumb scripts / nightly-report / `run_3f9a1c2e8b7d4a60`; a
  headline of status, start time and duration; the run card (status, script,
  commit, ref, started, finished, duration, trigger, user, request, output
  sizes); then Input, Standard output, Standard error, each a `pre` with a
  Download button, and Files, the `out/` folder as a table with a download
  per file. The run card's header says what started it, `p#started-by`:
  "Started by the `run` tool." or "Started by the event `evt_<16 hex>`."
  (the id in `code`). States: started by an event, still running (reload, no streaming), timed out with
  output truncated, failed to start with its reason, files gone.
- Run status reads as a `.status` word: running is info, exited 0 is ok,
  a non-zero exit, timed out and killed are warn, failed is err.

**events.html** — events at `events.acme.ikigenba.com`, the suite's internal
event bus. Banner and footer as in app.html, the service `events`,
`events v0.1.0`.
- `h1` events, a lede, the subscribers as a table of Service / Status /
  Cursor / Lag / Since: `scripts` ok and caught up, `sites` paused, showing,
  muted, the event it is stuck on and the error it answered. An MCP tools
  list naming `catalog`, `search`, `subscribers`, `skip`, `resume`, and a
  link to the about screen. State: no subscribers.
- Subscriber status reads as a `.status` word carrying two attributes:
  `data-status` is the real status (`ok`, `paused` or `gone`) and `data-kind`
  is the theme kind it shows as: ok is ok, paused is warn, gone is info.

**cron.html** — cron at `cron.acme.ikigenba.com`, triggers that emit events
on the suite's event bus on a schedule. Banner and footer as in app.html, the
service `cron`, `cron v0.1.0`.
- `h1` cron, a lede, every trigger in the space, whoever owns it, sorted by
  slug, as a table of ID / Slug / When / Owner / Status / Last fired / Next:
  `month_end` (`@monthly`, `grace@acme.dev`, active, never fired, next
  `2026-11-01 00:00`), `weekly_digest` (`0 8 * * 1`, the user's own, marked
  with the `yours` badge, paused, last fired `2026-09-28 08:00`, no next). An
  MCP tools list naming `list`, `show`, `create`, `update`, `pause`,
  `resume`, `delete`, and a link to the about screen. State: no triggers
  (`div#no-triggers.empty`).
- Trigger status reads as a `.status` word whose `data-status` is the status:
  active is ok, paused is warn. Times are UTC to the minute in a `time`
  carrying the RFC 3339 moment; a cell with no time is empty.

**login.html** — auth at `auth.acme.ikigenba.com`.
- Sign-in: product mark, "Sign in to acme", note that access is limited to
  `@acme.dev` Google accounts, `Continue with Google` (links `/login/google`).
- Error state: "Sign-in failed" — the account `ada@gmail.com` is not in the
  `acme.dev` workspace; `Try another account`.
- Signed-in profile: email, signed in via Google,
  last sign-in `2026-09-24 14:02 UTC`, session expires in 11h 58m, `Sign out`.

**specimen.html** — the parts: color tokens (swatch, name, value); type scale
h1–h4, body, small, mono; paragraph with a link, inline `code`, `kbd`; a code
block (a `curl` call); buttons (primary, secondary, danger, disabled); text
input, select, checkbox, input in error; a table; status badges for active /
paused / retired; alerts info / success / warning / error; a copy block and the toast, ok and
error; a key/value list; a card/panel; an empty state.

## The style

- Character comes from type, spacing, structure, and color alone.
- Light grounds.
- Type: **Inter** (variable, opsz 14–32, wght 100–900, with italic) for
  everything; **JetBrains Mono** (variable 400–700) for code.
- Palette **ink**: ground `#fdfdfd`, surface `#fff`, subtle `#f7f7f7`, fg
  `#0a0a0a`, muted `#646464`, faint `#949494`, line `#e5e5e5`, line-soft
  `#f0f0f0`. Black is the accent; blue `#0060f0` is for links and focus only.
  A light banner.
- Status colors (**deep**): ok `#166534`, warn `#c2410c`, err `#b91c1c`, info
  `#1d4ed8`.
- Status color is always solid: a solid mark, dot, edge, or pill on a white
  or grey ground.
- Status treatments: `.status` (filled icon + word), `.badge` (white pill with
  a colored dot), `.badge.strong` (solid pill). The kind comes from
  `data-kind` (ok, warn, err, info, accent); dummy's widgets use
  `data-status` (active, paused, retired).
- Alerts: `.alert` (white card, colored icon), `.alert.callout` (adds a
  colored left edge), `.alert.quiet` (grey fill).
- Breadcrumbs: `nav.crumbs > ol`, small and muted, levels separated by a
  faint `/`, the current page in the foreground color. They wrap on a narrow
  screen.
- Field errors: message only. The input keeps its normal border; the red
  message with its icon beneath it, matched by `[id$="-error"]`, carries the
  error.
- Icons: **Tabler** (`@tabler/icons` 3.48.0, MIT), outline at its native 2px
  stroke — the library's own SVGs, used at their native sizes. Inline
  `<svg class="ico">` is 16px in buttons and links, 18px by default. Filled
  variants, as CSS masks in `--i-ok`, `--i-warn`, `--i-err`, `--i-info`,
  `--i-alert`, `--i-neutral`, mark status, alerts, and field errors.
- Favicon: `favicon.svg`, *ik* stroked in an outline rounded square at the
  outline icons' weight, ink on light; it flips to near-white under
  `prefers-color-scheme: dark`. One icon for the whole suite; every page links
  it with `<link rel="icon" type="image/svg+xml">`.
- Wordmark: in the banner the product mark opens with the favicon itself, an
  18px `img`, then **Ikigenba**. Elsewhere (the sign-in card, the landing and
  prose pages) it opens with the favicon's *ik* glyph, drawn as a CSS mask
  filled with `--accent` so it follows the page theme, not the OS scheme;
  14px, 22px on the sign-in card.
- Motion is feedback only, and brief. Every button scales to .96 while held
  and, on click, springs back just past full size (`.pulse`, .96 → 1.04 → 1
  over .28s). The press stays under reduced motion: it is small and follows
  the user's own click.
- Copy: a `.secret` is a `code` beside a `Copy` button. A copy is confirmed by
  a toast, "Copied to clipboard", in the bottom-right corner for about two
  seconds; the button itself doesn't change. If the clipboard is unavailable
  (a plain-http host) or refuses, the code is selected and an error toast
  says to press Ctrl+C (⌘C on a Mac), shown longer. Toasts sit in one
  `aria-live` region; under reduced motion they fade without sliding.
