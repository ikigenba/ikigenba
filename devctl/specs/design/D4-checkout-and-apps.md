# D4-checkout-and-apps

D3 is the account; this is the checkout. Everything devctl knows about the
developer's machine before a command does anything specific lives in two
packages: `internal/checkout`, the checkout and the apps in it, and
`internal/keyring`, one secret value from the login keyring. Nothing here is a
command: no grammar, no usage text, no step output, and no decision about what
a failure means to the developer beyond the one diagnostic the stories fix.
D5–D9 sit on top of it, the way they sit on top of D3.

**The root is git's, not the shell's.** Every path the stories name is relative
to the checkout — `<app>/dist/<app>-<tag>.tar.xz` and `crm/etc/manifest.toml`
— while `Deps.Dir` is wherever the developer
happened to type the command, which for this repository is as likely to be
`devctl/` or `crm/` as the root. So the root is asked of `git`, one process
through `Deps.Exec`: a checkout is opened from the deps, carries the absolute
root it found and the deps it found it through, and joins every later path
under that root.

`git rev-parse --show-toplevel` run in `Deps.Dir` prints the absolute root and
exits 0 from anywhere inside a checkout, and exits non-zero from outside one —
which is the whole of the discovery and the whole of the refusal. The
alternative, requiring `Deps.Dir` to be the root, was rejected: nothing can
enforce it, every other tool in the developer's hands (git itself included)
works from anywhere in the tree, and the failure it produces is a lie —
`devctl build crm` typed in `crm/` would report `no app 'crm' in the checkout`.
`Checkout` carries `Deps` as an ordinary field so a test that does not care
about discovery writes `&checkout.Checkout{Root: dir, Deps: deps}` and runs no
`git` at all.

**An app is a directory with a `main` package and a manifest.** The stories say
it twice and say nothing more: "a sub-project of the checkout that has a `main`
package and a committed `etc/manifest.toml`; its name is the directory name".
So discovery is a directory listing of the root: an entry is an app when it
holds `etc/manifest.toml` and at least one non-test `.go` file whose package
clause is `main` — which is exactly the set `go build ./<name>` can build and
`build` can tar. `devctl/`, `opsctl/` and `infra/` have no manifest, and no
`dist/` sits at the root at all: build output belongs to the app that produced
it, in `<app>/dist/`. Apps come back in name order because all three readers
print them in name order. An app is carried as its directory name, its
directory under the root, and its decoded manifest; the checkout answers both
for every app at once and for one app by name, and a name that is not an app is
a refusal rather than an empty result.

**One manifest reader, two sources of bytes.** D1 made this the only package
that decodes TOML on purpose: `secrets push` reads the manifest of a directory
in the checkout, and `deploy` reads `etc/manifest.toml` out of a `.tar.xz`
member (D9). Both are bytes, so the reader takes an `io.Reader` and knows
nothing about where they came from; `Apps` is the only thing that knows about
directories. The format is the one `secrets.md` already shows and the whole of
it: the app's name, the port it listens on, whether it is the default app, the
list of secret names it needs, and an `[env]` table of plain string settings.

devctl reads `secrets` and `app`; `port`, `default` and `env` are the host's
business, carried through the tarball to `opsctl install`. They are still
decoded and still type-checked, because the manifest is one contract and a file
devctl accepts is a file devctl claims is well-formed. An unknown key is
ignored, as the account properties ignore one (D3), so the file may grow a key
for opsctl without every devctl in the world refusing it. `app` must be the
directory's name: they are two spellings of one identity, and `secrets push`
writes `/ikigenba/<domain>/<app>` from one of them while the developer typed
the other.

**The three git facts, one at a time.** `build` reports them one at a time with
three different messages, two of which quote the full `HEAD` sha, so the facts
are three separate observations and the sha is a fourth: the sha of `HEAD`,
whether the working tree is clean, which tags point at `HEAD`, and whether
`HEAD` is reachable from `origin/main`, each its own method over its own `git`.

Each runs one `git`, in `Root`, and answers one question; none of them decides
anything. `TagsAtHead` is a list rather than a tag because more than one tag can
point at a commit and what to do about it is `build`'s policy, not a fact about
the checkout. `Clean` counts an untracked file as a change — an untracked
`crm/handler.go` is in the binary `go build` produces and is not in the commit
the tag names, which is the thing the story is protecting against.

A `git` that fails in a way none of them has an answer for — no `origin/main`
ref, a corrupt index, a `HEAD` that is not a commit — becomes a `*GitError`
carrying the command, its status and its stderr, so the diagnostic is the shape
D2 fixed for another program's failure: `devctl: git merge-base --is-ancestor
HEAD origin/main: exit status 128`, a blank line, then `fatal: Not a valid
object name origin/main`.

**The keyring, and the one place a value may go.** A secret value comes from
`secret-tool lookup name <NAME>` with an environment variable of the same name
overriding it. `secrets push` and `space create` both read it and both report
the same refusal, so the refusal is one error type here rather than two copies
there.

Trailing newlines come off both sources and nothing else does: `secret-tool`
adds a newline only when it is printing to a tty, which devctl's captured pipe
is not, but a value stored with one would otherwise travel to Parameter Store
with it. An empty value is no value.

**Where a value may not go.** Never to stdout or stderr, and never into a
`seam.Cmd` — not as an argument, where any process on the machine can read it
out of `/proc`, and not in `Cmd.Env`, where the test fakes that record every
`Cmd` would hold it. That is affordable because the only destination a value has
is Parameter Store, reached through `Account.Clients.SSM` (D3) — an API call,
not a process. `seam.Cmd` has no standard input, which is the design's answer to
"how does a value reach another process": it does not. A command that one day
must hand a secret to `ssh` or `opsctl` needs `seam.Cmd` to grow a `Stdin`
field in D1 first; it may not reach for `Args` or `Env`.

**The diagnostics this design owns.** Five in all. Two are the stories' text —
the app the checkout does not have, and the secret name neither the keyring nor
the environment answers for. Three are this design's, for failures no story
covers: a directory that is not inside a git checkout, a manifest whose `app`
key disagrees with the directory holding it, and a `git` that failed with a
status no fact has an answer for. The first four are preflight — the command
was refused before it did anything, which is D2's exit 2. The last is an
external program that ran and failed, which is the exit 1 `build`'s own compile
failure gets.

**What is deliberately not here.** `<app>/dist/`, the tarball's layout, the
`<app>-<tag>.tar.xz` name and the manifest-freshness check are `build`'s (D8);
reading a manifest out of a tarball member is `deploy`'s use of
`DecodeManifest` (D9); the JSON object the secret values are written into, and
the order apps are pushed in, are `secrets`' (D5); the space role's policy and
getting opsctl onto a host are `space create`'s (D7), and neither is a path in
this checkout. This design
answers "what is on this machine", not "what should happen next".

## REQUIREMENTS

- R-VGLU-EXV4: Package `internal/checkout` MUST export a `Checkout` struct whose fields are exactly `Root string` and `Deps seam.Deps`, and `Open(ctx context.Context, deps seam.Deps) (*Checkout, error)`.
- R-VHTQ-SPLT: Package `internal/checkout` MUST export the methods `Path(elem ...string) string`, `Apps() ([]App, error)`, and `App(name string) (App, error)` on `*Checkout`.
- R-VK9J-K937: Package `internal/checkout` MUST export the methods `Head(ctx context.Context) (string, error)`, `Clean(ctx context.Context) (bool, error)`, `TagsAtHead(ctx context.Context) ([]string, error)`, and `ReachableFromOriginMain(ctx context.Context) (bool, error)` on `*Checkout`.
- R-VLHF-Y0TW: Package `internal/checkout` MUST export an `App` struct whose fields are exactly `Name string`, `Dir string`, and `Manifest Manifest`.
- R-VMPC-BSKL: Package `internal/checkout` MUST export a `Manifest` struct whose fields are exactly `App string`, `Port int`, `Default bool`, `Secrets []string`, and `Env map[string]string`, decoded from the TOML keys `app`, `port`, `default`, `secrets`, and `env` respectively.
- R-VNX8-PKBA: Package `internal/checkout` MUST export `DecodeManifest(r io.Reader) (Manifest, error)`.
- R-VP55-3C1Z: Package `internal/checkout` MUST export `ManifestFile = "etc/manifest.toml"` and `MainRef = "origin/main"`.
- R-VQD1-H3SO: Package `internal/checkout` MUST export a `NotInCheckoutError` struct whose only field is `Dir string` and whose `Error()` returns `'<Dir>' is not inside a git checkout`, and a `NoAppError` struct whose only field is `Name string` and whose `Error()` returns `no app '<Name>' in the checkout`.
- R-VRKX-UVJD: Package `internal/checkout` MUST export a `ManifestError` struct whose fields are exactly `App string`, `Detail string`, and `Err error`, with the methods `Error() string`, returning `<App>: etc/manifest.toml: <Detail>`, and `Unwrap() error`, returning `Err`.
- R-VSSU-8NA2: Package `internal/checkout` MUST export a `GitError` struct whose fields are exactly `Args []string`, `ExitCode int`, and `Stderr string`, whose `Error()` returns `git <Args joined by single spaces>: exit status <ExitCode>`.
- R-VU0Q-MF0R: Package `internal/keyring` MUST export `Lookup(ctx context.Context, deps seam.Deps, name string) (string, error)` and a `NoValueError` struct whose only field is `Name string` and whose `Error()` returns `no value for '<Name>' in the keyring or the environment`.
- R-VV8N-06RG: `checkout.Open` MUST pass exactly one `seam.Cmd` to `deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `rev-parse` and `--show-toplevel`, and whose `Dir` is `deps.Dir`, and when that process exits 0 with a non-empty standard output MUST return a `*Checkout` whose `Root` is that standard output with its trailing newlines removed and whose `Deps` is `deps`.
- R-VWGJ-DYI5: `checkout.Open` MUST return a nil `*Checkout` and a `*NotInCheckoutError` whose `Dir` is `deps.Dir` when that process exits non-zero or its standard output is empty after trailing newlines are removed, and MUST return a nil `*Checkout` and an error whose message contains `git rev-parse --show-toplevel` when `deps.Exec` returns a non-nil error.
- R-VYWC-5HZJ: `(*Checkout).Path` MUST return `Root` joined with `elem` by the path separator, cleaned, and MUST return `Root` when `elem` is empty.
- R-W048-J9Q8: Every filesystem path devctl reads or writes inside the checkout MUST be resolved under `Checkout.Root` and MUST NOT be resolved under `Deps.Dir` when the two differ, and every `seam.Cmd` that `internal/checkout` passes to `Deps.Exec` other than `Open`'s MUST carry a `Dir` equal to `Root`; verified with a `Deps.Dir` that is a subdirectory of the directory a fake `Exec` reports for `git rev-parse --show-toplevel` and a command that reads an app's manifest, observing that the manifest is read under the reported directory.
- R-W1C4-X1GX: `(*Checkout).Apps` MUST return one `App` for each entry of `Root` that is a directory, holds a regular file at `ManifestFile`, and holds at least one file directly in itself whose name ends in `.go` but not in `_test.go` and whose package clause is `main`, each carrying that directory's name as `Name`, `Path(Name)` as `Dir`, and the `Manifest` decoded from its `ManifestFile` as `Manifest`; MUST return them sorted ascending by `Name`; MUST return an empty result and a nil error when `Root` holds no such entry; and MUST pass no `seam.Cmd` to `Deps.Exec`.
- R-W2K1-AT7M: `(*Checkout).Apps` MUST return a `*ManifestError` whose `App` is the directory's name when that directory's `ManifestFile` cannot be read or `DecodeManifest` of its contents fails, with that failure's error as `Err`, and MUST return a `*ManifestError` whose `App` is the directory's name and whose `Detail` is `app is '<decoded app>', not '<directory name>'` when the decoded `Manifest.App` is not that directory's name.
- R-W3RX-OKYB: `(*Checkout).App` MUST return the `App` whose `Name` equals `name` among those `Apps` returns, MUST return a `*NoAppError` for `name` when there is none, and MUST return the error `Apps` returned when `Apps` failed.
- R-W4ZU-2CP0: `checkout.DecodeManifest` MUST decode a document holding `app = "crm"`, `port = 3100`, `default = false`, `secrets = ["CRM_API_KEY", "CRM_API_SECRET", "CRM_ORG"]`, and an `[env]` table holding `OUTBOX_RETENTION_DAYS = "7"` into a `Manifest` whose `App` is `crm`, `Port` is `3100`, `Default` is false, `Secrets` is those three names in that order, and `Env` is that one pair; MUST ignore every key of the document that `Manifest` does not name; and MUST return a `Manifest` whose `Default` is false, whose `Secrets` has no element, and whose `Env` has no element when the document carries none of `default`, `secrets`, and `env`.
- R-W67Q-G4FP: `checkout.DecodeManifest` MUST return a non-nil error when `r` does not yield a valid TOML document, when the document has no `app` key or its value is not a non-empty string, when it has no `port` key or its value is not an integer between 1 and 65535, or when it carries `default`, `secrets`, or `env` as anything other than a boolean, an array of strings, or a table whose every value is a string respectively.
- R-W7FM-TW6E: `(*Checkout).Head` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `rev-parse` and `HEAD`, and whose `Dir` is `Root`, and when that process exits 0 MUST return its standard output with its trailing newlines removed.
- R-W8NJ-7NX3: `(*Checkout).Clean` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `status` and `--porcelain`, and whose `Dir` is `Root`, and when that process exits 0 MUST return true if and only if its standard output is empty, so that an untracked file reported as a change makes the result false.
- R-W9VF-LFNS: `(*Checkout).TagsAtHead` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `tag`, `--points-at` and `HEAD`, and whose `Dir` is `Root`, and when that process exits 0 MUST return one element for each non-empty line of its standard output, in the order the lines appear, and a result with no element when it has none.
- R-WB3B-Z7EH: `(*Checkout).ReachableFromOriginMain` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `git`, whose `Args` are exactly `merge-base`, `--is-ancestor`, `HEAD` and `MainRef`, and whose `Dir` is `Root`, and MUST return true and a nil error when that process exits 0 and false and a nil error when it exits 1.
- R-WCB8-CZ56: `Head`, `Clean`, `TagsAtHead`, and `ReachableFromOriginMain` MUST each return a `*GitError` whose `Args` are the `Args` of the `seam.Cmd` it passed to `Deps.Exec`, whose `ExitCode` is that process's exit status, and whose `Stderr` is that process's standard error, whenever that status is one the method defines no result for — any non-zero status for `Head`, `Clean`, and `TagsAtHead`, and any status other than 0 and 1 for `ReachableFromOriginMain` — and MUST each return an error that `errors.As` does not match to a `*GitError` when `Deps.Exec` returns a non-nil error.
- R-WDJ4-QQVV: `keyring.Lookup` MUST return the value of `deps.Getenv(name)` with its trailing newlines removed, and MUST pass no `seam.Cmd` to `deps.Exec`, when that value is not empty after that removal.
- R-WER1-4IMK: When `deps.Getenv(name)` is empty after its trailing newlines are removed, `keyring.Lookup` MUST pass exactly one `seam.Cmd` to `deps.Exec`, whose `Path` is `secret-tool`, whose `Args` are exactly `lookup`, `name` and `name`'s value, and whose `Dir` is `deps.Dir`; MUST return that process's standard output with its trailing newlines removed when it exits 0 and that output is not empty after the removal; MUST return an empty string and a `*NoValueError` for `name` when it exits non-zero or that output is empty after the removal; and MUST return an empty string and an error whose message contains `secret-tool` when `deps.Exec` returns a non-nil error.
- R-WFYX-IAD9: A value `keyring.Lookup` returns MUST NOT appear in the `Path`, `Args`, `Dir`, or `Env` of any `seam.Cmd` passed to `Deps.Exec`, and MUST NOT appear in anything any command writes to stdout or stderr, verified by running `secrets push` and `space create` through `cli.Run` with a sentinel value reachable through `Deps.Getenv` and asserting that the sentinel appears in neither stream nor in any recorded `seam.Cmd`.
- R-WIEQ-9TUN: When a command fails because `checkout.Open`, `(*Checkout).Apps`, or `(*Checkout).App` returned an error that `errors.As` matches to a `*NotInCheckoutError`, a `*NoAppError`, or a `*ManifestError`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line on stderr, write nothing further to stdout, and return 2, verified at least by `devctl build bogus` and `devctl --account <name> secrets push foo.sbx.ikigenba.dev bogus` each writing the single line `devctl: no app 'bogus' in the checkout`.
- R-WJMM-NLLC: When a command fails because a method of `*checkout.Checkout` returned an error that `errors.As` matches to a `*GitError`, `cli.Run` MUST write `devctl: ` followed by that error's message to stderr, followed — when the `*GitError`'s `Stderr` is not empty — by one empty line and that `Stderr` unprefixed, MUST write nothing further to stdout, and MUST return 1.
- R-WKUJ-1DC1: When a command fails because `keyring.Lookup` of a name that an app's `Manifest.Secrets` lists returned a `*NoValueError`, `cli.Run` MUST write the single line `devctl: <app>: no value for '<name>' in the keyring or the environment` to stderr, where `<app>` is that app's `Name`, write nothing further to stdout, and return 2, verified at least for `secrets push` and `space create` each reproducing `devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment`.
