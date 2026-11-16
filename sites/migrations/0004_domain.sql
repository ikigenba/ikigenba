ALTER TABLE sites ADD COLUMN domain TEXT;
CREATE UNIQUE INDEX sites_domain ON sites(domain);
