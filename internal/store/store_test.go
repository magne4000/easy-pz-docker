package store

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), slog.Default(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrationsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	for range 2 {
		db, err := Open(context.Background(), slog.Default(), p)
		require.NoError(t, err)
		require.NoError(t, db.Close())
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	db, ctx := open(t), context.Background()
	var v map[string]int
	require.ErrorIs(t, db.GetJSON(ctx, "k", &v), ErrNotFound)
	require.NoError(t, db.PutJSON(ctx, "k", map[string]int{"a": 1}))
	require.NoError(t, db.PutJSON(ctx, "k", map[string]int{"a": 2}))
	require.NoError(t, db.GetJSON(ctx, "k", &v))
	require.Equal(t, 2, v["a"])
}

func TestItemsCascadeAndOrder(t *testing.T) {
	db, ctx := open(t), context.Background()
	now := time.Now()
	added, err := db.AddItem(ctx, "1", now)
	require.NoError(t, err)
	require.True(t, added)
	added, _ = db.AddItem(ctx, "1", now)
	require.False(t, added)
	_, _ = db.AddItem(ctx, "2", now)
	require.NoError(t, db.EnsureModEntry(ctx, "1", "A", true))
	require.NoError(t, db.EnsureModEntry(ctx, "2", "B", true))
	require.NoError(t, db.SetModOrder(ctx, []string{"B", "A"}))
	es, _ := db.ListModEntries(ctx)
	require.Equal(t, "B", es[0].ModID)
	require.NoError(t, db.RemoveItem(ctx, "1"))
	es, _ = db.ListModEntries(ctx)
	require.Len(t, es, 1)
	require.ErrorIs(t, db.RemoveItem(ctx, "1"), ErrNotFound)
}

func TestBackupsNewestFirst(t *testing.T) {
	db, ctx := open(t), context.Background()
	t0 := time.Unix(1000, 0)
	_, err := db.InsertBackup(ctx, Backup{File: "a", CreatedAt: t0})
	require.NoError(t, err)
	b, err := db.InsertBackup(ctx, Backup{File: "b", CreatedAt: t0.Add(time.Hour), Pinned: true})
	require.NoError(t, err)
	l, _ := db.LatestBackup(ctx)
	require.Equal(t, b.ID, l.ID)
	require.True(t, l.Pinned)
	all, _ := db.ListBackups(ctx)
	require.Equal(t, "b", all[0].File)
}
