CREATE TABLE events (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 time INTEGER NOT NULL,
 service TEXT NOT NULL,
 event TEXT NOT NULL,
 request_id TEXT NOT NULL,
 user TEXT NOT NULL,
 attrs TEXT NOT NULL,
 cause TEXT NOT NULL,
 depth INTEGER NOT NULL,
 received INTEGER NOT NULL
);
CREATE TABLE attrs (seq INTEGER NOT NULL REFERENCES events(seq) ON DELETE CASCADE, key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(seq,key));
CREATE TABLE subscribers (service TEXT PRIMARY KEY, status TEXT NOT NULL, cursor INTEGER NOT NULL, since INTEGER NOT NULL, event TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '', seq INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '');
CREATE TABLE declarations (service TEXT PRIMARY KEY, emits TEXT NOT NULL, accepts TEXT NOT NULL, asked INTEGER NOT NULL);
CREATE INDEX events_service_seq ON events(service,seq);
CREATE INDEX events_event_seq ON events(event,seq);
CREATE INDEX events_user_seq ON events(user,seq);
CREATE INDEX events_cause_seq ON events(cause,seq);
CREATE INDEX events_request_id_seq ON events(request_id,seq);
CREATE INDEX events_received ON events(received);
CREATE INDEX attrs_key_value ON attrs(key,value);
