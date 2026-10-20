package boot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/sched"
)

type starter struct {
	errs  []error
	calls int
}

func (s *starter) Start(context.Context) error {
	s.calls++
	if len(s.errs) == 0 {
		return nil
	}
	err := s.errs[0]
	s.errs = s.errs[1:]
	return err
}

func TestStartAtBoot(t *testing.T) {
	busyRetry = time.Millisecond
	t.Cleanup(func() { busyRetry = time.Second })
	log := slog.New(slog.DiscardHandler)
	pelican, docker := app.Config{Host: app.HostPelican}, app.Config{Host: app.HostDocker}
	spawn := errors.New("prepare launch: boom")
	busy := &sched.BusyError{Op: "restart"}
	ctx := context.Background()

	s := &starter{errs: []error{pz.ErrAlreadyRunning}}
	require.NoError(t, startAtBoot(ctx, pelican, s, log), "started by someone else during boot")

	s = &starter{errs: []error{busy, busy}}
	require.NoError(t, startAtBoot(ctx, pelican, s, log), "an operation in progress is waited out")
	require.Equal(t, 3, s.calls)

	s = &starter{errs: []error{busy, pz.ErrAlreadyRunning}}
	require.NoError(t, startAtBoot(ctx, pelican, s, log), "a restart that ran meanwhile started it")

	s = &starter{errs: []error{spawn}}
	require.ErrorIs(t, startAtBoot(ctx, pelican, s, log), spawn, "no game behind: fatal")

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	s = &starter{errs: []error{busy, busy}}
	require.ErrorIs(t, startAtBoot(cctx, pelican, s, log), context.Canceled)

	s = &starter{errs: []error{busy}}
	require.ErrorAs(t, startAtBoot(ctx, docker, s, log), &busy, "docker mode unchanged")
	require.Equal(t, 1, s.calls)
}

// syncBuf is an io.Writer tests can poll.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuf) waitFor(t *testing.T, sub string) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Contains(s.String(), sub) }, 30*time.Second, 20*time.Millisecond,
		"waiting for %q in:\n%s", sub, s)
}

type pelicanRun struct {
	in        *io.PipeWriter
	out, logs *syncBuf
	done      chan error
	coord     chan *sched.Coordinator
}

// runPelican starts Run in Pelican mode in a scratch server folder, with the
// console on a pipe and a buffer.
func runPelican(t *testing.T, drivers string) *pelicanRun {
	t.Helper()
	root := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	for k, v := range map[string]string{
		"PZMAN_HOST": "pelican", "PANEL_DRIVERS": drivers, "PANEL_PORT": strconv.Itoa(port), "USE_STEAM": "false",
		"PZMAN_PELICAN_SERVER": "9c3b6a3e-2f1d-4f6e-9a51-0d2b8c7e4a10", "PZMAN_PELICAN_PANEL_URL": "https://panel.example.com",
		"PZMAN_PELICAN_PUBLIC_KEY": "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c",
		"PANEL_INSTALL_DIR":        root, "PANEL_DATA_DIR": filepath.Join(root, ".cache"),
		"PANEL_DB_PATH": filepath.Join(root, ".easy-pz", "pzman.db"), "PANEL_BACKUP_DIR": filepath.Join(root, ".easy-pz", "backups"),
		"PANEL_CACHE_DIR": filepath.Join(root, ".easy-pz", "cache"), "PANEL_STEAM_HOME": filepath.Join(root, ".easy-pz", "steam"),
		"PANEL_LOG_DIR": filepath.Join(root, ".easy-pz", "logs"), "PANEL_STEAMCMD": filepath.Join(root, "steamcmd", "steamcmd.sh"),
		"PANEL_STOP_TIMEOUT": "5s",
	} {
		t.Setenv(k, v)
	}
	cfg, err := app.Load()
	require.NoError(t, err)
	cfg.Root = root // Load roots it at the test binary's directory

	r := &pelicanRun{out: &syncBuf{}, logs: &syncBuf{}, done: make(chan error, 1), coord: make(chan *sched.Coordinator, 1)}
	var pr *io.PipeReader
	pr, r.in = io.Pipe()
	consoleIn, consoleOut = pr, r.out
	onCoordinator = func(c *sched.Coordinator) { r.coord <- c }
	t.Cleanup(func() {
		r.in.Close()
		consoleIn, consoleOut, onCoordinator = nil, nil, nil
	})
	log := slog.New(slog.NewJSONHandler(r.logs, nil))
	go func() { r.done <- Run(context.Background(), cfg, log, "test") }()
	return r
}

func (r *pelicanRun) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(60 * time.Second):
		t.Fatalf("Run did not return; console:\n%s", r.out)
		return nil
	}
}

func TestRunPelicanQuit(t *testing.T) {
	r := runPelican(t, "fake")
	r.out.waitFor(t, "SERVER STARTED")
	_, err := io.WriteString(r.in, "quit\n")
	require.NoError(t, err)
	require.NoError(t, r.wait(t), "quit is a clean exit (0)")
	require.Contains(t, r.out.String(), "[pzman] stop requested")
	require.Contains(t, r.out.String(), "[pzman] stopped")
}

func TestRunPelicanBootFailure(t *testing.T) {
	r := runPelican(t, "real") // no game, no SteamCMD: the install fails
	err := r.wait(t)
	require.ErrorContains(t, err, "boot failed: game install failed")
	require.Contains(t, r.out.String(), "[pzman] boot failed: game install failed")
}

// A start from the UI or a schedule while the boot is still fetching mods
// must not make the boot's own start fatal.
func TestRunPelicanStartDuringBoot(t *testing.T) {
	r := runPelican(t, "fake")
	coord := <-r.coord
	r.out.waitFor(t, "[steamcmd] Downloading item") // a fresh database fetches the scenario's mods first
	require.NoError(t, coord.Start(context.Background()))
	r.out.waitFor(t, "SERVER STARTED")
	r.logs.waitFor(t, "game server already started during boot")
	select {
	case err := <-r.done:
		t.Fatalf("Run returned %v; console:\n%s", err, r.out)
	default:
	}
	require.NotContains(t, r.out.String(), "boot failed")
	_, err := io.WriteString(r.in, "quit\n")
	require.NoError(t, err)
	require.NoError(t, r.wait(t))
}
