package mods

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

// diskCMD "downloads" an item by writing one mod into its content dir and
// recording it in its ACF, which — like appworkshop_108600.acf — keeps listing
// the item if its files are later deleted.
type diskCMD struct {
	steam.CMD
	install string
	mu      sync.Mutex
	calls   [][]string
	acf     map[string]bool
}

func (c *diskCMD) WorkshopDownload(_ context.Context, items []steam.WorkshopItem, _ func(steam.Progress)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	c.calls = append(c.calls, ids)
	for _, id := range ids {
		p := filepath.Join(steam.WorkshopContentDir(c.install, id), "mods", "M"+id, "mod.info")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte("id=M"+id+"\n"), 0o644); err != nil {
			return err
		}
		c.acf[id] = true
	}
	return nil
}

func (c *diskCMD) WorkshopInstalled(context.Context) (map[string]steam.WorkshopItemState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]steam.WorkshopItemState{}
	for id := range c.acf {
		out[id] = steam.WorkshopItemState{ID: id}
	}
	return out, nil
}

func newTestService(t *testing.T, loaded func() []string) (*Service, *diskCMD, *store.DB) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	db, err := store.Open(context.Background(), log, filepath.Join(dir, "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	bus := events.NewBus(log, 16)
	cmd := &diskCMD{install: filepath.Join(dir, "install"), acf: map[string]bool{}}
	svc := NewService(Options{InstallDir: cmd.install, DataDir: filepath.Join(dir, "data"), ServerName: "s", NonSteam: true,
		DB: db, CMD: cmd, Bus: bus, Tasks: tasks.NewRegistry(bus), Log: log, Loaded: loaded})
	return svc, cmd, db
}

// A mod tracked while its download never happened must not keep the server
// down: the pre-start fetch downloads exactly the absent items.
func TestDownloadMissingThenVerify(t *testing.T) {
	ctx := context.Background()
	svc, cmd, db := newTestService(t, nil)
	require.NoError(t, cmd.WorkshopDownload(ctx, []steam.WorkshopItem{{ID: "1"}}, nil))
	cmd.calls = nil
	for _, id := range []string{"1", "2"} {
		_, err := db.AddItem(ctx, id, time.Now())
		require.NoError(t, err)
	}
	require.ErrorContains(t, svc.Verify(ctx), "2 is tracked but not downloaded")

	got, err := svc.DownloadMissing(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"2"}, got)
	require.Equal(t, [][]string{{"2"}}, cmd.calls)
	_, err = svc.Apply(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.Verify(ctx))

	// A download that vanished after it was scanned is fetched again.
	require.NoError(t, os.RemoveAll(steam.WorkshopContentDir(cmd.install, "1")))
	got, err = svc.DownloadMissing(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"1"}, got)
}

// Updating rewrites files a live server has loaded.
func TestDownloadOutdatedRefusedWhileRunning(t *testing.T) {
	ctx := context.Background()
	svc, cmd, db := newTestService(t, func() []string { return []string{} })
	_, err := db.AddItem(ctx, "1", time.Now())
	require.NoError(t, err)
	_, err = svc.DownloadOutdated(ctx, nil)
	require.ErrorContains(t, err, "running")
	require.Empty(t, cmd.calls)
}

// Reads never write: a mod the DB has not seen yet (e.g. fetched by PZ itself
// in Steam mode) is enabled and loads last until an explicit choice records it.
func TestReadsDoNotWriteEntries(t *testing.T) {
	ctx := context.Background()
	svc, cmd, db := newTestService(t, nil)
	for _, id := range []string{"1", "2", "3"} {
		require.NoError(t, cmd.WorkshopDownload(ctx, []steam.WorkshopItem{{ID: id}}, nil))
		_, err := db.AddItem(ctx, id, time.Now())
		require.NoError(t, err)
	}
	require.NoError(t, svc.SetOrder(ctx, []string{"M2", "M1"}))
	_, err := db.AddItem(ctx, "4", time.Now())
	require.NoError(t, err)
	require.NoError(t, cmd.WorkshopDownload(ctx, []steam.WorkshopItem{{ID: "4"}}, nil))
	svc.Refresh()

	before, err := db.ListModEntries(ctx)
	require.NoError(t, err)
	ov, err := svc.Overview(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"M2", "M1", "M3", "M4"}, ov.LoadOrder, "the unrecorded mod is enabled and last")
	after, err := db.ListModEntries(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "Overview wrote to the database")

	require.NoError(t, svc.SetEnabled(ctx, "4", "M4", false))
	ov, err = svc.Overview(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"M2", "M1", "M3"}, ov.LoadOrder)
	require.ErrorIs(t, svc.SetEnabled(ctx, "4", "Nope", false), store.ErrNotFound)
}
