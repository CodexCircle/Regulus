-- Key/value settings owned by the hub (instance id, feature flags, ...).
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;
