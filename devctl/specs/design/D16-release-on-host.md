# D16-release-on-host

A release is the suite built at one commit (D08's suite build), and two
commands put one on a space: `deploy <space> <sha|tag>` (D09) and `space
create` (D07). What they share lives in `internal/release`: turning what the
developer typed into a sha and a label, putting the built tarball on the host,
and having the release's own opsctl activate it. Rollback (D17) needs none of
it, because it builds nothing and copies nothing.

A release is named by a commit, typed as a full sha, a shorter sha, or a tag,
and resolved in the local repository by D04's `ResolveCommit`, fetching
nothing. What is typed decides the label the release carries on the host: a
tag is the label exactly as typed, slashes and all, and a sha, full or short,
gives none. The rule follows `ResolveCommit`'s own classification, so a
string of 4 to 40 lowercase hex digits is always a sha and never a label.
devctl does not judge the label further; opsctl, which writes it, does.

On the host a release lives in its folder, `/opt/ikigenba/releases/<sha>/`,
which is exactly the tarball's one top-level entry, and the opsctl that
belongs to it is `opsctl/bin/opsctl` inside that folder. That path is used
absolutely, because on a fresh host nothing else names opsctl until the first
activate links `/usr/local/bin/opsctl`.

`Put` gets the tarball there in two steps, each one line. `copy` asks the host
for a temporary file and copies the tarball into it with `scp` (D06's
`Copy`). `unpack` makes `/opt/ikigenba/releases/` when the host has none,
owned by root with mode `0755`; keeps a folder already present for that sha
exactly as it is, since the release it holds may be running; and otherwise
extracts the tarball as root into a fresh temporary directory beside the
folders and renames the sha's folder into place, so a folder is either whole
or not there. Extraction gives every file root's ownership, never the
builder's; opsctl's `activate` normalises ownership and modes in any case.
The temporary directory is named with a leading dot, so it is never a
release folder (those are named by a full sha), and it is removed whether the
extraction succeeds or fails. The temporary copy of the tarball is removed
once the unpack is over, whatever its outcome. Each host command is one
`Sudo` or `Run` with literal arguments, so no shell interprets anything.

`Activate` runs `activate <sha> [label]` with the release's opsctl and copies
its stdout to devctl's as it is written, adding no line of its own; a failure
is opsctl's `*host.CommandError`, which `cli.Run` prints as one line with
opsctl's stderr quoted under it. What activate does, and what its lines say,
is opsctl's.

## REQUIREMENTS

- R-WKI3-E9K7: Package `internal/release` MUST export `ReleasesDir = "/opt/ikigenba/releases"`, `Folder(sha string) string`, returning `ReleasesDir`, `/`, and `sha` joined, and `Opsctl(sha string) string`, returning `Folder(sha)` followed by `/opsctl/bin/opsctl`, verified at least by `Folder("4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a")` being `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a` and `Opsctl` of that sha being `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl`.

- R-WLPZ-S1AW: Package `internal/release` MUST export `Label(rev string) string`.

- R-WMXW-5T1L: `release.Label` MUST return the empty string when `rev` is 4 to 40 bytes each an ASCII digit or a lowercase letter `a` to `f`, the form D04's `ResolveCommit` resolves as an object name, and `rev` unchanged otherwise; verified at least by `r1`, `r3-rc1`, `auth/v0.18.2`, `4B22285`, the 3-byte `abc` and the 41-byte `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a0` each returned unchanged, and by `4b22`, `4b22285`, `deadbeef` and `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a` each giving the empty string.

- R-WO5S-JKSA: Package `internal/release` MUST export a `NotCommitError` struct whose only field is `Rev string`, with the methods `Error() string`, returning `'<Rev>' is not a commit`, and `ExitCode() int`, returning 2, verified at least by reproducing `'r9' is not a commit`.

- R-WQLL-B49O: Package `internal/release` MUST export `Resolve(ctx context.Context, c *checkout.Checkout, rev string) (sha, label string, err error)`.

- R-WRTH-OW0D: `release.Resolve` MUST call `(*Checkout).ResolveCommit(ctx, rev)` on `c` exactly once and pass no other `seam.Cmd` to `c.Deps.Exec`; when it reports a commit, `Resolve` MUST return that full sha, `Label(rev)`, and a nil error; when it reports none, the empty strings and a `*NotCommitError` whose `Rev` is `rev` as given; and when it returns an error, the empty strings and that error unchanged; verified at least by `r1` resolving to `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a` with the label `r1`, `4b22285` resolving to the same sha with an empty label, and `r9`, `main` and `HEAD`, with a fake `git` that resolves the last two as revisions but not as tags, each giving a `*NotCommitError` for the argument as typed.

- R-WT1E-2NR2: Package `internal/release` MUST export `Put(ctx context.Context, h host.Host, stdout io.Writer, file, sha string) error` and `Activate(ctx context.Context, h host.Host, stdout io.Writer, sha, label string) error`.

- R-WU9A-GFHR: `release.Put` MUST begin with the `copy` step: exactly one `(host.Host).Run` with the step `copy` and the single argument `mktemp`, whose standard output with its trailing newlines removed is the remote path, then exactly one `(host.Host).Copy` with the step `copy`, `file` as `local`, and that remote path as `remote`, and on success MUST write, through `space.Step`, the line `copy: ok (<base> -> <h.Address>)`, `<base>` being the last element of `file`; when the `mktemp` call fails it MUST return its error unchanged, and when its output is empty an error whose message contains `mktemp`, in either case running nothing further; when `Copy` fails it MUST make exactly one `(host.Host).Run` with the step `copy` and the arguments `rm`, `-f`, and the remote path, then return `Copy`'s error unchanged whatever that removal's outcome, writing no `copy` line and running no `unpack` command; verified at least by reproducing `copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)` for `file` `/w/dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz`, and through `cli.Run` by a fake `scp` exiting 1 with the standard error `No space left on device` giving the stderr lines `devctl: copy: scp /w/dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz ec2-user@18.118.7.42:<remote path>: exit status 1`, an empty line, and `> No space left on device`, with exit 1.

- R-WVH6-U78G: After the `copy` line, `release.Put` MUST run the `unpack` step, every command of which is a `(host.Host).Sudo` with the step `unpack` unless stated otherwise: first `mkdir`, `-p`, `-m`, `0755`, `ReleasesDir`; then `test`, `-e`, `Folder(sha)`, whose exit 0 means the folder is present and whose `*host.CommandError` with `Status` 1 means it is absent; when absent, `mktemp`, `-d`, and `ReleasesDir` followed by `/.unpack.XXXXXXXXXX`, whose standard output with its trailing newlines removed is the temporary directory, then `tar`, `-x`, `-J`, `--no-same-owner`, `-f`, the remote path, `-C`, and the temporary directory, then `mv`, `-T`, the temporary directory followed by `/` and `sha`, and `Folder(sha)`, then `rmdir` and the temporary directory; when present, none of `mktemp`, `tar`, `mv` and `rmdir`; and last, whether present or absent, one `(host.Host).Run` with the step `unpack` and the arguments `rm`, `-f`, and the remote path; on success it MUST write, through `space.Step`, `unpack: ok (<Folder(sha)>)` when the folder was absent and `unpack: ok (<Folder(sha)> already present, kept)` when it was present, and return nil; verified at least by the recorded remote argument vectors of both cases for the sha `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, and by reproducing `unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)` and `unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a already present, kept)`.

- R-WWP3-7YZ5: When a command of the `unpack` step fails, `release.Put` MUST write no `unpack` line and return that first failure's error unchanged, after cleaning up, a failure of the cleanup never replacing it: after a failed `mkdir`, `test` other than with `Status` 1, or `mktemp`, it MUST run only the closing `rm -f` of the remote path; after a failed `tar`, `mv` or `rmdir`, it MUST run one `(host.Host).Sudo` with the step `unpack` and the arguments `rm`, `-rf`, and the temporary directory, then the closing `rm -f`; when the temporary directory `mktemp -d` printed is not `ReleasesDir` followed by `/.unpack.` and one or more bytes none of which is `/`, it MUST run neither `tar` nor any `rm -rf`, run the closing `rm -f`, and return an error naming that output; and when every earlier command succeeded but the closing `rm -f` fails, it MUST return that failure's error; verified at least through `cli.Run` by a fake `tar` exiting 2 with the two standard-error lines `tar: 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/auth/bin/auth: Cannot write: No space left on device` and `tar: Exiting with failure status due to previous errors`, giving the `copy` line alone on stdout and on stderr `devctl: unpack: ssh ec2-user@18.118.7.42 sudo tar -x -J --no-same-owner -f <remote path> -C <temporary directory>: exit status 2`, an empty line, and those two lines each prefixed `> `, with exit 1, the recorded remote vectors holding `sudo rm -rf <temporary directory>` and then `rm -f <remote path>` after the `tar`, and no `mv`.

- R-WXWZ-LQPU: `release.Activate` MUST make exactly one call to `(host.Host).StreamSudo` with `stdout`, the step `activate`, and exactly the arguments `Opsctl(sha)`, `activate`, and `sha`, followed by `label` when `label` is not empty, so that opsctl's standard output reaches `stdout` as it is written and unchanged; it MUST write nothing to `stdout` of its own and return that call's error unchanged; verified at least by the remote argument vectors `sudo /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a r1` for the label `r1` and the same without `r1` for an empty label, by a fake `ssh` writing arbitrary standard output before exiting 0 reproducing exactly those bytes on `stdout`, and through `cli.Run` by one exiting 1 after writing standard output and the standard error `opsctl: activate failed`, an empty line, and `> ikigenba-events.service: Main process exited, code=exited, status=1/FAILURE`, giving that standard output on stdout and on stderr `devctl: activate: ssh ec2-user@18.118.7.42 sudo /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a r1: exit status 1`, an empty line, and those three lines each prefixed `> `, with exit 1.

- R-WZ4V-ZIGJ: `release.Put` and `release.Activate` MUST pass to `deps.Exec` and `deps.Stream` no `seam.Cmd` other than those of the `(host.Host).Run`, `Sudo`, `Copy` and `StreamSudo` calls stated above, and MUST make no cloud call.
