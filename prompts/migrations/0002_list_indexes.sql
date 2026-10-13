CREATE INDEX prompts_owner_name ON prompts(owner_id, name);
CREATE INDEX runs_prompt_started_id ON runs(prompt, started, id);
