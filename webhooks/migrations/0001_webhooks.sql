CREATE TABLE webhooks (
    id TEXT NOT NULL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    scheme TEXT NOT NULL,
    secret_sha256 TEXT,
    secret_plain TEXT,
    owner_id TEXT NOT NULL,
    owner_email TEXT NOT NULL,
    created TEXT NOT NULL,
    last_received TEXT
);

CREATE TABLE deliveries (
    id TEXT NOT NULL PRIMARY KEY,
    hook_id TEXT NOT NULL,
    received TEXT NOT NULL,
    content_type TEXT NOT NULL,
    github_event TEXT NOT NULL,
    github_delivery TEXT NOT NULL,
    body BLOB NOT NULL
);

CREATE INDEX deliveries_hook ON deliveries (hook_id);
CREATE INDEX deliveries_received ON deliveries (received);
