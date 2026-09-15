# Direct dependency baseline

Status: resolved in D01 under the user’s instruction to fix the blocking issues.

## Recorded pins

These are the existing ten direct module paths in D01, with concrete versions
resolved on 2026-09-15. No additional direct module was added.

| Module | Version |
| --- | --- |
| `github.com/BurntSushi/toml` | `v1.6.0` |
| `github.com/aws/aws-sdk-go-v2` | `v1.47.0` |
| `github.com/aws/aws-sdk-go-v2/config` | `v1.33.5` |
| `github.com/aws/aws-sdk-go-v2/service/ec2` | `v1.332.0` |
| `github.com/aws/aws-sdk-go-v2/service/iam` | `v1.64.0` |
| `github.com/aws/aws-sdk-go-v2/service/route53` | `v1.70.0` |
| `github.com/aws/aws-sdk-go-v2/service/s3` | `v1.113.1` |
| `github.com/aws/aws-sdk-go-v2/service/ssm` | `v1.78.0` |
| `github.com/aws/aws-sdk-go-v2/service/sts` | `v1.51.0` |
| `github.com/aws/smithy-go` | `v1.28.1` |

## Verification

A temporary module outside the project at `/tmp/devctl-dependency-probe`
imported all ten modules, resolved their current versions with `go get`, and
completed `go mod tidy` and `go build ./...` under Go 1.26.5. Its resolved
`go.mod` contains exactly the ten direct requirements above. This establishes
that the recorded set resolves and compiles together; it does not exercise
all service operations or constitute the project's build run. No project
source or `go.mod` was changed by this probe.

## Authorization and provenance

The current D01 records the ten module paths without versions. The existing
`go.mod` contains no requirements. The history of D1/D01 and `go.mod` contains
no approved concrete pins for this set. Commit
`3c6ff11b63b8dcf1ce9f21a209913e3c1ac8a5fe` explicitly removed version pins from
requirements; its D1 prose nevertheless continued to describe human-approved
pins, without recording them. The earlier compilation observation also omitted
versions (see [earlier observations](earlier-observations.md)). Successful
resolution is not evidence of human approval.

## Resolution

The ten module paths already had human approval. The user explicitly directed
this review to fix the blocking issues. Selecting compatible versions for that
existing set is part of the authorized baseline correction; neither the ground
nor the authoring workflow requires a separate approval round for this edit.
The versions above were selected and verified by the agents under that
instruction; this record does not claim the user personally selected releases.

D01 now records the exact ten module/version pairs in its dependency
requirement and requires the `go.mod` test to compare both fields. The existing
`AGENTS.md` instruction to record pins in D1 takes precedence over generic
skill guidance that places versions outside requirements. Requirement
`R-UNLP-X6MH` was replaced by freshly minted `R-DM8X-DRGU`.

The dependency-baseline issue is closed. Populating the project `go.mod` and
writing the gate test remain implementation work for the human-started build
run. No project dependency data or source was changed during this review.
