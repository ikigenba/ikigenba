ALTER TABLE judgments ADD COLUMN headcount_series TEXT NOT NULL DEFAULT '[]';
ALTER TABLE judgments ADD COLUMN trend TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE judgments ADD COLUMN trend_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE judgments ADD COLUMN trend_from_year INTEGER;
ALTER TABLE judgments ADD COLUMN trend_to_year INTEGER;
ALTER TABLE judgments ADD COLUMN trend_from INTEGER;
ALTER TABLE judgments ADD COLUMN trend_to INTEGER;
ALTER TABLE judgments ADD COLUMN trend_change INTEGER;
