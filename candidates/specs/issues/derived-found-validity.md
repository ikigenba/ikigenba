# Derived results and validation have conflicting domains

Filed during the user-started candidates build-spec run at `710a1f84f0901866bfe824161e86e2bcca3db810`. Independently identified by `/root/gap_verify` and validated by `/root/foundation/drive_validate`.

## Requirements

`R-IAJ1-KQOE`, `R-ILI5-0OCN`, `R-IMQ1-EG3C`, `R-ULDH-9549`, `R-UMLD-MWUY`, `R-UNTA-0OLN`.

## Friction and evidence

In `D16-enrichers.md`, `R-ILI5-0OCN` applies to every `enrichers.Subject s` and requires DriveMinutes.Derive to copy `*s.Minutes` unchanged into its Found value. The exported Subject in `R-IAJ1-KQOE` permits a `Minutes *int64` without a nonnegative precondition. A subject with a ZIP and Minutes pointing to -1 therefore produces `{"minutes":-1}`. `R-IMQ1-EG3C` requires Valid to reject that value, while also accepting every Found value Derive returns. Both outcomes cannot hold for this result.

Similarly, `R-ULDH-9549` and `R-UMLD-MWUY` require LinkFacts.Derive to take the first live FMCSA link's numeric pointers unchanged. A Subject containing a live FMCSA Reading with PowerUnits pointing to -1 therefore produces a negative power_units value. `R-UNTA-0OLN` requires nonnegative counts while also accepting every Found value Derive returns. The Reading contract in D01 does not impose a numeric range, and D16's hand-built Subject test seam has no validity precondition. Production source normalization or the store's ZIP validation does not resolve the requirements for direct calls.

## Why the build cannot resolve this

Rejecting or normalizing these subjects would change the specified derivation behavior; accepting their Found values would change the specified validation behavior. The build treats the design as read-only and cannot choose either change. The conflicting derivation/validation requirements remain unproved; independent requirements continue.

## Suggested resolution

Define the admissible Subject/Reading domain explicitly and qualify the universal claims, or specify how Derive handles invalid input so every returned Found satisfies Valid. Re-mint each changed requirement ID through the design workflow.
