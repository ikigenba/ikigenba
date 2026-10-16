ALTER TABLE enrichments ADD COLUMN inputs TEXT NOT NULL DEFAULT '';
UPDATE enrichments SET inputs = source WHERE kind = 'website_check';
CREATE INDEX enrichments_inputs ON enrichments(kind,inputs,seq);
CREATE TABLE facts (candidate TEXT PRIMARY KEY, known_since TEXT, government_match TEXT, newest_filing TEXT, categories TEXT, fleet_verdict TEXT NOT NULL DEFAULT 'unknown', power_units INTEGER, drivers INTEGER, website TEXT NOT NULL DEFAULT 'unknown', website_url TEXT NOT NULL DEFAULT '', minutes INTEGER, drive_minutes_at TEXT NOT NULL DEFAULT '', link_facts_at TEXT NOT NULL DEFAULT '', website_check_at TEXT NOT NULL DEFAULT '');
ALTER TABLE passes ADD COLUMN derived INTEGER NOT NULL DEFAULT 0;
