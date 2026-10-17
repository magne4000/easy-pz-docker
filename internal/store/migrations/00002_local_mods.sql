-- +goose Up
-- Mods copied into the server's mods folder by hand have entries too, under
-- workshop_id '': mod_entries no longer references tracked_items, and a
-- trigger still removes a Workshop item's entries with the item.
CREATE TABLE mod_entries_new (
    workshop_id TEXT NOT NULL,
    mod_id      TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    position    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (workshop_id, mod_id)
);
INSERT INTO mod_entries_new(workshop_id, mod_id, enabled, position)
SELECT workshop_id, mod_id, enabled, position FROM mod_entries;
DROP TABLE mod_entries;
ALTER TABLE mod_entries_new RENAME TO mod_entries;

-- +goose StatementBegin
CREATE TRIGGER tracked_items_delete_entries AFTER DELETE ON tracked_items
BEGIN
    DELETE FROM mod_entries WHERE workshop_id = OLD.workshop_id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER tracked_items_delete_entries;
CREATE TABLE mod_entries_old (
    workshop_id TEXT NOT NULL REFERENCES tracked_items(workshop_id) ON DELETE CASCADE,
    mod_id      TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    position    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (workshop_id, mod_id)
);
INSERT INTO mod_entries_old(workshop_id, mod_id, enabled, position)
SELECT workshop_id, mod_id, enabled, position FROM mod_entries
WHERE workshop_id IN (SELECT workshop_id FROM tracked_items);
DROP TABLE mod_entries;
ALTER TABLE mod_entries_old RENAME TO mod_entries;
