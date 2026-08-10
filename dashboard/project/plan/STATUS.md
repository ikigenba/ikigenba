# dashboard — Plan Status (web surface & sign-in)

This is the **manifest**: one line per **pending** phase in build order, and the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown bullet
beginning `- Phase` and carrying `⬜` (pending). The build loop finds its next
unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`,
reads only that phase's `project/plan/phase-NN.md`, and builds it. On completion
the build loop **deletes** that phase's line here and its `phase-NN.md` body
file — there is no done marker; done is gone. This file deliberately carries
**no bare status glyph** anywhere but on a phase line, so the anchored grep
matches only phase lines.

Next phase: 52

- Phase 49 ⬜ realizes R-VKP5-N8HN, R-VLX2-108C, R-VS0J-XUXT — static foundation of the wiki shell: system fonts, shell CSS, logo
- Phase 50 ⬜ realizes R-VH1G-HX9K, R-VI9C-VP09, R-VJH9-9GQY, R-VVO9-365W, R-VWW5-GXWL — shell templates, the home service directory, and the install page
- Phase 51 ⬜ realizes R-VN4Y-ERZ1, R-VOCU-SJPQ, R-VQSN-K374, R-VT8G-BMOI, R-VUGC-PEF7 — metrics, profile, authorize, and show-once pages adopt the shell


