CREATE TABLE drafts (
 id TEXT PRIMARY KEY, gmail_draft TEXT NOT NULL, message TEXT NOT NULL, thread TEXT NOT NULL,
 status TEXT NOT NULL, to_addrs TEXT NOT NULL, cc_addrs TEXT NOT NULL, bcc_addrs TEXT NOT NULL,
 subject TEXT NOT NULL, body TEXT NOT NULL, reply_to TEXT NOT NULL, in_reply_to TEXT NOT NULL,
 references_header TEXT NOT NULL, attachments TEXT NOT NULL, created TEXT NOT NULL,
 modified TEXT NOT NULL, sent TEXT NOT NULL, user_id TEXT NOT NULL
);
CREATE INDEX drafts_user_created ON drafts(user_id, created);
