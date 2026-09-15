# Initial independent release verification

Verifier: fresh agent `/root/release_ground/release_verifier`; read-only review of all release stories, D15, D01/D02 seams, inventory and consumer usage.

Result: 37/39 criteria covered, REL.01.07 and REL.04.02 partial due to correctable drafting defects. No unresolved intent decision. Eight story headings: fresh 7/8 criteria, upgrade 7/7, repeat 4/4, missing operand 2/3, non-root 3/3, missing release 3/3, checksum 3/3, version mismatch 3/3. Preamble 5/5. Full per-criterion evidence uses mappings in release-inventory.md; each mapped criterion except the two identified passed independent review. External-consumer REL.02.07 passes as context with D15 installer and separately owned D05 init.

Corrections required:

- Replace same-release-only fresh saved-script requirement: fresh installation always retains executing script; upgrade retains target release script. Explicit distinct preconditions resolve previous issue without user question.
- Replace partial missing-operand output with exact diagnostic-only stderr and existing empty stdout/exit2/no-effects. Controlling user AGENTS forbids usage stderr. Record deliberate story-output adjustment; no invented help surface.
- Replace wrapper-only testability guidance with ground bubblewrap sandbox reference.
- Split independent public assertions currently grouped: tag publication vs binary artifact; installer declaration vs no CLI addition; installed paths vs host prerequisites. Mint new IDs for every changed text.
- Show actual v0.1.0 invocation and fixture preconditions in checksum/version failure consumer tasks (preceding v9.9.9 invocation otherwise misleading).

Format, D01/D02 ownership, current/proposed usage source evidence, project independence, and checksum wire-format necessity pass. Retain release-external-observations.md: observed HEAD404 cannot establish successful asset availability. No implementation, checks, installation, publication or commits performed.
