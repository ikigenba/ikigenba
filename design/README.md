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
    banner.html     the banner: the home link, the breadcrumb trail and its
                    page menu, profile, sign out, on every kind of page, and
                    at 375px
    login.html      auth's sign-in, its error state, the signed-in profile
    profile.html    account, sessions, API tokens
    scripts.html    scripts' catalog: your scripts and their last runs
    scripts-script.html
                    one script and its runs, two levels down the trail
    scripts-run.html
                    one run: details, input, output, files, other states
    tools.html      scripts' tools page: its MCP tools, as every app's
    events.html     events' landing: its subscribers
    cron.html       cron's landing: the space's triggers
    prompts.html    prompts' catalog: your prompts and their last runs
    prompts-prompt.html
                    one prompt: its fields, subscriptions and runs
    prompts-run.html
                    one run: details, usage and cost, answer, transcript,
                    files, other states
    artifacts.html  artifacts' landing: every artifact and the bytes held
    artifacts-artifact.html
                    one artifact: its metadata in full and a download
    connections.html
                    connections' landing: your connections, one per
                    provider, and the notice a failed connect answers
    gdrive.html     gdrive's landing: your transfers between Google Drive
                    and artifacts
    gdrive-transfer.html
                    one transfer: its record in full, running and failed
    gmail.html      gmail's landing: your drafts, and your transfers
                    between Gmail and artifacts
    gmail-draft.html
                    one draft: its record and attachments, sent and
                    discarded
    gmail-transfer.html
                    one transfer: its record in full, running and failed
    scrapes.html    scrapes' landing: each scraper's current and running
                    run, then the kept runs, paged
    scrapes-run.html
                    one run: its record, its rows, paged, running
                    and failed
    scrapes-row.html
                    one row: its envelope and its raw record, two levels
                    down the trail
    candidates.html candidates' landing: the cohorts, by name, then
                    the kept passes, each list paged
    candidates-candidate.html
                    one candidate: its fields, assessment, operating
                    evidence, estimates, evidence, rulings and rejects;
                    merged and excluded
    candidates-cohort.html
                    one cohort: its definition, filters, members and
                    standing rejects; showing excluded, and empty
    candidates-pass.html
                    one pass: its record, running and failed
    webhooks.html   webhooks' landing: the space's webhooks
    home.html       home's landing: every service on the space, as tiles,
                    application services over core services
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
- Where a page sits is the banner's to say: its breadcrumb trail (see
  banner.html) names every level, root first. No page draws a `nav.crumbs`
  under `main`. The `h1` of a page below the root repeats the current level's
  name; the root page's `h1` is the root.
- Every app has an `about` page and, when it has MCP tools, a `tools` page;
  both are reached from the banner's page menu, not from a card on the
  landing page.
- A page that lists navigates (decided 2026-10-13: a person pages a list
  on the page itself rather than being told which tool to call; it replaces
  the `p#next` note that named the tool and token, with no link; decided
  2026-10-13: Prev is not deferred; a person steps back a window as they
  step forward; decided 2026-10-13: Last joins them, so the end of a list
  is one step away). The page takes the `page` token of the list tool over it
  as a query parameter on its own URL, `?page=<token>`; without it the page
  shows the first window. It shows a window of 25 entries; `limit` is not a
  page parameter. Below the list, a `nav.paging#paging` labelled `Pages`
  holds, in order:
  - First, always, so the navigation keeps one shape: a link to the page's
    own path without the parameter; on the first window it is the current
    item instead, a `span` with `aria-current="page"` and no link, as the
    breadcrumb draws its last level.
  - Prev, only on a window that is not the first: a link to the page's own
    path with `?page=<before-token>`.
  - Next, only when more entries follow: a link to the page's own path with
    `?page=<token>`, the token the tool's own `next` would carry.
  - Last, always: a link to the page's own path with `?page=last` while
    more entries follow; on the last window it is the current item instead,
    a `span` with `aria-current="page"` and no link, so a single window
    shows First and Last both as the current item.
  Prev uses the same `page` parameter. Its before-token is issued by the
  page under the list tool's name with `-before` appended (for example
  `paging.Token{Tool: "list-before"}`), over the same key fields as the
  tool's token, naming this window's first entry; the checksum keeps it
  apart from a tool token, so one parameter serves both directions. A
  before-token names the window of 25 entries that precede the named entry,
  drawn in forward order. When 25 or fewer entries precede it, the
  previous window is the first window, and the page answers a 303 redirect
  to its own path without the parameter. A window reached by a before-token
  shows First as a link, Prev when entries precede it, and Next as the
  token of its last entry, the tool's form. The template receives `Prev`,
  the before-token for this window's first entry and empty on the first
  window, beside `Page` and `Next`. The `page` value `last`, the literal
  word, decodes as no token and names the last window: the last 25 entries
  in the list's order, drawn forward. When the list holds 25 or fewer
  entries in all, the last window is the first window, and the page answers
  the same 303 redirect. On the last window `Page` is `last`, `Prev` is the
  before-token of its first entry and `Next` is empty; Last needs no field
  of its own, drawn as the current item exactly when `Next` is empty. A page with two lists (candidates'
  cohorts and passes, gmail's drafts and transfers) has one nav per list,
  `nav.paging#paging-<list>` (`#paging-cohorts`, `#paging-passes`,
  `#paging-drafts`, `#paging-transfers`), each with its own parameter
  `<list>_page` (`cohorts_page`, `passes_page`, `drafts_page`,
  `transfers_page`), its Last link `<list>_page=last`, and its own fields (`<List>Prev` beside `<List>Page`
  and `<List>Next`); each nav's hrefs, Prev's and Last's included, carry the other
  list's current `page` value, so paging one list never resets the other;
  its First link and its underflow redirect drop only its own list's
  parameter, keeping the other's. A `page` value the page cannot
  decode (a token issued for a different list included) is answered 404 with
  the app's existing not-found page, never quietly the first window; a URL
  carrying a token the page never issued names no page. An empty list shows its empty state and
  no nav. scrapes-run.html is the worked example.
- Every page ends with the feedback script (`ikigenba/feedback.js`), then the
  lab toolbar script (`ikigenba/lab.js`).

## Shared content

The audience is technical: these are tools for people who build and operate
systems. The product name is **Ikigenba**. The example space is
`acme.ikigenba.com`, its workspace domain `acme.dev`, the user
`ada@acme.dev`.

**app.html** — dummy's control panel.
- Banner: as banner.html has it, the service `dummy`; the profile links to
  `https://auth.acme.ikigenba.com/`, sign out posts to
  `https://auth.acme.ikigenba.com/logout`.
- Footer (decided 2026-10-09): the release alone, `release-7`, then the
  commit it was built from in parentheses, `(85e082b)`, in a `span`; muted
  and small, the commit fainter, the whole line at the right edge. The
  service's name is not repeated: the banner's trail names it. Banner and
  footer appear only on signed-in pages.
- `h1` Widgets; a table of Name / Count / Status: `alpha` 3 active, `beta` 0
  paused, `gamma` 12 retired, `delta` 128 active, `epsilon` 7 paused,
  `zeta` 1024 active.
- The Add widget form (Name, Count, Status select, `Add widget` button) shown
  mid-error: Name `alpha` → "that name is already taken", Count `-2` → "the
  count cannot be negative", Status `active` accepted.
- A states section: the table with no widgets; the message page
  ("Widget created." + `Back to widgets`).

**banner.html** — the banner every app page carries, in this order (decided
2026-10-09, banner navigation; it replaces the mark that held the service;
the same day the service launcher left the banner, since home's tiles are
the way to every service):
- The product mark, `a.mark`: the favicon as an image (`img` of
  `favicon.svg`, `alt=""`, 18px; the glyph is hidden when the mark holds
  one), then the text **Ikigenba**, capitalised, bold. It links to home: the
  URL of the service named `home` in the services file. With no `home` the
  mark is a `strong.mark`, not a link. The mark holds the product only.
- A hairline, then the breadcrumb trail: `nav.crumbs` labelled `Breadcrumb`
  holding an `ol`. The first item, `li.root`, is a link to the app's landing
  page `/` holding the app's own icon and its name, muted, then the chevron.
  The icon is the app's `share/icon.svg` inserted verbatim, no class, 16px.
  Each further item is one level of the page's hierarchy, a link except the
  last, the current page, which carries `aria-current="page"` and no link,
  its name in a `span`, drawn darker. The `/` separators are the style's,
  never text. The root page has no further level. A page's levels: scripts'
  script page is `scripts / <name>`, its run page `scripts / <name> / <run
  id>`; every other page below a root is one level named by its path segment
  (`tools`, `about`); a notice page (not found, unavailable) stays at the
  root.
- The chevron, `button.menu` after the service name: a Tabler
  `chevron-down` at 14px, labelled and titled `<service> pages`, the only
  trigger of the page menu, on every signed-in page, root pages included.
  It opens a native popover (`popovertarget=pages`), so no script opens or
  closes it. The service name stays a
  plain link to the landing page and never opens a menu.
- The page menu, `nav.pages#pages[popover]` labelled `<service> pages`,
  hanging under the trail's root: a `ul` of the service's pages, the landing
  page first (the app's icon and name, set off by a hairline), then `Tools`,
  only when the service has MCP tools, then `About`. The current page's row
  carries `aria-current="page"`, bolder, with a `check` icon at its right.
  The list never grows beyond these: deeper navigation starts from the
  landing page's content, never from the banner. The menu is placed with CSS
  anchor positioning; a browser without it gets a fixed offset.
- At the right edge two quiet 36px icon buttons: the profile (`a.profile`,
  a `user-circle`, labelled `Profile`, titled with the email) and sign out
  (`button.signout`, the `logout` icon, labelled and titled `Sign out`, in
  `form.inline` posting to the logout URL). No service launcher: the mark
  leads to home, and home's tiles lead to every service.
- Two breakpoints. Below 900px the product name hides and the favicon stays
  as the home link, so the trail has the room: the rule sets the mark's
  `font-size` to 0. Below 640px the trail's middle levels collapse to one
  `…` between the root and the current page. At any width the current
  level truncates with
  an ellipsis when it must; the `h1` beneath repeats it in full.
- The separator between mark and trail is the hairline; a `|` glyph was
  drawn and dropped.
- Shown on the landing page with the menu closed and open, on a two-level
  and a three-level trail, on tools (menu open) and about, with `telemetry`,
  and without home, each also in a 375px frame; the three-level trail also
  at 880px. The page's own banner opens the page menu. The lab frames a
  banner in `.frame`, which shares
  `body > header`'s rules (`:is(body, .frame) > header`); `.frame.mid` (880px)
  takes the below-900px rules, `.frame.narrow` (375px) those and the
  below-640px rules, and `.frame.tall` leaves room for a menu drawn open,
  without `popover`, under its chevron.

**scripts.html, scripts-script.html, scripts-run.html** — scripts at
`scripts.acme.ikigenba.com`, three levels deep: the catalog, one script, one
run. Banner and footer as in app.html, the service `scripts`.
- The catalog: `h1` scripts, a lede, the user's own scripts (never another
  user's) as a table of Script / Repository / Ref / Last run / When:
  `nightly-report` (exited 0), `sync-crm` (running), `rotate-keys` (failed),
  `backfill` (never run). The catalog stores a repository as its `rep_` id;
  a page resolves the name from the repository's directory when it renders,
  the id in the cell's `title`, and shows the id, muted, when the directory
  is gone (`backfill`). Its tools (`list`, `show`, `create`, `update`,
  `delete`, `run`, `runs`, `result`, `cancel`) are on tools.html. State: no
  scripts.
- One script, `nightly-report`: the trail scripts / nightly-report; a key/
  value card (id, repository, ref, created, runs kept); the runs table, newest
  first, Run / Status / Commit / Started / Duration / Exit, with one row per
  status. The repository shows its name over its id as secondary text.
  `section#subscriptions` (h2 Subscriptions) sits between the card and the
  runs: `ul#subscription-list`, one `li[data-event=<name>] > code` per
  subscribed event, sorted by name. States: no subscriptions
  (`div#no-subscriptions.empty`, "This script is subscribed to no events."),
  no runs.
- One run: the trail scripts / nightly-report / `run_3f9a1c2e8b7d4a60`; a
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
event bus. Banner and footer as in app.html, the service `events`.
- `h1` events, a lede, the subscribers as a table of Service / Status /
  Cursor / Lag / Since: `scripts` ok and caught up, `sites` paused, showing,
  muted, the event it is stuck on and the error it answered, and a link to
  the about screen. Its tools (`catalog`, `search`, `subscribers`, `skip`,
  `resume`) are on its tools page. State: no subscribers.
- Subscriber status reads as a `.status` word carrying two attributes:
  `data-status` is the real status (`ok`, `paused` or `gone`) and `data-kind`
  is the theme kind it shows as: ok is ok, paused is warn, gone is info.

**cron.html** — cron at `cron.acme.ikigenba.com`, triggers that emit events
on the suite's event bus on a schedule. Banner and footer as in app.html, the
service `cron`.
- `h1` cron, a lede, every trigger in the space, whoever owns it, sorted by
  slug, as a table of ID / Slug / When / Owner / Status / Last fired / Next:
  `month_end` (`@monthly`, `grace@acme.dev`, active, never fired, next
  `2026-11-01 00:00`), `weekly_digest` (`0 8 * * 1`, the user's own, marked
  with the `yours` badge, paused, last fired `2026-09-28 08:00`, no next),
  and a link to the about screen. Its tools (`list`, `show`, `create`,
  `update`, `pause`, `resume`, `delete`) are on its tools page. State: no
  triggers (`div#no-triggers.empty`).
- Trigger status reads as a `.status` word whose `data-status` is the status:
  active is ok, paused is warn. Times are UTC to the minute in a `time`
  carrying the RFC 3339 moment; a cell with no time is empty.

**artifacts.html, artifacts-artifact.html** — artifacts at
`artifacts.acme.ikigenba.com`, binary files agents and services share, kept
by path: an agent puts and gets the bytes at `/files/<path>` with curl, and
the MCP tools carry metadata only. Two levels: the landing page and one
artifact. Banner and footer as in app.html, the service `artifacts` with the
Tabler `archive` icon. Every page is for a signed-in user; no form but sign
out, no delete control.
- The landing: `h1` artifacts, a lede, `section#artifacts` whose header
  carries `p#total`, the bytes held across every artifact, then every
  artifact in the space, whoever owns it, sorted by path, as
  `table#artifact-list` of Path / Bytes / Content type / Owner / Modified /
  Expires: `datasets/customers.csv` (`grace@acme.dev`, never expires),
  `reports/2026-10/digest.pdf` (the user's, expires `2026-11-08 07:01`),
  `screenshots/home.png`; each path links to the artifact's page `/<id>`;
  and a link to the about screen. Its tools (`list`, `show`, `delete`,
  `import`) are on its tools page. State: no artifacts
  (`div#no-artifacts.empty`).
- One artifact, `reports/2026-10/digest.pdf`: the trail artifacts /
  `reports/2026-10/digest.pdf`, the path as the level's name and the `h1`;
  `section#artifact-card`, its header naming the content type and the bytes
  with a Download button (`a#download`, to the artifact's `/files/<path>`
  URL), and `dl#artifact.kv`: ID, Path, URL, Bytes, Content type, SHA-256,
  Owner (an email), Created, Modified, Expires. State: never expires (the
  Expires value is the muted word `never`). An unknown id is the not-found
  page.
- Bytes are the plain integer, never a humanised size, in rows, the total
  and the card. Times are UTC in a `time` carrying the RFC 3339 moment: to
  the minute in the table, where a cell with no time is empty, to the second
  with `UTC` on the artifact page.

**connections.html** — connections at `connections.acme.ikigenba.com`, an
OAuth broker: a person connects an account at an outside provider once, the
broker keeps the grant, and the suite's connectors on the host get
short-lived tokens from it; no tool returns a credential. One level: the
landing page, plus the notice a refused or failed connect answers. Banner and
footer as in app.html, the service `connections` with the Tabler
`plug-connected` icon. Every page is for a signed-in user and shows only the
user's own connections.
- The landing: `h1` connections, a lede, `section#connections` whose header
  says the connections are the user's alone, then `table#connection-list`
  of Provider / Status / Account / Scopes / Connected and an actions column,
  one row per provider the broker knows, `tr[data-provider=<name>]`: Google,
  connected as `ada@acme.dev` with its five scopes, each a `code`, since
  `2026-10-11 14:22`, with `Reconnect` and `Disconnect`; and a link to the
  about screen. Its tools (`list`, `connect`, `disconnect`) are on its tools
  page. States: not connected (no account, scopes or time; `Connect`),
  broken (the muted words "needs reconnecting" after the status;
  `Reconnect` and `Disconnect`).
- Each control is a `form.inline` posting to `/<provider>/connect` or
  `/<provider>/disconnect` with a small button in `td.row-actions`, as the
  profile's token rows: `Connect` secondary, `Reconnect` and `Disconnect`
  ghost.
- Connection status reads as a `.status` word carrying `data-status`, the
  list tool's status, and `data-kind`: connected is ok, broken is warn, not
  connected has no kind.
- The notice page stays at the root: `h1` Not connected, `div#notice.alert`
  of kind err naming the provider and the failure in words (here the
  request expired), a line that the connections are unchanged, and
  `a#back` to the landing. It never shows a code, token or state value.
- Times are UTC to the minute in a `time` carrying the RFC 3339 moment; a
  cell with no time is empty.

**gdrive.html, gdrive-transfer.html** — gdrive at `gdrive.acme.ikigenba.com`,
the Google Drive connector: `pull` copies a Drive file into an artifact and
`push` copies an artifact into Drive, each recording a transfer that moves the
bytes on the host; no tool carries bytes or a credential. Two levels: the
landing page and one transfer. Banner and footer as in app.html, the service
`gdrive` with the Tabler `brand-google-drive` icon (added to `icons/tabler/`
on 2026-10-11 for this app). Every page is for a signed-in user and shows only
the user's own transfers; no form but sign out.
- The landing: `h1` gdrive, a lede, `section#transfers` whose header says the
  transfers are the user's alone, newest first, then `table#transfer-list` of
  Transfer / Direction / Status / Drive file / Artifact / Bytes / Started /
  Finished, one row per transfer, `tr[data-transfer=<id>]`: a push running,
  a pull queued, a pull of a Google Doc done, a pull failed `reconnect`, a
  push failed `interrupted`, a push done. The id links to the transfer's page
  `/<id>`; the Drive file's name links to its Drive `link` when the record
  has one; the artifact path links to the artifact's page on artifacts, by
  the artifact id the record holds, else to its `/files/<path>` URL, else is
  plain. A failed row's reason follows its status, muted. A link to the
  about screen. Its tools (`drives`, `list`, `search`, `show`, `pull`,
  `push`, `transfers`, `transfer`) are on its tools page. State: no
  transfers (`div#no-transfers.empty`).
- One transfer, `xfr_e5a07c3d81f94b26`, a pull of the Google Doc "Q3 report"
  exported to `.docx`: the trail gdrive / the id, the id as the level's name
  and the `h1`; `p#headline` with the status and the started and finished
  times; `section#transfer-card`, its header saying which way the bytes
  went, and `dl#transfer.kv`: ID, Direction, Status, Reason (failed only),
  Drive file, Name, Kind, Path, Content type, Format (exported only), Bytes,
  Started, Finished (once finished), URL (done only), Link (when held).
  States: queued or running (`div#running.alert.quiet`, "Queued" or
  "Running", and a Reload button), failed (`div#failure.alert` of kind err,
  "The transfer failed", the reason's sentence chosen from the reason word,
  and a line that nothing was changed). An unknown id, a pruned one or
  another user's is the not-found page.
- Transfer status reads as a `.status` word, as stored, carrying
  `data-status` and `data-kind`: queued and running are info, done is ok,
  failed is err. Direction, kind and reason words are shown as stored.
- Bytes are the plain integer, the count moved so far. Times are UTC in a
  `time` carrying the RFC 3339 moment: to the minute in the table, where a
  cell with no time is empty, to the second with `UTC` on the transfer page.

**gmail.html, gmail-draft.html, gmail-transfer.html** — gmail at
`gmail.acme.ikigenba.com`, the Gmail connector: its tools search, read and
show messages and threads, `compose` a draft (optionally a reply), `send` or
`discard` it; `pull` copies an attachment, or the whole message as `.eml`,
into an artifact and `push` attaches an artifact to a draft, each recording a
transfer that moves the bytes on the host. Mail text reaches the agent through
tool answers; no tool carries attachment bytes, a raw message or a credential,
and nothing marks, labels, archives or trashes mail. Two levels: the landing
page, and one draft at `/drafts/<id>` or one transfer at `/transfers/<id>`.
Banner and footer as in app.html, the service `gmail` with the Tabler
`brand-gmail` icon (added to `icons/tabler/` on 2026-10-12 for this app).
Every page is for a signed-in user and shows only the user's own drafts and
transfers; no form but sign out.
- The landing: `h1` gmail, a lede, then two sections, each with a header
  saying its rows are the user's alone, newest first. `section#drafts` holds
  `table#draft-list` of Draft / Status / Subject / To / Attachments / Created
  / Sent, a window of 25 with `nav.paging#paging-drafts` below it
  (parameter `drafts_page`), `tr[data-draft=<id>]`: one open with two
  attachments, one sent, one discarded. The id links to the draft's page
  `/drafts/<id>`; the subject links to the draft in Gmail by the record's
  `link` when it has one (an open draft's `#drafts/<message id>`, a sent
  one's `#all/<message id>`, none once discarded); an empty subject reads
  `(no subject)`, muted. `section#transfers` holds `table#transfer-list` of
  Transfer / Direction / Status / Name / Artifact / Bytes / Started /
  Finished, a window of 25 with `nav.paging#paging-transfers` below it
  (parameter `transfers_page`), `tr[data-transfer=<id>]`: a push running,
  a pull of an attachment done, a whole-message pull queued, a pull failed
  `too_large`, a push done. The id links to `/transfers/<id>`; the name links
  to the Gmail `link` when the record has one; the artifact path links as in
  gdrive. A failed row's reason follows its status, muted. A link to the
  about screen. Its tools (`search`, `thread`, `show`, `labels`, `pull`,
  `compose`, `push`, `send`, `discard`, `drafts`, `draft`, `transfers`,
  `transfer`) are on its tools page. States: no drafts
  (`div#no-drafts.empty`), no transfers (`div#no-transfers.empty`), a draft
  with no subject.
- One draft, `drf_4c1e8a7b30f95d62`, an open reply with two attachments: the
  trail gmail / the id, the id as the level's name and the `h1`;
  `p#headline` with the status and the created, modified and sent times;
  `section#draft-card`, its header saying what the status means and that
  the text stays in Gmail, and `dl#draft.kv`: ID, Status, Gmail draft (open
  only), Message, Thread, To, Cc (when set), Bcc (when set), Subject, In
  reply to (a reply only), Link (when held), Created, Modified, Sent (once
  sent); then `section#attachments` with `table#attachment-list` of Artifact
  / Name / Content type / Bytes, the path linking to artifacts as on the
  landing. State: no attachments (`div#no-attachments.empty`). The draft's
  body text is never shown. An unknown id, a pruned one or another user's is
  the not-found page.
- One transfer, `xfr_b37e0a1d9c64f258`, a pull of attachment part `2` of a
  message: as gdrive's transfer page, with the mail references in place of
  the Drive file: ID, Direction, Status, Reason (failed only), Message,
  Attachment (a part pulled only), Draft (a push only, linking to
  `/drafts/<id>`), Name, Path, Content type, Bytes, Started, Finished (once
  finished), URL (done only), Link (when held). Running and failed states as
  gdrive's, the reason's sentence in Gmail's terms. Not-found as for a draft.
- Draft status reads as a `.status` word, as stored, carrying `data-status`
  and `data-kind`: open is info, sent is ok, discarded has no kind.
  Transfer status, direction and reason words, bytes and times as in gdrive;
  draft times are to the minute in the table and to the second with `UTC` on
  the draft page.

**scrapes.html, scrapes-run.html, scrapes-row.html** — scrapes at
`scrapes.acme.ikigenba.com`, the funnel's evidence store: each scraper walks
one public source and keeps every record it answers as a row of a run, the
raw record beside a common envelope (name, names, address, people, facts).
`scrape` starts a run, as does a fire of the cron trigger `scrape_<scraper>`;
the run lands in the background. Runs belong to no one: every signed-in user
sees every run and row. Three levels: the landing page, one run at
`/runs/<id>`, and one row at `/runs/<id>/rows/<source_id>`. Banner and
footer as in app.html, the service `scrapes` with the Tabler
`database-import` icon (added to `icons/tabler/` on 2026-10-12 for this
app). No form but sign out. The sample data is FMCSA's motor carrier census
for Minnesota and Wisconsin, with made-up carriers.
- The landing: `h1` scrapes, a lede, then `section#scrapers` with
  `table#scraper-list` of Scraper / Current / Status / Started / Finished /
  Rows / New / Changed / Gone / Running, one row per registered scraper,
  `tr[data-scraper=<name>]`: `fmcsa` with its current run, the latest done,
  and a run in progress in Running. Then `section#runs` with
  `table#run-list` of Run / Scraper / Status / Started by / Started /
  Finished / Rows, the kept runs newest first, a window of 25 with
  `nav.paging#paging` below it, `tr[data-run=<id>]`: one
  running, one failed `source`, two done by the trigger. Every run id links
  to `/runs/<id>`; Started by is the caller's email, or `cron` and the
  trigger's slug. A failed row's reason follows its status, muted. A link to
  the about screen. Its tools (`scrapers`, `scrape`, `runs`, `run`, `rows`,
  `row`) are on its tools page. States: a scraper with no run yet (its run
  cells empty), no runs (`div#no-runs.empty`).
- One run, `run_7c2e9f41a0b3d856`, done by the trigger: the trail scrapes /
  the id, the id as the level's name and the `h1`; `p#headline` with the
  status and the started and finished times; `section#run-card`, its header
  saying what new, changed, unchanged and gone count against, and
  `dl#run.kv`: ID, Scraper, Status, Reason (failed only), Started by,
  Started, Finished (once finished), Rows, New, Changed, Unchanged, Gone.
  When done, `section#rows` with `table#row-list` of Source ID / Name / City
  / State / County, the first page in source id order,
  `tr[data-row=<source_id>]`, each id linking to its row's page; then
  `nav.paging#paging`, First as the current item and, since more follow,
  Next linking to `/runs/<id>?page=6yGAPAqiIKZP1wGaUvHJ`, the token `rows`
  would hand back as `next`, and Last linking to `/runs/<id>?page=last`. States: a later window (First a link back to
  `/runs/<id>`, Prev a link to `/runs/<id>?page=<before-token>`, no Next, Last the
  current item),
  running
  (`div#running.alert.quiet`, "Running", a Reload button, no rows), failed (`div#failure.alert` of kind err, "The run
  failed", the reason's sentence chosen from `source`, `interrupted` or
  `store`, and a line that a failed run keeps no rows), done with no rows
  (`div#no-rows.empty`). An unknown id or a pruned one is the not-found
  page.
- One row, `1023916` of that run, a carrier at Sandstone: the trail scrapes
  / the run id / the source id, the source id as the `h1`; `p#headline` with
  the name, the scraper and when it was collected; `section#row-card` with
  `dl#row.kv`: Source ID, Scraper, Run (linking to the run), Collected,
  Digest, Name, Names, then each when set Street, City, State, Zip, County,
  Latitude, Longitude, People (one `div[data-person]` each, the role muted)
  and Facts (`dl#facts.kv`, `dt[data-fact=<key>]`, each value as JSON); then
  `section#raw-card` with `pre#raw`, the raw record as formatted JSON. An
  unknown run or row, or a pruned run, is the not-found page.
- Run status reads as a `.status` word, as stored, carrying `data-status`
  and `data-kind`: running is info, done is ok, failed is err. Reason words,
  scraper names, source ids and county codes are shown as stored; counts are
  the plain integer. Times are UTC in a `time` carrying the RFC 3339 moment:
  to the minute on the landing, where a cell with no time is empty, to the
  second with `UTC` on the run and row pages.

**candidates.html, candidates-candidate.html, candidates-pass.html** —
candidates at `candidates.acme.ikigenba.com`, the funnel's judgment over
scrapes' evidence: a pass resolves each scrapes run's records into
candidate businesses, one per business with every record that describes it
attached as a link, and a judge makes two separate judgments of each,
whether it is a real business operating today and how big it is; the owner
reads them through cohorts, saved searches of filters, an order, a size and
rejects whose members are worked out at each read, and rules on candidates
with `decide`, `exclude` and `include`. Candidates and cohorts belong to no
one: every signed-in user sees every candidate, cohort, pass, ruling and
reject. Four pages: the landing page, one candidate at
`/candidates/<id>`, one cohort at `/cohorts/<id>`, and one pass at
`/passes/<id>`. Banner and footer as
in app.html, the service `candidates` with the Tabler `filter` icon (added
to `icons/tabler/` on 2026-10-12 for this app). No form but sign out. The
sample data is made-up carriers from FMCSA's census for Minnesota and
Wisconsin.
- The landing: `h1` candidates, a lede, then `section#cohorts` whose
  header says a cohort is a saved search whose members are worked out each
  time its page is read, with `table#cohort-list` of Name / Cohort / Size /
  Created by / Changed, by name then id, a window of 25 with
  `nav.paging#paging-cohorts` below it (parameter `cohorts_page`),
  `tr[data-cohort=<id>]`, each name linking to `/cohorts/<id>`; Created by
  is the email, or the user id in a `code` when none. No phone or email of
  a candidate shows on this page. Then `section#passes` with `table#pass-list` of Pass / Kind /
  Trigger / Status / Started / Finished / Rows / Created / Judged, the kept
  passes newest first, a window of 25 with `nav.paging#paging-passes` below
  it (parameter `passes_page`), `tr[data-pass=<id>]`: a running judge from a
  rebuild, a judge failed `scrapes`, a judge started by the cron trigger
  `judge_candidates`, two ingests done by a scrapes run. Every pass id links
  to `/passes/<id>`; Kind is `ingest` or `judge`; Trigger is the run id, the
  cron trigger's slug or `rebuild`. A failed row's reason follows its
  status, muted. A link to the about screen. Its tools (`cohorts`,
  `cohort`, `candidates`, `candidate`, `passes`, `pass`, `rebuild`,
  `enrich`, `decide`, `exclude`, `include`, `create_cohort`,
  `edit_cohort`, `delete_cohort`, `reject`, `unreject`, sixteen in all) are
  on its tools page. States: no
  cohorts (`div#no-cohorts.empty`), no passes (`div#no-passes.empty`).
- One candidate, `cand_4b9e1f07c2d83a65`, a for-hire carrier at Hudson, WI,
  with two records, one moved onto it by a merge: the trail candidates /
  the id, the id as the `h1`; `p#headline` with the name, the status and
  when it was updated, plus a `.badge` excluded when excluded;
  `section#candidate-card` with `dl#candidate.kv`: ID, Status, Merged into
  (merged only), then each when set Name, Names, Street, City, State, Zip,
  County, Phone, Email, Website (the state, `https`, `http` or `none`, with the URL it
  landed on as an `a.website-link`, its title in `div[data-website-title]`
  and a muted "checked" time; muted `unknown` when it has no result), People (one `div[data-person]` each), Class and
  Cargo (one `code[data-cargo]` per tag), then Created and Updated.
  `section#assessment-card` with `dl#assessment.kv`, the two judgments and
  the facts beside them as the last judge calculated them: Operating
  confidence, Revenue low, Revenue high, Revenue confidence, Categories
  (one `div[data-category]` each, its source muted), Known since, Newest
  filing, Government match, Fleet (the verdict `pass`, `fail` or, muted,
  `unknown`), Trucks and Drivers (muted "unknown" when FMCSA gives none),
  Drive minutes, Excluded (yes or no), Rules, Calculated. `section#operating` (Operating evidence) with
  `table#evidence-list` of Evidence / Source / Weight / Description,
  `tr[data-evidence=<name>]`. `section#estimates` with one
  `dl.kv[data-estimate=<source>]` per source, its facts in a nested
  `dl.kv` with `dt[data-fact]`, each value as JSON. `section#links`
  (Evidence) with `table#link-list` of Scraper / Source ID / Name / City /
  Zip / Reason / Confidence / Filed / First seen / Last seen / Gone,
  `tr[data-link=<scraper>:<source_id>]`, each source id linking to its row
  at scrapes, `/runs/<run>/rows/<source_id>`. `section#rulings` with
  `table#ruling-list` of Kind / With / Records / By / When / Why, newest
  first, `tr[data-ruling=<kind>]`. `section#enrichments` (Enrichments), on
  an active candidate only, with `table#enrichment-list` of Kind / State /
  Landed / Title / Status / Bytes / Error / Domain / When, newest first,
  `tr[data-enrichment=<kind>][data-state=<state>]`, Landed an
  `a.website-link` when a URL was reached. `section#rejects` with
  `table#reject-list` of Kind / Cohort / By / When / Why, every reject and
  unreject naming the candidate, newest first,
  `tr[data-reject=<kind>][data-cohort=<id>]`, the cohort's name linking to
  `/cohorts/<id>` beside its id, or its id and a muted "deleted" when the
  cohort is gone. States: merged (`div#merged.alert.quiet`
  of kind info, "Merged", linking to the survivor; the record and the
  rulings and rejects only), a gone record (`.badge` gone of kind warn) and a split's
  moved records, excluded (`div#excluded.alert.quiet` of kind warn; the
  facts unchanged, Excluded yes, here with an unknown fleet verdict and
  counts), a ZIP not in the table, no high estimate
  and no filing (muted words), no evidence, no estimate and no categories
  (muted words, and `div#no-operating-evidence.empty`), not judged
  (`div#no-assessment.empty`), no estimates (`div#no-estimates.empty`), no
  rulings (`div#no-rulings.empty`), no enrichments
  (`div#no-enrichments.empty`), no rejects (`div#no-rejects.empty`).
- One cohort, `coh_d05a3e86b4c17f29`, "Trucking and construction, $5M to
  $10M, 2 hours": the trail candidates / the id, the name as the `h1`;
  `p#headline` with `#matching`, a `.badge` of kind info counting the
  matching candidates, and when it was changed. `section#cohort-card` with
  `dl#cohort.kv`: ID, Name, Size, Order (one `div[data-order]` per field,
  its direction muted), Created by, Created, Changed by, Changed.
  `section#filters` with `dl#filters.kv`: Revenue low, Drive minutes,
  Categories, Fleet, Website, Government, Filing age, Class and Excluded, each muted
  "any" when not carried, and a `.badge[data-unknown]` "unknown included"
  of kind info when its switch is set (Fleet and Website each have one;
  Website's states are `code[data-website]`, its switch `website_unknown`).
  `section#members` with
  `table#member-list`, the landing's old ranked row (Rank / Name / City /
  State / Operating confidence / Low / High / Revenue confidence /
  Categories / Class / Fleet / Minutes / Government) plus Website, Known
  since and Newest filing, `tr[data-candidate=<id>]`, each name linking to
  `/candidates/<id>`; Fleet is the verdict, muted when `unknown`, carried
  in `data-fleet`; Website is the state, an `a.website-link` to the URL it
  landed on when there is one, muted when `unknown`, carried in
  `data-website`. `section#rejects` with `table#reject-list` of Candidate /
  By / When / Why, the standing rejects newest first,
  `tr[data-reject=<cand id>]`. No form: cohorts change through the tools.
  States: a cohort showing excluded candidates (a `.badge` excluded of kind
  warn after the name), no members (`div#no-members.empty`), no rejects
  (`div#no-rejects.empty`). An unknown or malformed id is the not-found
  page. An unknown id is the not-found page.
- One pass, `pass_9a27f6c1e3b40d58`, an ingest done over a scrapes run: the
  trail candidates / the id, the id as the `h1`; `p#headline` with the
  status and the started and finished times; `section#pass-card` with
  `dl#pass.kv`: ID, Kind, Trigger (the run id linking to its page at
  scrapes, the cron trigger's slug or `rebuild`), Status, Reason (failed
  only), Started by (scrapes, `cron` and the trigger's slug, or the
  caller's email for a rebuild), Started, Finished (once finished), Rows,
  New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged; an
  enrich pass shows five counts in their place, Due, Fetched, `https`,
  `http` and `none` (`#pass-due`, `#pass-fetched`, `#pass-https`,
  `#pass-http`, `#pass-none`). States:
  running (`div#running.alert.quiet`, "Running", a Reload button), failed
  (`div#failure.alert` of kind err, "The pass failed", the reason's
  sentence chosen from `scrapes`, `interrupted` or `store`, and a line that
  what it wrote stays), a judge started by cron. An unknown id or a pruned
  one is the not-found page.
- The about page's facts paragraph lists the website (https, http, none or
  unknown) among the facts kept beside the judgments, and a paragraph
  describes the enrich pass: it follows every ingest that ends done, or
  starts on a call of `enrich`, fetches each own-domain candidate's domain
  once and keeps the result, and a judge pass follows when it wrote one.
- Candidate status reads as a `.status` word, as stored: active is ok,
  merged is info; pass status as scrapes' run status: running info, done
  ok, failed err. Class tags, cargo tags, category words, evidence kinds,
  kinds, ruling and reject kinds, fleet verdicts, ids and county codes are
  shown as stored. Dollars
  are whole with a `$` and thousands commas; confidences and weights have
  two decimals; counts and minutes are the plain integer. Dates are
  `YYYY-MM-DD` in a `time`. Times are UTC in a `time` carrying the RFC 3339
  moment: to the minute on the landing and in tables, to the second with
  `UTC` in the candidate's and the pass's records.

**webhooks.html** — webhooks at `webhooks.acme.ikigenba.com`, webhooks that
turn an authenticated delivery from outside into an event on the suite's
event bus. Banner and footer as in app.html, the service `webhooks` (Tabler
`webhook` icon).
- `h1` webhooks, a lede, every webhook in the space, whoever owns it, sorted
  by slug, as a table of ID / Slug / Scheme / URL / Owner / Last received:
  `gh_push` (`github-hmac`, the user's own, marked with the `yours` badge,
  last received `2026-10-09 14:12`), `n8n_invoice` (`bearer`,
  `grace@acme.dev`, never received), and a link to the about screen. Its
  tools (`create`, `list`, `show`, `rotate`, `delete`, `delivery`) are on its
  tools page. No per-webhook or per-delivery page. State: no webhooks
  (`div#no-webhooks.empty`).
- The scheme and the URL are `code`; the URL is the address a sender posts
  to, `/in/<slug>` on webhooks' own host. Times are UTC to the minute in a
  `time` carrying the RFC 3339 moment; a cell with no time is empty.

**tools.html** — scripts' tools page at `/tools`, the example for every app
whose manifest sets `mcp = true`. Banner and footer as in scripts.html, the
trail scripts / tools, the menu's `Tools` row current.
- `h1` Tools; a lede naming the MCP gateway's `call` and `mutate`; the tools
  as `dl#tools.kv` in a card, each `dt[data-tool=<name>] > code` the name and
  its `dd` the description. Each app's tools page carries the words its
  landing page's MCP tools card carried; the landing page no longer has the
  card.

**home.html** — home at `home.acme.ikigenba.com`, the default app, so it also
answers at `acme.ikigenba.com`: the suite's front door, where the product mark
leads. Banner and footer as in app.html, the service `home` with the Tabler
`grid-dots` icon (decided 2026-10-09: the launcher's grid, now home's own);
its menu holds home and About, no Tools.
- `h1` Services, no lede, then the services of the space in two groups
  (decided 2026-10-09), each a `main > section` with a `.section-head` `h2`
  and no subtitle over a `nav.services` grid of tiles, each the service's
  icon over its name linking to its URL, in the services file's order,
  entries with an icon only, no search field: `Application Services`
  (`section#application-services`), today only `dummy`; then `Core
  Services` (`section#core-services`), every other service. `home` itself
  draws no tile: the banner's mark is the way to it, so no tile is ever
  current. `repos` is disabled, faint and unlinked.
- State: no services (`div#no-services.empty`).

**prompts.html, prompts-prompt.html, prompts-run.html** — prompts at
`prompts.acme.ikigenba.com`, prompts run by an agent over the suite's models,
three levels deep like scripts: the catalog, one prompt, one run. Banner and
footer as in app.html, the service `prompts` with the Tabler `prompt` glyph.
Where a part is the same as scripts' it keeps scripts'
hooks, words and states; what differs is listed here.
- The catalog: `h1` prompts, a lede, the user's own prompts as
  `table#prompt-list` of Prompt / Model / Last run / Cost / When:
  `daily-digest` (`claude-sonnet-5`, exited 0, its cost), `triage-inbox`
  (running, no cost yet), `summarize-crm` (exited 2), `echo` (never run), and
  a link to the about screen. Its tools (`list`, `show`, `create`, `update`,
  `delete`, `subscribe`, `unsubscribe`, `run`, `runs`, `result`, `cancel`)
  are on its tools page. State: no prompts (`div#no-prompts.empty`).
- One prompt, `daily-digest`: the trail prompts / daily-digest; a line
  naming the model and the tool groups; `section#prompt-card`, a key/value
  card (id, model, tools as `span#tool-groups` of one `code[data-group]` per
  group, created, runs kept); then `section#text` (the prompt text),
  `section#system` (the system prompt) and `section#schema` (the output
  schema), each a `pre`; then subscriptions as scripts'; then the runs table,
  newest first, Run / Status / Model / Started / Duration / Cost / Exit, with
  one row per status, queued included. States: no tools (`span#no-tools`,
  "none"), no system prompt (`div#no-system.empty`), no schema
  (`div#no-schema.empty`), no subscriptions, no runs.
- One run: the trail prompts / daily-digest / `prr_3f9a1c2e8b7d4a60`; a
  headline of status, start time, duration and cost; the run card (status,
  prompt, model, started, finished, duration, trigger, user, request, usage,
  cost, output sizes). The usage line, `dd#usage`, reads calls, tool calls,
  input tokens with the cached ones in parentheses, output tokens and
  reasoning tokens; the cost, `dd#cost`. Then, in this order, each with a
  Download button: Answer (`section#stdout`, the `stdout` file, here the JSON
  object the schema asked for), Standard error, Input (`input.json`, here the
  delivered event), Transcript (`section#transcript`, `transcript.jsonl`, a
  size and a download, not rendered), and Files, the `work/` folder as
  scripts' table. States: still running (reload; usage is counted when the
  run ends), timed out with output truncated and the usage it spent, a
  provider failure (exited 1, the category in `stderr`), failed to start
  (never left the queue), started by the `run` tool, files gone.
- Run status reads as scripts' `.status` word, queued and running info.
  Cost is US dollars to six decimal places (`$0.023714`), from the run's
  nano-dollar count; a run that has not ended shows no cost.

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
- Breadcrumbs live in the banner only: `nav.crumbs > ol` after the mark's
  hairline, 15px and muted, levels separated by a faint `/`, the current page
  in the foreground color, weight 500. The trail never wraps: below 640px
  its middle levels collapse to `/ … /`, and the current level truncates
  with an ellipsis.
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
