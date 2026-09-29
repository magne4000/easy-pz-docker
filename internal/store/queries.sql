-- name: GetSetting :one
SELECT value FROM settings WHERE key = ?;

-- name: PutSetting :exec
INSERT INTO settings(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;

-- name: ListItems :many
SELECT * FROM tracked_items ORDER BY position, added_at, workshop_id;

-- AddItem appends at the end of the list; an existing item is left untouched.
-- name: AddItem :execrows
INSERT INTO tracked_items(workshop_id, position, added_at)
VALUES(?, (SELECT COALESCE(MAX(position), 0) + 1 FROM tracked_items), ?)
ON CONFLICT DO NOTHING;

-- name: RemoveItem :execrows
DELETE FROM tracked_items WHERE workshop_id = ?;

-- name: UpdateItemRemote :exec
UPDATE tracked_items SET title = ?, remote_updated = ?, file_size = ?, preview_url = ?, checked_at = ?
WHERE workshop_id = ?;

-- name: ListModEntries :many
SELECT * FROM mod_entries ORDER BY position, workshop_id, mod_id;

-- name: EnsureModEntry :exec
INSERT INTO mod_entries(workshop_id, mod_id, enabled, position)
VALUES(?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM mod_entries))
ON CONFLICT DO NOTHING;

-- name: SetModEnabled :execrows
UPDATE mod_entries SET enabled = ? WHERE workshop_id = ? AND mod_id = ?;

-- name: ShiftModPositions :exec
UPDATE mod_entries SET position = position + 1000000;

-- name: SetModPosition :exec
UPDATE mod_entries SET position = ? WHERE mod_id = ?;

-- name: DeleteModEntry :exec
DELETE FROM mod_entries WHERE workshop_id = ? AND mod_id = ?;

-- name: ListSchedules :many
SELECT * FROM schedules ORDER BY id;

-- name: GetSchedule :one
SELECT * FROM schedules WHERE id = ?;

-- name: CreateSchedule :execlastid
INSERT INTO schedules(name, action, cron, message, warn_minutes, enabled, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?);

-- name: UpdateSchedule :execrows
UPDATE schedules SET name = ?, action = ?, cron = ?, message = ?, warn_minutes = ?, enabled = ?
WHERE id = ?;

-- name: DeleteSchedule :execrows
DELETE FROM schedules WHERE id = ?;

-- name: RecordScheduleRun :exec
UPDATE schedules SET last_run_at = ?, last_result = ? WHERE id = ?;

-- name: ListBackups :many
SELECT * FROM backups ORDER BY created_at DESC, id DESC;

-- name: GetBackup :one
SELECT * FROM backups WHERE id = ?;

-- name: LatestBackup :one
SELECT * FROM backups ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: InsertBackup :execlastid
INSERT INTO backups(file, created_at, size, fingerprint, reason, note, pinned, build_id, file_count, mod_count)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: SetBackupPinned :execrows
UPDATE backups SET pinned = ? WHERE id = ?;

-- name: DeleteBackup :exec
DELETE FROM backups WHERE id = ?;
