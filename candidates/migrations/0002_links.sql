CREATE TABLE links (scraper TEXT NOT NULL, source_id TEXT NOT NULL, candidate TEXT NOT NULL, run TEXT NOT NULL, digest TEXT NOT NULL, name_key TEXT NOT NULL, state TEXT NOT NULL, zip TEXT NOT NULL, phone_key TEXT NOT NULL, domain_key TEXT NOT NULL, reading TEXT NOT NULL, reason TEXT NOT NULL, confidence REAL NOT NULL, first_seen TEXT NOT NULL, last_seen TEXT NOT NULL, gone INTEGER NOT NULL, UNIQUE(scraper,source_id));
CREATE INDEX links_match ON links(name_key,state,zip);
CREATE INDEX links_candidate ON links(candidate);
