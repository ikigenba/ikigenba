-- CRM domain schema (PLAN.md §3, §5). Greenfield replacement of the placeholder
-- contacts schema. Five entities — organization, contact, deal, interaction,
-- task — plus the owned child tables (emails, phones, tags) and the
-- many-to-many join table (deal participants). Every entity shares id (ULID),
-- created_at, updated_at, deleted_at (soft delete). Vocabularies are enforced
-- as CHECK constraints; greenfield-cheap to revise. Every read path filters
-- deleted_at IS NULL — orphaned cross-entity FKs are tolerated, never enforced
-- at delete time (PLAN.md §8 shallow-delete rule).

-- ── organizations ───────────────────────────────────────────────────────────
CREATE TABLE organizations (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    domain      TEXT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    deleted_at  TEXT NULL
);
CREATE INDEX idx_organizations_live
    ON organizations(id) WHERE deleted_at IS NULL;
CREATE INDEX idx_organizations_name_nocase
    ON organizations(name COLLATE NOCASE) WHERE deleted_at IS NULL;
-- Dedup probe: organization by exact domain (PLAN.md §4).
CREATE INDEX idx_organizations_domain
    ON organizations(domain) WHERE deleted_at IS NULL AND domain IS NOT NULL;

-- ── contacts ─────────────────────────────────────────────────────────────────
CREATE TABLE contacts (
    id            TEXT PRIMARY KEY,
    given_name    TEXT NULL,
    family_name   TEXT NULL,
    display_name  TEXT NOT NULL,
    org_id        TEXT NULL REFERENCES organizations(id),
    title         TEXT NULL,
    lifecycle     TEXT NOT NULL DEFAULT 'lead'
        CHECK (lifecycle IN ('subscriber','lead','opportunity','customer')),
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    deleted_at    TEXT NULL
);
CREATE INDEX idx_contacts_live
    ON contacts(id) WHERE deleted_at IS NULL;
CREATE INDEX idx_contacts_display_name_nocase
    ON contacts(display_name COLLATE NOCASE) WHERE deleted_at IS NULL;
CREATE INDEX idx_contacts_org
    ON contacts(org_id) WHERE deleted_at IS NULL;

CREATE TABLE contact_emails (
    id          TEXT PRIMARY KEY,
    contact_id  TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    label       TEXT NULL,
    is_primary  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    deleted_at  TEXT NULL
);
CREATE INDEX idx_contact_emails_contact_live
    ON contact_emails(contact_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_contact_emails_contact_email_live
    ON contact_emails(contact_id, email) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_contact_emails_primary_live
    ON contact_emails(contact_id) WHERE is_primary = 1 AND deleted_at IS NULL;
-- Dedup probe: contact by normalized primary email (PLAN.md §4).
CREATE INDEX idx_contact_emails_primary_email
    ON contact_emails(email) WHERE is_primary = 1 AND deleted_at IS NULL;

CREATE TABLE contact_phones (
    id          TEXT PRIMARY KEY,
    contact_id  TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    phone       TEXT NOT NULL,
    label       TEXT NULL,
    is_primary  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    deleted_at  TEXT NULL
);
CREATE INDEX idx_contact_phones_contact_live
    ON contact_phones(contact_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_contact_phones_contact_phone_live
    ON contact_phones(contact_id, phone) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_contact_phones_primary_live
    ON contact_phones(contact_id) WHERE is_primary = 1 AND deleted_at IS NULL;

-- Tags are contact-only for now (PLAN.md §3). The newsletter audience is a tag.
-- Declarative-replace set field (PLAN.md §4): adds insert a live row, removes
-- soft-delete it; the partial unique index keeps at most one live row per
-- (contact, tag). The tag diff is what emits contact.tagged/untagged (§6).
CREATE TABLE contact_tags (
    id          TEXT PRIMARY KEY,
    contact_id  TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    tag         TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    deleted_at  TEXT NULL
);
CREATE UNIQUE INDEX uq_contact_tags_contact_tag_live
    ON contact_tags(contact_id, tag) WHERE deleted_at IS NULL;
CREATE INDEX idx_contact_tags_tag_live
    ON contact_tags(tag) WHERE deleted_at IS NULL;

-- ── deals (opportunities) ────────────────────────────────────────────────────
-- `status` (open|won|lost) is DERIVED from stage in code, never stored
-- (PLAN.md §3); crm_save rejects a client-supplied status.
CREATE TABLE deals (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    org_id        TEXT NULL REFERENCES organizations(id),
    stage         TEXT NOT NULL DEFAULT 'lead'
        CHECK (stage IN ('lead','qualified','proposal','negotiation','won','lost')),
    amount_cents  INTEGER NULL,
    currency      TEXT NOT NULL DEFAULT 'USD',
    close_date    TEXT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    deleted_at    TEXT NULL
);
CREATE INDEX idx_deals_live
    ON deals(id) WHERE deleted_at IS NULL;
CREATE INDEX idx_deals_org
    ON deals(org_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_deals_stage
    ON deals(stage) WHERE deleted_at IS NULL;
CREATE INDEX idx_deals_open
    ON deals(id) WHERE deleted_at IS NULL AND stage NOT IN ('won','lost');

-- Deal participants — the one many-to-many we carry now (PLAN.md §2). Roles are
-- free-text. Declarative-replace set field, same soft-delete discipline as tags.
CREATE TABLE deal_contacts (
    id          TEXT PRIMARY KEY,
    deal_id     TEXT NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    contact_id  TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    role        TEXT NULL,
    created_at  TEXT NOT NULL,
    deleted_at  TEXT NULL
);
CREATE UNIQUE INDEX uq_deal_contacts_pair_live
    ON deal_contacts(deal_id, contact_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_deal_contacts_deal_live
    ON deal_contacts(deal_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_deal_contacts_contact_live
    ON deal_contacts(contact_id) WHERE deleted_at IS NULL;

-- ── interactions (timeline) ──────────────────────────────────────────────────
-- Created via crm_log, append-only (PLAN.md §3). At least one subject ref is
-- required (enforced by CHECK and re-validated in code with a corrective
-- message).
CREATE TABLE interactions (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL
        CHECK (kind IN ('note','call','email','meeting')),
    body        TEXT NOT NULL DEFAULT '',
    occurred_at TEXT NOT NULL,
    contact_id  TEXT NULL REFERENCES contacts(id),
    org_id      TEXT NULL REFERENCES organizations(id),
    deal_id     TEXT NULL REFERENCES deals(id),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    deleted_at  TEXT NULL,
    CHECK (contact_id IS NOT NULL OR org_id IS NOT NULL OR deal_id IS NOT NULL)
);
CREATE INDEX idx_interactions_live
    ON interactions(id) WHERE deleted_at IS NULL;
CREATE INDEX idx_interactions_contact
    ON interactions(contact_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_interactions_org
    ON interactions(org_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_interactions_deal
    ON interactions(deal_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_interactions_occurred
    ON interactions(occurred_at) WHERE deleted_at IS NULL;

-- ── tasks (follow-ups) ───────────────────────────────────────────────────────
-- No owner/assignee — single-tenant (PLAN.md §3). Optional subject ref.
CREATE TABLE tasks (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','done')),
    due_at      TEXT NULL,
    done_at     TEXT NULL,
    contact_id  TEXT NULL REFERENCES contacts(id),
    org_id      TEXT NULL REFERENCES organizations(id),
    deal_id     TEXT NULL REFERENCES deals(id),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    deleted_at  TEXT NULL
);
CREATE INDEX idx_tasks_live
    ON tasks(id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_contact
    ON tasks(contact_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_org
    ON tasks(org_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_deal
    ON tasks(deal_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_open
    ON tasks(id) WHERE deleted_at IS NULL AND status = 'open';
