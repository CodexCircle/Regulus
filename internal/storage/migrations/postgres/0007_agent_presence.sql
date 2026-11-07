-- What the hub last knew about an enrolled agent, so the server list can show
-- it while it is offline.
ALTER TABLE agents ADD COLUMN version      TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN os_version   TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN last_seen_at TEXT;
