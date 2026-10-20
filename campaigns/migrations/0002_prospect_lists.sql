ALTER TABLE members ADD COLUMN prospect_list TEXT NOT NULL DEFAULT '';
UPDATE members SET prospect_list = 'pl_'||substr(cohort,5) WHERE substr(cohort,1,4) = 'coh_';
