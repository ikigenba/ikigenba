# Open items — the spaces workflow

Gaps found by reading the `opsctl` and `devctl` stories against the spaces
workflow they describe, together with the infra ground they depend on. Each
item names the stories involved, the evidence, and a suggested resolution.
An item is closed by deleting it; git holds the history. Items are ordered
largest first within each section.

These are story-level items across three projects, so they live here rather
than in any one project's `specs/issues/`, which is gitignored working state
for the build run and halts it while non-empty.

## Where the stories disagree with infra

These are outside the stories, but the workflow depends on them.

### 13. Litestream retention versus the no-delete role

- Litestream's retention expects to delete objects. The space role
  deliberately holds no `s3:DeleteObject`. The stories do not say which
  wins.
- `opsctl retire` also relies on litestream shipping every WAL frame it
  holds when systemd stops it, which is how it is documented but is
  unverified against the pinned version.
- Resolution: check litestream's behaviour when delete is refused; either
  set retention to never in the generated `litestream.yml` and lean on
  bucket expiry, or grant delete under the space's own prefix. Check the
  shutdown sync at the same time.

### 14. Sandbox recreation and the duplicate-certificate limit

- Recreating the same sandbox domain repeatedly hits Let's Encrypt's
  duplicate-certificate limit. The sandbox has no host backup to restore a
  certificate from.
- Resolution: state the limit in the create story, or give the sandbox a
  host backup period so the certificate survives a recreate.

## Smaller inconsistencies

### 15. secrets push requires an instance that create has not launched yet

- `secrets push` preconditions: "The space exists in the account (an
  instance tagged `Space=<domain>`)". create pushes secrets before it
  launches the instance.
- Resolution: create's secrets step skips the existence check, and the
  story says so.

### 16. Reserved app names

- An app named `host` or `deploy` collides with the `host/` and `deploy/`
  prefixes under the space's backup URI. Nothing refuses them.
- Resolution: build and install refuse those names.

### 17. create checks app names against the checkout, not the host

- create refuses `<app>.<space>` by reading the checkout's apps. An app
  deployed from an older checkout and since removed from it is not seen.
- Resolution: accept as is and say so, or ask the existing space's host.
