# Open items — the spaces workflow

Gaps found by reading the `opsctl` and `devctl` stories against the spaces
workflow they describe, together with the infra ground they depend on. Each
item names the stories involved, the evidence, and a suggested resolution.
An item is closed by deleting it; git holds the history. Items are ordered
largest first within each section.

These are story-level items across three projects, so they live here rather
than in any one project's `specs/issues/`, which is gitignored working state
for the build run and halts it while non-empty.

## Smaller inconsistencies

### 16. Reserved app names

- An app named `host` or `deploy` collides with the `host/` and `deploy/`
  prefixes under the space's backup URI. Nothing refuses them.
- Resolution: build and install refuse those names.

### 17. create checks app names against the checkout, not the host

- create refuses `<app>.<space>` by reading the checkout's apps. An app
  deployed from an older checkout and since removed from it is not seen.
- Resolution: accept as is and say so, or ask the existing space's host.
