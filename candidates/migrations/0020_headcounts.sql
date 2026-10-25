ALTER TABLE facts ADD COLUMN headcounts TEXT NOT NULL DEFAULT '[]';
ALTER TABLE facts ADD COLUMN brief_labels TEXT NOT NULL DEFAULT '[]';
ALTER TABLE judgments ADD COLUMN headcount_low INTEGER;
ALTER TABLE judgments ADD COLUMN headcount_high INTEGER;
ALTER TABLE judgments ADD COLUMN headcount_basis TEXT NOT NULL DEFAULT '';
