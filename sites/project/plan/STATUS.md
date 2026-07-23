# sites — Plan Status

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, builds it, and on completion
**deletes** that phase's line here and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
anywhere but on a phase line, so the anchored grep matches only phase lines.

Next phase: 42

- Phase 39 ⬜ realizes R-ZN05-WU3D R-ZO82-ALU2 R-ZQNV-25BG R-ZRVR-FX25 R-ZT3N-TOSU R-ZUBK-7GJJ R-ZVJG-L8A8 R-ZWRC-Z00X R-ZXZ9-CRRM R-ZZ75-QJIB R-00F2-4B90 R-Z8DD-BL71 R-01MY-I2ZP R-0A69-6H6K — the MCP surface speaks name + slug: create, set_visibility, rename, guide
- Phase 40 ⬜ realizes R-ZI4K-DR4L R-ZJCG-RIVA R-ZKKD-5ALZ R-ZLS9-J2CO R-ZGWN-ZZDW — the landing page lists sites by name
- Phase 41 ⬜ realizes R-02UU-VUQE R-042R-9MH3 R-05AN-NE7S R-06IK-15YH R-08YC-SPFV — the client controls filter and sort by name
