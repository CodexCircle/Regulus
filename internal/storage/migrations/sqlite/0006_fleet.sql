-- Remote agents: enrollment tokens and the agents that used them. Only the
-- SHA-256 of a token is stored. An agent is identified by the serial number of
-- its client certificate; revoking it sets revoked_at, and a revoked agent can
-- no longer connect.
CREATE TABLE enrollment_tokens (
    token_hash TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_by TEXT NOT NULL
) STRICT;

CREATE TABLE agents (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    hostname      TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    enrolled_from TEXT NOT NULL DEFAULT '',
    cert_serial   TEXT NOT NULL,
    revoked_at    TEXT
) STRICT;
