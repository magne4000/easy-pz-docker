package pz

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fakeInstall(t *testing.T, script string) string {
	dir := t.TempDir()
	// deliberately not executable: the supervisor must chmod it
	require.NoError(t, os.WriteFile(filepath.Join(dir, "start-server.sh"), []byte("#!/bin/sh\n"+script), 0o644))
	return dir
}

type recorder struct {
	mu     sync.Mutex
	states []State
	lines  []string
}

func (r *recorder) hooks() Hooks {
	return Hooks{
		OnState: func(s Status) { r.mu.Lock(); r.states = append(r.states, s.State); r.mu.Unlock() },
		OnLine:  func(l string) { r.mu.Lock(); r.lines = append(r.lines, l); r.mu.Unlock() },
	}
}

func (r *recorder) has(s State) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.states {
		if x == s {
			return true
		}
	}
	return false
}

func TestSupervisorCleanStop(t *testing.T) {
	dir := fakeInstall(t, `echo "args: $@"; echo "*** SERVER STARTED ***"; while read l; do echo "got $l"; [ "$l" = quit ] && exit 0; done`)
	rec := &recorder{}
	p := NewProcess(ProcessOptions{InstallDir: dir, DataDir: t.TempDir(), ServerName: "s", UID: -1, StopTimeout: 5 * time.Second, Hooks: rec.hooks(),
		PreStart: func(context.Context) ([]string, error) { return []string{"-adminusername", "a"}, nil }})
	require.NoError(t, p.Start(context.Background()))
	require.Eventually(t, func() bool { return p.Status().State == StateRunning }, 5*time.Second, 10*time.Millisecond)
	require.ErrorIs(t, p.Start(context.Background()), ErrAlreadyRunning)
	require.NoError(t, p.Stop(context.Background()))
	st := p.Status()
	require.Equal(t, StateStopped, st.State)
	require.Equal(t, "clean", st.LastExit.Classification)
	require.Contains(t, rec.lines[0], "-servername s -adminusername a")
	require.True(t, rec.has(StateStopping))
}

func TestSupervisorCrashAndKill(t *testing.T) {
	dir := fakeInstall(t, `echo "*** SERVER STARTED ***"; sleep 0.2; exit 3`)
	p := NewProcess(ProcessOptions{InstallDir: dir, DataDir: t.TempDir(), ServerName: "s", UID: -1})
	require.NoError(t, p.Start(context.Background()))
	require.Eventually(t, func() bool { return p.Status().State == StateCrashed }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, 3, p.Status().LastExit.Code)

	dir = fakeInstall(t, `trap '' TERM; echo "*** SERVER STARTED ***"; while true; do sleep 0.05; done`)
	p = NewProcess(ProcessOptions{InstallDir: dir, DataDir: t.TempDir(), ServerName: "s", UID: -1, StopTimeout: 200 * time.Millisecond})
	p.killGrace = 200 * time.Millisecond
	require.NoError(t, p.Start(context.Background()))
	require.Eventually(t, func() bool { return p.Status().State == StateRunning }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, p.Stop(context.Background()))
	require.Equal(t, "killed", p.Status().LastExit.Classification)
}

func TestSupervisorMissingScript(t *testing.T) {
	p := NewProcess(ProcessOptions{InstallDir: t.TempDir(), DataDir: t.TempDir(), ServerName: "s", UID: -1})
	require.Error(t, p.Start(context.Background()))
	require.Equal(t, "start-failed", p.Status().LastExit.Classification)
}
