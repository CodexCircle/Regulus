-- Two-factor authentication (TOTP) with recovery codes.

-- One TOTP secret per user. The secret is stored encrypted, with a key kept
-- outside the database (secret.key in the data directory). The row exists
-- from the moment the user starts the enrollment; two-factor is on only once
-- confirmed_at is set, after the user proved they can produce a valid code.
-- last_step is the last 30 second time step accepted: a code is never
-- accepted twice.
CREATE TABLE user_totp (
    user_id      INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret_enc   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    confirmed_at TEXT,
    last_step    INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Single-use recovery codes. Only the SHA-256 of a code is stored.
CREATE TABLE recovery_codes (
    id        INTEGER PRIMARY KEY,
    user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at   TEXT,
    UNIQUE (user_id, code_hash)
) STRICT;

-- A login that passed the password and waits for the second factor. It is not
-- a session: it only lets the holder submit a code, a few times, soon.
CREATE TABLE pending_logins (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE INDEX pending_logins_expires ON pending_logins (expires_at);
