# sites — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line here and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 38

- Phase 35 ⬜ realizes R-H0LI-XQXI, R-H1TF-BIO7, R-Z3ZN-5BFE, R-H31B-PAEW, R-H498-325L, R-H5H4-GTWA, R-H6P0-ULMZ, R-QYP6-P587, R-H7WX-8DDO — visibility-enum domain: migration, store, layout, token generator
- Phase 36 ⬜ realizes R-H94T-M54D, R-HACP-ZWV2, R-HBKM-DOLR, R-HCSI-RGCG, R-HF8B-IZTU, R-HGG7-WRKJ, R-HHO4-AJB8, R-HIW0-OB1X — MCP surface: explicit visibility, unlisted create, the transition matrix, guide
- Phase 37 ⬜ realizes R-HK3X-22SM, R-HLBT-FUJB, R-HMJP-TMA0 — landing page speaks the visibility enum
