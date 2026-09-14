# D8-build

`build` turns one app of the checkout into the one file `deploy` carries to a
host and `opsctl` installs: `<app>/dist/<app>-<tag>.tar.xz`. It is the smallest
the five command designs and the only one that acts entirely on the developer's
machine — no `--account`, no `Deps.Cloud`, no AWS call of any kind, which is
D2's rule that only `space`, `secrets`, `deploy` and `restore` require a profile
— so `internal/build` is a package of one command, three error types, three path
functions and nothing reusable: nothing else in devctl builds an app.

This design adds nothing of D1–D7's. `seam.Deps`, `seam.Cmd` and `cli.Run` are
D1's; the grammar, the diagnostic shape and the four exit codes are D2's; the
checkout root, app discovery, `etc/manifest.toml`, `ManifestFile` and the three
git facts are D4's; the command-package shape — `Run` with stdout only, errors
that classify themselves through `ExitCode()` and `Detail()` — is D5's; the
`UsageError` form is D5's and D6's, declared again here because each command
owns its own. `build` prints one line and no step lines, so it does not need
`space.Step` and does not import `internal/space`.

**The preflight is D4's facts, reported one at a time.** D4 deliberately returns
the four observations separately — the app, the clean tree, `HEAD`, the tags at
`HEAD`, reachability from `origin/main` — because `build` has a different
diagnostic for each, and two of them quote the sha. The order is D4's canonical
usage, and the stories' preconditions agree with it: the app is resolved first,
so `devctl build bogus` says `no app 'bogus' in the checkout` — D4's rendering
of a `*NoAppError` — whether or not the tree happens to be dirty; then the
tree, then the sha, then the tags, then `origin/main`. Each of the last three
observations has its own one-line refusal: a working tree with uncommitted
changes, no tag pointing at `HEAD`, and a `HEAD` that `origin/main` cannot
reach, the last two naming the sha. The requirements below fix their wording.

All three are preflight refusals: the command was well-formed and was turned
away before it did anything, which is D2's exit 2. They are three `*UsageError`
values with no `Help`, which is why `UsageError.Detail()` here returns nothing
when `Help` is empty — a one-line refusal and a two-block usage error are the
same mechanism with and without the second block.

More than one tag may point at `HEAD`; D4 left what to do about that to this
design. `build` takes the first tag `TagsAtHead` returns — the first line of
`git tag --points-at HEAD`, which git sorts by refname. Every tag at `HEAD`
names the same tree, so every name the choice could produce is truthful, and
refusing would be a diagnostic no story asks for.

**One compile, and the binary is then run.** The tarball's `etc/manifest.toml`
is the manifest *the built binary emits*, so the binary must exist before the
manifest does, and the binary that emits it is the `linux/amd64` one that goes
into the tarball — the story's postcondition says "emitted by running that
binary with the argument `manifest`", and its precondition says "the built
binary runs on the developer's machine". So there is exactly one `go build` —
cross-compiled for `linux/amd64` with cgo disabled, writing its binary into the
staging tree — and the thing it produced is then executed, with the argument
`manifest`, to produce the staged manifest.

`CGO_ENABLED=0` is what makes it static. D7 hit the mirror image of this and
answered it the other way: it needed the *version* of the cross-compiled
`opsctl` and refused to ask the binary locally, because "the developer's machine
is not promised to be one" — it asked the host instead, after installing it.
`build` has no host: it makes no network call at all, and the manifest has to be
had before the file exists. Building a second, native copy of the app only to
ask it was rejected — two binaries from one commit is two things to be wrong
about, and the manifest would then come from a binary nobody ships, which is
exactly the thing the freshness check exists to prevent. So this design takes
the story's precondition as the contract: the developer's machine runs the
`linux/amd64` binary it just built. A machine that cannot is the one failure
with no story, and it surfaces as what it is — `Deps.Exec` refusing to start
the process, reported as exit 1 with the program's path in the message.

**Freshness, and where the output goes while it is decided.** The committed
`<app>/etc/manifest.toml` exists so the checkout can be read without a build
(`secrets push` does exactly that, D5), and the binary is the source of truth,
so the two must agree byte for byte or the build is refused — with a one-line
diagnostic that names the app and tells the developer to regenerate the
committed file from the binary and commit it.

That refusal comes *after* a successful compile, and the story's postcondition
for it is not "nothing has changed" — as it is for the three git refusals — but
"nothing under `crm/dist/` has changed". That is the latitude this ordering
needs, and this design spends it in one place: everything `build` produces
before the archive exists is written under `<app>/dist/.build`, which is created
on the way in and removed on the way out, whatever the outcome. So the
compile's output and the emitted manifest have somewhere to live, and the only
thing `build` ever leaves behind is `<app>/dist/<app>-<tag>.tar.xz`. Nothing
outside `<app>/dist/` is written, read-only or not, on success or on failure.

**The archive is the staging tree, rooted.** `<app>/dist/.build` *is* the
tarball's layout, so the archive is one `tar` over it with nothing to rewrite: a
single invocation that writes the archive at its versioned path and, with `-C`,
takes its members from the staging directory by their top-level names.

`-C` is why no member path can carry a version: the members are `bin/crm`,
`etc/manifest.toml`, `etc/nginx.conf`, `share/...`, relative to the tarball
root, and the tag appears only in the file's own name. That is what makes one
file deployable to any space — `deploy` promotes the same bytes from sandbox to
production (D9) — and it is contract, not a convenience. `-J` is the system
`tar` calling the system `xz`, as the house decided; devctl links no compression
library and `seam.Cmd` has no standard input to pipe one process into another
with (D4).

**Three processes, three failures, and the one that is not exit 2.** `go`, the
app's own binary, and `tar` each ran and each can fail. Every one of them is a
`*ProcessError` — a label, the exit status, and the program's stderr as the
second block — and every one of them is exit 1, not 2: the command was
well-formed and an external program it ran could not do the job. The story fixes
the compile's bytes, and notably the label is not the `go` command line the way
D7's `opsctl` build is; it is the app: a failed compile is reported as a build
of that app, carrying the toolchain's own output underneath it.

**The usage text.** `devctl build --help` is declared byte for byte in the
requirements below, from the story. There are no subcommands, so it promises
nothing further.

The grammar is one operand and no options of its own. The story fixes the
missing operand; the other two refusals — more than one operand, and an option
`build` does not know — are in D5's and D6's voice, each one message line
followed by the pointer at `devctl build --help`.

**`<app>/dist/` and the clean tree.** An app owns its own build output, the way
every sub-project of the checkout owns its `bin/` and `dist/`: `build` writes
`<app>/dist/` and nothing at the checkout root. The repository's `.gitignore`
ignores `dist/` at any depth, so the output of one build never makes the next
one fail the clean check — `build` refuses to run when `git status --porcelain`
reports anything, untracked files included, as D4's `Clean` fact has it, and
loosening that would let an uncommitted `crm/handler.go` into a tarball whose
name claims a tag, the very thing the check exists for. What this design owes
the problem is containment, and that is a requirement: the only path `build`
ever writes is under `<app>/dist/`, and the only path it leaves there is the
archive, so no future step of `build` can re-open it.

**What is deliberately not here.** Reading `etc/manifest.toml` back out of a
tarball member, parsing `<app>-<tag>.tar.xz` into an app and a tag, and copying
the file to a host are `deploy`'s (D9). The checkout root, app discovery, the
manifest decoder and the git facts are D4's. Getting opsctl onto a host is
`space create`'s (D7), and it is not a build at all: opsctl is a separate
project consumed as a published release, never compiled from this checkout.

## REQUIREMENTS

- R-63VS-7J2D: Package `internal/build` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout` and no profile name.
- R-ZXH8-IG5H: Package `internal/build` MUST export `DistDir(app string) string`, `StageDir(app string) string`, and `File(app, tag string) string`.
- R-67JH-CUAG: Package `internal/build` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string when `Help` is empty, and `ExitCode() int`, returning 2.
- R-68RD-QM15: Package `internal/build` MUST export a `ProcessError` struct whose fields are exactly `Label string`, `Status int`, and `Stderr string`, with the methods `Error() string`, returning `<Label>: exit status <Status>`, `Detail() string`, returning `Stderr` with its trailing newlines removed, and `ExitCode() int`, returning 1.
- R-69ZA-4DRU: Package `internal/build` MUST export a `StaleManifestError` struct whose only field is `App string`, with the methods `Error() string`, returning `<App>: etc/manifest.toml does not match what the binary emits; run '<App> manifest > <App>/etc/manifest.toml' and commit`, and `ExitCode() int`, returning 2.
- R-ZYP4-W7W6: `build.DistDir` MUST return `app`, a `/`, and `dist` concatenated in that order; `build.StageDir` MUST return `DistDir(app)` followed by `/.build`; and `build.File` MUST return `DistDir(app)`, a `/`, `app`, a `-`, `tag`, and `.tar.xz` concatenated in that order; verified at least by `DistDir("crm")` being `crm/dist`, `StageDir("crm")` being `crm/dist/.build`, and `File("crm", "v0.1.0")` being `crm/dist/crm-v0.1.0.tar.xz`.
- R-JOL7-17TH: `devctl build --help` and `devctl build -h` MUST print exactly this text to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl build <app>

  Build <app> for linux/amd64 and write <app>/dist/<app>-<tag>.tar.xz, the file
  deploy copies to a host and opsctl installs. HEAD must be a commit on
  origin/main that a tag points at, with no uncommitted changes.
  ```
- R-6DMZ-9OZX: When `--help` or `-h` appears anywhere among the arguments `build.Run` is given, `build.Run` MUST write the `build` usage text to `stdout`, return a nil error, call no method of `*checkout.Checkout`, and pass no `seam.Cmd` to `deps.Exec`, verified at least by `devctl build --help` and `devctl build crm --help` each printing that text to stdout with empty stderr and exit 0.
- R-6EUV-NGQM: `build` MUST take exactly one `<app>` operand and MUST accept no option other than `--help` and `-h`.
- R-6G2S-18HB: `devctl build` with no operand MUST write exactly the three lines `devctl: build needs <app>`, an empty line, and `see 'devctl build --help' for usage` to stderr, write nothing to stdout, and exit 2.
- R-6HAO-F080: `devctl build` with more than one operand MUST write exactly the three lines `devctl: build takes one <app>`, an empty line, and `see 'devctl build --help' for usage` to stderr, write nothing to stdout, and exit 2.
- R-6IIK-SRYP: An argument of `build` that begins with `-` and is neither `--help` nor `-h` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl build --help' for usage` to be written to stderr, nothing to stdout, and exit 2.
- R-6JQH-6JPE: `cli.Run` MUST dispatch the command `build` to `build.Run`, passing the arguments that follow `build`, the `stdout` writer `cli.Run` was given, and `deps`, MUST return 0 when `build.Run` returns a nil error, and `build` MUST call `deps.Cloud` not at all, verified with a recording fake `Deps.Cloud` that is left with no call by `devctl build crm` and by `devctl --account <name> build crm`.
- R-6KYD-KBG3: `build` MUST call `checkout.Open`, then `(*Checkout).App` with the `<app>` operand, then `(*Checkout).Clean`, then `(*Checkout).Head`, then `(*Checkout).TagsAtHead`, then `(*Checkout).ReachableFromOriginMain`, in that order, before it passes any `seam.Cmd` whose `Path` is not `git` to `deps.Exec`, and when any of those calls fails or refuses, every `seam.Cmd` `build` passed to `deps.Exec` MUST have `git` as its `Path`.
- R-6M69-Y36S: When `(*Checkout).Clean` returns false, `build` MUST return a `*UsageError` whose `Message` is `the working tree has uncommitted changes; commit them first` and whose `Help` is empty, so that `devctl build crm` writes that message as the single stderr line `devctl: the working tree has uncommitted changes; commit them first`, writes nothing to stdout, and exits 2.
- R-6NE6-BUXH: When `(*Checkout).TagsAtHead` returns no element, `build` MUST return a `*UsageError` whose `Message` is `no tag points at HEAD (<head>)`, where `<head>` is what `(*Checkout).Head` returned, and whose `Help` is empty, verified at least by reproducing the single stderr line `devctl: no tag points at HEAD (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)` with empty stdout and exit 2.
- R-6PTZ-3EEV: When `(*Checkout).ReachableFromOriginMain` returns false, `build` MUST return a `*UsageError` whose `Message` is `HEAD (<head>) is not reachable from origin/main`, where `<head>` is what `(*Checkout).Head` returned, and whose `Help` is empty, verified at least by reproducing the single stderr line `devctl: HEAD (9f8e7d6c5b4a39281706f5e4d3c2b1a0f9e8d7c6) is not reachable from origin/main` with empty stdout and exit 2.
- R-6R1V-H65K: The tag `build` builds at MUST be the first element `(*Checkout).TagsAtHead` returns, verified at least by `TagsAtHead` returning two tags and the written path carrying the first of them.
- R-014X-NRDK: `build` MUST pass to `deps.Exec` exactly one `seam.Cmd` whose `Path` is `go`, whose `Args` are exactly `build`, `-o`, `StageDir(<app>)` followed by `/bin/` and the app's `Name`, and `./` followed by the app's `Name`, whose `Dir` is `Checkout.Root`, and whose `Env` is exactly `GOOS=linux`, `GOARCH=amd64`, and `CGO_ENABLED=0`; and when that process exits non-zero MUST return a `*ProcessError` whose `Label` is `build ` followed by the app's `Name`, whose `Status` is that exit status, and whose `Stderr` is that process's standard error, verified at least by reproducing the stderr `devctl: build dashboard: exit status 1`, one empty line, and `# github.com/ikigenba/ikigenba/dashboard` and `./main.go:41:2: undefined: render` unprefixed, with empty stdout and exit 1.
- R-6THO-8PMY: After that process exits 0, `build` MUST pass to `deps.Exec` exactly one `seam.Cmd` whose `Path` is `(*Checkout).Path(StageDir, "bin", <app>)`, whose `Args` are exactly `manifest`, whose `Dir` is `Checkout.Root`, and whose `Env` has no entry; and when that process exits non-zero MUST return a `*ProcessError` whose `Label` is the app's `Name` followed by ` manifest`, whose `Status` is that exit status, and whose `Stderr` is that process's standard error.
- R-6UPK-MHDN: `build` MUST compare that process's standard output with the contents of the file at `checkout.ManifestFile` under the app's `Dir` and MUST return a `*StaleManifestError` for the app's `Name` when the two are not byte-identical, verified at least by reproducing the single stderr line `devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit` with empty stdout, exit 2, and no `seam.Cmd` whose `Path` is `tar` passed to `deps.Exec`.
- R-6VXH-094C: When the two are byte-identical, the directory `(*Checkout).Path(StageDir)` MUST hold, before `build` archives it, exactly these regular files and no other: `bin/<app>`, the file the `go` process wrote; `etc/manifest.toml`, whose bytes are exactly the standard output of the app's binary; one file at `etc/` followed by its path relative to the app's `etc` directory for every regular file under that directory other than `manifest.toml`, with those bytes; and, when the app's `Dir` holds a directory named `share`, one file at `share/` followed by its path relative to that directory for every regular file under it, with those bytes; verified at least with an app whose `etc` holds `manifest.toml` and `nginx.conf` and whose `share` holds a file in a subdirectory, and a fake `Deps.Exec` that writes the `-o` path of the `go` `Cmd`.
- R-02CU-1J49: `build` MUST then pass to `deps.Exec` exactly one `seam.Cmd` whose `Path` is `tar`, whose `Args` are exactly `-c`, `-J`, `-f`, `File(<app>, <tag>)`, `-C`, `StageDir(<app>)`, `bin`, `etc`, and — only when the app's `Dir` holds a directory named `share` — `share`, and whose `Dir` is `Checkout.Root`, so that no member path of the archive contains `<tag>`; and when that process exits non-zero MUST return a `*ProcessError` whose `Label` is that `Cmd`'s `Path` followed by its `Args` joined by single spaces, whose `Status` is that exit status, and whose `Stderr` is that process's standard error.
- R-03KQ-FAUY: `build` MUST create the directory `(*Checkout).Path(DistDir(<app>))` when it does not exist, MUST leave no file or directory at `(*Checkout).Path(StageDir(<app>))` when `Run` returns, whether it returns a nil error or not, MUST write no path other than `(*Checkout).Path(DistDir(<app>))` and paths under it, and MUST leave under `(*Checkout).Path(DistDir(<app>))` no path other than `File(<app>, <tag>)` and the paths that were there before it ran; verified at least for a successful build, for the stale-manifest refusal, and for a `go` process that exits non-zero, each with a recorded listing of the checkout before and after.
- R-04SM-T2LN: On a successful build `build` MUST write to `stdout` exactly `File(<app>, <tag>)` followed by one newline and nothing else, write nothing to stderr, and exit 0, verified at least by reproducing the single line `crm/dist/crm-v0.1.0.tar.xz` for the app `crm` at the tag `v0.1.0`; and `build` MUST write nothing to `stdout` in every outcome in which it returns a non-nil error.
- R-70T2-JC34: When `deps.Exec` returns a non-nil error for the `go` `Cmd`, the app binary's `Cmd`, or the `tar` `Cmd`, `build` MUST return an error that `errors.As` does not match to a `*ProcessError` and whose message contains that `seam.Cmd`'s `Path`, so that `cli.Run` reports it as a single stderr line and exit 1.
