# repos — Plan Status

This is the manifest: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning with `- Phase`, carrying `⬜` (pending). The build loop finds
its next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the
build loop **deletes** the phase's line and its body file — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside the phase lines, so the anchored grep matches only phase lines.

Next phase: 44

- Phase 43 ⬜ realizes R-SPNZ-LS3V, R-SQVV-ZJUK, R-SS3S-DBL9, R-STBO-R3BY, R-SUJL-4V2N, R-SVRH-IMTC, R-SWZD-WEK1, R-SY7A-A6AQ, R-SZF6-NY1F — browser wiring proof via chromedp (D26)
