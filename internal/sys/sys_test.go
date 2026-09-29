package sys

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

func TestFixOwnershipNoop(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "f"), nil, 0o644))
	require.NoError(t, os.Symlink("/nonexistent", filepath.Join(root, "dangling")))
	res, err := FixOwnership(context.Background(), root, os.Getuid(), os.Getgid())
	require.NoError(t, err)
	require.Equal(t, 5, res.Checked)
	require.Zero(t, res.Fixed)

	res, err = FixOwnership(context.Background(), filepath.Join(root, "missing"), 0, 0)
	require.NoError(t, err)
	require.Zero(t, res.Checked)
}

func TestUsageAndClassify(t *testing.T) {
	u, err := Usage(t.TempDir())
	require.NoError(t, err)
	require.Positive(t, u.Total)
	require.Equal(t, LevelCritical, Classify(DiskUsage{UsedPercent: 96}, 85, 95))
	require.Equal(t, LevelWarning, Classify(DiskUsage{UsedPercent: 90}, 85, 95))
	require.Equal(t, LevelOK, Classify(DiskUsage{UsedPercent: 10}, 85, 95))
}

func TestMonitorFiresOnChange(t *testing.T) {
	clk := clockwork.NewFakeClock()
	var mu sync.Mutex
	pct := 10.0
	var calls [][]Volume
	m := NewMonitor(MonitorConfig{Paths: map[string]string{"data": "/d"}, WarnPct: 85, CritPct: 95, Interval: time.Minute, Clock: clk,
		OnChange: func(s []Volume) { mu.Lock(); calls = append(calls, s); mu.Unlock() }})
	m.usage = func(p string) (DiskUsage, error) {
		mu.Lock()
		defer mu.Unlock()
		return DiskUsage{Path: p, UsedPercent: pct}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(time.Minute) // unchanged level: no call
	mu.Lock()
	pct = 90
	mu.Unlock()
	clk.Advance(time.Minute)
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(calls) == 2 }, time.Second, 5*time.Millisecond)
	require.Equal(t, LevelWarning, m.Snapshot()[0].Level)
}
