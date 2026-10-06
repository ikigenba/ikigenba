CREATE TABLE subscriptions (
    script TEXT NOT NULL,
    event TEXT NOT NULL,
    created INTEGER NOT NULL,
    PRIMARY KEY (script, event)
);
CREATE TABLE event_runs (
    script TEXT NOT NULL,
    event TEXT NOT NULL,
    PRIMARY KEY (script, event)
);
