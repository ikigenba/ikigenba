CREATE TABLE "rows" (
 "run" TEXT NOT NULL, source_id TEXT NOT NULL, scraper TEXT NOT NULL, collected TEXT NOT NULL,
 digest TEXT NOT NULL, name TEXT NOT NULL, names TEXT NOT NULL, names_lower TEXT NOT NULL,
 street TEXT NOT NULL, city TEXT NOT NULL, state TEXT NOT NULL, zip TEXT NOT NULL, county TEXT NOT NULL,
 lat REAL, lon REAL, people TEXT NOT NULL, facts BLOB, raw BLOB,
 UNIQUE("run",source_id)
);
CREATE INDEX rows_run_digest ON "rows"("run",digest);
