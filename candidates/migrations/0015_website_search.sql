ALTER TABLE facts ADD COLUMN website_source TEXT NOT NULL DEFAULT '';
ALTER TABLE facts ADD COLUMN website_search_at TEXT NOT NULL DEFAULT '';
UPDATE facts SET website_source = 'check' WHERE website IN ('https','http');
