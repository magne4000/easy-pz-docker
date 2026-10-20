package pz

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStartCommand(t *testing.T) {
	getenv := func(k string) string {
		return map[string]string{"PATH": "/usr/bin:/bin", "LD_LIBRARY_PATH": "/opt/lib", "LD_PRELOAD": ""}[k]
	}

	dir := t.TempDir()
	_, _, err := startCommand(dir, getenv)
	require.ErrorContains(t, err, "neither start-server.sh nor ProjectZomboid64 found")

	// The stock Pelican egg deletes start-server.sh after installing.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ProjectZomboid64"), []byte("#!/bin/sh\n"), 0o755))
	bin, env, err := startCommand(dir, getenv)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "ProjectZomboid64"), bin)
	require.Equal(t, []string{
		"PATH=" + dir + "/jre64/bin:/usr/bin:/bin",
		"LD_LIBRARY_PATH=" + dir + "/linux64:" + dir + "/natives:" + dir + ":" + dir + "/jre64/lib/amd64:/opt/lib",
		"LD_PRELOAD=:libjsig.so",
	}, env, "start-server.sh's 64-bit branch")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "start-server.sh"), []byte("#!/bin/sh\n"), 0o644))
	bin, env, err = startCommand(dir, getenv)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "start-server.sh"), bin, "the script wins when present")
	require.Nil(t, env)
}

// Without start-server.sh the supervisor runs ProjectZomboid64 from the
// install directory, with the script's arguments and environment.
func TestSupervisorWithoutStartScript(t *testing.T) {
	dir := t.TempDir()
	game := `echo "cwd=$(pwd) args=$* preload=$LD_PRELOAD"; echo "lib=$LD_LIBRARY_PATH"; echo "*** SERVER STARTED ***"; ` +
		`while read l; do [ "$l" = quit ] && exit 0; done`
	// deliberately not executable: the supervisor must chmod it
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ProjectZomboid64"), []byte("#!/bin/sh\n"+game), 0o644))
	rec := &recorder{}
	data := t.TempDir()
	p := NewProcess(ProcessOptions{InstallDir: dir, DataDir: data, ServerName: "s", UID: -1, StopTimeout: 5 * time.Second,
		Hooks: rec.hooks()})
	require.NoError(t, p.Start(context.Background()))
	require.Eventually(t, func() bool { return p.Status().State == StateRunning }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, p.Stop(context.Background()))

	rec.mu.Lock()
	out := strings.Join(rec.lines, "\n")
	rec.mu.Unlock()
	real, err := filepath.EvalSymlinks(dir) // pwd prints the resolved path (macOS /private/var)
	require.NoError(t, err)
	require.Contains(t, out, "cwd="+real+" args=-cachedir="+data+" -servername s")
	require.Contains(t, out, "preload="+os.Getenv("LD_PRELOAD")+":libjsig.so")
	require.Contains(t, out, "lib="+dir+"/linux64:"+dir+"/natives:")
	require.Equal(t, "clean", p.Status().LastExit.Classification)
}
