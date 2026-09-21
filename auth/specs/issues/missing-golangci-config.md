# Missing golangci-lint configuration

## Filing context

The human-invoked `build-spec` run reached its required lint gate while the
canonical gap was nonempty. The independently verified blocker is present in
the integrated working tree: the tool declared by project ground is installed,
but the configuration that ground says is in this project does not exist.

The current canonical gap contains 46 additions and 2 removals:

- D01 layout and run seam additions (18): `R-3FKW-RNJY`, `R-3GST-5FAN`,
  `R-3I0P-J71C`, `R-3J8L-WYS1`, `R-3KGI-AQIQ`, `R-3LOE-OI9F`,
  `R-3MWB-2A04`, `R-3O47-G1QT`, `R-3QK0-7L87`, `R-3RRW-LCYW`,
  `R-3SZS-Z4PL`, `R-3U7P-CWGA`, `R-3VFL-QO6Z`, `R-3WNI-4FXO`,
  `R-3XVE-I7OD`, `R-3Z3A-VZF2`, `R-40B7-9R5R`, `R-YNFB-36HN`.
- D02 CLI additions (11): `R-OV73-80LN`, `R-OWEZ-LSCC`, `R-OXMV-ZK31`,
  `R-OYUS-DBTQ`, `R-P02O-R3KF`, `R-P1AL-4VB4`, `R-P2IH-IN1T`,
  `R-P4YA-A6J7`, `R-P666-NY9W`, `R-P7E3-1Q0L`, `R-P8LZ-FHRA`.
- D03 serve additions (15): `R-IFR6-6220`, `R-IGZ2-JTSP`, `R-II6Y-XLJE`,
  `R-IJEV-BDA3`, `R-IKMR-P50S`, `R-ILUO-2WRH`, `R-IN2K-GOI6`,
  `R-IOAG-UG8V`, `R-IRY5-ZRGY`, `R-IT62-DJ7N`, `R-KTXG-XSI4`,
  `R-KWD9-PBZI`, `R-KXL6-33Q7`, `R-KYT2-GVGW`, `R-UR0L-ZVDJ`.
- D05 sign-in additions (2): `R-FZI9-R5CX`, `R-J2JU-FY8K`.
- Obsolete test tags to remove from `internal/server/server_test.go` (2):
  `R-IC3H-0QTX`, `R-IDBD-EIKM`.

## Friction

`AGENTS.md` declares both the tool and its project-local configuration:

> `golangci-lint` v2 (config: `.golangci.yml` in this directory)

It also declares the fifth exact gate:

> `golangci-lint run`

There is no `.golangci.yml` in this project. There is also no applicable
configuration at the repository root under any supported filename. As a
result, the installed linter cannot resolve the configuration declared by
ground, and the exact gate cannot be established against its intended policy.

## Evidence

From `/mnt/projects/ikigenba/wip-auth/auth`:

```text
$ golangci-lint version
golangci-lint has version 2.12.2 built with go1.26.5 from (unknown, modified: ?, mod sum: "h1:7+d1uY0bq1MU2UV3R5pW5Q7QWdcoq4naMRXM+gsJKrs=") on (unknown)
$ golangci-lint config path
level=warning msg="No config file detected"
$ echo $?
6
```

Direct checks of the declared project path and the repository root produced:

```text
repo_root=/mnt/projects/ikigenba/wip-auth
absent /mnt/projects/ikigenba/wip-auth/auth/.golangci.yml
absent /mnt/projects/ikigenba/wip-auth/.golangci.yml
absent /mnt/projects/ikigenba/wip-auth/.golangci.yaml
absent /mnt/projects/ikigenba/wip-auth/.golangci.toml
absent /mnt/projects/ikigenba/wip-auth/.golangci.json
```

The canonical commands declared by ground report:

```text
design ids: 155
test ids:   111
additions:   46
removals:     2
```

## Why this is unresolvable in-role

The build run treats project ground as human-authored and read-only. Creating a
lint configuration would choose policy and would manufacture the concrete
ground artifact that `AGENTS.md` already claims exists. The builder cannot
infer that policy from sibling projects, substitute defaults, alter ground, or
skip the declared gate. Therefore it cannot complete or verify the current gap
within its authority.

## Suggested human resolution

Author and add `auth/.golangci.yml` with the intended golangci-lint v2 policy,
or revise the human-owned project ground if a different configuration contract
is intended. Confirm that `golangci-lint config path` resolves the chosen file
from `auth/`, delete this issue, and invoke a fresh `build-spec` run to
recompute and close the remaining canonical gap.
