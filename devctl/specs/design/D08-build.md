# D08-build

Build produces one linux/amd64 artifact from a clean commit named by that app’s
own version tag. The binary’s reported version and emitted manifest must match
the tag and committed manifest. Branch ancestry imposes no restriction. Artifact
publishing preserves an earlier file on failure. A committed manifest that
still names a `port` is refused when the app is resolved, before anything is
compiled (D04 R-J90Z-RU8G), so no file opsctl would refuse is ever written.

## REQUIREMENTS

- R-63VS-7J2D: Package `internal/build` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout` and no profile name.

- R-67JH-CUAG: Package `internal/build` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage` when `Help` is not empty and the empty string when `Help` is empty, and `ExitCode() int`, returning 2.

- R-69ZA-4DRU: Package `internal/build` MUST export a `StaleManifestError` struct whose only field is `App string`, with the methods `Error() string`, returning `<App>: etc/manifest.toml does not match what the binary emits; run '<App> manifest > <App>/etc/manifest.toml' and commit`, and `ExitCode() int`, returning 2.

- R-6DMZ-9OZX: When `--help` or `-h` appears anywhere among the arguments `build.Run` is given, `build.Run` MUST write the `build` usage text to `stdout`, return a nil error, call no method of `*checkout.Checkout`, and pass no `seam.Cmd` to `deps.Exec`, verified at least by `devctl build --help` and `devctl build crm --help` each printing that text to stdout with empty stderr and exit 0.

- R-6EUV-NGQM: `build` MUST take exactly one `<app>` operand and MUST accept no option other than `--help` and `-h`.

- R-6G2S-18HB: `devctl build` with no operand MUST write exactly the three lines `devctl: build needs <app>`, an empty line, and `see 'devctl build --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-6HAO-F080: `devctl build` with more than one operand MUST write exactly the three lines `devctl: build takes one <app>`, an empty line, and `see 'devctl build --help' for usage` to stderr, write nothing to stdout, and exit 2.

- R-6IIK-SRYP: An argument of `build` that begins with `-` and is neither `--help` nor `-h` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl build --help' for usage` to be written to stderr, nothing to stdout, and exit 2.

- R-R7Z4-R65Q: `cli.Run` MUST dispatch the command `build` to `build.Run`, passing the arguments that follow `build`, the `stdout` writer `cli.Run` was given, and `deps`, MUST return 0 when `build.Run` returns a nil error, and `build` MUST call `deps.Cloud` not at all, verified with a recording fake `Deps.Cloud` that is left with no call by `devctl build crm`.

- R-RBMT-WHDT: `build` MUST NOT read the root file `infra/terraform.tfvars.json`, verified at least by `devctl build --help` and `devctl build crm` each producing the same stdout, stderr, and exit code in a checkout whose root file is absent, in one whose root file is malformed, and in one whose root file is well-formed.

- R-6M69-Y36S: When `(*Checkout).Clean` returns false, `build` MUST return a `*UsageError` whose `Message` is `the working tree has uncommitted changes; commit them first` and whose `Help` is empty, so that `devctl build crm` writes that message as the single stderr line `devctl: the working tree has uncommitted changes; commit them first`, writes nothing to stdout, and exits 2.

- R-6UPK-MHDN: `build` MUST compare that process's standard output with the contents of the file at `checkout.ManifestFile` under the app's `Dir` and MUST return a `*StaleManifestError` for the app's `Name` when the two are not byte-identical, verified at least by reproducing the single stderr line `devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit` with empty stdout, exit 2, and no `seam.Cmd` whose `Path` is `tar` passed to `deps.Exec`.

- R-04SM-T2LN: On a successful build `build` MUST write to `stdout` exactly `File(<app>, <tag>)` followed by one newline and nothing else, write nothing to stderr, and exit 0, verified at least by reproducing the single line `crm/dist/crm-v0.1.0.tar.xz` for the app `crm` at the tag `v0.1.0`; and `build` MUST write nothing to `stdout` in every outcome in which it returns a non-nil error.

- R-70T2-JC34: When `deps.Exec` returns a non-nil error for the `go` `Cmd`, the app binary's `Cmd`, or the `tar` `Cmd`, `build` MUST return an error that `errors.As` does not match to a `*ProcessError` and whose message contains that `seam.Cmd`'s `Path`, so that `cli.Run` reports it as a single stderr line and exit 1.

- R-EM3N-CKUL: Package `internal/build` MUST export a `ProcessError` struct whose fields are exactly `Label string`, `Status int`, and `Stderr string`, with the methods `Error() string`, returning `<Label>: exit status <Status>`, `Detail() string`, returning `seam.QuoteOutput(Stderr)`, and `ExitCode() int`, returning 1.

- R-HGAQ-5Q0J: `devctl build --help` and `devctl build -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0; subject to the superuser refusal, help MUST work without reading the root file and before any external operation:

  ```
  Usage: devctl build <app>

  Build <app> for linux/amd64 and write <app>/dist/<app>-<version>.tar.xz, the
  file deploy copies to a host and opsctl installs. HEAD must be a commit that
  the app's version tag (<app>/v<semver>) points at, with no uncommitted
  changes.
  ```

- R-EOJG-44BZ: Build MUST validate the app name, resolve the checkout and app, require a clean working tree, and resolve HEAD and its app-specific version tags before compilation; a refusal MUST leave dist untouched.

- R-EPRC-HW2O: When no tag at HEAD matches `appref.VersionForTag` for the requested app, build MUST return a `UsageError` with message `no tag <app>/v<semver> points at HEAD (<head>)` and empty help, yielding empty stdout and exit 2.

- R-EQZ8-VNTD: When multiple matching app tags name HEAD, build MUST select the lexicographically first complete tag and use only its version suffix in `File` and stdout; unrelated tags MUST not affect selection.

- R-RAEX-IPN4: Build MUST accept matching release, prerelease and metadata tags on any branch or detached HEAD and MUST NOT test reachability from `origin/main`.

- R-EUMY-0Z1G: Build MUST reject an app name for which `appref.ValidName` is false before compiling or writing dist, with a `UsageError` whose message is `'<app>' is not a usable app name` and whose help is empty, yielding exit 2.

- R-J7ER-X23Q: Build MUST compile the selected app exactly once with Go for linux/amd64 with cgo disabled, into temporary output under that app’s dist directory, by passing to `deps.Exec` exactly one `seam.Cmd` whose `Path` is `go`, whose `Dir` is the app’s `Dir` so its own module is used, and whose `Args` name the package path `./cmd/<app>`, where `<app>` is the app’s `Name`, as the package to build; it MUST not inject a version at build time. A compiler nonzero exit MUST become ProcessError labeled `build <app>` with its exit status and stderr.

- R-EX2Q-SIIU: After compilation and before publishing an archive, build MUST execute that same staged binary with `--version` through `Deps.Exec`, remove only trailing newlines from stdout, and compare it byte for byte with the selected version suffix; a mismatch MUST return `UsageError` with message `<app>: tagged <app>/<version> but the binary reports <reported>` and empty help, with no dist artifact changed.

- R-EYAN-6A9J: Failure of the staged binary’s `--version` command MUST use `ProcessError` labeled `<app> --version` for a nonzero exit and an error naming its path for failure to start, with no artifact published.

- R-EZIJ-K208: Build MUST create an xz-compressed tar artifact whose relative root members are exactly `bin/<app>`, the emitted `etc/manifest.toml`, the other regular files under the app’s `etc/`, and optional `share/` files with their original relative paths and bytes; no archive member path MUST contain a version directory. Compression failures MUST use `ProcessError` and preserve an earlier artifact unchanged.

- R-F0QF-XTQX: Build MUST replace the final artifact only after all compilation, version, manifest and archive operations succeed; any failure MUST preserve earlier files and remove temporary output. The static linux/amd64 executable MUST remain executable inside the archive.

- R-F1YC-BLHM: Package `internal/build` MUST export `DistDir(app string) string` and `File(app, version string) string`.

- R-F368-PD8B: `build.DistDir` MUST return `<app>/dist`; `build.File` MUST return `<app>/dist/<app>-<version>.tar.xz`, preserving the supplied version verbatim, including prerelease and metadata suffixes.

- R-F4E5-34Z0: After successful compilation, build MUST execute the staged executable with the single argument `manifest` through Deps.Exec and use that stdout as the emitted manifest; a nonzero exit MUST yield ProcessError labeled `<app> manifest` carrying the exit status and stderr.

- R-F5M1-GWPP: On successful build the artifact MUST contain the same executable used for version and manifest validation, its emitted manifest, and unchanged bytes for all other regular files under the app’s etc directory and optional share directory.

- R-F6TX-UOGE: Build MUST write only under the selected app’s dist directory, creating it if needed; on return it MUST leave only the completed artifact and paths that existed before invocation, with temporary paths removed on success and failure.
