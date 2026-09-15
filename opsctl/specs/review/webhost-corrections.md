# Webhost correction author review

Fresh correction author: `/root/webhost/webhost_correct`. This is a proposed
contract review, not runtime evidence. Initial independent verifier records in
`webhost-nginx-verification.md` and `webhost-cert-verification.md` remain
unchanged. A new independent verifier must assess this correction.

## Complete affected consumer tasks: current and proposed

Each nginx CLI task uses `cli.Run` with D01 dependencies, EUID 0, an isolated
fixture Root and a recording Execute. Paths below resolve beneath that Root.
Begin with `/etc/nginx/conf.d/ikigenba.conf` containing known bytes and mode;
repeat with the destination absent. Snapshot persistent state before each task.
These are design review exercises for the later build, not tests run here.

| Task and input | Before correction | Proposed completion and observable result |
|---|---|---|
| Invoke `opsctl nginx show extra`, `opsctl nginx apply extra`, and each subcommand with `--unknown`, using an unreadable config fixture. | No-argument grammar lacked explicit rejection precedence and exit behavior. | Each returns 2, no stdout, a D02 diagnostic explaining invalid arguments/options, no configuration reads, no execution and no state changes. Valid missing/unknown-subcommand story fixtures retain their exact diagnostics. R-FSNN-9FKB. |
| With otherwise valid command grammar invoke each of `opsctl nginx show` and `opsctl nginx apply` against corrupt JSON, then a store access failure. | General CLI failure contracts did not explicitly cover reads before the domain call. | Each returns 1 with no stdout and a D02 diagnostic identifying the store failure; no Render/Apply or Execute call and unchanged state, including corrupt bytes. Repair config via the fixture and the ordinary show/apply task can then proceed. R-FTVJ-N7B0. |
| Set `host.name=ikigenba.dev`, make `/opt` discovery fail, and invoke show then apply separately. | Discovery/rendering error propagation was incomplete. | Render returns no candidate bytes and an operational error; Apply propagates it. CLI returns 1 with D02 reason and empty stdout, unchanged destination or absence, no test/reload. Resolve fixture access and retry to obtain the normal candidate. R-G0B8-HTXJ. |
| With valid discovery, make candidate preparation fail, such as denying temporary-file creation in the destination directory; invoke apply. Separately inject a failed atomic publication. | Failure before a published candidate lacked explicit downstream ordering. | Apply returns an operational error; CLI returns 1 with D02 reason and empty stdout; temporary candidate is cleaned up and neither nginx test nor reload runs. Failed preparation preserves destination bytes/mode or absence. A failed rename cannot publish a partial candidate under R-5I4M-D78O. Restore fixture access and retry the full normal publication/test/reload. R-G0B8-HTXJ. |
| Put invalid TOML in `/opt/crm/etc/manifest.toml`; call `nginx.Render(ctx, env, "ikigenba.dev")` and `nginx.Apply(ctx, env, "ikigenba.dev")` separately, then exercise show/apply through CLI. Repeat with an unreadable manifest and with app-name mismatch exposed by D08. | Per-service ManifestError response was unresolved. | Domain returns an operational error identifying crm and its manifest failure; Render supplies no candidate. CLI returns 1, no stdout and D02 reason. No candidate rendering, writes, Execute calls or state change occurs. Correct the manifest and repeat the desired task normally. R-G1J4-VLO8. |
| Set valid crm and dashboard manifests to nonzero ports and both `default=true`; call the same Render/Apply and CLI consumers. | Multiple-default input response was unresolved. | Error identifies conflicting services before rendering/output/writes/Execute; Render supplies no bytes, CLI returns 1 with D02 diagnostic, state unchanged. Set one default false and retry; the remaining default receives the apex under the unchanged rendering requirements. No winner is chosen by directory order. R-G1J4-VLO8. |
| Read setup keys with `config.Store{Root: env.Root}.Get`, map missing keys to empty strings, then call `cert.Obtain(ctx, env, hostName, email)`. Exercise pairs `("", "")`, `("", "ops@ikigenba.dev")`, and `("ikigenba.dev", "")`. | cert CLI guarded empty keys, while D05 passed empty email into a domain whose execution contract required nonempty values. | Obtain returns respectively exact errors `host.name not set`, `host.name not set`, and `acme.email not set`; no Execute or state changes. The caller returns the error through its command's existing diagnostic contract. Supply both keys and retry for normal certbot issuance. R-FRFQ-VNTM. |
| Run init with normalized nonempty host.name and missing/empty acme.email, with fixtures allowing preceding init steps to succeed. | D05 R-A61A-1YZM passed empty email into an undefined domain branch. | The certificate step calls the unchanged Obtain API and receives `acme.email not set`; D05 owns reporting, failed-step handling and omission of later nginx.conf/setup steps. No execution or mutation is performed by Obtain; earlier init steps are not promised rolled back. R-FRFQ-VNTM closes this seam without altering D05. |

Names resolve to D01 (`cli.Run`, dependencies and `host.Env`), D03
(`config.Store.Get`), D06 (`nginx.Render`, `nginx.Apply`), D07 (`cert.Obtain`)
and D08 (`Service.ManifestError`, manifests and discovery). Standard-library
context and errors require no new module dependency. Package ownership and
signatures remain appropriate and unchanged.

## Requirement changes

The initial five IDs were minted together using `idgen -n 5`. This current map includes the subsequent two D06 replacements documented in [webhost-stdout-correction.md](webhost-stdout-correction.md).

| Old ID | Current ID | Change |
|---|---|---|
| None | R-FRFQ-VNTM | D07 domain empty input rejection, host first. |
| R-59LB-OT1T | R-FSNN-9FKB | D06 grammar with explicit extra argument/option rejection. |
| None | R-FTVJ-N7B0 | D06 configuration read failure propagation. |
| None | R-G0B8-HTXJ | D06 discovery/render/preparation/publication failures. |
| None | R-G1J4-VLO8 | D06 invalid manifest and multiple default rejection. |

All other D06/D07 requirement text remains byte-identical. D06 now has 18
requirements, D07 has 20. The five new IDs are absent from the declared test
set; the replaced ID is also absent from tests, so its removal is a design
revision, not a canonical test-removal entry. The parent owns the complete
project implementation gap.

## Resolution and remaining work

NG-USAGE, NG-STRUCT, NG-APPLY and NG-I2/NG-APPS gain explicit failure behavior;
CERT-ENTRY and CERT-UNCONFIGURED gain the domain/init validation seam.
The root resolved malformed manifests and multiple defaults as invalid input
using the apps story's “may never have two” invariant. The resolved
[nginx-input-ambiguities issue](resolved-nginx-input-ambiguities.md) records
that decision. No default-selection policy was added.

The destination state after successful test but failed reload remains the
product choice in [nginx-reload-failure.md](../issues/nginx-reload-failure.md),
immediately affecting that apply branch. Live generated-config, issuance,
hook and renewal behavior remain unproven before check-spec under
[webhost-external-observations.md](../issues/webhost-external-observations.md).
Current host-backup ownership is D13-host-backup.md; D14-service-restore.md
owns service restore. Cross-document integration remains independently reviewed.

No source/test changes, runtime tests, live probes, build gates, build-spec,
check-spec, or commits were performed by this author.

## Current stdout scope correction

D06 now has 22 requirements including Write. The current failure IDs are
R-G0B8-HTXJ and R-G1J4-VLO8; standalone nginx show/apply keeps empty
stdout while composed callers own output and exit policy. See
[webhost-stdout-correction.md](webhost-stdout-correction.md) for replacement
accounting and complete composition tasks. Historical verifier reports remain unchanged.
