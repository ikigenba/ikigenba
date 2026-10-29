CREATE TABLE owner_records (id TEXT PRIMARY KEY, candidate TEXT NOT NULL, founded INTEGER NOT NULL, note TEXT NOT NULL, user_id TEXT NOT NULL, user_email TEXT NOT NULL, at TEXT NOT NULL, name TEXT, headcount INTEGER, headcount_year INTEGER, revenue_low INTEGER, revenue_high INTEGER, category TEXT, website TEXT, street TEXT, city TEXT, state TEXT, zip TEXT, county TEXT, phone TEXT, email TEXT, people TEXT, known_since INTEGER, ein TEXT, fleet INTEGER, cleared TEXT NOT NULL) WITHOUT ROWID;
CREATE INDEX owner_records_candidate ON owner_records(candidate,at,id);
CREATE INDEX owner_records_ein ON owner_records(ein);
CREATE INDEX owner_records_place ON owner_records(state,zip);
