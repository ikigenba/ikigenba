CREATE TABLE prompts (
id TEXT NOT NULL PRIMARY KEY,
name TEXT NOT NULL UNIQUE,
owner_id TEXT NOT NULL,
owner_email TEXT NOT NULL,
model TEXT NOT NULL,
prompt TEXT NOT NULL,
system TEXT NOT NULL,
tools TEXT NOT NULL,
schema TEXT,
created TEXT NOT NULL
);
CREATE TABLE subscriptions (prompt TEXT NOT NULL, event TEXT NOT NULL, created TEXT NOT NULL, PRIMARY KEY (prompt,event));
CREATE TABLE runs (
id TEXT NOT NULL PRIMARY KEY,
prompt TEXT NOT NULL,
model TEXT NOT NULL,
user_id TEXT NOT NULL,
request_id TEXT NOT NULL,
trigger_kind TEXT NOT NULL,
event TEXT NOT NULL,
status TEXT NOT NULL,
exit_code INTEGER NOT NULL,
started TEXT NOT NULL,
finished TEXT,
stdout_bytes INTEGER NOT NULL,
stderr_bytes INTEGER NOT NULL,
stdout_truncated INTEGER NOT NULL,
stderr_truncated INTEGER NOT NULL,
reason TEXT NOT NULL,
calls INTEGER NOT NULL,
tool_calls INTEGER NOT NULL,
input_tokens INTEGER NOT NULL,
cached_tokens INTEGER NOT NULL,
output_tokens INTEGER NOT NULL,
reasoning_tokens INTEGER NOT NULL,
cost_nanos INTEGER NOT NULL
);
CREATE TABLE event_runs (prompt TEXT NOT NULL, event TEXT NOT NULL, PRIMARY KEY(prompt,event));
