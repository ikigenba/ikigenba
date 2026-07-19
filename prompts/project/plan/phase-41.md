# Phase 41 — Sessions on the record and under the run cap

*Realizes design Decision 33 (runs write `calls` rows) and the runner-level slice of Decision 31 (R-6B3L-11EL). Depends on Phase 37 and Phase 38.*

Wire the run lifecycle into the accounting and admission surfaces. `prompt.Store.FinishRun` additionally inserts the run's `session` calls row via `calls.Store.InsertTx` in the same transaction as the terminal write and outcome event (origin from trigger vs manual, name from prompt_name with prompt_id fallback, group_id = run id, aggregate usage/cost, no bodies); the runner threads usage/cost/provider/model into `FinishRun` and wraps `execute` in `admit.AcquireRun` (acquired after the run row exists, released at the terminal write).

**Done when:** the suite is green and these ids are covered by tagged tests:

- R-6JMV-PFLG — one `session` row per completed run, group_id = run id, usage/cost/model match
- R-6KUS-37C5 — origin `user:<owner>` for manual runs, `trigger:<source>` for event runs
- R-6M2O-GZ2U — calls-insert failure rolls back the terminal write (atomicity)
- R-6NAK-UQTJ — a failed run's row carries its error and consumed usage
- R-6B3L-11EL — with run capacity 1, two runs execute serially
