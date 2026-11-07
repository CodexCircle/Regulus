-- Roles are named sets of permissions. The built-in roles (admin, operator and
-- viewer) live in the code, so a new version can give them the permissions of
-- its new features; only the roles an administrator creates are stored here.
-- users.role holds the name of either kind.

-- users.role could only be admin or viewer. PostgreSQL can drop the CHECK in
-- place, so unlike SQLite no table is rebuilt. The column becomes citext, to
-- compare names without regard to case like the SQLite column does.
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ALTER COLUMN role TYPE CITEXT;

CREATE TABLE roles (
    name        CITEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

-- A permission that no longer exists (for example from a removed plugin) stays
-- here and is ignored until it exists again.
CREATE TABLE role_permissions (
    role       CITEXT NOT NULL REFERENCES roles (name) ON DELETE CASCADE ON UPDATE CASCADE,
    permission TEXT NOT NULL,
    PRIMARY KEY (role, permission)
);
