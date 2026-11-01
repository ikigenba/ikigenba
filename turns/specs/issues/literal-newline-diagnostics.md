# Literal diagnostic values conflict with fixed line counts

Filed during the user-invoked first `build-spec` run for `turns`, against design commit `47ac54fc548849f93ac7f97b28ccb190aba275d3`. Workflow's fresh run and install/status verifiers reported these conflicts with synthetic overlay probes. A separate fresh root validator independently reproduced all six cases through `cli.Run` and checked the shared diagnostic framing rules.

## Affected requirements

- `R-JD6A-YEOY` requires exactly one diagnostic line containing the literal invalid space name.
- `R-WMHL-EWTW` requires exactly three lines on a configure write failure, with the literal failed file path in the first line.
- `R-WNPH-SOKL` requires exactly three lines on a configure directory-creation failure, with the literal `Paths.Config` directory in the first line.
- `R-HOKG-YQ0R` requires exactly one diagnostic line containing the paths-resolution error text. `R-IIOT-E55L` requires that error to contain the literal relative home value.
- `R-JOW4-66TZ` requires the literal user-unit directory in the first install error line, followed by an empty line and exactly one error-detail line.
- `R-JRBW-XQBD` requires the literal unit file path in the first install error line, followed by an empty line and exactly one error-detail line.
- `R-WAR4-G0HT` requires the shared diagnostic detail framing. The same six cases violate its empty second line and detail-line layout, so this dependent requirement cannot be closed concurrently with the literal substitutions above.

No requirement excludes newline bytes from a supplied invalid space name, a relative home value or an absolute XDG configuration directory. `R-IJWP-RWWA` and `R-IL4M-5OMZ` allow the injected absolute configuration paths used in the proof.

## Independent executed proof

The root validator created temporary overlay tests outside the repository using `mktemp "${TMPDIR:-/tmp}/scratch.XXXXXX"`. The tests call `cli.Run` directly with injected ordinary-user uid, environment, temporary home and system directories, fake HTTP/process dependencies, and synthetic fixtures. No real home, environment, credentials, session files, network or external process was used.

From `turns/`, both proof commands exited zero because the probes assert the contradictory observations rather than claiming compliance:

```sh
GOPROXY=off GOSUMDB=off go test -overlay /tmp/scratch.3wwqzw ./internal/cli -run '^TestFreshConfigureContradictionProof$' -count=1 -v
GOPROXY=off GOSUMDB=off go test -overlay /tmp/scratch.Uz4qSf ./internal/cli -run '^TestFreshInstallStatusContradictionProof$' -count=1 -v
```

The observations were:

| Case | Synthetic input or failure | Exit | Stderr lines |
| --- | --- | --- | --- |
| Invalid configure space | `bad\nspace`, with a newline byte | 2 | 2 |
| Configure file write | Newline-bearing XDG directory; non-empty directory blocks token replacement | 1 | 6 |
| Configure directory creation | Newline-bearing XDG path is a regular file | 1 | 5 |
| Status paths resolution | Relative home `relative\nvalue`, with a newline byte | 1 | 2 |
| Install directory creation | Newline-bearing XDG path is a regular file | 1 | 5 |
| Install unit write | Newline-bearing XDG path; non-empty directory blocks `turns.service` | 1 | 4 |

For example, invalid-space stderr was `"turns: 'bad\nspace' is not a space name\n"`, and status stderr was `"turns: HOME 'relative\nvalue' is not an absolute path\n"`. In these quoted representations, `\n` denotes an actual newline byte. Every required literal summary contained a newline. An escaped candidate had no embedded newline but was unequal to the required literal substitution.

For all six cases the independent framing probe reported:

```text
five_codes=true nonzero_EUID=true prefix_LF=true diagnostic_detail=false no_usage_stderr=true success_silent=true stdout_product=true
```

The probe did not establish a reason to leave any of the other six shared framing ids uncovered; the injected uid was nonzero, including for the scope obligation in `R-CIE0-1WAF`.

## Why this is unresolvable in the build

A required first line containing a literal newline-bearing value necessarily contains an additional line break. No choice of following detail can remove that byte. Escaping the supplied value changes the required text; rejecting the value still invokes the literal invalid-input diagnostic; silently restricting filesystem path bytes changes the accepted-input contract. The builder cannot choose among these changes to the read-only design.

These seven ids remain open and their coverage claims are removed. Independent requirements, including propagation of an actual error where no fixed line count is required, can remain verified. This issue is separate from `config-load-newline-error.md`, which records the corresponding contradiction inside `config.Load` itself.

## Suggested resolution

Replace the affected requirements outside this build with newly minted ids that consistently specify an escaped representation of supplied names, paths and error text, or relax the fixed line-count guarantees. Reconcile the shared framing rule with the chosen representation. Resolve and delete the issue, then start a new `build-spec` run to recompute the gap.
