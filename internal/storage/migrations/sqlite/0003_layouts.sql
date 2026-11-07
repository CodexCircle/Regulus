-- Dashboard layout of each user (widgets and their positions), stored as the
-- JSON the dashboard sends.
CREATE TABLE dashboard_layouts (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    layout     TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;
