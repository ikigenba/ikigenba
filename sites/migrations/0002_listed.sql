ALTER TABLE sites ADD COLUMN listed INTEGER NOT NULL DEFAULT 0;
UPDATE sites SET listed = CASE WHEN json_extract(payload,'$.Listed') THEN 1 ELSE 0 END;
CREATE INDEX sites_owner_name ON sites(owner, name);
CREATE INDEX sites_slug ON sites(slug);
CREATE INDEX sites_listed_name ON sites(listed, name);
