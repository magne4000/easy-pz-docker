package sys

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/jonboulle/clockwork"
	"golang.org/x/sys/unix"
)

type Clock = clockwork.Clock

func RealClock() Clock { return clockwork.NewRealClock() }

type ChownResult struct {
	Checked int
	Fixed   int
	Errors  []error
}

const maxChownErrors = 20

// FixOwnership chowns only the entries whose owner differs. Symlinks are
// never followed; their own ownership is fixed with Lchown.
func FixOwnership(ctx context.Context, root string, uid, gid int) (ChownResult, error) {
	var res ChownResult
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if len(res.Errors) < maxChownErrors {
				res.Errors = append(res.Errors, err)
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		res.Checked++
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (int(st.Uid) == uid && int(st.Gid) == gid) {
			return nil
		}
		if err := os.Lchown(p, uid, gid); err != nil {
			if len(res.Errors) < maxChownErrors {
				res.Errors = append(res.Errors, err)
			}
			return nil
		}
		res.Fixed++
		return nil
	})
	return res, err
}

type DiskUsage struct {
	Path        string  `json:"path"`
	Total       uint64  `json:"total"`
	Free        uint64  `json:"free"`
	Used        uint64  `json:"used"`
	UsedPercent float64 `json:"usedPercent"`
}

func Usage(path string) (DiskUsage, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return DiskUsage{}, fmt.Errorf("statfs %s: %w", path, err)
	}
	bs := blockSize(&st)
	total := uint64(st.Blocks) * bs
	free := uint64(st.Bavail) * bs
	used := total - uint64(st.Bfree)*bs
	u := DiskUsage{Path: path, Total: total, Free: free, Used: used}
	if used+free > 0 {
		u.UsedPercent = float64(used) / float64(used+free) * 100
	}
	return u, nil
}

type Level string

const (
	LevelOK       Level = "ok"
	LevelWarning  Level = "warning"
	LevelCritical Level = "critical"
	LevelUnknown  Level = "unknown"
)

type Volume struct {
	DiskUsage
	Label string `json:"label"`
	Level Level  `json:"level"`
	Error string `json:"error,omitempty"`
}

func Classify(u DiskUsage, warnPct, critPct float64) Level {
	switch {
	case u.UsedPercent >= critPct:
		return LevelCritical
	case u.UsedPercent >= warnPct:
		return LevelWarning
	default:
		return LevelOK
	}
}

// Worst returns the most severe level of the given statuses.
func Worst(ss []Volume) Level {
	rank := map[Level]int{LevelOK: 0, LevelUnknown: 1, LevelWarning: 2, LevelCritical: 3}
	w := LevelOK
	for _, s := range ss {
		if rank[s.Level] > rank[w] {
			w = s.Level
		}
	}
	return w
}

type MonitorConfig struct {
	Paths    map[string]string
	WarnPct  float64
	CritPct  float64
	Interval time.Duration
	Clock    Clock
	OnChange func([]Volume)
	Log      *slog.Logger
}

type Monitor struct {
	c     MonitorConfig
	usage func(string) (DiskUsage, error)
	mu    sync.RWMutex
	last  []Volume
}

func NewMonitor(c MonitorConfig) *Monitor {
	if c.Clock == nil {
		c.Clock = RealClock()
	}
	if c.Interval <= 0 {
		c.Interval = time.Minute
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	return &Monitor{c: c, usage: Usage}
}

func (m *Monitor) sample() []Volume {
	labels := make([]string, 0, len(m.c.Paths))
	for l := range m.c.Paths {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	out := make([]Volume, 0, len(labels))
	for _, l := range labels {
		u, err := m.usage(m.c.Paths[l])
		s := Volume{DiskUsage: u, Label: l}
		if err != nil {
			s.Path, s.Level, s.Error = m.c.Paths[l], LevelUnknown, err.Error()
		} else {
			s.Level = Classify(u, m.c.WarnPct, m.c.CritPct)
		}
		out = append(out, s)
	}
	return out
}

func (m *Monitor) tick(first bool) {
	cur := m.sample()
	m.mu.Lock()
	prev := m.last
	m.last = cur
	m.mu.Unlock()
	changed := first || len(prev) != len(cur)
	for i := range cur {
		if !changed && prev[i].Level != cur[i].Level {
			changed = true
		}
		if !first && (i >= len(prev) || prev[i].Level != cur[i].Level) && cur[i].Level != LevelOK {
			m.c.Log.Warn("disk space", "volume", cur[i].Label, "path", cur[i].Path, "level", cur[i].Level, "used_percent", cur[i].UsedPercent)
		}
	}
	if changed && m.c.OnChange != nil {
		m.c.OnChange(cur)
	}
}

func (m *Monitor) Run(ctx context.Context) {
	m.tick(true)
	t := m.c.Clock.NewTicker(m.c.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.Chan():
			m.tick(false)
		}
	}
}

func (m *Monitor) Snapshot() []Volume {
	m.mu.RLock()
	last := m.last
	m.mu.RUnlock()
	if last == nil {
		return m.sample()
	}
	return append([]Volume(nil), last...)
}
