CREATE TABLE row_names ("run" TEXT NOT NULL, source_id TEXT NOT NULL, name TEXT NOT NULL, name_lower TEXT NOT NULL);
CREATE INDEX row_names_run_source ON row_names("run",source_id);
INSERT INTO row_names SELECT r."run",r.source_id,j.value,lower(j.value) FROM "rows" r,json_each(r.names) j;
