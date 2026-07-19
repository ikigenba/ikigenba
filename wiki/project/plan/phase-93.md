# Phase 93 — Origin and correlation attribution

*Realizes design Decision 62 (origin + correlation rules). Depends on Phase 91 and Phase 92.*

Thread the D62 `Attribution` explicitly through both call chains: the worker derives origin from `jobs.owner` (`user:<owner>`, else `service:wiki`) and sets `group_id` to the job id on every extract/compile/embed-page call (merge's LLM work included, D38); the ask entrances (MCP tool and web `?q=`) derive `user:<email>` from the request identity, mint one fresh correlation id per ask via `internal/ids` (which gains a suite-ULID `New()` per `docs/correlation-ids.md`), and carry both through embed-query, every ask-subject call, and the synthesis call. The old `WithJobID` context threading is gone from these paths.

**Done when:** the suite is green and these ids are covered by tagged tests:

- R-16VU-W6UV — an identity-driven ask posts all its calls with `origin user:<email>`.
- R-183R-9YLK — a job with an owner posts `user:<owner>`; an ownerless job posts `service:wiki`.
- R-19BN-NQC9 — one ask mints one ULID-shaped correlation id shared across its whole fan-out; a second ask mints a different one.
- R-1AJK-1I2Y — every call the worker makes for a job carries that job's id as `group_id`, verbatim.
