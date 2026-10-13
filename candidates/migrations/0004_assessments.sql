CREATE TABLE assessments (candidate TEXT PRIMARY KEY, fleet INTEGER NOT NULL, low INTEGER NOT NULL, high INTEGER, confidence REAL NOT NULL, filed TEXT NOT NULL, minutes INTEGER, exclusions TEXT NOT NULL, excluded INTEGER NOT NULL, base REAL NOT NULL, rules INTEGER NOT NULL, scored TEXT NOT NULL);
CREATE INDEX assessments_base ON assessments(base);
