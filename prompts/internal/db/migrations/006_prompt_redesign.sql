-- 006_prompt_redesign.sql — forward, data-preserving migration from the original
-- session-oriented schema (002 sessions / 003 session_triggers) to the redesigned
-- prompt model: first-class concurrent runs, the per-run on-disk unit, tombstone
-- delete (no FK / no cascade), and the multi-source trigger model.
--
-- Why this is a NEW migration instead of an edit to 002/003: the appkit runner
-- tracks migrations by their numeric version and is forward-only. A deployed DB
-- that already has versions 2 and 3 applied will NEVER re-run them, so editing
-- those files in place would silently leave a live box on the old schema. All of
-- the reshape — and the carry-over of existing rows — therefore happens HERE, so an
-- existing database upgrades in place WITHOUT losing prompts, runs, or triggers,
-- while a fresh database reaches the identical end state by applying 002→003→006.
--
-- SQLite runs with foreign_keys=ON and each migration is one transaction, so the
-- rebuilds are ordered child-first: the new runs/prompt_triggers tables carry NO
-- foreign key, and the old FK-bearing tables (runs, session_triggers) are dropped
-- before their parent (sessions), so no constraint is violated mid-transaction.

-- 1. sessions -> prompts: drop the `status` column (no prompt lifecycle anymore),
--    rename `prompt` -> `user_prompt`. -----------------------------------------
CREATE TABLE prompts (
    id            TEXT PRIMARY KEY,
    owner_email   TEXT NOT NULL,
    name          TEXT,
    user_prompt   TEXT NOT NULL,
    system_prompt TEXT,
    config_json   TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
INSERT INTO prompts (id, owner_email, name, user_prompt, system_prompt, config_json, created_at, updated_at)
SELECT id, owner_email, name, prompt, system_prompt, config_json, created_at, updated_at
FROM sessions;

-- 2. runs: rebuild without the FK/cascade (runs are first-class and survive a
--    tombstone-deleted prompt), denormalize owner_email/prompt_name from the
--    session, and add the trigger-context columns. Existing rows keep their
--    log_path as-is (their output already lives at that path); pre-redesign runs
--    have no trigger context, so the trigger_* columns are NULL — matching what
--    the new code writes for a manual run. -------------------------------------
CREATE TABLE runs_new (
    id            TEXT PRIMARY KEY,
    prompt_id     TEXT NOT NULL,
    owner_email   TEXT NOT NULL,
    prompt_name   TEXT,
    status        TEXT NOT NULL,
    started_at    TEXT NOT NULL,
    ended_at      TEXT,
    usage_json    TEXT,
    error         TEXT,
    trigger_source   TEXT,
    trigger_type     TEXT,
    trigger_event_id TEXT,
    log_path      TEXT NOT NULL
);
INSERT INTO runs_new (id, prompt_id, owner_email, prompt_name, status, started_at,
                      ended_at, usage_json, error, trigger_source, trigger_type, trigger_event_id, log_path)
SELECT r.id, r.session_id, COALESCE(s.owner_email, ''), s.name, r.status, r.started_at,
       r.ended_at, r.usage_json, r.error, NULL, NULL, NULL, r.log_path
FROM runs r
LEFT JOIN sessions s ON s.id = r.session_id;
DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;
CREATE INDEX idx_runs_prompt ON runs(prompt_id, started_at);
CREATE INDEX idx_runs_status ON runs(status);

-- 3. session_triggers -> prompt_triggers: the old single cron trigger per session
--    becomes a (prompt, 'cron', event_filter) binding in the multi-source model.
--    The cron-only knobs (max_staleness_secs, max_attempts, updated_at) are
--    dropped — fire-and-forget, symmetric with the scripts service. -------------
CREATE TABLE prompt_triggers (
    prompt_id    TEXT NOT NULL,
    source       TEXT NOT NULL,
    event_filter TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (prompt_id, source, event_filter)
);
INSERT INTO prompt_triggers (prompt_id, source, event_filter, created_at)
SELECT session_id, 'cron', trigger_event, created_at
FROM session_triggers;
DROP TABLE session_triggers;
CREATE INDEX idx_prompt_triggers_lookup ON prompt_triggers(source, event_filter);

-- 4. drop the now-superseded, empty parent table. -------------------------------
DROP TABLE sessions;
