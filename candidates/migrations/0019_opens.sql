CREATE TABLE opens (seq INTEGER PRIMARY KEY, prospect_list TEXT NOT NULL, at TEXT NOT NULL, user_id TEXT NOT NULL, user_email TEXT NOT NULL, filters TEXT NOT NULL, ordering TEXT NOT NULL, size INTEGER NOT NULL, members TEXT NOT NULL, passing TEXT NOT NULL);
CREATE INDEX opens_prospect_list ON opens(prospect_list,seq);
