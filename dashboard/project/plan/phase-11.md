# Phase 11 — Replace the banner email link with a monogram profile avatar

*Realizes design Decision 5 (`R-XO4W-LKAI`, new; retires the old `R-DB13-MAIL`
email-text-link requirement). Touches `ui/html/index.html` (logged-in `{{if .Owner}}`
banner), `internal/server/index.go` (the `indexData` view model + index handler),
`ui/static/app.css` (a new `.avatar` rule; remove the now-dead `.identity .owner`
rule), and the landing tests in `internal/server/landing_composition_test.go`. No
route, no migration, no schema; the only server-logic change is computing the owner
initial in the view builder.*

**1. Compute the owner initial in the view builder (D5 / R-XO4W-LKAI).** In
`internal/server/index.go`, add an `OwnerInitial string` field to `indexData`
(alongside `Owner`). Inside the existing `if data.Owner != ""` block of the index
handler, set `data.OwnerInitial = ownerInitial(data.Owner)`. Add a small unexported
helper `ownerInitial(email string) string` that returns the **uppercased first rune**
of the email (use `unicode.ToUpper` over the first rune via `utf8.DecodeRuneInString`,
or equivalent), returning `"?"` as a defensive fallback when the string is empty or
begins with an invalid rune. Do not template-compute the initial — `html/template`
has no upper-case function; it must come from the view model.

**2. Render the monogram avatar in the banner (D5 / R-XO4W-LKAI).** In
`ui/html/index.html`, in the logged-in `{{if .Owner}}` branch's
`<nav class="identity">`, reorder to **sign-out first, avatar last** and replace the
email text link with the avatar:

```html
<nav class="identity">
  <form method="POST" action="/logout">
    <button type="submit" class="btn btn-danger-ghost btn-sm">Sign out</button>
  </form>
  <a href="/profile" class="avatar" aria-label="Profile — {{.Owner}}" title="{{.Owner}}">{{.OwnerInitial}}</a>
</nav>
```

The old `<a href="/profile">{{.Owner}}</a>` email text link is removed; `{{.Owner}}`
now appears only inside the avatar's `aria-label`/`title`, never as visible banner
text. The wordmark, the `connect-agent` section, and the `services` section are
unchanged.

**3. Style the avatar; drop the dead email rule (D5 / R-XO4W-LKAI).** In
`ui/static/app.css`, add an `.identity .avatar` rule: a solid **accent**-filled
(`background: var(--color-accent)`) circle (`border-radius: var(--radius-pill)`)
sized to match the sign-out button (`width`/`height: var(--control-h-sm)`, i.e. 30px)
with the initial centered (`display: inline-flex; align-items: center;
justify-content: center`), in `var(--color-accent-fg)` text using `var(--font-display)`
at `var(--text-small-size)` weight 600, `text-decoration: none`, and
`flex-shrink: 0`. Add a `:focus-visible` box-shadow ring
(`0 0 0 3px var(--color-accent-weak)`) to match the other controls' focus treatment.
**Remove** the now-unused `.identity .owner` rule (the email text link it styled is
gone). Leave all other markup and CSS untouched.

**4. Update the landing tests (D5 / R-XO4W-LKAI).** In
`internal/server/landing_composition_test.go`, rework `TestLandingOwnerEmailLinksToProfile`
(rename to reflect the avatar, e.g. `TestLandingProfileAvatarLinksToProfile`) so it
asserts, on the logged-in `GET /` body (live session via the `dashboard_session`
cookie, mirroring the existing setup):

- the banner contains an avatar link to `/profile` carrying the `avatar` class and
  the uppercased first-letter monogram — assert the substring
  `<a href="/profile" class="avatar"` is present **and** the rendered avatar text is
  the uppercased first rune of `googleidp.StubIdentity.Email`;
- the full email appears in the avatar's `aria-label`/`title` (assert
  `title="`+`googleidp.StubIdentity.Email` is present);
- the old email-**text** link form `<a href="/profile">`+email+`</a>` is **not**
  present (the email is no longer rendered as visible banner text);
- sign-out precedes the avatar in the banner (assert the index of the
  `POST /logout` form markup is less than the index of the avatar link markup).

Keep the existing logged-out assertion intact: the logged-out `GET /` still exposes
no `href="/profile"` (the avatar lives only in the `{{if .Owner}}` branch).

**Done when:** the suite is green — `cd dashboard && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), `go test ./...`, and
`bin/check-migrations dashboard` all succeed with zero failures (per design
*Conventions*) — and this id is covered:

- **R-XO4W-LKAI** — the logged-in `GET /` banner renders a monogram profile avatar:
  an `<a href="/profile" class="avatar" …>` whose text is the uppercased first
  letter of the owner's email and whose `aria-label`/`title` carries the full email;
  the email is not rendered as visible text; and the avatar is the last element of
  the `.identity` nav, following the sign-out control. *(httptest via the existing
  landing harness, live session cookie)*
