CREATE TABLE hosts (
    name TEXT PRIMARY KEY,
    seq INTEGER NOT NULL UNIQUE,
    state TEXT NOT NULL,
    reason TEXT NOT NULL,
    instance_type TEXT NOT NULL,
    root_gb INTEGER NOT NULL,
    data_gb INTEGER NOT NULL,
    instance_id TEXT NOT NULL,
    root_volume TEXT NOT NULL,
    data_volume TEXT NOT NULL,
    allocation_id TEXT NOT NULL,
    association_id TEXT NOT NULL,
    eip TEXT NOT NULL,
    az TEXT NOT NULL,
    version INTEGER NOT NULL,
    applied INTEGER NOT NULL,
    attempted INTEGER NOT NULL,
    report_error INTEGER NOT NULL,
    alarm TEXT NOT NULL,
    running_since INTEGER NOT NULL,
    reported INTEGER NOT NULL,
    operation TEXT NOT NULL,
    created INTEGER NOT NULL,
    created_by TEXT NOT NULL
) WITHOUT ROWID;
CREATE UNIQUE INDEX hosts_live_instance ON hosts(instance_id) WHERE instance_id <> '' AND state <> 'deleted';
CREATE INDEX hosts_root_volume ON hosts(root_volume);
CREATE INDEX hosts_data_volume ON hosts(data_volume);
CREATE INDEX hosts_allocation ON hosts(allocation_id);
CREATE INDEX hosts_instance ON hosts(instance_id);
CREATE TABLE operations (
    id TEXT PRIMARY KEY,
    host TEXT NOT NULL REFERENCES hosts(name) DEFERRABLE INITIALLY DEFERRED,
    account TEXT NOT NULL,
    kind TEXT NOT NULL,
    step TEXT NOT NULL,
    status TEXT NOT NULL,
    generation INTEGER NOT NULL,
    target_type TEXT NOT NULL,
    target_root_gb INTEGER NOT NULL,
    target_data_gb INTEGER NOT NULL,
    resume_state TEXT NOT NULL,
    error TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    started INTEGER NOT NULL,
    step_started INTEGER NOT NULL,
    next_try INTEGER NOT NULL,
    finished INTEGER NOT NULL,
    supersedes TEXT NOT NULL,
    caller_user TEXT NOT NULL,
    caller_email TEXT NOT NULL,
    caller_request TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX operations_status_next_try ON operations(status, next_try);
CREATE INDEX operations_status_id ON operations(status, id);
CREATE INDEX operations_host_id ON operations(host, id);
CREATE INDEX operations_account_kind_status ON operations(account, kind, status);
CREATE TABLE names (name TEXT PRIMARY KEY) WITHOUT ROWID;
CREATE TABLE accounts (
    slug TEXT PRIMARY KEY REFERENCES names(name),
    host TEXT NOT NULL REFERENCES hosts(name),
    state TEXT NOT NULL,
    reason TEXT NOT NULL,
    gid INTEGER NOT NULL,
    version INTEGER NOT NULL,
    removed_version INTEGER NOT NULL,
    change_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    created INTEGER NOT NULL,
    created_by TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX accounts_host_slug ON accounts(host, slug);
CREATE TABLE ssh_users (
    name TEXT PRIMARY KEY REFERENCES names(name),
    account TEXT NOT NULL REFERENCES accounts(slug),
    state TEXT NOT NULL,
    uid INTEGER NOT NULL,
    version INTEGER NOT NULL,
    last_sign_in INTEGER NOT NULL,
    created INTEGER NOT NULL,
    created_by TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX ssh_users_account_name ON ssh_users(account, name);
CREATE TABLE ssh_keys (
    name TEXT NOT NULL REFERENCES ssh_users(name) ON DELETE CASCADE,
    key_type TEXT NOT NULL,
    body TEXT NOT NULL,
    line TEXT NOT NULL,
    PRIMARY KEY (name, key_type, body)
) WITHOUT ROWID;
CREATE INDEX ssh_keys_name_line ON ssh_keys(name, line);
CREATE TABLE reports (
    host TEXT PRIMARY KEY REFERENCES hosts(name),
    received INTEGER NOT NULL,
    body BLOB NOT NULL
) WITHOUT ROWID;
CREATE TABLE counters (
    name TEXT PRIMARY KEY,
    value INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE strays (
    resource TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    host_tag TEXT NOT NULL,
    noted INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE volume_grows (
    id INTEGER PRIMARY KEY,
    volume TEXT NOT NULL,
    noted INTEGER NOT NULL
);
CREATE INDEX volume_grows_volume_noted ON volume_grows(volume, noted);
CREATE INDEX volume_grows_noted ON volume_grows(noted);
