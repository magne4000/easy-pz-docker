-- +goose Up
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE tracked_items (
    workshop_id  TEXT PRIMARY KEY,
    title        TEXT NOT NULL DEFAULT '',
    position     INTEGER NOT NULL DEFAULT 0,
    added_at     INTEGER NOT NULL,
    remote_updated INTEGER NOT NULL DEFAULT 0,
    file_size    INTEGER NOT NULL DEFAULT 0,
    preview_url  TEXT NOT NULL DEFAULT '',
    checked_at   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE mod_entries (
    workshop_id TEXT NOT NULL REFERENCES tracked_items(workshop_id) ON DELETE CASCADE,
    mod_id      TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    position    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (workshop_id, mod_id)
);

CREATE TABLE schedules (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    action       TEXT NOT NULL,
    cron         TEXT NOT NULL,
    message      TEXT NOT NULL DEFAULT '',
    warn_minutes TEXT NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    last_run_at  INTEGER NOT NULL DEFAULT 0,
    last_result  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE backups (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file        TEXT NOT NULL UNIQUE,
    created_at  INTEGER NOT NULL,
    size        INTEGER NOT NULL DEFAULT 0,
    fingerprint TEXT NOT NULL DEFAULT '',
    reason      TEXT NOT NULL DEFAULT '',
    note        TEXT NOT NULL DEFAULT '',
    pinned      INTEGER NOT NULL DEFAULT 0,
    build_id    TEXT NOT NULL DEFAULT '',
    file_count  INTEGER NOT NULL DEFAULT 0,
    mod_count   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX backups_created ON backups(created_at);

-- +goose Down
DROP TABLE backups;
DROP TABLE schedules;
DROP TABLE mod_entries;
DROP TABLE tracked_items;
DROP TABLE settings;
