# Phase 31 — File-share tools, completion: `FileList`/`FileDelete`/`FileMove`/`FileMkdir`, the 13-tool table, framing guidance

*Realizes design Decision 26 (slice: the remaining four tools + framing —
R-FASN-GK54, R-FC0J-UBVT, R-FEGC-LVD7) and Decision 5 (rewritten: the
13-tool count — R-F5X1-XH6C; retires R-64QY-QN1H with the seven-tool count).
Depends on Phase 30 (the share client seam and failure mapping).*

Observable end state:

- `FileList(path?, cursor?, limit?)` passes its params through to
  `GET /list` and returns the share's entries + continuation cursor;
  `FileDelete(share_path)`, `FileMkdir(share_path)`, and `FileMove(from, to)`
  wrap `DELETE /content`, `POST /mkdir`, and `POST /move`, each returning its
  pinned small success JSON. All four ride Phase 30's client seam (identity
  header, failure mapping).
- `tools.All` returns the full 13-tool set with the D5 table's names and
  descriptions; every tool description speaks of "the account's file share",
  never a service name.
- `framing_prompt.go` gains the file-share guidance paragraph (D26's fixed
  points: the share as the durable shared store vs the prompt-private folder,
  the file tools as the channel), keeps the verbatim "NO network access from
  bash" sentence, and still names no individual service.

**Done when:** the suite is green (design Conventions commands, from
`prompts/`) and:

- R-FASN-GK54 is covered by a test asserting `FileList` passes `path`,
  `cursor`, and `limit` through as query params on a recorded `GET /list` and
  returns the server's entries (`path`/`kind`/`size` observable) and cursor.
- R-FC0J-UBVT is covered by a test asserting exactly one recorded
  `DELETE /content?path=`, `POST /mkdir?path=`, and `POST /move?from=&to=`
  (params URL-escaped) with each tool's pinned success JSON — and extending
  Phase 30's R-F74Y-B8X1 header assertion across all six tools.
- R-F5X1-XH6C is covered by a test asserting `tools.All` returns exactly 13
  tools with the D5 table's names, each satisfying `agentkit.Tool`.
- R-FEGC-LVD7 is covered by a test asserting the conversation `System` names
  the six file tools, carries the file-share guidance, retains the verbatim
  "NO network access from bash" sentence, and contains no `ikigenba_` and no
  per-service enumeration.
