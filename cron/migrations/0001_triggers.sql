CREATE TABLE triggers (
    id TEXT NOT NULL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    owner_id TEXT NOT NULL,
    owner_email TEXT NOT NULL,
    schedule TEXT NOT NULL,
    status TEXT NOT NULL,
    created TEXT NOT NULL,
    last_fired TEXT
);
