# Phase 31 — Add the second sign-in-wall rule below the CTA

*Realizes design Decision 7 (login composition), the R-3JAM-JLZ6 slice.*

Add a second `<hr class="signin-rule">` to the `{{else}}` (logged-out) branch of
`dashboard/ui/html/index.html`, immediately after the "Sign in with Google"
anchor and immediately before the `<aside class="name-origin">` block. No new
CSS is needed — the existing `.signin-wall .signin-rule` rule and
`.signin-wall`'s flex `gap: var(--space-6)` already give this second rule the
same spacing on its outward side that the first rule already has on its
outward side.

**Done when:**
- `R-3JAM-JLZ6` appears as a tag comment in
  `dashboard/internal/server/index_test.go` on a test asserting: exactly two
  `signin-rule` elements render on the logged-out page, the second sits
  immediately after the CTA anchor and before the `name-origin` aside, and
  neither the `<hr>` markup nor the `.signin-wall .signin-rule` CSS rule
  carries a non-zero `margin`.
- `cd dashboard && go build ./... && go vet ./... && gofmt -l . && go test ./...`
  all succeed with zero failures and `gofmt -l .` prints no output.
