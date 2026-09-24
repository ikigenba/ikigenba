# design

Experiments toward one signature visual style for every Ikigenba app, service,
and page. This is not a sub-project: nothing here is spec-managed or built.
Open `index.html` in a browser; it always shows the current round.
Rejected rounds are kept under `round<N>/` for reference.

## Layout

```
design/
  index.html        the current work: links into ikigenba/
  ikigenba/         the chosen direction (theme.css, lab.js, six pages)
  status.html       status colors and pill / alert / field-error treatments
  icons.html        icon-set study (six libraries, inlined); Tabler chosen
  round1/           archived: console, blueprint, signal, with lab.js
  round2/           archived: twelve mood tiles
  round3/           archived: color study (font / top bar / palette)
  <theme>/
    theme.css       the entire style — tokens, elements, components
    specimen.html   the parts: tokens, type, controls, table, alerts, states
    app.html        dummy's panel: chrome, widgets table, add form, states
    login.html      auth's sign-in, its error state, the signed-in profile
    profile.html    (round two) account, sessions, API tokens
    landing.html    (round two) marketing: hero, features, call to action
    prose.html      (round two) long-form docs / blog / legal
```

## Rules for a theme

- Every theme renders the same pages with the same content (below), so any
  two can be compared by flipping between them.
- All styling lives in `theme.css`. Pages carry no `<style>` blocks and no
  `style=` attributes. Markup may differ between themes where the style needs
  it, but stays semantic.
- App markup stays close to what the Go services emit: `header`, `main`,
  `table`, `form`, `label` + `input` + an error `span` wired with
  `aria-describedby`. Style elements first; add a class only when an element
  alone can't say it. A service should get most of the look by linking the
  stylesheet.
- Typography is exact. Every face is the one we would ship, loaded from Google
  Fonts (a service self-hosts the same files in production), and every weight,
  style, and optical size a page uses is loaded — variable fonts with their
  full `wght`/`opsz` ranges. `font-synthesis: none` everywhere, so a missing
  weight or italic shows up as wrong instead of being faked. No system font
  ever stands in for a design face; system stacks are fallbacks only. No other
  external requests.
- Light grounds only; there is no dark mode. Colors are custom properties on
  `:root`.
- Works at 375px wide with no horizontal page scroll (tables may scroll inside
  their own container).
- Visible focus states. Error text is never conveyed by color alone.
- Every page ends with the lab toolbar script (`lab.js` in its theme folder).

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
- Signed-in profile (preview of round two): email, signed in via Google,
  last sign-in `2026-09-24 14:02 UTC`, session expires in 11h 58m, `Sign out`.

**specimen.html** — the parts: color tokens (swatch, name, value); type scale
h1–h4, body, small, mono; paragraph with a link, inline `code`, `kbd`; a code
block (a `curl` call); buttons (primary, secondary, danger, disabled); text
input, select, checkbox, input in error; a table; status badges for active /
paused / retired; alerts info / success / warning / error; a key/value list;
a card/panel; an empty state.

## Findings so far

- No metaphors: nothing that imitates a terminal, a blueprint, or any object.
- Light grounds. No dark-first themes.
- Crisp sans type (Inter, Geist, Sora were the favorites); no serifs, no
  high-contrast display faces.
- Restrained structure (round two's Nordic, Minimal, Midnight) liked.
- Round three picks: **Inter**, a **light top bar**, palettes **cobalt**,
  **azure**, and **ink**. These became `ikigenba/`.
- Of those, only **ink** survives: black as the accent, blue for links and
  focus only, a near-white ground (`#fdfdfd`).
- Status colors: the **deep** set — ok `#166534`, warn `#c2410c`, err
  `#b91c1c`, info `#1d4ed8`.
- No pale washes: status color is solid or absent, never a low-opacity fill.
  Accepted treatments (from `status.html`): pills P2 dot (`.badge`), P3 solid
  (`.badge.strong`), P6 mark (`.status`); alerts A3 neutral (`.alert`), A2
  bar (`.alert.callout`), A4 grey (`.alert.quiet`).
- Field errors: **E6, message only** — the input keeps its normal border; the
  red message with its icon beneath it carries the error.
- Icons matter as much as type: same rule as fonts — real library SVGs at
  real sizes, never redrawn stand-ins. From `icons.html` (Lucide, Tabler,
  Heroicons, Iconoir, Phosphor, Material Symbols) the pick is **Tabler**
  (`@tabler/icons` 3.48.0, MIT), outline at its native 2px stroke. The icons
  in use are vendored in `ikigenba/icons/tabler/` with the license; filled
  variants (as CSS masks) mark status, alerts, and field errors.
- Code font: **JetBrains Mono** (Google Fonts, variable 400–700), accepted.
- Dark mode: skipped. The style is light-only.
