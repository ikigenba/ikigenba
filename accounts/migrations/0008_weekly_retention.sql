ALTER TABLE backup_settings ADD COLUMN weekly INTEGER NOT NULL DEFAULT 5;
UPDATE backup_settings SET weekly = 5, monthly = 0;
