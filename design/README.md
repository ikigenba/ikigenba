# design

The signature visual style for every Ikigenba app, service, and page. This is
not a sub-project: nothing here is spec-managed or built. Open `index.html` in
a browser.

## Layout

```
design/
  index.html        links into ikigenba/
  ikigenba/
    theme.css       the entire style — tokens, elements, components
    lab.js          the page-switcher toolbar
    icons/tabler/   the Tabler SVGs in use, with LICENSE and VERSION
    specimen.html   the parts: tokens, type, controls, table, alerts, states
    app.html        dummy's panel: chrome, widgets table, add form, states
    login.html      auth's sign-in, its error state, the signed-in profile
    profile.html    account, sessions, API tokens
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
- Light grounds. Colors are custom properties on `:root`.
- Every page fits a 375px-wide screen; a wide table scrolls inside its own
  container.
- Visible focus states. Errors are stated in words, with an icon, as well as
  in color.
- Every page ends with the lab toolbar script (`ikigenba/lab.js`).

## Shared content

The audience is technical: these are tools for people who build and operate
systems. The product name is **Ikigenba**. The example space is
`acme.ikigenba.com`, its workspace domain `acme.dev`, the user
`ada@acme.dev`.

**app.html** — dummy's control panel.
- Chrome: product mark, service name `dummy`, the user's email, `Sign out`.
- `h1` Widgets; a table of Name / Count / Status: `alpha` 3 active, `beta` 0
  paused, `gamma` 12 retired, `delta` 128 active, `epsilon` 7 paused,
  `zeta` 1024 active.
- The Add widget form (Name, Count, Status select, `Add widget` button) shown
  mid-error: Name `alpha` → "that name is already taken", Count `-2` → "the
  count cannot be negative", Status `active` accepted.
- A states section: the table with no widgets; the message page
  ("Widget created." + `Back to widgets`).

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
paused / retired; alerts info / success / warning / error; a key/value list;
a card/panel; an empty state.

## The style

- Character comes from type, spacing, structure, and color alone.
- Light grounds.
- Type: **Inter** (variable, opsz 14–32, wght 100–900, with italic) for
  everything; **JetBrains Mono** (variable 400–700) for code.
- Palette **ink**: ground `#fdfdfd`, surface `#fff`, subtle `#f7f7f7`, fg
  `#0a0a0a`, muted `#646464`, faint `#949494`, line `#e5e5e5`, line-soft
  `#f0f0f0`. Black is the accent; blue `#0060f0` is for links and focus only.
  A light top bar.
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
- Field errors: message only. The input keeps its normal border; the red
  message with its icon beneath it, matched by `[id$="-error"]`, carries the
  error.
- Icons: **Tabler** (`@tabler/icons` 3.48.0, MIT), outline at its native 2px
  stroke — the library's own SVGs, used at their native sizes. Inline
  `<svg class="ico">` is 16px in buttons and links, 18px by default. Filled
  variants, as CSS masks in `--i-ok`, `--i-warn`, `--i-err`, `--i-info`,
  `--i-alert`, `--i-neutral`, mark status, alerts, and field errors.
