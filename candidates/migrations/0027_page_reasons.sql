ALTER TABLE pages ADD COLUMN reason TEXT;
CREATE TABLE page_refetches (seq INTEGER PRIMARY KEY, outcome TEXT);
INSERT INTO page_refetches (seq) SELECT seq FROM pages WHERE size = 262144 AND seq IN (SELECT MAX(seq) FROM pages GROUP BY candidate, url);
