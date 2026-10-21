ALTER TABLE facts ADD COLUMN minutes_source TEXT NOT NULL DEFAULT '';
ALTER TABLE facts ADD COLUMN miles REAL;
ALTER TABLE facts ADD COLUMN route_address TEXT NOT NULL DEFAULT '';
ALTER TABLE facts ADD COLUMN drive_route_at TEXT NOT NULL DEFAULT '';
UPDATE facts SET minutes_source = 'zip' WHERE minutes IS NOT NULL;
