# External observations, 2026-09-15

These are authoring observations, not a build run or implementation gates.
No devctl source or sibling source was built or inspected for this work.

## Published releases

`gh api --paginate 'repos/ikigenba/ikigenba/releases?per_page=100'` succeeded
using the CLI's existing authentication. The response contains nine releases,
all non-draft and non-prerelease, for agent-repl, oauth and idgen. No `opsctl/`
tag or opsctl installer asset exists in that response. The
[relevant fields](github-release-observation-2026-09-15.json) are retained.
`opsctl` is absent from PATH. Neither the authenticated response nor the older
public response supplies an installed opsctl interface to observe. Nine results
do not exercise a multiple-page response.

## Local Git, Go and tar

A disposable checkout outside the project contained a standalone, dependency-free
Go fixture called `crm-api`, an embedded manifest, another etc file and a share
file. The fixture is deliberately not a sibling application. Its
[command transcripts](external-observation-fixture-2026-09-15.json) record
stdout, stderr and exit status separately, with temporary paths normalized.

Observed successfully:

- `git rev-parse --show-toplevel` from the app subdirectory resolves the checkout;
  `git rev-parse HEAD` returns the commit. `git status --porcelain` is empty for
  a clean checkout and reports an untracked file after one is created.
- `git tag --points-at HEAD` reports both lightweight and annotated app tags,
  including `crm-api/v1.2.3-rc.1+build.7`, alongside an unrelated app tag. The
  same tags are returned after detaching HEAD. Tag filtering and selection remain
  devctl behavior to implement and test.
- Go 1.26.5 compiled the fixture with `GOOS=linux`, `GOARCH=amd64`,
  `CGO_ENABLED=0`, `GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local`, using its
  own module directory and no linker version injection. `file` reports an x86-64
  ELF executable, statically linked. The staged executable ran successfully:
  `--version` emitted `v1.2.3-rc.1+build.7` and `manifest` emitted bytes identical
  to the committed fixture manifest.
- `tar -c -J` made an xz archive named
  `crm-api-v1.2.3-rc.1+build.7.tar.xz`; D09's `tar -t -J -f` and
  `tar -x -J -O -f ... etc/manifest.toml` forms both succeeded. Members were
  exactly `bin/crm-api`, `etc/manifest.toml`, `etc/settings.txt`, and
  `share/page.txt`. Each member's bytes matched the staged file, and the binary
  retained executable mode. There was no version-directory prefix.

These observations close the outstanding local tool feasibility item. They do
not prove that any real app exposes `--version` and `manifest`, or that devctl
implements the contract.

## Literal shell arguments

A local `/bin/sh -c` experiment preserved spaces, single quotes, literal command
substitution, semicolons, newlines, wildcard characters and leading hyphens
when each value was shell-quoted. NUL-delimited output was compared with every
original value. This validates the local shell boundary only. No SSH connection,
remote shell, cancellation or host service behavior was exercised.

## AWS account and discovery reads

Read-only AWS CLI calls using the existing sandbox and production profiles
succeeded. The [recorded results](aws-observations-2026-09-15.json) contain
account configuration and discovery metadata, not credentials or application
secrets. Both `/ikigenba/account` values have all eleven expected keys with the
expected types. Sandbox backup periods are all zero and both deletion flags are
true. Production host/files/database periods are 86400 seconds, WAL is 900
seconds, and both deletion flags are false. Backup schedules are account
configuration; these values are observations, not new fixed requirements.

EC2 instance and address queries with the Project and Space tag filters returned
empty lists in both profiles. Each account's hosted-zone list contains its
configured domain with a trailing dot and a `/hostedzone/`-prefixed ID. These
reads validate the account-property and empty discovery response shapes, and
the zone normalization inputs. They do not prove mutation permissions, a
populated host response or multiple-page handling. No target host was found for
host command observations. No resource was created or changed to obtain proof.

## Remaining external evidence

The open [external observations issue](../issues/external-contract-observations.md)
tracks the installed opsctl and target-host observations needed before
check-spec. The absence of a released installer or provisioned host is an
environment precondition for those observations, not an inconsistency in the
story or a requirement to create infrastructure during this review.
Documentation or a constructed fixture cannot substitute for live observations
of the interface. No check-spec baseline is claimed here.
