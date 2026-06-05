# Versioning & Tagging — the tag→deploy workflow

> **Operator how-to.** This is the concrete, day-to-day procedure for cutting a
> release of an ikigai service and shipping it. The *why* lives in
> [`adr-deployment-redesign.md`](./adr-deployment-redesign.md) (§6 Versioning, the
> decisions list, and the "known build bugs" appendix) and in `PLAN.md` §1.6 /
> §F1 — on any conflict those win and this doc is corrected to match.

## TL;DR

```sh
# 1. cut a tag for the app you're releasing (loose SemVer, namespaced):
git tag crm/v1.4.0 HEAD            # <app>/vX.Y.Z

# 2. ship it (build off-box from the tag, hand the static binary to optctl):
bin/deploy crm v1.4.0             # explicit version
bin/deploy crm                    # or: newest crm/* tag reachable from HEAD
```

The binary self-reports what it is — `crm version` → `v1.4.0 (<sha>)` — so the
box can never lie about what's deployed.

## The tagging convention

- **Tags are `<app>/vX.Y.Z`** — loose SemVer with a leading `v`, **per service**.
  There is **no global suite version**; each service versions independently.
  Examples: `ledger/v1.0.0`, `dashboard/v2.3.1`, `wiki/v0.5.0`.
- The `<app>/` prefix is a **naming convention inside one shared tag namespace**,
  not a directory boundary — this is a mono-repo with a single `.git`. The slash
  is what scopes a tag to one service.
- **A tag pins the app's code AND its library source atomically.** Because the
  in-repo libraries are consumed via committed `replace` directives (not versioned
  `require`s), the tagged commit's tree already contains the exact
  `eventplane`/`agentkit`/`appkit` source that app builds against. One tag = one
  reproducible build.

### Libraries are NEVER tagged

`eventplane`, `agentkit`, and `appkit` are **in-repo sibling libraries consumed at
HEAD** via a committed `replace <lib> => ../<lib>` + `require <lib> v0.0.0`
(`agentkit` uses the zero-pseudo-version `v0.0.0-00010101000000-000000000000`;
same mechanism). They get **no `<lib>/vX.Y.Z` tags**.

> **HARD RULE:** never convert an internal `replace` into a versioned `require`.
> Doing so drags in the Go module proxy + subdir-tag machinery this whole design
> routes around. The libraries ship as source-in-the-tagged-tree, full stop.

So there is never a `appkit/v…` or `eventplane/v…` tag — `git tag --list` should
only ever show service namespaces.

## How `git describe` drives the build

`bin/deploy <app> [version]` resolves the version, then builds from the tagged
commit in a throwaway `git worktree`. Resolution is **always scoped to the app's
own tag namespace** so a deploy of one app can never pick up another app's tag
(mono-repo cross-contamination):

| invocation | resolves to |
|---|---|
| `bin/deploy crm v1.4.0` | the exact tag `crm/v1.4.0` (errors if it doesn't exist) |
| `bin/deploy crm` | `git describe --match 'crm/*' --tags --abbrev=0` — the newest `crm/*` tag reachable from HEAD |

The `--match 'crm/*'` is load-bearing: a plain `git tag | sort -V | tail` scan
would let a higher-sorting `dashboard/v9…` or `ledger/v2…` tag win a `crm` deploy.
`git describe --match '<app>/*'` cannot — it only ever considers tags in that one
namespace. (Verify by hand: `git describe --match 'ledger/*' --tags --abbrev=0`
returns the newest `ledger/*` even with a higher `crm/*` tag present.)

The build then runs **inside the throwaway worktree's `<app>/` dir** so
`./cmd/<app>` and the committed `replace … => ../<lib>` directives resolve against
the worktree's sibling library trees — no network, no `go.work`
(`GOWORK=off`). Flags: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off go build
-trimpath -buildvcs=false`. The worktree is removed on exit (success or failure).

## The SHA + dirty co-stamp

The version is **co-stamped with the git commit** so the running binary identifies
exactly what built it. Because `-buildvcs=false` is mandatory (the module is a
subdir of a **bare** mono-repo `.git`; Go's auto VCS stamp runs git at the bare
root and aborts with exit 128), Go's automatic `vcs.revision`/`vcs.modified` stamp
is dropped — so `bin/deploy` re-injects it via ldflags:

```
-ldflags "-s -w -X appkit.version=<bare-version> -X appkit.commit=<short-sha>[-dirty]"
```

- `appkit.version` / `appkit.commit` are package-level **`var`s** in
  `appkit/appkit.go` (NOT `const`s — `-X` against a `const` is silently ignored,
  leaving the `dev`/`none` defaults). `appkit.versionString()` renders them as
  `"<version> (<commit>)"`.
- **`-dirty` is clean-by-construction for tag builds.** The throwaway worktree is
  a clean detached checkout of the tagged commit, so its tree has no diff and the
  suffix is empty: a tag-built artifact always self-reports `vX.Y.Z (<sha>)`.
- **`-dirty` only surfaces on ad-hoc/dev builds** that build from a working tree
  with uncommitted changes. `bin/deploy` re-derives the flag *from the actual
  build source* (`git -C <worktree> status --porcelain`), so the stamp tells the
  truth about exactly what was compiled. A direct `go build` off a dirty tree with
  `-X appkit.commit=$(git rev-parse --short HEAD)-dirty` self-reports
  `vX.Y.Z (<sha>-dirty)`.

So: `<app> version` →
- `v1.4.0 (a1b2c3d)` — a clean, tagged, deployable build.
- `v1.4.0 (a1b2c3d-dirty)` — an ad-hoc build; do not ship.
- `dev (none)` — un-stamped (a bare `go build` with no ldflags); local dev only.

## The box release-dir prefix strip

On the box the release directory **strips the `<app>/` tag prefix**:

```
tag  crm/v1.4.0   →   /opt/crm/releases/v1.4.0/crm
```

`bin/deploy` passes the **bare** version (`v1.4.0`, prefix stripped) to
`optctl install <app> <bare-version> --artifact …`. `optctl` owns the release-dir
/ atomic-`current`-symlink / migrate / restart / rollback machinery on the box
(see the ADR §5 and `PLAN.md` §1.4). The laptop has **no install logic** — it only
builds and hands off the single static artifact.

## Cutting a release, end to end

1. **Land the change** on the deployable branch; make sure the working tree is
   clean (`git status --short` empty) so the build is honestly clean.
2. **Tag it:** `git tag <app>/vX.Y.Z HEAD` (and `git push --tags` if you want the
   tag on the GitHub backup remote — pushing ships nothing to the box).
3. **Ship it:** `bin/deploy <app> vX.Y.Z` (or `bin/deploy <app>` for the newest
   `<app>/*` tag). Use `--dry-run` (or `DRY_RUN=1`) to do the full off-box build
   and print the `scp`/`ssh`/`optctl` commands without shipping.
4. **Verify on the box:** `optctl`'s install runs preflight (static? amd64?
   `<app> version` matches the version arg? `<app> manifest` parses?), backs up
   the DB if the schema advances, migrates, atomically swaps `current`, restarts,
   and confirms `is-active`. Confirm `<app> version` self-reports the tag you cut.
5. **Roll back if needed:** `sudo optctl rollback <app>` repoints `current` to the
   prior release (restoring the DB first if the rolled-back-from release advanced
   the schema — the forward-only migration runner's downgrade guard requires it).
