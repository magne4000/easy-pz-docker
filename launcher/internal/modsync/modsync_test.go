package modsync

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/launcher/internal/pzclient"
)

// fakeDL serves in-memory zips keyed by pack URL; an item's file listing
// (/files/<id>) describes the pack /dl/<id>.zip.
type fakeDL struct {
	zips      map[string]map[string]string // url -> entry name -> content
	notReady  int                          // answer ErrNotReady this many times first
	calls     int
	whole     int        // whole-pack downloads
	requested [][]string // entries asked for by partial downloads
	corrupt   bool       // partial downloads send wrong content
}

func (f *fakeDL) DownloadFile(_ context.Context, dir, _, packURL, _ string, progress func(int64)) (string, error) {
	f.calls++
	if f.notReady > 0 {
		f.notReady--
		return "", pzclient.ErrNotReady
	}
	f.whole++
	return writeZip(dir, f.zips[packURL], progress)
}

func writeZip(dir string, entries map[string]string, progress func(int64)) (string, error) {
	out, err := os.CreateTemp(dir, ".easypz-download-*.zip")
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(out)
	for name, content := range entries {
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

func packOf(filesURL string) string { return "/dl/" + strings.TrimPrefix(filesURL, "/files/") + ".zip" }

func (f *fakeDL) listing(filesURL string) *publicapi.PackFiles {
	m := f.zips[packOf(filesURL)]
	l := &publicapi.PackFiles{SHA256: "listed-" + filesURL}
	for _, name := range slices.Sorted(maps.Keys(m)) {
		sum := sha256.Sum256([]byte(m[name]))
		l.Files = append(l.Files, publicapi.PackFile{Path: name, Size: int64(len(m[name])), Packed: int64(len(m[name])), SHA256: hex.EncodeToString(sum[:])})
	}
	return l
}

func (f *fakeDL) Files(_ context.Context, _, filesURL string) (*publicapi.PackFiles, error) {
	f.calls++
	return f.listing(filesURL), nil
}

func (f *fakeDL) DownloadFiles(_ context.Context, dir, _, filesURL string, sel publicapi.FilesRequest, progress func(int64)) (string, error) {
	f.calls++
	l := f.listing(filesURL)
	if sel.SHA256 != l.SHA256 {
		return "", pzclient.ErrChanged
	}
	m := f.zips[packOf(filesURL)]
	pick := map[string]string{}
	var names []string
	for _, i := range sel.Files {
		n := l.Files[i].Path
		pick[n] = m[n]
		if f.corrupt {
			pick[n] += "!"
		}
		names = append(names, n)
	}
	f.requested = append(f.requested, names)
	return writeZip(dir, pick, progress)
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

func TestPatchDownloadsOnlyChangedFiles(t *testing.T) {
	mods := t.TempDir()
	ctx := context.Background()
	big := strings.Repeat("x", 100)
	dl := &fakeDL{zips: map[string]map[string]string{"/dl/1.zip": {
		"ModA/42/mod.info": "id=ModA", "ModA/a.lua": "a1", "ModA/b.lua": "b1", "ModA/old/x.lua": "x", "ModA/big.txt": big}}}
	it := item("1", "v1", "ModA")
	it.Files = "/files/1"
	data := &publicapi.PublicData{Items: []publicapi.PublicItem{it}}
	in := Installed{}
	s := &Syncer{ModsDir: mods, DL: dl}
	noSave := func(Installed) error { return nil }
	require.NoError(t, s.Apply(ctx, MakePlan(mods, data, in), in, false, nil, noSave))
	require.Equal(t, 1, dl.whole, "nothing on disk yet: the whole pack")
	require.NoError(t, os.WriteFile(filepath.Join(mods, "ModA", "junk.txt"), []byte("mine"), 0o644))

	// v2 changes a.lua, adds new.lua, drops b.lua and old/.
	dl.zips["/dl/1.zip"] = map[string]string{"ModA/42/mod.info": "id=ModA", "ModA/a.lua": "a2", "ModA/new.lua": "n", "ModA/big.txt": big}
	data.Items[0].Download.SHA256 = "v2"
	var last Progress
	require.NoError(t, s.Apply(ctx, MakePlan(mods, data, in), in, false, func(p Progress) { last = p }, noSave))
	require.Equal(t, 1, dl.whole)
	require.Equal(t, [][]string{{"ModA/a.lua", "ModA/new.lua"}}, dl.requested)
	require.Equal(t, int64(3), last.Size, "progress counts the partial download")
	require.Equal(t, Item{SHA256: "listed-/files/1", Folders: []string{"ModA"}}, in["1"])
	var got []string
	require.NoError(t, filepath.WalkDir(filepath.Join(mods, "ModA"), func(p string, e os.DirEntry, err error) error {
		rel, _ := filepath.Rel(mods, p)
		if !e.IsDir() {
			b, _ := os.ReadFile(p)
			rel += "=" + string(b)
		}
		got = append(got, filepath.ToSlash(rel))
		return err
	}))
	require.Equal(t, []string{"ModA", "ModA/42", "ModA/42/mod.info=id=ModA", "ModA/a.lua=a2", "ModA/big.txt=" + big, "ModA/new.lua=n"}, got)

	// Mostly different: the whole pack again.
	dl.zips["/dl/1.zip"]["ModA/big.txt"] = strings.Repeat("y", 100)
	data.Items[0].Download.SHA256 = "v3"
	require.NoError(t, s.Apply(ctx, MakePlan(mods, data, in), in, false, nil, noSave))
	require.Equal(t, 2, dl.whole)
	require.Len(t, dl.requested, 1)
}

func TestPatchChecksBeforeReplacing(t *testing.T) {
	mods := t.TempDir()
	big := strings.Repeat("x", 100)
	dl := &fakeDL{zips: map[string]map[string]string{"/dl/1.zip": {"ModA/a.lua": "a1", "ModA/big.txt": big}}}
	it := item("1", "v1", "ModA")
	it.Files = "/files/1"
	data := &publicapi.PublicData{Items: []publicapi.PublicItem{it}}
	in := Installed{}
	s := &Syncer{ModsDir: mods, DL: dl}
	noSave := func(Installed) error { return nil }
	require.NoError(t, s.Apply(context.Background(), MakePlan(mods, data, in), in, false, nil, noSave))

	dl.zips["/dl/1.zip"] = map[string]string{"ModA/a.lua": "a2", "ModA/big.txt": big}
	data.Items[0].Download.SHA256 = "v2"
	dl.corrupt = true
	require.ErrorContains(t, s.Apply(context.Background(), MakePlan(mods, data, in), in, false, nil, noSave), "checksum")
	b, err := os.ReadFile(filepath.Join(mods, "ModA", "a.lua"))
	require.NoError(t, err)
	require.Equal(t, "a1", string(b))
	require.Equal(t, "v1", in["1"].SHA256, "still outdated")
}

// A file whose name only changed case must survive on case-insensitive disks
// (where it is the listed file) and be replaced on case-sensitive ones.
func TestPatchCaseOnlyRename(t *testing.T) {
	mods := t.TempDir()
	big := strings.Repeat("x", 100)
	dl := &fakeDL{zips: map[string]map[string]string{"/dl/1.zip": {"ModA/Readme.txt": "r", "ModA/big.txt": big}}}
	it := item("1", "v1", "ModA")
	it.Files = "/files/1"
	data := &publicapi.PublicData{Items: []publicapi.PublicItem{it}}
	in := Installed{}
	s := &Syncer{ModsDir: mods, DL: dl}
	noSave := func(Installed) error { return nil }
	require.NoError(t, s.Apply(context.Background(), MakePlan(mods, data, in), in, false, nil, noSave))

	dl.zips["/dl/1.zip"] = map[string]string{"ModA/README.txt": "r", "ModA/big.txt": big}
	data.Items[0].Download.SHA256 = "v2"
	require.NoError(t, s.Apply(context.Background(), MakePlan(mods, data, in), in, false, nil, noSave))
	b, err := os.ReadFile(filepath.Join(mods, "ModA", "README.txt"))
	require.NoError(t, err)
	require.Equal(t, "r", string(b))
	entries, err := os.ReadDir(filepath.Join(mods, "ModA"))
	require.NoError(t, err)
	require.Len(t, entries, 2)
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
