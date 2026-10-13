CREATE TABLE scripts (
    id TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    owner TEXT NOT NULL,
    repo TEXT NOT NULL,
    ref TEXT NOT NULL,
    created TEXT NOT NULL
);
CREATE TABLE runs (
    id TEXT NOT NULL PRIMARY KEY,
    script TEXT NOT NULL,
    sha TEXT NOT NULL,
    ref TEXT NOT NULL,
    user TEXT NOT NULL,
    request_id TEXT NOT NULL,
    trigger TEXT NOT NULL,
    event TEXT NOT NULL,
    status TEXT NOT NULL,
    exit_code INTEGER NOT NULL,
    started TEXT NOT NULL,
    finished TEXT,
    stdout_bytes INTEGER NOT NULL,
    stderr_bytes INTEGER NOT NULL,
    truncated INTEGER NOT NULL,
    reason TEXT NOT NULL
);
CREATE INDEX scripts_owner_name ON scripts(owner, name);
CREATE INDEX runs_script_started_id ON runs(script, started, id);
