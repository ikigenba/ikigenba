# Phase 07 — init-box creates the `web` group and adds nginx to it

*Realizes design Decision 6 (`project/design/D06.md`). Unit id `R-AQMT-9M04` is
loop-driven here; the live-box id `R-ARUP-NDQT` is a real-substrate check the fake
`System` cannot falsify — operator-verified out-of-loop (partial-Decision split).
Touches `internal/opsctl/seam.go` (new `AddUserToGroup` seam method + its
`RealSystem` impl + the unit-test fake), `internal/opsctl/initbox.go`, and the
init-box test. Reuses the existing `EnsureSystemGroup` seam unchanged.*

Make init-box establish the shared front-door group and nginx's membership in it,
so that the `:web` labels applied to served trees (phases 08–10) actually grant
nginx read access. The observable end state:

- **Seam.** `System` gains `AddUserToGroup(ctx, user, group string) error`.
  `RealSystem` implements it as the box operation (`usermod -aG <group> <user>`
  or `gpasswd -a`), idempotent (re-adding a member is a no-op success). The
  unit-test fake records the call (user, group) for assertion, matching the
  existing recorded-op pattern.
- **init-box.** After `InstallPackages("nginx", "certbot")` (which creates the
  `nginx` user) and **before** the nginx enable/bring-up, `InitBox` calls
  `EnsureSystemGroup(ctx, "web")` then `AddUserToGroup(ctx, "nginx", "web")`, each
  wrapped in a loud, named error on failure. Both run on the `--skip-cert` /
  deferred path too (they are independent of the cert).
- Nothing else in init-box changes: package install, locations dir + webroot,
  apex block render, cert flow, and nginx bring-up are untouched apart from the
  two new calls slotted between install and bring-up.

Non-goals: no per-app `setup`/`deploy`/`restore` change (phases 08–10 own the
tree-labelling side), no nginx restart logic (a greenfield box gets membership
before nginx first starts; an already-running box's restart is the operator's
out-of-loop step), and no change to what nginx serves.

**Done when** the suite is green — `GOWORK=off go build ./...` succeeds and
`GOWORK=off go test ./...` passes from `opsctl/` — and this id is covered by a
clearly-named test (temp `OPSCTL_ROOT` + the fake `System`):

- **R-AQMT-9M04** — after `InitBox` (including on the `--skip-cert` path), the
  fake `System` has recorded both an `EnsureSystemGroup("web")` and an
  `AddUserToGroup("nginx", "web")`, both ordered **after** `InstallPackages` and
  **before** the nginx `EnableUnit`/`NginxReload`. The test fails against today's
  `initbox.go`, which records neither call.

Operator-verified out-of-loop (not loop-driven): **R-ARUP-NDQT** — on
`int.ikigenba.com`, `getent group web` succeeds and `id nginx` lists `web`. Its
end-to-end proof is the phase-09 live-box check (a `sites` public page serving
200), which is impossible unless the real membership is in place.
