CREATE TABLE host_allowlist (
    host TEXT NOT NULL,
    entry TEXT NOT NULL,
    PRIMARY KEY (host, entry)
) WITHOUT ROWID;
CREATE TABLE host_unbans (
    host TEXT NOT NULL,
    address TEXT NOT NULL,
    version INTEGER NOT NULL,
    PRIMARY KEY (host, address)
) WITHOUT ROWID;
