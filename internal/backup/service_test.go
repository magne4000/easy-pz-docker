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

// PZ's save rewrites the world's databases with nobody online; only the other
// world files (game clock) and the account db tell a real change.
func TestGateIgnoresWorldDatabasesOnly(t *testing.T) {
	ctx := context.Background()
	s := seed(t)
	svc := newService(t, s)
	first, created, err := svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.True(t, created)
	change := func(rel, content string, after time.Duration) {
		p := filepath.Join(s.Root, rel)
		write(t, p, content)
		at := time.Now().Add(after)
		require.NoError(t, os.Chtimes(p, at, at))
	}

	change("Saves/Multiplayer/s/sub/players.db", "players, reshuffled", time.Minute)
	b, created, err := svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, b.ID)

	change("db/s.db", "accounts+1", 2*time.Minute)
	_, created, err = svc.Run(ctx, "scheduled", "", false)
	require.NoError(t, err)
	require.True(t, created)
}
