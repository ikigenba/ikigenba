# prompts — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 76

- Phase 73 ⬜ realizes R-ORZ1-QIWX, R-OT6Y-4ANM, R-OUEU-I2EB, R-P5DX-Y02K — suite discovery rewritten: the peer set and the live catalog
- Phase 74 ⬜ realizes R-OWUN-9LVP, R-OY2J-NDME, R-OZAG-15D3, R-P0IC-EX3S, R-P1Q8-SOUH — the gateway tools package (`internal/gateway`)
- Phase 75 ⬜ realizes R-OVMQ-VU50, R-P2Y5-6GL6, R-P461-K8BV, R-P7TQ-PJJY — runner cutover: gateway in, eager attachment out
