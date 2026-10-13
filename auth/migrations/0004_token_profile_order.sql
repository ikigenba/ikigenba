CREATE INDEX tokens_profile_idx ON tokens(user_id, kind, last_used_at DESC, created_at DESC, id);
