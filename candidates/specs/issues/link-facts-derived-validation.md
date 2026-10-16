# Link facts derivation and validation disagree

Filed during the user-started build-spec run on `wip-funnel`, starting at `bfcdb1b9ed42ab52a360fff0f7845104ddca3b3d`. The implementer reported this conflict and an independent verifier confirmed it against D16 and its dependency contracts.

## Requirements

R-3O5C-AQN3 requires copying an FMCSA reading's FleetVerdict whenever it is not empty. R-3LPJ-J75P requires copying every nonempty category with Source equal to its link's Scraper. R-UNTA-0OLN rejects verdicts outside pass/fail/unknown and category entries with empty sources, while promising acceptance of every Found returned by LinkFacts.Derive.

## Evidence

A constructed `enrichers.Subject` containing an FMCSA link with `sources.Reading{FleetVerdict: "maybe"}` must derive `{"categories":[],"fleet_verdict":"maybe"}`. Valid must reject that same Found under R-UNTA-0OLN, contradicting its universal acceptance promise.

A constructed Subject containing a link with an empty Scraper and `sources.Reading{Category: "category"}` must derive `{"categories":[{"name":"category","source":""}],"fleet_verdict":"unknown"}`. Valid must also reject that Found while promising to accept every derived Found.

The existing implementation copies these values and rejects them in validation. R-IAJ1-KQOE's public Subject/Link fields impose no such input restrictions. R-IJ2C-94V9 describes stored-candidate subjects, not every constructed Subject. R-GBIG-WI4Z's nonempty scraper rule for Attach and R-45BN-FM63's verdict rules for fmcsa.Read do not restrict direct calls; R-GGE2-FL3R also preserves supplied readings without that verdict precondition.

## Why the build cannot resolve it

No implementation can both copy these values exactly and validate every derived result under the specified strict validator. The build run cannot change the contract or invent an input precondition. The universal guarantee in R-UNTA-0OLN remains unproved.

## Suggested resolution

Replace the affected requirements to normalize unsupported verdicts to unknown and omit categories with empty scraper values, or explicitly define an input domain and narrow the derivation and validation guarantees to it. Re-mint every changed requirement id through the design workflow.
