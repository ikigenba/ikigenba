# D08-build

`devctl build` takes one operand and builds in one of two forms, which
coexist until the per-app form goes away. An operand that names an app in the
developer's checkout, an entry of the checkout root that D04's discovery
qualifies (`(*Checkout).HasApp`), is the per-app build; any other operand is
resolved as a commit in the local repository (`(*Checkout).ResolveCommit`)
and is the suite build. Release tags are `r<N>` or carry a slash, so no app
name is also a tag; when a name is both, the app wins.

The per-app build produces one linux/amd64 artifact from a clean commit, named
by `HEAD`'s full commit sha: `<app>/dist/<app>-<sha>.tar.xz`. It reads no tag,
and none is needed or made, so whatever tags point at `HEAD`, or none, the
file is the same; two apps built at one commit carry the same sha. Branch
ancestry imposes no restriction and nothing about the branch is recorded. The
binary's emitted manifest must match the committed manifest; the manifest is
the only thing build asks the binary for, and it never runs the binary's
`--version`. Artifact publishing preserves an earlier file on failure. A
committed manifest that still names a `port` is refused when the app is
resolved, before anything is compiled (D04 R-NM90-RHFN), so no file opsctl
would refuse is ever written.

The suite build makes one release of the whole suite from one commit. It
resolves the operand, a full sha, a shorter sha or a tag, to the full sha
without fetching, checks that sha out into a temporary detached worktree
under `dist/` at the checkout root, and builds from that tree alone: the
developer's working tree, `HEAD`, branch, index and cleanliness are not
consulted, and the release depends on the operand only through the sha. The
worktree is removed before build returns, whatever the outcome. The apps are
those D04's discovery finds in the worktree. Every refusal that needs no
compiler, a broken or `port`-naming manifest, an unusable app name (including
`opsctl`, whose directory in the release is opsctl's own), an app holding
`sbin/` or `include/`, happens before anything is compiled. The apps are then
compiled in name order, each checked against the manifest committed at that
commit exactly as the per-app build checks it, and opsctl, which has no
manifest, is compiled last; the first failure stops the build. Nothing is
injected into any binary.

The release is `dist/<sha>.tar.xz` at the checkout root (`ReleaseFile`),
printed relative to the checkout root. Its one top-level entry is `<sha>/`,
which holds `release.json` (the sha, the build time from the run seam's
clock, and the devctl version, which `cli.Run` hands to `build.Run`), one
directory per app with `bin/<app>`, the emitted `etc/manifest.toml`, the
rest of `etc/` and any `share/`, `libexec/` and `lib/`, and `opsctl/bin/opsctl`.
As in the per-app build, an earlier file of the same name survives any
failure.

`deploy` of a release (D09) and `space create` (D07) build the release the
same way, without re-running the CLI: `Suite` is the suite build for an
already resolved sha, called as a function on the caller's checkout. It
prints nothing, so each caller writes its own `build` step line, and it
returns what the callers need next: the sha, the file, and the manifests of
the apps it built, which are what the release's secrets are checked or pushed
against.

## REQUIREMENTS

- R-F6V0-EZM8: Package `internal/build` MUST export `Run(ctx context.Context, args []string, version string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout` and no profile name.

- R-67JH-CUAG: Package `internal/build` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string when `Help` is empty, and `ExitCode() int`, returning 2.

- R-69ZA-4DRU: Package `internal/build` MUST export a `StaleManifestError` struct whose only field is `App string`, with the methods `Error() string`, returning `<App>: etc/manifest.toml does not match what the binary emits; run '<App> manifest > <App>/etc/manifest.toml' and commit`, and `ExitCode() int`, returning 2.

- R-6DMZ-9OZX: When `--help` or `-h` appears anywhere among the arguments `build.Run` is given, `build.Run` MUST write the `build` usage text to `stdout`, return a nil error, call no method of `*checkout.Checkout`, and pass no `seam.Cmd` to `deps.Exec`, verified at least by `devctl build --help` and `devctl build crm --help` each printing that text to stdout with empty stderr and exit 0.

- R-F82W-SRCX: `build` MUST take exactly one operand, `<sha|tag>` or `<app>`, and MUST accept no option other than `--help` and `-h`.

- R-F9AT-6J3M: `devctl build` with no operand MUST write exactly the three lines `devctl: build needs <sha|tag> or <app>`, an empty line, and `see 'devctl build --help' for usage` to stderr, write nothing to stdout, pass no `seam.Cmd` to `deps.Exec`, and exit 2.

- R-FAIP-KAUB: `devctl build` with more than one operand MUST write exactly the three lines `devctl: build takes one <sha|tag> or <app>`, an empty line, and `see 'devctl build --help' for usage` to stderr, write nothing to stdout, pass no `seam.Cmd` to `deps.Exec`, and exit 2.

- R-6IIK-SRYP: An argument of `build` that begins with `-` and is neither `--help` nor `-h` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl build --help' for usage` to be written to stderr, nothing to stdout, and exit 2.

- R-FBQL-Y2L0: `cli.Run` MUST dispatch the command `build` to `build.Run`, passing the arguments that follow `build`, as `version` the version string that `devctl --version` prints without its newline, the `stdout` writer `cli.Run` was given, and `deps`, MUST return 0 when `build.Run` returns a nil error, and `build` MUST call `deps.Cloud` not at all, verified with a recording fake `Deps.Cloud` that is left with no call by `devctl build crm` and by `devctl build r1`.

- R-RBMT-WHDT: `build` MUST NOT read the root file `infra/terraform.tfvars.json`, verified at least by `devctl build --help` and `devctl build crm` each producing the same stdout, stderr, and exit code in a checkout whose root file is absent, in one whose root file is malformed, and in one whose root file is well-formed.

- R-6M69-Y36S: When `(*Checkout).Clean` returns false, `build` MUST return a `*UsageError` whose `Message` is `the working tree has uncommitted changes; commit them first` and whose `Help` is empty, so that `devctl build crm` writes that message as the single stderr line `devctl: the working tree has uncommitted changes; commit them first`, writes nothing to stdout, and exits 2.

- R-6UPK-MHDN: `build` MUST compare that process's standard output with the contents of the file at `checkout.ManifestFile` under the app's `Dir` and MUST return a `*StaleManifestError` for the app's `Name` when the two are not byte-identical, verified at least by reproducing the single stderr line `devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit` with empty stdout, exit 2, and no `seam.Cmd` whose `Path` is `tar` passed to `deps.Exec`.

- R-FCYI-BUBP: On a successful per-app build `build` MUST write to `stdout` exactly `File(<app>, <sha>)`, where `<sha>` is what `(*Checkout).Head` returned, followed by one newline and nothing else, write nothing to stderr, and exit 0, verified at least by reproducing the single line `crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` for the app `crm` when the fake `git rev-parse HEAD` prints `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`; and `build` MUST write nothing to `stdout` in every outcome in which it returns a non-nil error.

- R-70T2-JC34: When `deps.Exec` returns a non-nil error for the `go` `Cmd`, the app binary's `Cmd`, or the `tar` `Cmd`, `build` MUST return an error that `errors.As` does not match to a `*ProcessError` and whose message contains that `seam.Cmd`'s `Path`, so that `cli.Run` reports it as a single stderr line and exit 1.

- R-EM3N-CKUL: Package `internal/build` MUST export a `ProcessError` struct whose fields are exactly `Label string`, `Status int`, and `Stderr string`, with the methods `Error() string`, returning `<Label>: exit status <Status>`, `Detail() string`, returning `seam.QuoteOutput(Stderr)`, and `ExitCode() int`, returning 1.

- R-FE6E-PM2E: `devctl build --help` and `devctl build -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work without reading the root file and before any external operation:

  ```
  Usage: devctl build <sha|tag>
         devctl build <app>

  Build the suite at <sha|tag> for linux/amd64 and write dist/<sha>.tar.xz, one
  release holding every app and opsctl. <sha> is the full commit sha the argument
  resolves to; the working tree is not read.

  Build <app> for linux/amd64 and write <app>/dist/<app>-<sha>.tar.xz, the file
  deploy copies to a host and opsctl installs. <sha> is HEAD's full commit sha;
  the working tree must have no uncommitted changes.
  ```

- R-FPLY-TD2F: The per-app build MUST refuse an app name for which `appref.ValidName` is false, as R-FOE2-FLBQ states, passing to `deps.Exec` no `seam.Cmd` other than that of `checkout.Open`, even when the working tree has uncommitted changes; it MUST resolve the app with `(*Checkout).App`, require a clean working tree with `(*Checkout).Clean`, and resolve HEAD with `(*Checkout).Head` before passing any `seam.Cmd` whose `Path` is `go` to `deps.Exec`; a refusal MUST leave dist untouched.

- R-FOE2-FLBQ: The per-app build MUST reject an app name for which `appref.ValidName` is false before compiling or writing dist, with a `UsageError` whose message is `'<app>' is not a usable app name` and whose help is empty, yielding exit 2.

- R-FTJ7-VZFC: The per-app build MUST compile the selected app exactly once with Go for linux/amd64 with cgo disabled, into temporary output under that app’s dist directory, by passing to `deps.Exec` exactly one `seam.Cmd` whose `Path` is `go`, whose `Dir` is the app’s `Dir` so its own module is used, and whose `Args` name the package path `./cmd/<app>`, where `<app>` is the app’s `Name`, as the package to build; it MUST not inject a version at build time. A compiler nonzero exit MUST become ProcessError labeled `build <app>` with its exit status and stderr.

- R-FGM7-H5JS: The only `seam.Cmd` values whose `Path` is `git` that the per-app build passes to `deps.Exec` MUST be those of `checkout.Open`, `(*Checkout).Clean`, and `(*Checkout).Head`, so that no tag or branch is read, created, or moved and the file is named the same whether `HEAD` is tagged or not and on a branch or detached; verified at least by a successful `devctl build crm` with a recording fake `deps.Exec` that writes `crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` for a `HEAD` of `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`.

- R-FHU3-UXAH: The per-app build MUST create an xz-compressed tar artifact whose relative root members are exactly `bin/<app>`, the emitted `etc/manifest.toml`, the other regular files under the app’s `etc/`, and optional `share/` files with their original relative paths and bytes; no archive member path MUST contain a version directory. Compression failures MUST use `ProcessError` and preserve an earlier artifact unchanged.

- R-5LK0-1DC3: Build MUST replace the final artifact only after all compilation, manifest and archive operations succeed; any failure MUST preserve earlier files and remove temporary output. The static linux/amd64 executable MUST remain executable inside the archive.

- R-5MRW-F52S: Package `internal/build` MUST export `DistDir(app string) string` and `File(app, sha string) string`.

- R-5NZS-SWTH: `build.DistDir` MUST return `<app>/dist`; `build.File` MUST return `<app>/dist/<app>-<sha>.tar.xz`, preserving the supplied sha verbatim, verified at least by `File("crm", "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a")` returning `crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz`.

- R-FK9W-MGRV: After compiling an app, build MUST execute that app's staged executable with the single argument `manifest` through Deps.Exec and use that stdout as the emitted manifest; a nonzero exit MUST yield ProcessError labeled `<app> manifest` carrying the exit status and stderr.

- R-5P7P-6OK6: On successful build the artifact MUST contain the same executable used for manifest validation, its emitted manifest, and unchanged bytes for all other regular files under the app's etc directory and optional share directory.

- R-FJ20-8P16: The per-app build MUST write only under the selected app’s dist directory, creating it if needed; on return it MUST leave only the completed artifact and paths that existed before invocation, with temporary paths removed on success and failure.

- R-FLHT-08IK: For each app it compiles, `build` MUST pass to `deps.Exec` exactly one `seam.Cmd` whose `Path` is that app's staged binary, the one whose `Args` are exactly `manifest`, MUST NOT run that binary with `--version` or any other argument, and MUST pass no `seam.Cmd` whose `Path` is opsctl's staged binary, verified at least by a successful `devctl build crm` and a successful `devctl build r1` with a recording fake `deps.Exec` in which every staged binary's `--version` would exit non-zero.

- R-FNXL-RRZY: Package `internal/build` MUST export `ReleaseFile(sha string) string`.

- R-FP5I-5JQN: `build.ReleaseFile` MUST return `dist/<sha>.tar.xz`, preserving the supplied sha verbatim, verified at least by `ReleaseFile("4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a")` returning `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz`.

- R-FQDE-JBHC: `build` MUST perform the per-app build of its one operand when `(*Checkout).HasApp` of that operand returns true on the checkout `checkout.Open` found from `deps.Dir`, and the suite build of that operand otherwise; verified at least by `devctl build crm` in a temporary checkout where `crm/` qualifies as an app writing `crm/dist/crm-<sha>.tar.xz`, with `<sha>` what the fake `git rev-parse HEAD` prints, and passing no `seam.Cmd` whose `Args` begin with `rev-parse` and `--verify`, although the fake `git` would resolve `crm` as a commit; and by `devctl build r1` in a temporary checkout holding no `r1` entry writing `dist/<sha>.tar.xz`, with `<sha>` the full sha the fake `git` resolves `r1` to.

- R-HZ5O-K5OE: When the suite build's `(*Checkout).ResolveCommit` of the operand reports no commit, `build` MUST return a `*UsageError` whose `Message` is `'<operand>' is neither an app in the checkout nor a commit` and whose `Help` is empty, and MUST pass no further `seam.Cmd` to `deps.Exec`, so that `devctl build bogus` writes the single stderr line `devctl: 'bogus' is neither an app in the checkout nor a commit`, writes nothing to stdout, exits 2, and leaves every file of the checkout, `dist/` included, as it was; verified at least by `bogus`, and by `main`, `HEAD` and `HEAD~1` with a fake `git` that would resolve each of them as a revision but exits non-zero for `refs/tags/<operand>^{commit}`, each passing no `seam.Cmd` other than those of `checkout.Open` and `ResolveCommit`.

- R-I0DK-XXF3: The only `seam.Cmd` values whose `Path` is `git` that the suite build passes to `deps.Exec` MUST be that of `checkout.Open`, one of `(*Checkout).ResolveCommit` with the operand as typed, one of `(*Checkout).AddWorktree`, and one of `(*Checkout).RemoveWorktree`, so that nothing is fetched, no tag, branch, `HEAD`, index or cleanliness of the developer's checkout is changed, and none is read except the tag `refs/tags/<operand>` that `ResolveCommit` reads for an operand in the tag form; and the release MUST depend on the operand only through the full sha `ResolveCommit` returned; verified at least by `devctl build r1`, `devctl build 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a` and `devctl build 4b22285`, with a fake `git` resolving each to `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a` and the same fake `Now`, each writing the stdout line `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` and a release with the same member paths, member bytes and `release.json`.

- R-FLY9-O1UC: The suite build MUST call `(*Checkout).AddWorktree` with the full sha `ResolveCommit` returned and a `dir` that is an absolute path under the checkout's `Path("dist")` that did not exist when `build.Run` was invoked, and, once `AddWorktree` has returned a nil error, MUST call `(*Checkout).RemoveWorktree` with that same `dir` exactly once, after every `seam.Cmd` whose `Path` is `go` and before it returns, whether the build succeeds or fails, and with a context that the cancellation of `Run`'s `ctx` does not cancel; when `AddWorktree` returns an error, `build` MUST return that error unchanged and pass no further `seam.Cmd`; verified at least by a success and by each of a refused `port`, a reserved app name, an `sbin/` directory, a stale manifest, a failed app compile and a failed opsctl compile, each leaving a recording fake `deps.Exec` with exactly one `git worktree remove` whose directory equals that of the one `git worktree add`; by two successive runs whose fake `git worktree remove` exits 1 and leaves its directory in place, the second passing to `git worktree add` a directory other than the first's; and by a fake `go` that cancels `ctx` and exits non-zero, the `git worktree remove` `seam.Cmd` then arriving with a context whose `Err()` is nil.

- R-FN66-1TL1: The apps of a suite build MUST be exactly those `(*Checkout).Apps` returns for a `checkout.Checkout` whose `Root` is the worktree `dir` and whose `Deps` is `deps`, and every file of the release other than `release.json`, the staged binaries and each app's emitted `etc/manifest.toml`, whose bytes are the binary's output, MUST be taken from under `dir`, so that the developer's checkout outside `dist/` contributes nothing; verified at least by a temporary checkout whose root holds an app `extra` the fake worktree lacks, a `crm/etc/nginx.conf` and a `crm/share/icon.svg` whose bytes differ from the fake worktree's, and an uncommitted `crm/etc/local.conf`, producing a release with no `extra/` member, the fake worktree's bytes for `crm/etc/nginx.conf` and `crm/share/icon.svg`, and no `crm/etc/local.conf` member.

- R-FWGW-G66T: When `(*Checkout).Apps` on the worktree returns an error, the suite build MUST return that error unchanged and pass no `seam.Cmd` whose `Path` is `go` to `deps.Exec`, so that a fake worktree whose `crm/etc/manifest.toml` holds a top-level `port` key makes `devctl build r1` write the single stderr line `devctl: crm: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket`, write nothing to stdout, and exit 2.

- R-GCBL-F6TU: Before passing any `seam.Cmd` whose `Path` is `go` to `deps.Exec`, the suite build MUST return a `*UsageError` whose `Message` is `'<name>' is not a usable app name` and whose `Help` is empty for the first app in ascending `Name` order whose `Name` `appref.ValidName` rejects or is `opsctl`, verified at least through `cli.Run` by fake worktrees each holding an app `0app`, which sorts before the other, and one of an app `host`, an app `seed`, an app `Crm` and an app `opsctl`, each making `devctl build r1` write the single stderr line `devctl: '<name>' is not a usable app name` for that other app, write nothing to stdout, exit 2, and pass no `go` `seam.Cmd`; and by a fake worktree holding the apps `0app`, `host`, `opsctl` and `seed` writing that line for `host` alone.

- R-FQTV-74T4: Before passing any `seam.Cmd` whose `Path` is `go` to `deps.Exec`, and when no app's name is refused as R-GCBL-F6TU states, the suite build MUST return a `*UsageError` whose `Help` is empty and whose `Message` is, for the first app in ascending `Name` order whose directory in the worktree holds a directory named `sbin` or `include`, `<app>: sbin/ is not allowed in a release` for an app whose directory in the worktree holds a directory named `sbin`, and `<app>: include/ is not allowed in a release` for one that holds a directory named `include` and none named `sbin`, verified at least through `cli.Run` by a fake worktree holding an app `auth` and an app `crm` that holds `crm/sbin/tool`, and by one whose `crm` holds `crm/include/x.h` instead, each making `devctl build r1` write the single stderr line `devctl: crm: sbin/ is not allowed in a release` or `devctl: crm: include/ is not allowed in a release` respectively, write nothing to stdout, exit 2, and pass no `go` `seam.Cmd`; and by a fake worktree whose app `auth` holds `auth/include/x.h` and whose app `crm` holds `crm/sbin/tool` writing that line for `auth` and `include/` alone, and by one whose app `host` beside them makes the line `devctl: 'host' is not a usable app name`.

- R-I1LH-BP5S: For each app in ascending `Name` order, and then for opsctl, the suite build MUST compile exactly once by passing to `deps.Exec` one `seam.Cmd` whose `Path` is `go`, whose `Env` holds exactly the entries `GOOS=linux`, `GOARCH=amd64`, `CGO_ENABLED=0` and `GOWORK=off`, so that every other Go setting, `GOFLAGS` included, is inherited from devctl's environment, whose `Dir` is the app's `Dir`, or for opsctl `<dir>/opsctl` where `<dir>` is the worktree, and whose `Args` hold `-buildvcs=false`, name the package path `./cmd/<app>`, or for opsctl `./cmd/opsctl`, as the package to build, and hold no argument that is or begins with `-ldflags`; these `seam.Cmd` values MUST be passed in that order and MUST be the only ones whose `Path` is `go`; verified at least by a successful `devctl build r1` over the apps `auth`, `dummy`, `events`, `mcp`, `repos`, `scripts`, `sites` and `telemetry` in a temporary checkout whose root holds a `go.work` file.

- R-G1CH-Z95L: When an app's compile exits non-zero, its `manifest` run exits non-zero, or its manifest comparison fails, the suite build MUST return that failure's error, a `*ProcessError` labeled `build <app>`, a `*ProcessError` labeled `<app> manifest`, or a `*StaleManifestError` for the app, and MUST pass no `go` `seam.Cmd` for any later app or for opsctl and no `seam.Cmd` whose `Path` is `tar`; when opsctl's compile exits non-zero it MUST return a `*ProcessError` labeled `build opsctl` carrying that exit status and stderr and pass no `tar` `seam.Cmd`; verified at least through `cli.Run` by an app `dashboard`, between apps `auth` and `events`, whose fake compile exits 1 with the two stderr lines `# github.com/ikigenba/ikigenba/dashboard/cmd/dashboard` and `cmd/dashboard/main.go:41:2: undefined: render`, writing to stderr exactly `devctl: build dashboard: exit status 1`, an empty line, and those two lines each prefixed `> `, writing nothing to stdout, and exiting 1, with no `go` `seam.Cmd` for `events` or opsctl.

- R-G2KE-D0WA: The suite build's manifest comparison for an app MUST be against the file at `checkout.ManifestFile` under that app's `Dir` in the worktree, verified at least by `devctl build r1` with a fake worktree whose `crm/etc/manifest.toml` differs from what the staged `crm` binary emits while the developer's checkout holds a `crm/etc/manifest.toml` identical to it, writing the single stderr line `devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit`, writing nothing to stdout, and exiting 2; and by the reverse, a worktree manifest identical to the emitted bytes and a developer's copy that differs, succeeding.

- R-FUR4-9R61: On success the file at `ReleaseFile(<sha>)` under the checkout root, `<sha>` being the full sha `ResolveCommit` returned, MUST be an xz-compressed tar archive written by a `seam.Cmd` whose `Path` is `tar`, whose regular-file members are exactly `<sha>/release.json`; for each app, `<sha>/<app>/bin/<app>`, the staged binary that ran `manifest`, `<sha>/<app>/etc/manifest.toml`, holding the bytes that run emitted, and `<sha>/<app>/<path>` for every other regular file at a relative `<path>` under the app's `Dir` whose first element is `etc`, `share`, `libexec` or `lib`; and `<sha>/opsctl/bin/opsctl`, opsctl's staged binary; and whose every other member is a directory that is `<sha>/` or an ancestor directory of one of those regular files, so that the archive holds no other member of any type; each copied file MUST keep its bytes, each binary MUST be executable, every other file MUST be executable by its owner exactly when its source is, and member paths MUST NOT contain the `version` given to `Run`; verified at least by a fake worktree whose app `crm` holds `etc/nginx.conf`, `share/doc/a.md`, an owner-executable `libexec/helper`, `lib/x.so`, `cmd/crm/main.go`, `internal/x.go`, `specs/s.md`, `go.mod` and `AGENTS.md`, and whose `opsctl/` holds `cmd/opsctl/main.go`, `etc/x.conf` and `go.mod`, with no member under `<sha>/crm/cmd/`, `<sha>/crm/internal/` or `<sha>/crm/specs/`, no `go.mod` or `AGENTS.md` member, and no member under `<sha>/opsctl/` but `<sha>/opsctl/bin/` and `<sha>/opsctl/bin/opsctl`.

- R-G683-IC4D: `<sha>/release.json` MUST be exactly one JSON object whose members are exactly `sha`, the full sha `ResolveCommit` returned; `built`, the time `deps.Now` returned, converted to UTC and truncated to the second, in the RFC 3339 form `YYYY-MM-DDTHH:MM:SSZ`; and `devctl`, the `version` given to `Run`; each a JSON string; verified at least by a fake `Now` returning 2026-10-08 09:03:12.9 at UTC−05:00 giving a `built` of `2026-10-08T14:03:12Z`, and through `cli.Run` by `devctl` being exactly what `devctl --version` prints without its newline.

- R-FZMP-SU4T: On success the suite build MUST write to `stdout` exactly `ReleaseFile(<sha>)` followed by one newline, write nothing to stderr, and exit 0, that path being relative to the checkout root whatever `deps.Dir` is; it MUST create `dist/` at the checkout root when absent and MUST replace an earlier file at `ReleaseFile(<sha>)`; nothing in the checkout's working tree outside `dist/` MUST change, git's own administrative files for the worktree, which `RemoveWorktree` removes, being exempt; when `RemoveWorktree` returned nil or was not called, `dist/` MUST hold on return nothing but the paths it held before invocation and, on success, the release, and MUST be absent after a failure if it was absent before; and on every failure an earlier file at `ReleaseFile(<sha>)` MUST be left byte-identical; verified at least with `deps.Dir` the checkout's `auth/` subdirectory reproducing the single stdout line `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz`.

- R-G8NW-9VLR: When `(*Checkout).RemoveWorktree` returns an error after an otherwise successful build, the suite build MUST return that error, write nothing to `stdout`, and leave at `ReleaseFile(<sha>)` no file other than an earlier one, byte-identical; when it returns an error after another failure, the suite build MUST return the earlier failure's error; verified at least through `cli.Run` by a fake `git worktree remove` exiting 1, with an otherwise successful build giving exit 1 and the `*GitError` diagnostic of D04 R-CPT9-XFBP, and with a stale manifest giving the stale-manifest line and exit 2.

- R-G9VS-NNCG: The suite build MUST call `deps.Cloud` not at all and MUST NOT read the root file `infra/terraform.tfvars.json`, verified at least by `devctl build r1` producing the same stdout, stderr, exit code and release, with the same fake `Now`, in a checkout whose root file is absent, in one whose root file is malformed, and in one whose root file is well-formed, each leaving a recording fake `Deps.Cloud` with no call.

- R-GB3P-1F35: When the suite build's `tar` `seam.Cmd` exits non-zero, the suite build MUST return a `*ProcessError` carrying that exit status and stderr, write nothing to `stdout`, still call `(*Checkout).RemoveWorktree`, and leave an earlier file at `ReleaseFile(<sha>)` byte-identical, so that `cli.Run` exits 1.

- R-FS1R-KWJT: When the suite build's `(*Checkout).ResolveCommit` returns a non-nil error, `build` MUST return that error unchanged and pass no further `seam.Cmd` to `deps.Exec`, so that `cli.Run` writes it as a single stderr line, writes nothing to stdout, and exits 1, as R-70T2-JC34 does for a runner error.

- R-VL50-AX22: Package `internal/build` MUST export a `Release` struct whose fields are exactly `SHA string`, `File string`, and `Manifests []checkout.Manifest`, and `Suite(ctx context.Context, c *checkout.Checkout, sha, version string) (Release, error)`.

- R-VNKT-2GJG: `build.Suite` MUST perform the suite build of the full commit sha `sha` in the checkout `c`, with `c.Deps` as `deps` and `version` as the `version` that `release.json` records, so that every requirement of this design about the suite build holds for it as it holds for `devctl build <sha>`, except that `Suite` passes no `seam.Cmd` of `checkout.Open` or `(*Checkout).ResolveCommit`, writes to no stdout, and returns its failures to its caller instead of through `cli.Run`; on success it MUST return a `Release` whose `SHA` is `sha`, whose `File` is `ReleaseFile(sha)`, relative to the checkout root, and whose `Manifests` hold, in ascending app order, one `checkout.Manifest` per app of the release, the one `checkout.DecodeManifest` gives for the bytes of that app's `<sha>/<app>/etc/manifest.toml` member; verified at least by `Suite` over a fake worktree holding the apps `auth`, whose manifest lists `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` in `secrets`, and `dummy`, which lists none, returning `SHA` `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, `File` `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz`, and those two manifests in that order, and leaving at that `File` a release with the same member paths, member bytes and `release.json` as `devctl build 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a` given the same fakes and `Now`; and by a failed `dashboard` compile returning the same `*ProcessError` labeled `build dashboard`.
