-- One-time password reset tokens. Only the SHA-256 of a token is stored, and
-- an account has at most one pending reset: a new one replaces the old.
CREATE TABLE password_resets (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_by TEXT NOT NULL
) STRICT;
