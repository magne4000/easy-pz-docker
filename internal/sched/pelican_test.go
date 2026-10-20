package sched

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/store"
)

func pelican(c *app.Config) { c.Host = app.HostPelican }

func newScheduler(t *testing.T, e *env) *Scheduler {
	t.Helper()
	s, err := NewScheduler(e.c, e.db, e.st, e.bus, time.UTC, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { s.Shutdown() })
	return s
}

func TestPelicanRefusesStop(t *testing.T) {
	e := setupCfg(t, pelican)
	require.ErrorIs(t, e.c.Stop(LifecycleRequest{}), ErrStopUnavailable)
	op, _ := e.c.Operation()
	require.Empty(t, op, "a refused stop starts no operation")
	require.Zero(t, e.sup.stops)

	require.NoError(t, e.c.Restart(LifecycleRequest{}), "restarts stop and start internally")
	require.Eventually(t, func() bool { op, _ := e.c.Operation(); return op == "" }, time.Second, time.Millisecond)
	require.Equal(t, 1, e.sup.stops)
	require.Equal(t, "running", string(e.sup.Status().State))
}

func TestDockerAllowsStop(t *testing.T) {
	e := setup(t)
	require.NoError(t, e.c.Stop(LifecycleRequest{}))
	require.Eventually(t, func() bool { op, _ := e.c.Operation(); return op == "" }, time.Second, time.Millisecond)
	require.Equal(t, 1, e.sup.stops)
}

func TestScheduleStopAction(t *testing.T) {
	ctx := context.Background()
	stop := ScheduleInput{Name: "nightly stop", Action: "stop", Cron: "0 4 * * *", Enabled: true}
	restart := ScheduleInput{Name: "nightly restart", Action: "restart", Cron: "0 4 * * *", Enabled: true}

	t.Run("docker", func(t *testing.T) {
		s := newScheduler(t, setup(t))
		_, err := s.Create(ctx, stop)
		require.NoError(t, err)
	})
	t.Run("pelican", func(t *testing.T) {
		s := newScheduler(t, setupCfg(t, pelican))
		_, err := s.Create(ctx, stop)
		require.ErrorIs(t, err, ErrInvalidSchedule)
		require.ErrorContains(t, err, "Pelican")

		sc, err := s.Create(ctx, restart)
		require.NoError(t, err)
		_, err = s.Update(ctx, sc.ID, stop)
		require.ErrorIs(t, err, ErrInvalidSchedule, "an update cannot switch to stop either")
		_, err = s.Update(ctx, sc.ID, restart)
		require.NoError(t, err)
	})
	t.Run("pelican skips existing stop schedules", func(t *testing.T) {
		e := setupCfg(t, pelican)
		old, err := e.db.CreateSchedule(ctx, store.Schedule{Name: "from docker", Action: "stop", Cron: "0 4 * * *",
			Enabled: true, CreatedAt: time.Now()})
		require.NoError(t, err)
		kept, err := e.db.CreateSchedule(ctx, store.Schedule{Name: "restart", Action: "restart", Cron: "0 4 * * *",
			Enabled: true, CreatedAt: time.Now()})
		require.NoError(t, err)
		s := newScheduler(t, e)
		sctx, cancel := context.WithCancel(ctx)
		defer cancel()
		require.NoError(t, s.Start(sctx))
		s.mu.Lock()
		_, oldRegistered := s.jobs[old.ID]
		_, keptRegistered := s.jobs[kept.ID]
		s.mu.Unlock()
		require.False(t, oldRegistered)
		require.True(t, keptRegistered)
	})
}
