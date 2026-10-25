-- User management: accounts can be disabled, sessions remember where they
-- came from, and account events are recorded.
ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1));

ALTER TABLE sessions ADD COLUMN remote_addr  TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN user_agent   TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_used_at TEXT NOT NULL DEFAULT '';

-- The actor and target are kept as names, so the history survives the deletion
-- of an account.
CREATE TABLE audit_log (
    id     INTEGER PRIMARY KEY,
    at     TEXT NOT NULL,
    actor  TEXT NOT NULL,
    event  TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    remote TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT ''
) STRICT;
