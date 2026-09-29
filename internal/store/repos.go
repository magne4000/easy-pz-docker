package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/store/sqlcdb"
)

// SQL lives in queries.sql; sqlcdb is generated from it (go generate ./internal/store).
// This file maps the generated rows to the domain types.

// ---- settings ----

// GetJSON decodes the setting into v; returns ErrNotFound when absent.
func (d *DB) GetJSON(ctx context.Context, key string, v any) error {
	raw, err := d.q.GetSetting(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: get %s: %w", key, err)
	}
	return json.Unmarshal([]byte(raw), v)
}

func (d *DB) PutJSON(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := d.q.PutSetting(ctx, sqlcdb.PutSettingParams{Key: key, Value: string(raw)}); err != nil {
		return fmt.Errorf("store: put %s: %w", key, err)
	}
	return nil
}

// ---- tracked workshop items ----

type TrackedItem struct {
	WorkshopID    string
	Title         string
	Position      int
	AddedAt       time.Time
	RemoteUpdated time.Time
	FileSize      int64
	PreviewURL    string
	CheckedAt     time.Time
}

type ModEntry struct {
	WorkshopID string
	ModID      string
	Enabled    bool
	Position   int
}

func (d *DB) ListItems(ctx context.Context) ([]TrackedItem, error) {
	rows, err := d.q.ListItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list items: %w", err)
	}
	out := make([]TrackedItem, len(rows))
	for i, r := range rows {
		out[i] = TrackedItem{WorkshopID: r.WorkshopID, Title: r.Title, Position: int(r.Position), AddedAt: fromUnix(r.AddedAt),
			RemoteUpdated: fromUnix(r.RemoteUpdated), FileSize: r.FileSize, PreviewURL: r.PreviewUrl, CheckedAt: fromUnix(r.CheckedAt)}
	}
	return out, nil
}

// AddItem inserts the item at the end of the list; existing items are left untouched.
func (d *DB) AddItem(ctx context.Context, id string, now time.Time) (bool, error) {
	n, err := d.q.AddItem(ctx, sqlcdb.AddItemParams{WorkshopID: id, AddedAt: unix(now)})
	if err != nil {
		return false, fmt.Errorf("store: add item: %w", err)
	}
	return n > 0, nil
}

func (d *DB) RemoveItem(ctx context.Context, id string) error {
	n, err := d.q.RemoveItem(ctx, id)
	if err != nil {
		return fmt.Errorf("store: remove item: %w", err)
	}
	return affected(n, nil)
}

func (d *DB) UpdateItemRemote(ctx context.Context, id, title string, updated time.Time, size int64, preview string, checked time.Time) error {
	return d.q.UpdateItemRemote(ctx, sqlcdb.UpdateItemRemoteParams{Title: title, RemoteUpdated: unix(updated), FileSize: size,
		PreviewUrl: preview, CheckedAt: unix(checked), WorkshopID: id})
}

func (d *DB) ListModEntries(ctx context.Context) ([]ModEntry, error) {
	rows, err := d.q.ListModEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list mod entries: %w", err)
	}
	out := make([]ModEntry, len(rows))
	for i, r := range rows {
		out[i] = ModEntry{WorkshopID: r.WorkshopID, ModID: r.ModID, Enabled: r.Enabled, Position: int(r.Position)}
	}
	return out, nil
}

// EnsureModEntry records a discovered mod (enabled by default) without touching an existing choice.
func (d *DB) EnsureModEntry(ctx context.Context, wsid, modID string, enabled bool) error {
	return d.q.EnsureModEntry(ctx, sqlcdb.EnsureModEntryParams{WorkshopID: wsid, ModID: modID, Enabled: enabled})
}

func (d *DB) SetModEnabled(ctx context.Context, wsid, modID string, enabled bool) error {
	return affected(d.q.SetModEnabled(ctx, sqlcdb.SetModEnabledParams{Enabled: enabled, WorkshopID: wsid, ModID: modID}))
}

// SetModOrder rewrites mod_entries.position following ids (mod IDs); unlisted entries keep their relative order after them.
func (d *DB) SetModOrder(ctx context.Context, modIDs []string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := d.q.WithTx(tx)
	if err := q.ShiftModPositions(ctx); err != nil {
		return err
	}
	for i, id := range modIDs {
		if err := q.SetModPosition(ctx, sqlcdb.SetModPositionParams{Position: int64(i + 1), ModID: id}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) DeleteModEntry(ctx context.Context, wsid, modID string) error {
	return d.q.DeleteModEntry(ctx, sqlcdb.DeleteModEntryParams{WorkshopID: wsid, ModID: modID})
}

// ---- schedules ----

type Schedule struct {
	ID          int64
	Name        string
	Action      string
	Cron        string
	Message     string
	WarnMinutes []int
	Enabled     bool
	CreatedAt   time.Time
	LastRunAt   time.Time
	LastResult  string
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ",")
}

func splitInts(s string) []int {
	out := []int{}
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func toSchedule(r sqlcdb.Schedule) Schedule {
	return Schedule{ID: r.ID, Name: r.Name, Action: r.Action, Cron: r.Cron, Message: r.Message, WarnMinutes: splitInts(r.WarnMinutes),
		Enabled: r.Enabled, CreatedAt: fromUnix(r.CreatedAt), LastRunAt: fromUnix(r.LastRunAt), LastResult: r.LastResult}
}

func (d *DB) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := d.q.ListSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list schedules: %w", err)
	}
	out := make([]Schedule, len(rows))
	for i, r := range rows {
		out[i] = toSchedule(r)
	}
	return out, nil
}

func (d *DB) GetSchedule(ctx context.Context, id int64) (Schedule, error) {
	r, err := d.q.GetSchedule(ctx, id)
	return toSchedule(r), notFound(err)
}

func (d *DB) CreateSchedule(ctx context.Context, s Schedule) (Schedule, error) {
	id, err := d.q.CreateSchedule(ctx, sqlcdb.CreateScheduleParams{Name: s.Name, Action: s.Action, Cron: s.Cron, Message: s.Message,
		WarnMinutes: joinInts(s.WarnMinutes), Enabled: s.Enabled, CreatedAt: unix(s.CreatedAt)})
	if err != nil {
		return s, fmt.Errorf("store: create schedule: %w", err)
	}
	return d.GetSchedule(ctx, id)
}

func (d *DB) UpdateSchedule(ctx context.Context, s Schedule) (Schedule, error) {
	n, err := d.q.UpdateSchedule(ctx, sqlcdb.UpdateScheduleParams{Name: s.Name, Action: s.Action, Cron: s.Cron, Message: s.Message,
		WarnMinutes: joinInts(s.WarnMinutes), Enabled: s.Enabled, ID: s.ID})
	if err != nil {
		return s, fmt.Errorf("store: update schedule: %w", err)
	}
	if err := affected(n, nil); err != nil {
		return s, err
	}
	return d.GetSchedule(ctx, s.ID)
}

func (d *DB) DeleteSchedule(ctx context.Context, id int64) error {
	return affected(d.q.DeleteSchedule(ctx, id))
}

func (d *DB) RecordScheduleRun(ctx context.Context, id int64, at time.Time, result string) error {
	return d.q.RecordScheduleRun(ctx, sqlcdb.RecordScheduleRunParams{LastRunAt: unix(at), LastResult: result, ID: id})
}

// ---- backups ----

type Backup struct {
	ID          int64
	File        string
	CreatedAt   time.Time
	Size        int64
	Fingerprint string
	Reason      string
	Note        string
	Pinned      bool
	BuildID     string
	FileCount   int
	ModCount    int
}

func toBackup(r sqlcdb.Backup) Backup {
	return Backup{ID: r.ID, File: r.File, CreatedAt: fromUnix(r.CreatedAt), Size: r.Size, Fingerprint: r.Fingerprint, Reason: r.Reason,
		Note: r.Note, Pinned: r.Pinned, BuildID: r.BuildID, FileCount: int(r.FileCount), ModCount: int(r.ModCount)}
}

// ListBackups returns newest first.
func (d *DB) ListBackups(ctx context.Context) ([]Backup, error) {
	rows, err := d.q.ListBackups(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list backups: %w", err)
	}
	out := make([]Backup, len(rows))
	for i, r := range rows {
		out[i] = toBackup(r)
	}
	return out, nil
}

func (d *DB) GetBackup(ctx context.Context, id int64) (Backup, error) {
	r, err := d.q.GetBackup(ctx, id)
	return toBackup(r), notFound(err)
}

// LatestBackup returns the newest backup, or ErrNotFound.
func (d *DB) LatestBackup(ctx context.Context) (Backup, error) {
	r, err := d.q.LatestBackup(ctx)
	return toBackup(r), notFound(err)
}

func (d *DB) InsertBackup(ctx context.Context, b Backup) (Backup, error) {
	id, err := d.q.InsertBackup(ctx, sqlcdb.InsertBackupParams{File: b.File, CreatedAt: unix(b.CreatedAt), Size: b.Size,
		Fingerprint: b.Fingerprint, Reason: b.Reason, Note: b.Note, Pinned: b.Pinned, BuildID: b.BuildID,
		FileCount: int64(b.FileCount), ModCount: int64(b.ModCount)})
	if err != nil {
		return b, fmt.Errorf("store: insert backup: %w", err)
	}
	b.ID = id
	return b, nil
}

func (d *DB) SetBackupPinned(ctx context.Context, id int64, pinned bool) error {
	return affected(d.q.SetBackupPinned(ctx, sqlcdb.SetBackupPinnedParams{Pinned: pinned, ID: id}))
}

func (d *DB) DeleteBackup(ctx context.Context, id int64) error {
	return d.q.DeleteBackup(ctx, id)
}
