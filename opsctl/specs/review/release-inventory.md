# Release inventory (non-normative)

Input: `specs/stories/release.md`, read from worktree based on commit
`1e9b63f0254d153fa7a041e86649cfcbb994bfb4`. Labels below are stable source
locators: P is the preamble; 01–08 are the successive scenario headings.
All mapped requirements are in `../design/D15-release.md` unless stated.
These are author coverage claims awaiting independent verification.

| Criterion | Source outcome | Requirement / status |
|---|---|---|
| REL.P.01 | Preamble: host installs tool without building; separate installer only | R-UQ51-HSPA, R-URCX-VKFZ, R-UNP8-Q97W, R-UOX5-40YL |
| REL.P.02 | The release: tag shape, three assets, URL | R-IH58-0PHG, R-UL9F-YPQI, R-UMHC-CHH7; external evidence pending |
| REL.P.03 | Binary is static Linux amd64 | R-UL9F-YPQI, R-UMHC-CHH7 |
| REL.P.04 | No new top-level usage command | R-UNP8-Q97W, R-UOX5-40YL |
| REL.P.05 | Operand determines binary even with different script release; no fetcher replacement first | R-IOGM-BBXM, R-USKU-9C6O |
| REL.01.01 | Fresh host: curl fetch then bash installer operand | R-IH58-0PHG, R-UNP8-Q97W, R-UOX5-40YL, R-UQ51-HSPA, R-URCX-VKFZ |
| REL.01.02 | Fresh success exact output, streams, exit | R-IS4B-GN5P |
| REL.01.03 | Linux tool/root/network prerequisites | R-UQ51-HSPA, R-URCX-VKFZ; host environment observations |
| REL.01.04 | Requested release exists with assets (precondition) | R-IH58-0PHG; not observed available |
| REL.01.05 | Binary path, requested version, permissions, ownership, atomic rename | R-IPOI-P3OB, R-IQWF-2VF0; D02 R-NAKC-ICYV |
| REL.01.06 | SHA-256 matches selected release | R-IKSX-60PJ, R-IQWF-2VF0 |
| REL.01.07 | Saved executing script reusable later | R-USKU-9C6O; applies also across different script/operand releases |
| REL.01.08 | No platform configuration change | R-IVS0-LYDS |
| REL.02.01 | Upgrade via saved script, requested operand, no preliminary fetcher fetch | R-UNP8-Q97W, R-UOX5-40YL, R-USKU-9C6O, R-IOGM-BBXM |
| REL.02.02 | Upgrade success exact output/streams/exit | R-IS4B-GN5P |
| REL.02.03 | Existing old install and new release (preconditions) | R-IUK4-86N3 |
| REL.02.04 | New binary and requested release script retained | R-IUK4-86N3; upgrade precondition differs from first installation |
| REL.02.05 | Config store/nginx/units/opt untouched; init later separately | R-IVS0-LYDS |
| REL.02.06 | Same version repeats successfully with same bytes | R-0I5P-90PU |
| REL.02.07 | External developer tool runs installer then init | Consumer context only: opsctl exposes installer here; D05 owns init; no sibling implementation prescribed |
| REL.03.01 | Invoke same saved script/version with existing version | R-UNP8-Q97W, R-UOX5-40YL, R-0I5P-90PU |
| REL.03.02 | Repeat success exact output/streams/exit | R-IS4B-GN5P |
| REL.03.03 | Binary byte-identical, download and checksum repeated | R-0I5P-90PU |
| REL.03.04 | Nothing else did | R-IVS0-LYDS, R-0I5P-90PU; saved installer unchanged and no host configuration action, consistent with required verification before success |
| REL.04.01 | No default; missing operand with installer present | R-IOGM-BBXM, R-UTSQ-N3XD |
| REL.04.02 | Exact diagnostic plus usage on stderr, stdout empty, exit 2 | R-UTSQ-N3XD; exact diagnostic-only stderr follows controlling user AGENTS; deliberate story-output adjustment explained below |
| REL.04.03 | Nothing changed | R-UTSQ-N3XD |
| REL.05.01 | Ordinary effective uid invocation | R-IZFP-R9LV |
| REL.05.02 | Exact root refusal stderr, empty stdout, exit 3 | R-IZFP-R9LV |
| REL.05.03 | Refusal before downloads; nothing changed | R-IZFP-R9LV |
| REL.06.01 | Unreleased operand; checksum URL 404 | R-J0NM-51CK; 404 observed, release nonexistence not independently established |
| REL.06.02 | Exact no-release diagnostic/URL, streams, exit 1 | R-J0NM-51CK |
| REL.06.03 | Nothing changed; existing binary and saved script preserved | R-J0NM-51CK |
| REL.07.01 | Truncated/wrong/tampered bytes yield unequal checksum | R-IQWF-2VF0, R-J1VI-IT39 |
| REL.07.02 | Exact mismatch diagnostic with expected/actual digest; streams/exit | R-J1VI-IT39 |
| REL.07.03 | Nothing installed; download removed; existing binary preserved | R-J1VI-IT39 |
| REL.08.01 | Requested version differs from binary embedded version | R-UL9F-YPQI, R-UMHC-CHH7, R-IQWF-2VF0, R-J4BB-ACKN |
| REL.08.02 | Exact version mismatch diagnostic; streams; exit 1 | R-J4BB-ACKN |
| REL.08.03 | Previous binary retained; candidate never renamed | R-IQWF-2VF0, R-J4BB-ACKN |

39 criteria are mapped: 38 normative outcomes and 1 external-consumer context
(REL.02.07). The initial verifier passed 37 and requested correction of
REL.01.07 and REL.04.02; both corrections await fresh verification here.
External observations still block checking release asset availability.

## Corrections to initial review

First installation retains the executing script even when its release differs
from the operand. Upgrade using the saved installer retains the requested
release's script. These are separate story preconditions, so they require no
unresolved origin decision.

REL.04.02 deliberately adjusts the story's stderr example: the controlling
user-provided ancestor AGENTS command-line convention says “The usage text is
never written to stderr.” The contract therefore specifies only the diagnostic
and its trailing newline, with empty stdout, exit 2 and no effects. It adds no
help interface and leaves no unspecified trailing output. The story file stays
unchanged. The obsolete origin and missing-version issue files are removed.

## Current implementation evidence

Read-only local inspection: `Makefile` defines build/deploy using Go, scp and
remote install; `internal/cli/cli.go` has source `version` and dispatches its
bare output. No install/release script was found under this project by file
search. This establishes current usage only; no existing release was assumed.
The proposed contract does not preserve Makefile deploy as the consumer path.
D02 already owns the version string shape and output, so D15 adds no Go export
or package. D01's owner was notified of the standalone installer boundary.

## Testability within existing ground

Future Go tests remain under `cmd/` or `internal/` and the existing ordered
Go gates remain unchanged. Follow the project's `AGENTS.md` sandbox ground:
execute the unmodified installer inside bubblewrap with fresh writable
fixture paths, isolated networking, and explicit namespace uid 0/nonzero.
Read-only host tools and runtime libraries may be mounted; no live deployment
paths, home, credentials, or communication sockets are exposed. The boundary
covers Bash and every descendant, including candidate execution; wrappers
alone cannot contain shell redirects, absolute paths, or Bash's builtin EUID.
Missing tools/namespaces fail the harness before installer execution. This is
not a public root override or a test-only installer option. Requirement tags
remain solely in the declared Go tests. See `ground-usage.md` for capability
probes and their limits.
Artifact/tag publication may be checked through deterministic publisher
fixtures and local artifact inspection; an actual remote publication is not
a test gate and this draft does not authorize one.

## External evidence

Root-coordinated read-only observation on `dev`: at response date
`Tue, 15 Sep 2026 03:28:55 GMT`, ran `curl -sSIL --max-time 20` for each URL:

- `https://github.com/ikigenba/ikigenba/releases/download/opsctl/v0.1.0/install.sh`
  returned `HTTP/2 404`, content type `text/plain; charset=utf-8`, length 9;
  GitHub request id `9C92:321CCD:2A49A55:36BB452:6AA8BB76`.
- `https://github.com/ikigenba/ikigenba/releases/download/opsctl/v9.9.9/checksums.txt`
  returned `HTTP/2 404`, content type `text/plain; charset=utf-8`, length 9;
  GitHub request id `9C9A:12E3A1:2B7B0CD:3845134:6AA8BB77`.

Both HEAD requests completed without redirects. This proves reachability and
observed 404 responses, not why GitHub returned 404 (missing/private release),
asset availability, or successful asset download. No installation occurred.
Tool observations are in `environment-observations.md` when supplied by root.
See `../issues/release-external-observations.md` for the remaining check blocker.
