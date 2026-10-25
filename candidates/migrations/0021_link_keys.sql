ALTER TABLE links ADD COLUMN ein_key TEXT NOT NULL DEFAULT '';
CREATE INDEX links_ein ON links(ein_key);
CREATE TABLE link_name_keys (scraper TEXT NOT NULL, source_id TEXT NOT NULL, position INTEGER NOT NULL, name_key TEXT NOT NULL, PRIMARY KEY(scraper,source_id,position));
CREATE INDEX link_name_keys_key ON link_name_keys(name_key);
