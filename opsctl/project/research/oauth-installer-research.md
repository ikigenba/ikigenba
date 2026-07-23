# Research — the `github.com/ikigenba/oauth` release installer

> **Free-form research note** (slug `oauth-installer`). Non-contractual: this
> informs the design author (D11); nothing in the build loop reads it. It is a
> `*-research.md` working note, not the mode-owned `research.md` spine.
>
> Date: 2026-07-23. Authored from the public repo's `README.md` and `install.sh`
> (`github.com/ikigenba/oauth`, `main`), fetched directly.

## What oauth is

A standalone, provider-agnostic OAuth 2.0 login CLI (`oauth`). It runs the full
authorization-code + PKCE handshake against any protocol-compliant service,
serves its own loopback callback, opens a browser, exchanges the code, and writes
the token endpoint's JSON response **verbatim to stdout** (human-facing output
goes to stderr, so stdout can be redirected to a file). It holds no
provider-specific knowledge — a service is described entirely by flags. This is
the tool prompts' `auth=sub` capability drives to produce an `auth.json` for
agentkit. **Where that file lives and how prompts finds it is out of scope for
this session** — this session only gets the binary onto the box.

## The install mechanism (authoritative — from `install.sh` on `main`)

Published install command (README):

    curl -fsSL https://raw.githubusercontent.com/ikigenba/oauth/main/install.sh | sh

`install.sh` (POSIX `sh`, `set -eu`) does, in order:

1. `version="${OAUTH_VERSION:-latest}"`.
2. `bindir="${BINDIR:-${PREFIX:-$HOME/.local}/bin}"` — **`BINDIR` wins**; else
   `$PREFIX/bin`; else `~/.local/bin`.
3. Detect OS (`uname -s`: Linux/Darwin) and arch (`uname -m`: `x86_64|amd64` →
   `amd64`, `arm64|aarch64` → `arm64`); anything else → error + exit 1.
4. `archive="oauth_${os}_${arch}.tar.gz"`. For `latest`, URL is
   `https://github.com/ikigenba/oauth/releases/latest/download/$archive`; for a
   pinned version, `.../releases/download/$version/$archive`.
5. `curl -fsSL "$url" -o "$tmpdir/$archive"` (a `mktemp -d` tempdir, trap-cleaned),
   `tar -xzf` it, `install -d "$bindir"`, then
   **`install -m 0755 "$tmpdir/oauth" "$bindir/oauth"`**.
6. Warn to stderr if `$bindir` is not on `PATH`; print `oauth installed to
   $bindir/oauth`.

### Consequences that shaped D11

- **Override the install dir with `BINDIR`.** `BINDIR=/usr/local/bin` puts the
  binary at exactly `/usr/local/bin/oauth`, overriding the `~/.local/bin` default.
  (`PREFIX=/usr/local` would reach the same path via `$PREFIX/bin`; `BINDIR` is
  the more direct knob and takes precedence.)
- **"Usable by any user" is automatic.** `install -m 0755` makes the file
  world-executable, and `/usr/local/bin` is world-readable and on every user's
  `PATH`. No extra chmod, no per-user install. The install itself is privileged
  (writing `/usr/local/bin`), which fits init-box running as root via `sudo`.
- **Always-latest.** With no `OAUTH_VERSION`, the script pulls the **latest**
  release. Re-running reinstalls the newest build (`install -m 0755` overwrites in
  place), so re-provisioning refreshes oauth rather than pinning it. This is the
  operator's chosen behavior.
- **linux/amd64.** The box is `linux/amd64`; the script fetches
  `oauth_linux_amd64.tar.gz`. Covered.
- **Runtime deps of the installer:** `curl` (with `-fsSL`), `tar`, `mktemp`,
  `install`, `uname`. `mktemp`/`install`/`uname` are coreutils (base). `curl` and
  `tar` are present on AL2023 (curl via `curl-minimal`), but D10 now names `tar`
  and `curl-minimal` in the init-box baseline to make the guarantee explicit
  rather than assumed. `curl-minimal` (not `curl`) avoids the AL2023
  base-image package swap/conflict.
- **Failure is loud.** `set -eu` plus `curl -f` means a network failure, a missing
  release asset, or an unpack failure exits non-zero, which init-box surfaces as
  an aborting error.

## Other install paths (not used)

The README also documents `make build` / `make install [PREFIX=…]` from source
(requires a Go toolchain). D11 uses the prebuilt-binary `curl … | sh` path — no
Go toolchain on the box, and it establishes the `curl … | sh` pattern for future
box-global tools. Version check on the box: `oauth -V`.

## Source

- `github.com/ikigenba/oauth` — `README.md`, `install.sh` (branch `main`), fetched
  2026-07-23 via `raw.githubusercontent.com`.
