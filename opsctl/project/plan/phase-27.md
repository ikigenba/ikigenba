# Phase 27 — `opsctl seed-state`: bootstrap a rotating credential into `state/`

*Realizes design Decision 19 (the `seed-state` verb), which realizes the
umbrella's rotating-credential contract ids R-EK9S-F7RY, R-ELHO-SZIN
(`root project/design/D32.md`, `[proof: opsctl]`).*

Build the `opsctl seed-state <svc> <NAME> [--force]` verb (D19): read the
credential value from **stdin** (never argv; trailing newline trimmed, interior
bytes verbatim), compose the target `${IKIGENBA_ROOT}/<svc>/state/<NAME>` from
the box-global `IKIGENBA_ROOT` opsctl already loads (D3), and write it mode
`0600` owned by the service user, atomically (temp file in the same dir, then
rename). Refuse and change nothing (non-zero exit) when the target already
exists and `--force` is absent; `--force` replaces it. Fail loudly (non-zero, no
write), naming the service and credential, on: a pre-existing file without
`--force`, an unknown `<svc>` (no `${IKIGENBA_ROOT}/<svc>/` tree), an unset
`IKIGENBA_ROOT`, or empty stdin.

**Done when** (from `opsctl/`, the loop's working directory):
- `GOWORK=off go test ./...` exits 0.
- **R-EK9S-F7RY** covered by a tagged test: fed a value on stdin, the verb writes
  `${IKIGENBA_ROOT}/<svc>/state/<NAME>` as a `0600` file whose bytes equal the
  supplied value (trailing newline trimmed), over a real temp-dir `IKIGENBA_ROOT`
  tree.
- **R-ELHO-SZIN** covered by a tagged test: against a pre-existing credential
  file the verb refuses (non-zero, file byte-unchanged); with `--force` it
  replaces the file.
