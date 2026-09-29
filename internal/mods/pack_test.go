package mods

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPackerBuildsOnceAndReportsFailureOnce(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "src", "ModA")
	require.NoError(t, os.MkdirAll(mod, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mod, "mod.info"), []byte("id=A\n"), 0o644))
	p := NewPacker(filepath.Join(dir, "cache"), slog.New(slog.DiscardHandler))
	ms := []ModInfo{{ID: "A", FolderName: "ModA", Dir: mod}}

	_, ready, err := p.Get("k", ms)
	require.NoError(t, err)
	require.False(t, ready)
	var pk Pack
	require.Eventually(t, func() bool {
		pk, ready, err = p.Get("k", ms)
		return ready
	}, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, pk.SHA256, 64)
	require.Positive(t, pk.Size)

	bad := []ModInfo{{ID: "B", FolderName: "ModB", Dir: filepath.Join(dir, "missing")}}
	_, _, err = p.Get("bad", bad)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, _, err = p.Get("bad", bad)
		return err != nil
	}, 2*time.Second, 5*time.Millisecond)
	_, _, err = p.Get("bad", bad)
	require.NoError(t, err, "a failure is reported once, then the build is retried")
	require.Eventually(t, func() bool {
		_, _, err = p.Get("bad", bad)
		return err != nil
	}, 2*time.Second, 5*time.Millisecond, "the retry fails again")
}
