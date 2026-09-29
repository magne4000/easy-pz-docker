package modsync

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/launcher/internal/pzclient"
)

// fakeDL serves in-memory zips keyed by pack URL.
type fakeDL struct {
	zips     map[string]map[string]string // url -> entry name -> content
	notReady int                          // answer ErrNotReady this many times first
	calls    int
}

func (f *fakeDL) DownloadFile(_ context.Context, dir, _, packURL, _ string, progress func(int64)) (string, error) {
	f.calls++
	if f.notReady > 0 {
		f.notReady--
		return "", pzclient.ErrNotReady
	}
	out, err := os.CreateTemp(dir, ".easypz-download-*.zip")
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(out)
	for name, content := range f.zips[packURL] {
		w, err := zw.Create(name)
		if err != nil {
			return "", err
		}
		w.Write([]byte(content))
	}
	zw.Close()
	out.Close()
	progress(1)
	return out.Name(), nil
}

func item(id, sha string, folders ...string) publicapi.PublicItem {
	it := publicapi.PublicItem{WorkshopID: id, Title: "item " + id,
		Download: publicapi.PublicPack{URL: "/dl/" + id + ".zip", SHA256: sha, Size: 10}}
	for _, f := range folders {
		it.Mods = append(it.Mods, publicapi.PublicMod{ID: f, Folder: f})
	}
	return it
}

func TestPlanAndApply(t *testing.T) {
	mods := t.TempDir()
	ctx := context.Background()
	dl := &fakeDL{zips: map[string]map[string]string{
		"/dl/1.zip": {"ModA/42/mod.info": "id=ModA", "ModA/common/x.lua": "v1"},
		"/dl/2.zip": {"ModB/42/mod.info": "id=ModB", "ModC/42/mod.info": "id=ModC"},
	}}
	data := &publicapi.PublicData{Items: []publicapi.PublicItem{item("1", "aa", "ModA"), item("2", "bb", "ModB", "ModC")}}
	in := Installed{}

	p := MakePlan(mods, data, in)
	require.Len(t, p.Download, 2)
	require.Equal(t, int64(20), p.Bytes)
	require.Empty(t, p.Foreign)

	var saves int
	var last Progress
	s := &Syncer{ModsDir: mods, Page: "http://x/mods/t/", DL: dl}
	require.NoError(t, s.Apply(ctx, p, in, false, func(pr Progress) { last = pr }, func(Installed) error { saves++; return nil }))
	require.Equal(t, 2, saves)
	require.Equal(t, Progress{Index: 1, Total: 2, Title: "item 2", Bytes: 1, Size: 10}, last)
	require.Equal(t, Installed{"1": {SHA256: "aa", Folders: []string{"ModA"}}, "2": {SHA256: "bb", Folders: []string{"ModB", "ModC"}}}, in)
	require.FileExists(t, filepath.Join(mods, "ModA", "common", "x.lua"))
	require.FileExists(t, filepath.Join(mods, "ModC", "42", "mod.info"))

	p = MakePlan(mods, data, in)
	require.Empty(t, p.Download)
	require.Equal(t, 2, p.UpToDate)

	// A new version of item 1 replaces the folder (old files go away).
	dl.zips["/dl/1.zip"] = map[string]string{"ModA/42/mod.info": "id=ModA", "ModA/common/y.lua": "v2"}
	data.Items[0].Download.SHA256 = "a2"
	p = MakePlan(mods, data, in)
	require.Len(t, p.Download, 1)
	require.Empty(t, p.Foreign, "ModA is ours")
	require.NoError(t, s.Apply(ctx, p, in, false, nil, func(Installed) error { return nil }))
	require.FileExists(t, filepath.Join(mods, "ModA", "common", "y.lua"))
	require.NoFileExists(t, filepath.Join(mods, "ModA", "common", "x.lua"))

	// A folder deleted by the player is reinstalled.
	require.NoError(t, os.RemoveAll(filepath.Join(mods, "ModB")))
	require.Len(t, MakePlan(mods, data, in).Download, 1)

	entries, _ := os.ReadDir(mods)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".easypz-", "temp files left behind")
	}
}

func TestForeignFolders(t *testing.T) {
	mods := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(mods, "ModA"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mods, "ModA", "mine.txt"), []byte("x"), 0o644))
	data := &publicapi.PublicData{Items: []publicapi.PublicItem{item("1", "aa", "ModA")}}
	in := Installed{}
	p := MakePlan(mods, data, in)
	require.Equal(t, []string{"ModA"}, p.Foreign)

	dl := &fakeDL{zips: map[string]map[string]string{"/dl/1.zip": {"ModA/42/mod.info": "id=ModA"}}}
	s := &Syncer{ModsDir: mods, DL: dl}
	noSave := func(Installed) error { return nil }
	require.ErrorIs(t, s.Apply(context.Background(), p, in, false, nil, noSave), ErrForeign)
	require.FileExists(t, filepath.Join(mods, "ModA", "mine.txt"), "untouched without takeOver")

	require.NoError(t, s.Apply(context.Background(), p, in, true, nil, noSave))
	require.NoFileExists(t, filepath.Join(mods, "ModA", "mine.txt"))
	require.Empty(t, MakePlan(mods, data, in).Foreign)
}

func TestNotReadyRetries(t *testing.T) {
	mods := t.TempDir()
	dl := &fakeDL{notReady: 2, zips: map[string]map[string]string{"/dl/1.zip": {"ModA/42/mod.info": "id=ModA"}}}
	data := &publicapi.PublicData{Items: []publicapi.PublicItem{item("1", "aa", "ModA")}}
	s := &Syncer{ModsDir: mods, DL: dl, Retry: time.Millisecond, Attempts: 3}
	require.NoError(t, s.Apply(context.Background(), MakePlan(mods, data, Installed{}), Installed{}, false, nil, func(Installed) error { return nil }))
	require.Equal(t, 3, dl.calls)

	dl = &fakeDL{notReady: 5}
	s = &Syncer{ModsDir: mods, DL: dl, Retry: time.Millisecond, Attempts: 2}
	var nr *NotReadyError
	require.ErrorAs(t, s.Apply(context.Background(), MakePlan(t.TempDir(), data, Installed{}), Installed{}, false, nil, nil), &nr)
}

func TestExtractRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "evil.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../evil.txt")
	w.Write([]byte("x"))
	zw.Close()
	f.Close()
	require.ErrorContains(t, extract(zp, filepath.Join(dir, "out")), "unsafe path")
	require.NoFileExists(t, filepath.Join(dir, "evil.txt"))
}

func TestCleanup(t *testing.T) {
	mods := t.TempDir()
	for _, n := range []string{".easypz-extract-1", ".easypz-old-ModA1", "ModA"} {
		require.NoError(t, os.MkdirAll(filepath.Join(mods, n), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(mods, ".easypz-download-1.zip"), nil, 0o644))
	Cleanup(mods)
	entries, _ := os.ReadDir(mods)
	require.Len(t, entries, 1)
	require.Equal(t, "ModA", entries[0].Name())
}
