# Phase 18 — Live attachment round-trip check against the real Gmail API

*Realizes design Decision 19 (tagged live test). Depends on Phase 17.*

The mock-blindness that let Phase 14/15 ship green-but-broken gets its real
substrate: one self-contained, self-cleaning end-to-end test compiled only
under `-tags live`, per D19 — `GetProfile` → send-to-self with a generated
attachment → bounded poll until visible → resolve a D17-shaped `content_url`
through the real `AttachmentHandler` over `httptest` with the real `Client` →
assert byte-equality → deferred permanent `MessageDelete` of the fixture.
Credentials come from the environment; absence fails loudly naming the
variable (never a skip).

End state: `internal/gmail/live_test.go` (first line `//go:build live`)
containing `TestLiveAttachmentRoundTrip`; the normal suite neither compiles
nor runs it.

**Done when:** R-3NGL-AMPW is covered by `TestLiveAttachmentRoundTrip`, the
offline suite is green per design Conventions (proving the tagged file is
excluded), and `cd gmail && go test -tags live -run TestLiveAttachmentRoundTrip
./internal/gmail/` exits 0 with real credentials in the environment — the
live leg is operator-run (credentials are not assumed present in the build
loop's environment; the loop's mechanical bar is the offline suite plus the
file compiling under `-tags live` via `go vet -tags live ./internal/gmail/`).
