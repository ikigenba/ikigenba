# Ground consumer usage (non-normative)

## Run the project checks

Current and proposed usage are identical, from `opsctl/`, as an ordinary
user with the declared toolchain and provider key. Run each command only
after its predecessor exits 0:

```sh
test -z "$(gofmt -l .)"
go build ./...
go test -race ./...
golangci-lint run
llm-lint cmd internal
```

These commands are review examples; this draft did not execute the gates.
Missing tools, unavailable isolation, and findings fail; none are skipped.
Compute implementation tags with the existing Go-only grep in `AGENTS.md`.

## Exercise installed-host behavior

Current CLI tests use temporary host roots and injected EUID. Proposed
D01 consumers retain that pattern and also inject process, cloud, DNS, and
time boundaries. Complete tasks seed a host tree, call `cli.Run`, capture
both output streams and returned status, and assert effects and preserved
files solely in the temporary tree. Domain tests use the matching root and
`host.Env` fields. No real nginx, certbot, systemctl, Litestream, DNS, or
cloud credentials participate. Production tool observations are collected
on `dev` and recorded separately; they are not permission to invoke them
against the gate machine.

## Exercise the standalone installer

There is currently no installer source to execute. Proposed Go tests under
`cmd/` or `internal/` launch the exact script artifact through Bash, inside
a bubblewrap process tree. This is a harness exercise, not a proposed
installer flag, environment variable, helper export, or additional gate.

For each case Go creates temporary `usr-local`, `etc`, `opt`, `tmp`, and
`case` directories. `case` holds the unmodified script, canned release
assets, and controlled command fixtures. Initial binary/script state and
sentinel configuration belong to those temporary directories. On the
observed Linux layout the invocation shape is:

```sh
bwrap --unshare-user --uid 0 --gid 0 \
  --unshare-pid --unshare-net --unshare-ipc --unshare-uts \
  --die-with-parent --clearenv \
  --ro-bind /usr /usr --symlink usr/bin /bin \
  --ro-bind /lib /lib --ro-bind /lib64 /lib64 \
  --proc /proc --dev /dev \
  --bind "$fixture/usr-local" /usr/local \
  --bind "$fixture/etc" /etc --bind "$fixture/opt" /opt \
  --bind "$fixture/tmp" /tmp --ro-bind "$fixture/case" /case \
  --setenv PATH /case/bin:/usr/bin:/bin --setenv TMPDIR /tmp \
  --chdir /case -- bash /case/install.sh v0.1.0
```

`fixture` is the Go-created temporary directory; the Go harness passes
arguments directly, without interpolating a shell command. Library mounts
follow the installed Bash runtime layout. No host root or home bind is
allowed; `/usr/local` is overlaid before execution. Network and PID
namespaces isolate all descendants. Fixtures need no real credentials.
Namespace uid/gid `1` replace `0` for the non-root refusal case. A failed
sandbox launch fails the test before any installer runs.

A complete successful test provides checksum, candidate binary, and script
assets for the selected version, invokes the installer, and checks exact
streams/status, binary bytes and mode, saved script bytes, and untouched
configuration sentinels. Ownership is read inside the sandbox and asserted
as namespace uid 0; host filesystem uid values are not compared to 0.
Repeat/upgrade cases start from previous fixture installations. Failure
cases supply missing operand, uid 1, checksum HTTP 404, wrong digest, and
wrong candidate version, then assert the D15 output and preservation rules.
Checks of partial-file exposure observe installation transitions inside the
fixture tree. D15 now resolves fresh-install script origin from the fresh
postcondition and missing-operand output under the controlling stderr usage
convention; the harness asserts those finalized contracts.

PATH fixtures provide deterministic downloads; isolation, not PATH, blocks
uncontrolled effects. Absolute programs, Bash redirects, builtin EUID,
and candidate execution remain inside the sandbox. A wrapper-only proposal
in `release-inventory.md` cannot establish these properties by itself.

## Inspect and prepare release artifacts

A Go test builds release artifacts into a temporary directory using the
project's own release entry point once that entry point is implemented.
Inspect the binary with standard-library `debug/elf` for Linux amd64 and
absence of dynamic-loader dependencies, compute its SHA-256 using
`crypto/sha256`, compare the checksum entry and exact asset names, and run
its version command in the same sandbox. A local publication fixture
captures tag and upload inputs and errors without contacting GitHub.
No new Go module, live publication, feature-branch push, or release-tag push
is part of this test task. Actual successful release availability remains
an external observation outstanding in the release scope.

## Local capability evidence

Read-only authoring probes on 2026-09-14 found Bash `5.2.37` and bubblewrap
`0.11.0`. Two local invocations used `--unshare-user`, explicit uid/gid 0
and 1 respectively, `--unshare-pid --unshare-net --unshare-ipc --unshare-uts`,
read-only `/usr`, `/lib`, `/lib64`, a `/bin` symlink, private `/proc` and
`/dev`, and tmpfs mounts for `/tmp` and `/usr/local`. Each executed Bash
printing builtin `EUID` and asserting `/etc/ikigenba` and `/opt` absent.
Both exited 0, with `euid=0` and `euid=1` respectively. No deployment paths
were written. This establishes namespace startup and controlled uid in
this environment, not an implemented installer harness or passed gates.
The future harness must also verify its writable fixture bindings and
artifact behavior. Supporting Bash/bubblewrap tools were accepted within
the coordinator's assigned ground-authoring scope; direct-module approvals
remain unchanged.
