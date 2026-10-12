CREATE TABLE runs (
 id TEXT PRIMARY KEY, scraper TEXT NOT NULL, status TEXT NOT NULL, reason TEXT NOT NULL,
 started TEXT NOT NULL, finished TEXT NOT NULL, "rows" INTEGER NOT NULL, "new" INTEGER NOT NULL,
 changed INTEGER NOT NULL, unchanged INTEGER NOT NULL, gone INTEGER NOT NULL,
 user_id TEXT NOT NULL, user_email TEXT NOT NULL, "trigger" TEXT NOT NULL,
 request_id TEXT NOT NULL, delivery TEXT NOT NULL
);
CREATE INDEX runs_scraper_started ON runs(scraper,started);
CREATE UNIQUE INDEX runs_delivery ON runs(delivery) WHERE delivery <> '';
