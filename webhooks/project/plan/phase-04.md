# Phase 4 — Public ingress endpoint (`/in/<name>`)

*Realizes design Decision 4 (Public ingress endpoint). Depends on Phase 2 (secret verify) and Phase 3 (Record).*

Build the one unauthenticated-by-OAuth side-effecting surface:
`internal/webhooks/ingress.go` exporting `NewIngressHandler(svc *Service, log
*slog.Logger) http.Handler`, plus the `maxBodyBytes = 1 << 20` constant. It is
mounted **bare** (never wrapped in `RequireIdentity`) and self-guards through the
strict pipeline of design D4:

1. method ≠ `POST` → `405` (name-agnostic);
2. presence of `X-Owner-Email` **or** `X-Client-Id` → `404` (front-door leak);
   `X-Forwarded-Proto` is **not** rejected;
3. resolve `name` via `TrimPrefix(path, "/in/")`; empty → `404`;
4. authenticate `Authorization: Bearer <secret>` against `Store.GetByName` +
   `verifySecret` — **any** failure (missing/empty/malformed header, unknown name,
   wrong secret) returns the **byte-identical** `404`; authentication completes
   before the body is read;
5. read the body under `http.MaxBytesReader` → over cap → `413`;
6. `svc.Record(...)` (Phase 3) → `202` with body `{"status":"accepted"}`.

End state: `cd webhooks && go build ./... && go vet ./... && go test ./...` green,
with the handler exercised via `httptest` over a real-SQLite `Service` + real
outbox.

**Done when:** design D4's Verification ids are each covered by a genuine
`httptest`+real-DB test and the suite is green —
- R-7ISQ-ZZCF — `POST /in/<name>` with the correct bearer and an in-cap body →
  `202` and body `{"status":"accepted"}`;
- R-7K0N-DR34 — wrong secret, unknown name, and missing `Authorization` produce
  **byte-identical** `404`s and none appends an outbox row;
- R-7L8J-RITT — a correct-secret request also carrying `X-Owner-Email` (or
  `X-Client-Id`) → `404` with no row, while the same request carrying only
  `X-Forwarded-Proto` → `202`;
- R-7MGG-5AKI — with a correct secret, `maxBodyBytes + 1` bytes → `413` with no
  row, exactly `maxBodyBytes` → `202`;
- R-7NOC-J2B7 — a non-`POST` (e.g. `GET`) → `405` whether or not `<name>` exists.
