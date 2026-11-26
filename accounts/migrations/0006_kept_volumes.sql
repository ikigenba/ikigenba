CREATE TABLE delete_choices (
    operation_id TEXT PRIMARY KEY,
    keep_data INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE kept_volumes (
    volume TEXT PRIMARY KEY,
    host TEXT NOT NULL,
    size_gb INTEGER NOT NULL,
    deleted INTEGER NOT NULL,
    state TEXT NOT NULL,
    reason TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    next_try INTEGER NOT NULL,
    caller_user TEXT NOT NULL,
    caller_email TEXT NOT NULL,
    caller_request TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX kept_volumes_listed ON kept_volumes(deleted, volume) WHERE state <> 'deleted';
CREATE INDEX kept_volumes_due ON kept_volumes(next_try, volume) WHERE state = 'deleting';
