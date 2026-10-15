CREATE TABLE assessments_new (candidate TEXT PRIMARY KEY, fleet INTEGER NOT NULL, low INTEGER NOT NULL DEFAULT 0, high INTEGER, confidence REAL, filed TEXT NOT NULL DEFAULT '', minutes INTEGER, exclusions TEXT NOT NULL DEFAULT '[]', excluded INTEGER NOT NULL, base REAL, rules INTEGER NOT NULL, scored TEXT NOT NULL, effective REAL, score REAL, score_key INTEGER, operating REAL, operating_key INTEGER NOT NULL DEFAULT -1, evidence TEXT, revenue_low INTEGER, revenue_high INTEGER, revenue_confidence REAL, known_since TEXT, government_match TEXT, newest_filing TEXT, categories TEXT);
DROP TABLE assessments;
ALTER TABLE assessments_new RENAME TO assessments;
CREATE INDEX assessments_base ON assessments(base);
CREATE INDEX assessments_score ON assessments(score_key DESC,candidate);
CREATE INDEX assessments_operating ON assessments(operating_key DESC,candidate);
ALTER TABLE passes ADD COLUMN kind TEXT NOT NULL DEFAULT 'ingest';
ALTER TABLE passes ADD COLUMN origin TEXT NOT NULL DEFAULT 'run';
UPDATE passes SET kind = 'rebuild', origin = 'rebuild' WHERE "trigger" = 'rebuild';
