# Environment observations

Observed 2026-09-14 during draft-spec. Read-only SSH probe on the designated live host (`ssh -o BatchMode=yes -o ConnectTimeout=10 dev`). No host configuration changed.

| Probe | Observation |
|---|---|
| `command -v nginx certbot systemctl` (individually) | `/usr/sbin/nginx`, `/usr/bin/certbot`, `/usr/bin/systemctl` |
| `command -v litestream` | No executable found on PATH |
| `command -v tar xz curl sha256sum aws opsctl` (individually) | `/usr/bin/tar`, `/usr/bin/xz`, `/usr/bin/curl`, `/usr/bin/sha256sum`, `/usr/bin/aws`, `/usr/local/bin/opsctl` |
| `uname -sm` | `Linux x86_64` |
| `nginx -v` | `nginx version: nginx/1.30.4` |
| `certbot --version` | `certbot 2.6.0` |
| `litestream version` | `bash: line 1: litestream: command not found`, exit 127 |

Local authoring environment: `go version` reports `go1.26.5 linux/amd64`; `golangci-lint version` reports `2.12.2`; `llm-lint` and `idgen` are on PATH. No build gates were run. No provider API keys were inspected or printed.

These observations establish tool availability only. They do not prove Litestream grammar, AWS permissions/protocols, CA behavior, release assets, or end-to-end readiness. The presence of AWS CLI does not authorize replacing the stories' SDK dependency.

## Archive capability follow-up

Read-only `ssh dev` probe, 2026-09-14: `command -v zstd` returned `/usr/bin/zstd`; `tar --version` reported GNU tar 1.34; `xz --version` reported XZ Utils 5.2.5; `tar --help` advertises `-J, --xz` and `--zstd` compression filters. This proves advertised grammar and availability; no archive round-trip was performed.

## Archive round-trip follow-up

On `dev`, a Python `TemporaryDirectory(prefix="opsctl-draft-")` held a synthetic `probe.txt` containing `opsctl draft archive probe` and LF. For each of `--zstd` and `--xz`, invoked `tar <filter> -cf <temporary-archive> -C <input-dir> probe.txt` then `tar <filter> -xf <temporary-archive> -C <output-dir>`. Both returned success and extracted bytes matched exactly. The temporary directory was automatically removed. No application or platform files were read or changed. This proves basic compression/container interoperability only, not hostile-member validation or application archive compatibility.
