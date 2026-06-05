-- wiki domain — PLACEHOLDER (Task 2.1 scaffold).
--
-- The real wiki schema (ingest/provenance + agentkit job-record tables, carrying
-- an owner-scoped, collection-keyed model) lands in Phase 3 (Task 3.1) as a
-- HIGHER-numbered, additive migration. Migrations are immutable once applied, so
-- this file stays minimal-but-valid and is never rewritten in place once shipped.
--
-- This single sentinel table exists only so the migration runner has real DDL to
-- apply on a fresh DB at this version; it carries no domain logic yet.
CREATE TABLE wiki_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO wiki_meta (key, value) VALUES ('schema', 'scaffold');
