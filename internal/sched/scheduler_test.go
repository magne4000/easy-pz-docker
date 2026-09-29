package sched

import (
	"log/slog"
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

func TestInternalJob(t *testing.T) {
	clock := clockwork.NewFakeClock()
	gs, err := gocron.NewScheduler(gocron.WithClock(clock))
	require.NoError(t, err)
	t.Cleanup(func() { gs.Shutdown() })
	s := &Scheduler{s: gs, log: slog.New(slog.DiscardHandler)}

	require.Nil(t, s.internalJob("backup", 0, func() {}), "0 minutes disables the job")

	j := s.internalJob("backup", 30, func() {})
	require.NotNil(t, j)
	require.Equal(t, "backup", j.Name())
	require.Equal(t, []string{"internal"}, j.Tags())
	gs.Start()
	require.Eventually(t, func() bool { return !nextRun(j).IsZero() }, time.Second, time.Millisecond)
	require.WithinDuration(t, clock.Now().Add(30*time.Minute), nextRun(j), 0)
}
