package mods

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
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

func TestPackerListing(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "src", "ModA")
	require.NoError(t, os.MkdirAll(filepath.Join(mod, "media"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mod, "mod.info"), []byte("id=A\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(mod, "media", "x.lua"), []byte("print(1)"), 0o644))
	cache := filepath.Join(dir, "cache")
	p := NewPacker(cache, slog.New(slog.DiscardHandler))
	ms := []ModInfo{{ID: "A", FolderName: "ModA", Dir: mod}}
	ready := func() Pack {
		var pk Pack
		require.Eventually(t, func() bool {
			var ok bool
			pk, ok, _ = p.Get("k", ms)
			return ok
		}, 2*time.Second, 5*time.Millisecond)
		return pk
	}
	pk := ready()

	var l publicapi.PackFiles
	b, err := os.ReadFile(pk.Files)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &l))
	require.Equal(t, pk.SHA256, l.SHA256)
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	require.Len(t, l.Files, 2)
	for i, want := range []publicapi.PackFile{{Path: "ModA/media/x.lua", Size: 8, SHA256: sum("print(1)")}, {Path: "ModA/mod.info", Size: 5, SHA256: sum("id=A\n")}} {
		require.Positive(t, l.Files[i].Packed, want.Path)
		want.Packed = l.Files[i].Packed
		require.Equal(t, want, l.Files[i])
	}

	// A pack cached without a listing (older version) is rebuilt.
	require.NoError(t, os.Remove(pk.Files))
	_, ok, err := p.Get("k", ms)
	require.NoError(t, err)
	require.False(t, ok)
	require.FileExists(t, ready().Files)

	p.Prune()
	entries, err := os.ReadDir(cache)
	require.NoError(t, err)
	require.Empty(t, entries)
}
