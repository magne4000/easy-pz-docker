package mods

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func ids(ms []ModInfo) string {
	var s []string
	for _, m := range ms {
		s = append(s, m.ID)
	}
	return strings.Join(s, ",")
}

func TestParseModInfo(t *testing.T) {
	m, err := ParseModInfo(strings.NewReader("\xef\xbb\xbfname=My Mod\r\nid=\\MyMod\r\nrequire=\\A, \\B\r\nposter=poster.png\r\n"))
	require.NoError(t, err)
	require.Equal(t, "MyMod", m.ID)
	require.Equal(t, []string{"A", "B"}, m.Requires)
	_, err = ParseModInfo(strings.NewReader("name=x"))
	require.ErrorIs(t, err, ErrNoID)
}

func TestScanWorkshopItem(t *testing.T) {
	item := t.TempDir()
	write(t, filepath.Join(item, "mods", "Old", "mod.info"), "id=Old\n")
	write(t, filepath.Join(item, "mods", "New", "42", "mod.info"), "id=New42\n")
	write(t, filepath.Join(item, "mods", "New", "42.5", "mod.info"), "id=New425\n")
	write(t, filepath.Join(item, "mods", "New", "common", "media", "maps", "MyMap", "x"), "")
	// The common B42 shape (TchernoLib, Wesch's Better Wringing): mod.info only
	// in common/, media in the version dir PZ picks.
	write(t, filepath.Join(item, "mods", "Common", "common", "mod.info"), "id=Common\n")
	write(t, filepath.Join(item, "mods", "Common", "42.15", "media", "maps", "VerMap", "x"), "")
	// B41-only version folders are below B42's 42.0 floor.
	write(t, filepath.Join(item, "mods", "TooOld", "41", "mod.info"), "id=TooOld\n")
	write(t, filepath.Join(item, "mods", "Empty", "readme"), "")
	ms, err := ScanWorkshopItem(item, "123")
	require.NoError(t, err)
	require.Equal(t, "Common,New425,Old", ids(ms))
	require.Equal(t, []string{"VerMap"}, ms[0].Maps)
	require.Equal(t, []string{"MyMap"}, ms[1].Maps)
	require.Equal(t, "123", ms[2].WorkshopID)
}

func TestSortLoadOrder(t *testing.T) {
	in := []ModInfo{{ID: "D", Requires: []string{"B", "C"}}, {ID: "B", Requires: []string{"A"}}, {ID: "C", Requires: []string{"A"}}, {ID: "A"}, {ID: "E", Requires: []string{"Z"}}}
	out, missing, cycles := SortLoadOrder(in)
	require.Equal(t, "A,B,C,D,E", ids(out))
	require.Equal(t, []string{"Z"}, missing["E"])
	require.Empty(t, cycles)

	out, _, cycles = SortLoadOrder([]ModInfo{{ID: "X"}, {ID: "P", Requires: []string{"Q"}}, {ID: "Q", Requires: []string{"P"}}})
	require.Equal(t, "X,Q,P", ids(out))
	require.Len(t, cycles, 1)
}

func TestFindConflictsThroughSymlinks(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "real", "A", "media", "lua", "shared", "x.lua"), "")
	write(t, filepath.Join(root, "real", "A", "mod.info"), "id=A")
	write(t, filepath.Join(root, "B", "media", "lua", "shared", "x.lua"), "")
	write(t, filepath.Join(root, "B", "media", "lua", "shared", "y.lua"), "")
	require.NoError(t, os.Symlink(filepath.Join(root, "real", "A"), filepath.Join(root, "linkA")))
	cs, err := FindConflicts([]ModInfo{{ID: "A", Dir: filepath.Join(root, "linkA")}, {ID: "B", Dir: filepath.Join(root, "B")}})
	require.NoError(t, err)
	require.Equal(t, []Conflict{{Path: "media/lua/shared/x.lua", Mods: []string{"A", "B"}}}, cs)
}

// requireMirror asserts the mirror layout: every directory under dst is real and
// every file is a symlink to the same path under src.
func requireMirror(t *testing.T, dst, src string) {
	t.Helper()
	files := 0
	require.NoError(t, filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		rel, _ := filepath.Rel(src, p)
		st, err := os.Lstat(filepath.Join(dst, rel))
		require.NoError(t, err, rel)
		if d.IsDir() {
			require.True(t, st.IsDir(), "%s must be a real directory", rel)
			return nil
		}
		files++
		target, err := os.Readlink(filepath.Join(dst, rel))
		require.NoError(t, err, "%s must be a symlink", rel)
		require.Equal(t, p, target)
		return nil
	}))
	require.Positive(t, files)
}

func TestReconcileAndVerify(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	modsDir := filepath.Join(root, "mods")
	a, b := filepath.Join(content, "1", "mods", "A"), filepath.Join(content, "1", "mods", "B")
	write(t, filepath.Join(a, "common", "mod.info"), "id=A")
	write(t, filepath.Join(a, "42", "media", "scripts", "a.txt"), "module A {}")
	write(t, filepath.Join(b, "common", "mod.info"), "id=B")
	write(t, filepath.Join(root, "elsewhere", "F", "mod.info"), "id=F")
	write(t, filepath.Join(modsDir, "Real", "common", "mod.info"), "id=Real")
	require.NoError(t, os.Symlink(filepath.Join(root, "elsewhere", "F"), filepath.Join(modsDir, "Foreign")))
	// directory links into the content dir, as earlier versions created them
	require.NoError(t, os.Symlink(filepath.Join(content, "9", "mods", "Gone"), filepath.Join(modsDir, "Gone")))
	require.NoError(t, os.Symlink(a, filepath.Join(modsDir, "A")))

	res, err := ReconcileLinks(modsDir, content, map[string]string{"A": a, "C": b, "Real": b})
	require.NoError(t, err)
	require.Equal(t, []string{"C"}, res.Created)
	require.Equal(t, []string{"A"}, res.Updated)
	require.Equal(t, []string{"Gone"}, res.Removed)
	require.Len(t, res.Errors, 1, "a user-placed directory is never replaced")
	_, err = os.Readlink(filepath.Join(modsDir, "Foreign"))
	require.NoError(t, err, "foreign links are left alone")
	requireMirror(t, filepath.Join(modsDir, "A"), a)
	requireMirror(t, filepath.Join(modsDir, "C"), b)

	res, err = ReconcileLinks(modsDir, content, map[string]string{"A": a, "C": b})
	require.NoError(t, err)
	require.Equal(t, []string{"A", "C"}, res.Kept)
	require.Empty(t, res.Removed, "the user's Real directory is not pzman's to remove")

	// A's source changes: files it no longer has disappear from the mirror.
	res, err = ReconcileLinks(modsDir, content, map[string]string{"A": b})
	require.NoError(t, err)
	require.Equal(t, []string{"A"}, res.Updated)
	require.Equal(t, []string{"C"}, res.Removed)
	requireMirror(t, filepath.Join(modsDir, "A"), b)
	_, err = os.Lstat(filepath.Join(modsDir, "A", "42"))
	require.ErrorIs(t, err, fs.ErrNotExist)

	require.NoError(t, VerifyLinks(modsDir, []string{"A", "Real"}))
	require.NoError(t, os.RemoveAll(b))
	require.ErrorContains(t, VerifyLinks(modsDir, []string{"A"}), "mod A")
	require.ErrorContains(t, VerifyLinks(modsDir, []string{"Foreign"}), "not a directory")
}
