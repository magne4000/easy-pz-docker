package sched

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/backup"
	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/settings"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

type stubSup struct {
	mu    sync.Mutex
	state pz.State
	stops int
}

func (s *stubSup) Start(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = pz.StateRunning
	return nil
}
func (s *stubSup) Stop(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state, s.stops = pz.StateStopped, s.stops+1
	return nil
}
func (s *stubSup) Status() pz.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pz.Status{State: s.state}
}
func (s *stubSup) SendConsole(string) error { return nil }

type stubRCON struct {
	mu      sync.Mutex
	players []string
	fail    bool
	sent    []string
}

func (r *stubRCON) Exec(_ context.Context, cmd string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, cmd)
	return "ok", nil
}
func (r *stubRCON) Players(context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return nil, errors.New("refused")
	}
	return append([]string{}, r.players...), nil
}
func (r *stubRCON) broadcasts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, s := range r.sent {
		if len(s) > 10 && s[:10] == "servermsg " {
			out = append(out, s[10:])
		}
	}
	return out
}

type stubCMD struct {
	mu      sync.Mutex
	updates int
}

func (c *stubCMD) AppUpdate(context.Context, string, bool, func(steam.Progress)) error {
	c.mu.Lock()
	c.updates++
	c.mu.Unlock()
	return nil
}
func (c *stubCMD) LatestBuildID(context.Context, string) (string, error) { return "2", nil }
func (c *stubCMD) InstalledBuild(context.Context) (steam.AppManifest, error) {
	return steam.AppManifest{BuildID: "1"}, nil
}
func (c *stubCMD) WorkshopDownload(context.Context, []string, func(steam.Progress)) error { return nil }
func (c *stubCMD) WorkshopInstalled(context.Context) (map[string]steam.WorkshopItemState, error) {
	return map[string]steam.WorkshopItemState{}, nil
}
func (c *stubCMD) WorkshopRemove(context.Context, string) error { return nil }

type stubWeb struct{}

func (stubWeb) PublishedFileDetails(context.Context, []string) ([]steam.FileDetails, error) {
	return nil, nil
}
func (stubWeb) CollectionDetails(context.Context, string) ([]string, error) { return nil, nil }

type env struct {
	c     *Coordinator
	sup   *stubSup
	rcon  *stubRCON
	cmd   *stubCMD
	clock *clockwork.FakeClock
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	cfg := app.Config{ServerName: "s", DataDir: filepath.Join(dir, "data"), InstallDir: filepath.Join(dir, "install"),
		UpdateMaxDelay: time.Hour, BackupKeep: 5}
	db, err := store.Open(ctx, log, filepath.Join(dir, "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	bus := events.NewBus(log, 64)
	tk := tasks.NewRegistry(bus)
	st, err := settings.Open(ctx, cfg, db, bus)
	require.NoError(t, err)
	s := st.Get()
	s.WarnMinutes, s.UpdateMaxDelayMinutes, s.AutoUpdate = []int{5, 1}, 10, false
	_, err = st.Update(ctx, s)
	require.NoError(t, err)
	e := &env{sup: &stubSup{state: pz.StateRunning}, rcon: &stubRCON{}, cmd: &stubCMD{}, clock: clockwork.NewFakeClock()}
	ms := mods.NewService(mods.Options{InstallDir: cfg.InstallDir, DataDir: cfg.DataDir, ServerName: "s", DB: db, CMD: e.cmd,
		WebAPI: stubWeb{}, Bus: bus, Tasks: tk, Log: log})
	bs := backup.NewService(backup.ServiceOptions{DB: db, Bus: bus, Tasks: tk, Log: log, Set: backup.DefaultSet(cfg.DataDir, "s"),
		Dir: filepath.Join(dir, "backups"), ServerName: "s"})
	e.c = NewCoordinator(Options{Cfg: cfg, Sup: e.sup, RCON: e.rcon, CMD: e.cmd, Mods: ms, Backups: bs, Settings: st, Bus: bus,
		Tasks: tk, Clock: e.clock, Log: log, SaveWait: time.Millisecond})
	return e
}

func TestPlayersFourStates(t *testing.T) {
	e := setup(t)
	require.Equal(t, "empty", e.c.Players(context.Background()).State)
	e.clock.Advance(10 * time.Second)
	e.rcon.players = []string{"a"}
	p := e.c.Players(context.Background())
	require.Equal(t, "populated", p.State)
	require.Equal(t, 1, *p.Count)
	e.clock.Advance(10 * time.Second)
	e.rcon.fail = true
	p = e.c.Players(context.Background())
	require.Equal(t, "unknown", p.State)
	require.Nil(t, p.Count)
	e.sup.state = pz.StateStopped
	require.Equal(t, "stopped", e.c.Players(context.Background()).State, "a stopped server must never read as empty")
}

func TestRestartCountdownAndBusy(t *testing.T) {
	e := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, e.c.Restart(LifecycleRequest{Countdown: 5 * time.Minute}))

	// never back up mid-restart
	_, err := e.c.RunBackup(ctx, "scheduled", "", false)
	var busy *BusyError
	require.ErrorAs(t, err, &busy)

	require.NoError(t, e.clock.BlockUntilContext(ctx, 1))
	require.Equal(t, []string{`"Server restart in 5 minutes."`}, e.rcon.broadcasts())
	e.clock.Advance(4 * time.Minute)
	require.Eventually(t, func() bool { return len(e.rcon.broadcasts()) == 2 }, time.Second, time.Millisecond)
	require.Equal(t, `"Server restart in 1 minute."`, e.rcon.broadcasts()[1])
	require.NoError(t, e.clock.BlockUntilContext(ctx, 1))
	e.clock.Advance(time.Minute)
	require.Eventually(t, func() bool { op, _ := e.c.Operation(); return op == "" }, time.Second, time.Millisecond)
	require.Equal(t, 1, e.sup.stops)
	require.Equal(t, pz.StateRunning, e.sup.Status().State)
}

func TestCancelCountdown(t *testing.T) {
	e := setup(t)
	require.NoError(t, e.c.Stop(LifecycleRequest{Countdown: 5 * time.Minute}))
	require.NoError(t, e.clock.BlockUntilContext(context.Background(), 1))
	require.True(t, e.c.CancelOperation())
	require.Eventually(t, func() bool { op, _ := e.c.Operation(); return op == "" }, time.Second, time.Millisecond)
	require.Zero(t, e.sup.stops)
}

func TestUpdateWindowWaitsThenForces(t *testing.T) {
	e := setup(t)
	e.rcon.players = []string{"alice"}
	_, err := e.c.CheckUpdates(context.Background())
	require.NoError(t, err)
	require.True(t, e.c.UpdateStatus().GameUpdateAvailable)
	require.NoError(t, e.c.OpenWindow("manual", false))
	require.ErrorIs(t, e.c.OpenWindow("manual", false), ErrWindowOpen)

	// players online: the window keeps waiting until ForceAt (10 min), warning at 5 and 1.
	start := e.clock.Now()
	for e.c.Window().State == WindowWaiting {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := e.clock.BlockUntilContext(ctx, 1)
		cancel()
		if err != nil {
			break
		}
		if e.c.Window().State == WindowWaiting {
			require.Less(t, e.clock.Since(start), 10*time.Minute, "waited past ForceAt")
			e.clock.Advance(30 * time.Second)
		}
	}
	require.GreaterOrEqual(t, e.clock.Since(start), 10*time.Minute, "proceeded before ForceAt with players online")
	require.Eventually(t, func() bool { return e.c.Window().State == WindowIdle }, 2*time.Second, time.Millisecond)
	require.Equal(t, 1, e.cmd.updates)
	require.Equal(t, 1, e.sup.stops)
	require.Equal(t, pz.StateRunning, e.sup.Status().State)
	b := e.rcon.broadcasts()
	require.Contains(t, b[0], "restart when empty, or in 10 minutes")
	require.Contains(t, b, `"Server restarting for an update in 5 minutes."`)
	require.Contains(t, b, `"Server restarting for an update in 1 minute."`)
}

func TestUpdateWindowProceedsWhenEmpty(t *testing.T) {
	e := setup(t)
	require.NoError(t, e.c.OpenWindow("manual", false))
	require.Eventually(t, func() bool { return e.c.Window().State == WindowIdle }, 2*time.Second, time.Millisecond)
	require.Equal(t, 1, e.cmd.updates)
}
