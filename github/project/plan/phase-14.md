# Phase 14 — The loopback `GET /token` twin

*Realizes design Decision 10 (installation tokens for repos' git plumbing).
Depends on Phase 13.*

An exported `Client.Token(ctx) (string, time.Time, error)` accessor
delegating to the existing `tokenSource` cache (surfacing the expiry
`mintTokenLocked` already tracks); `TokenHandler()` in
`internal/gh/token_route.go` returning `{"token", "expires_at"}` JSON,
registered in the composition root as
`rt.HandleLoopback("GET /token", client.TokenHandler())` beside `GET /pr`;
a `location = /srv/github/token { return 404; }` block added to
`etc/nginx.conf`; and the never-logged guarantee proven with a capturing
slog handler. Offline harness (stubbed mint flow, assembled router over
`httptest`).

**Done when:** R-GSI7-P8NI, R-GTQ4-30E7, R-GUY0-GS4W, and R-GW5W-UJVL are
each covered by a clearly-named test, and the suite is green per design
Conventions (`GOWORK=off go build ./...` and `GOWORK=off go test ./...`
clean with no SKIP, `gofmt -l .` empty, `go vet ./...` clean, from
`github/`).
