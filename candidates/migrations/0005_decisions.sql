CREATE TABLE decisions (seq INTEGER PRIMARY KEY, kind TEXT NOT NULL, candidate TEXT NOT NULL, other TEXT NOT NULL, "keys" TEXT NOT NULL, why TEXT NOT NULL, user_id TEXT NOT NULL, user_email TEXT NOT NULL, at TEXT NOT NULL);
CREATE INDEX decisions_candidate ON decisions(candidate);
CREATE INDEX decisions_other ON decisions(other);
