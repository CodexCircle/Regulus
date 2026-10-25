-- regulus:no-foreign-keys
-- Roles are named sets of permissions. The built-in roles (admin, operator and
-- viewer) live in the code, so a new version can give them the permissions of
-- its new features; only the roles an administrator creates are stored here.
-- users.role holds the name of either kind.

-- users.role could only be admin or viewer. SQLite cannot drop a CHECK, so the
-- table is rebuilt (the migration runs with foreign keys off for that).
CREATE TABLE users_new (
    id            INTEGER PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL COLLATE NOCASE,
    created_at    TEXT NOT NULL,
    disabled      INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1))
) STRICT;

INSERT INTO users_new (id, username, password_hash, role, created_at, disabled)
SELECT id, username, password_hash, role, created_at, disabled FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

CREATE TABLE roles (
    name        TEXT PRIMARY KEY COLLATE NOCASE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
) STRICT;

-- A permission that no longer exists (for example from a removed plugin) stays
-- here and is ignored until it exists again.
CREATE TABLE role_permissions (
    role       TEXT NOT NULL COLLATE NOCASE REFERENCES roles (name) ON DELETE CASCADE ON UPDATE CASCADE,
    permission TEXT NOT NULL,
    PRIMARY KEY (role, permission)
) STRICT;
