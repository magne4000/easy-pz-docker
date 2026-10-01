package backup

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

func newService(t *testing.T, s Set) *Service {
	t.Helper()
	db, err := store.Open(context.Background(), slog.Default(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	bus := events.NewBus(slog.Default(), 16)
	return NewService(ServiceOptions{DB: db, Bus: bus, Tasks: tasks.NewRegistry(bus), Set: s, Dir: t.TempDir(), ServerName: "s"})
}

// A running server is saved before every backup, which rewrites files with
// the same bytes: that alone must not defeat the content-change gate.
func TestGateIgnoresRewritesWithIdenticalBytes(t *testing.T) {
	ctx := context.Background()
	s := seed(t)
	svc := newService(t, s)
	first, created, err := svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.True(t, created)

	chunk := filepath.Join(s.Root, "Saves/Multiplayer/s/map_0_0.bin")
	touch := func(content string, at time.Time) {
		write(t, chunk, content)
		require.NoError(t, os.Chtimes(chunk, at, at))
	}
	touch("chunk", time.Now().Add(time.Minute))
	b, created, err := svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, b.ID)
	unchanged, err := svc.Unchanged(ctx)
	require.NoError(t, err)
	require.True(t, unchanged)

	// Same size, different bytes: a real change.
	touch("CHUNK", time.Now().Add(2*time.Minute))
	unchanged, err = svc.Unchanged(ctx)
	require.NoError(t, err)
	require.False(t, unchanged)
	_, created, err = svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.True(t, created)
}

// PZ's save commits vehicles.db even when no row changed (nobody online):
// only the header's commit counters move, which is not a world change.
func TestGateIgnoresSQLiteCommitCounters(t *testing.T) {
	ctx := context.Background()
	s := seed(t)
	svc := newService(t, s)
	db := filepath.Join(s.Root, "Saves/Multiplayer/s/vehicles.db")
	commit := func(counter byte, row string, at time.Time) {
		b := make([]byte, 512)
		copy(b, "SQLite format 3\x00")
		b[27], b[95] = counter, counter
		copy(b[sqliteHeaderLen:], row)
		require.NoError(t, os.WriteFile(db, b, 0o644))
		require.NoError(t, os.Chtimes(db, at, at))
	}
	commit(1, "car", time.Now())
	first, created, err := svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.True(t, created)

	commit(2, "car", time.Now().Add(time.Minute))
	b, created, err := svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, b.ID)

	commit(3, "CAR", time.Now().Add(2*time.Minute))
	_, created, err = svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.True(t, created)
}
