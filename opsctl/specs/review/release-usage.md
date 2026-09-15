# Release consumer tasks (non-normative)

## Fresh host: current and proposed

Current local evidence is `make deploy`, which builds locally, copies a binary,
and remotely installs it; no release installer exists in this project's current
implementation. This was inspected, not executed.

Proposed, using example release data from the story:

```sh
curl -fsSL -o /tmp/opsctl-install https://github.com/ikigenba/ikigenba/releases/download/opsctl/v0.1.0/install.sh
sudo bash /tmp/opsctl-install v0.1.0
# stdout: opsctl v0.1.0 -> /usr/local/bin/opsctl
# stderr empty; exit 0
opsctl version
# stdout: v0.1.0
```

The binary is complete, root-owned, mode 0755; the script is saved; platform
configuration remains unchanged. Task requires a published reachable release;
the live HEAD observation returned 404, so this task was not executed.

## Move the host to a newer version, then repeat

Current: repeat the local Makefile deployment using source at the desired
version. Proposed:

```sh
sudo bash /usr/local/share/ikigenba/opsctl-install.sh v0.2.0
# stdout: opsctl v0.2.0 -> /usr/local/bin/opsctl
sudo bash /usr/local/share/ikigenba/opsctl-install.sh v0.2.0
# stdout: opsctl v0.2.0 -> /usr/local/bin/opsctl
opsctl version
# stdout: v0.2.0
```

Both installs exit 0 with empty stderr. First command retains the target
release's script; repeat downloads and verifies again, retaining identical
binary bytes and leaving the saved installer unchanged. Neither configures the host. A caller may separately invoke
D05's `opsctl init`; that is a separate task whose readiness comes from its own
preconditions, and no sibling developer-tool implementation is designed here.

## Script release differs from binary operand

```sh
curl -fsSL -o /tmp/opsctl-install https://github.com/ikigenba/ikigenba/releases/download/opsctl/v0.1.0/install.sh
sudo bash /tmp/opsctl-install v0.2.0
opsctl version
# stdout: v0.2.0
```

On a fresh host with both releases available and the declared host prerequisites,
the binary and checksum come from v0.2.0. The saved script is the executing
v0.1.0 script. The installation prints `opsctl v0.2.0 -> /usr/local/bin/opsctl`
and a newline to stdout, with empty stderr and exit 0. A subsequent upgrade
using that saved script retains the requested release's script.

## Missing version and ordinary user

```sh
sudo bash /tmp/opsctl-install
# exit 2; stdout empty; exact stderr: opsctl-install: needs a version (then newline)
bash /tmp/opsctl-install v0.1.0
# as non-root: exit 3; stdout empty
# stderr: opsctl-install: must run as root
```

Both tasks require the fetched installer to be present. Neither changes the
host or downloads. Missing-version stderr contains only the diagnostic and
one trailing newline: the controlling user AGENTS forbids usage on stderr.
This deliberately adjusts the story's output example; see release-inventory.md.
No replacement help surface is introduced.

## Missing release, bad checksum, and wrong embedded version

Preconditions: installer present, effective uid 0, declared host tools, and a
fixture checksum request for nonexistent v9.9.9 returning HTTP 404.

```sh
sudo bash /tmp/opsctl-install v9.9.9
# exit 1; stdout empty; stderr:
# opsctl-install: no release for v9.9.9
#
# https://github.com/ikigenba/ikigenba/releases/download/opsctl/v9.9.9/checksums.txt: 404
```

For checksum failure, the installer is present and runs as root with the
declared host tools. The download fixture for v0.1.0 supplies a checksums entry
with the expected digest below and candidate bytes `hello` followed by a
newline, whose digest is the actual value below. Any prior installed binary
and saved script are fixture files whose bytes are recorded before invocation.

```sh
sudo bash /tmp/opsctl-install v0.1.0
# exit 1; stdout empty
```

Exact stderr (including the final newline):

```text
opsctl-install: checksum mismatch for opsctl-v0.1.0-linux-amd64

expected e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
     got 5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03
```

For version failure, use a separate fixture with the installer present, root
uid and declared tools. The v0.1.0 download fixture supplies an executable
candidate whose `version` command succeeds and prints `v0.0.9` followed by a
newline; its checksums entry matches that candidate's actual digest. Record
any existing installed binary and saved script before invocation.

```sh
sudo bash /tmp/opsctl-install v0.1.0
# exit 1; stdout empty
```

Exact stderr (including the final newline):

```text
opsctl-install: asked for v0.1.0 but the binary reports v0.0.9
```

Both diagnostics are stderr only, exit 1. All three failures preserve the
previous binary and saved script; checksum failure removes the candidate, and
version mismatch never renames it into place. Existing files may be absent on
a fresh host. No current installer exists to offer corresponding current tasks.

## Publish a release

Proposed trigger is a release tag of shape `opsctl/<version>`; consumers receive
exactly the named binary, checksums file, and installer at D15's URLs. This
review does not execute tagging, publication, or installation. Current local
build/deploy targets provide no evidence of this release publication contract.

## Names resolve to the contract

D15 structural requirements declare release tag/asset/URL shape, checksum entry,
installer invocation and persistent paths. D02 declares the binary version
surface; D05 declares init. `curl`, Bash and `sudo` are host tools used by the
story, not additional opsctl commands. Example numeric versions are release
data, never a dependency-version requirement.
