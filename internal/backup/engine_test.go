package backup

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func seed(t *testing.T) Set {
	root := t.TempDir()
	write(t, filepath.Join(root, "Saves/Multiplayer/s/map_0_0.bin"), "chunk")
	write(t, filepath.Join(root, "Saves/Multiplayer/s/sub/players.db"), "players")
	write(t, filepath.Join(root, "db/s.db"), "accounts")
	write(t, filepath.Join(root, "Server/s.ini"), "PVP=true\n")
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(root, "Saves/Multiplayer/s/link")))
	return DefaultSet(root, "s")
}

func TestFingerprint(t *testing.T) {
	s := seed(t)
	fingerprint := func() Fingerprint {
		_, fp, err := walk(s)
		require.NoError(t, err)
		return fp
	}
	a := fingerprint()
	require.Equal(t, 4, a.FileCount)
	require.Equal(t, a.String(), fingerprint().String())

	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(s.Root, "db/s.db"), future, future))
	require.NotEqual(t, a.String(), fingerprint().String())

	write(t, filepath.Join(s.Root, "Server/s_SandboxVars.lua"), "x")
	require.Equal(t, 5, fingerprint().FileCount)
}

func TestRoundTrip(t *testing.T) {
	s := seed(t)
	dst := filepath.Join(t.TempDir(), "b.tar.zst")
	m, size, err := Create(context.Background(), s, dst, Manifest{ServerName: "s", CreatedAt: time.Now()}, nil)
	require.NoError(t, err)
	require.Positive(t, size)
	require.Len(t, m.Files, 4)
	require.NoError(t, Verify(context.Background(), dst))

	os.Remove(dst + ".manifest.json")
	m2, err := ReadManifest(dst)
	require.NoError(t, err)
	require.Equal(t, m.Fingerprint.String(), m2.Fingerprint.String())

	// mutate the live tree, then restore over it
	write(t, filepath.Join(s.Root, "Saves/Multiplayer/s/map_0_0.bin"), "changed")
	write(t, filepath.Join(s.Root, "Saves/Multiplayer/s/new.bin"), "new")
	require.NoError(t, Restore(context.Background(), dst, s.Root, nil))
	b, _ := os.ReadFile(filepath.Join(s.Root, "Saves/Multiplayer/s/map_0_0.bin"))
	require.Equal(t, "chunk", string(b))
	_, err = os.Stat(filepath.Join(s.Root, "Saves/Multiplayer/s/new.bin"))
	require.ErrorIs(t, err, os.ErrNotExist)
	entries, _ := os.ReadDir(s.Root)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".pzman-")
	}

	other := t.TempDir()
	require.NoError(t, Restore(context.Background(), dst, other, nil))
	b, _ = os.ReadFile(filepath.Join(other, "Saves/Multiplayer/s/sub/players.db"))
	require.Equal(t, "players", string(b))
}

func rewrite(t *testing.T, src, dst string, mutate func(h *tar.Header, body []byte) (*tar.Header, []byte)) {
	f, _ := os.Open(src)
	defer f.Close()
	zr, _ := zstd.NewReader(f)
	defer zr.Close()
	tr := tar.NewReader(zr)
	out, _ := os.Create(dst)
	defer out.Close()
	zw, _ := zstd.NewWriter(out)
	tw := tar.NewWriter(zw)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		body, _ := io.ReadAll(tr)
		h, body = mutate(h, body)
		h.Size = int64(len(body))
		require.NoError(t, tw.WriteHeader(h))
		tw.Write(body)
	}
	tw.Close()
	zw.Close()
}

func TestTamperAndTraversal(t *testing.T) {
	s := seed(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "b.tar.zst")
	_, _, err := Create(context.Background(), s, dst, Manifest{}, nil)
	require.NoError(t, err)
	manifest, _ := os.ReadFile(dst + ".manifest.json")

	bad := filepath.Join(dir, "bad.tar.zst")
	rewrite(t, dst, bad, func(h *tar.Header, b []byte) (*tar.Header, []byte) {
		if h.Name == "db/s.db" {
			return h, []byte("ACCOUNTS")
		}
		return h, b
	})
	os.WriteFile(bad+".manifest.json", manifest, 0o644)
	require.ErrorContains(t, Verify(context.Background(), bad), "checksum")
	require.Error(t, Restore(context.Background(), bad, s.Root, nil))
	b, _ := os.ReadFile(filepath.Join(s.Root, "db/s.db"))
	require.Equal(t, "accounts", string(b))

	evil := filepath.Join(dir, "evil.tar.zst")
	rewrite(t, dst, evil, func(h *tar.Header, b []byte) (*tar.Header, []byte) {
		if h.Name == "db/s.db" {
			h.Name = "../escape"
		}
		return h, b
	})
	os.WriteFile(evil+".manifest.json", manifest, 0o644)
	require.ErrorContains(t, Restore(context.Background(), evil, s.Root, nil), "unsafe")
}

func TestCancelLeavesNoPartial(t *testing.T) {
	s := seed(t)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Create(ctx, s, filepath.Join(dir, "b.tar.zst"), Manifest{}, nil)
	require.ErrorIs(t, err, context.Canceled)
	entries, _ := os.ReadDir(dir)
	require.Empty(t, entries)
}

func TestRetention(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var items []Info
	for i := range 20 { // one per 12h, id 1 oldest
		items = append(items, Info{ID: int64(i + 1), CreatedAt: base.Add(time.Duration(i) * 12 * time.Hour), Size: 10})
	}
	del := PlanRetention(items, Policy{Keep: 4}, base)
	require.Len(t, del, 16)
	require.Equal(t, int64(1), del[0])

	// daily thinning: days already covered by the recent tier don't use a daily slot
	del = PlanRetention(items, Policy{Keep: 4, KeepDaily: 3}, base)
	require.Len(t, del, 13)
	for _, id := range []int64{15, 13, 11} {
		require.NotContains(t, del, id)
	}
	require.Contains(t, del, int64(16))

	// weekly tier reaches further back than the daily one
	del = PlanRetention(items, Policy{Keep: 4, KeepDaily: 1, KeepWeekly: 2}, base)
	require.Len(t, del, 14)

	items[0].Pinned = true
	del = PlanRetention(items, Policy{Keep: 4}, base)
	require.NotContains(t, del, int64(1))

	del = PlanRetention(items, Policy{Keep: 10, MaxTotalBytes: 35}, base)
	kept := 20 - len(del)
	require.Equal(t, 3, kept) // pinned + 2 newest = 30 bytes
	require.NotContains(t, del, int64(20))

	del = PlanRetention([]Info{{ID: 1, Size: 100}}, Policy{Keep: 0, MaxTotalBytes: 1}, base)
	require.Empty(t, del)
}
