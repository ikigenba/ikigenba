# Original-story fidelity: remote commands

Reviewed D09–D13 against the original `deploy.md`, `restore.md`, and
`space-lifecycle.md` stories, read in full from the preserved pre-review copy.
The direction is stories to design. No story was edited for this review.

## Correction

D09's restore help had gained a fresh-host explanation and changed the original
unit-stop wording. Replaced that requirement with R-FHIL-ORT8 so its help block
matches the original restore story byte for byte. R-3S9B-ATSP is retired.
The new requirement appears in the design-minus-test gap.

## Contract coverage reviewed

| Original workflow | Design coverage |
| --- | --- |
| Build-file validation before cloud access; deploy step order; missing secrets; promotion and prereleases; retained upload after failed install | D09 file validation, secret validation, upload and install requirements |
| Restore latest or at an unchanged timestamp; fresh-host restore before deploy; absent-space and host failures | D09 restore requirements; the host decides restore effects, and no local app installation is required |
| Create's newest published opsctl installation and ten host keys | D10 release discovery, installation and Configure requirements |
| Init with retained version/email, explicit upgrade, replacement email, nine/ten keys, and failed host readiness report | D10 Configure/Upgrade/Version and D11 sequence/failure requirements |
| Remove without a checkout; retained cloud secrets/artifacts; unknown app and stopped/missing space | D12 account checks and delegated uninstall requirements |
| Restart without applying new secrets; host failures | D13 delegated restart and unchanged cloud/parameter requirements |
| Logs default 100, unchanged since, follow, missing unit, and usage failures | D13 unit query, journal command, streaming and syntax requirements |

The reviewed remote calls match the original stories' responsibility boundary:
devctl invokes installed tools and reports their exits. Host-side data changes,
service state, backup replication and certificate regeneration remain the
installed host tool's behavior. No new hypothetical failure cases or deployment
prerequisites were introduced.
