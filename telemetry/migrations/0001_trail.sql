CREATE TABLE IF NOT EXISTS records (id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, svc TEXT NOT NULL, ev TEXT NOT NULL, req TEXT NOT NULL, user TEXT NOT NULL, attrs TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS attrs (record_id INTEGER NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS records_ts ON records(ts);
CREATE INDEX IF NOT EXISTS records_svc_ts ON records(svc,ts);
CREATE INDEX IF NOT EXISTS records_ev_ts ON records(ev,ts);
CREATE INDEX IF NOT EXISTS records_req_ts ON records(req,ts);
CREATE INDEX IF NOT EXISTS records_user_ts ON records(user,ts);
CREATE INDEX IF NOT EXISTS attrs_key_value ON attrs(key,value);
