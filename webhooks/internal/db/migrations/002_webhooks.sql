-- webhooks domain schema (D2). Greenfield; once this migration has shipped it is
-- FROZEN — every later schema change is a new, higher-numbered, additive
-- migration (see the mono-repo CLAUDE.md migration-immutability note).
--
-- A webhook is named per owner-account; `name` is the natural PK so a second
-- Insert with the same name fails on the real constraint (D2). `secret_hash`
-- holds only the hash of the signing secret — the Webhook value object carries
-- no secret material, so GetByName hands the hash back separately. The owner
-- index backs the owner-scoped ListByOwner/Delete/UpdateSecret paths.
CREATE TABLE webhooks (
    name              TEXT PRIMARY KEY,
    owner_email       TEXT NOT NULL,
    secret_hash       TEXT NOT NULL,
    created_at        TEXT NOT NULL,
    last_triggered_at TEXT
);
CREATE INDEX idx_webhooks_owner ON webhooks(owner_email);
