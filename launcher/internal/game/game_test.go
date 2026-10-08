package game

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func touch(t *testing.T, parts ...string) {
	t.Helper()
	p := filepath.Join(parts...)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, nil, 0o755))
}

func TestOpenPerOS(t *testing.T) {
	lin := t.TempDir()
	touch(t, lin, "projectzomboid.sh")
	touch(t, lin, "projectzomboid.jar")
	in, err := open(lin, "linux")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(lin, "projectzomboid.jar"), in.Jar())
	require.Equal(t, []string{filepath.Join(lin, "projectzomboid.sh"), "-nosteam"}, in.Command().Args)

	// Steam's Linux layout (42.x): the script at the root, the game one level down.
	nested := t.TempDir()
	touch(t, nested, "projectzomboid.sh")
	touch(t, nested, "projectzomboid", "projectzomboid.jar")
	touch(t, nested, "projectzomboid", "ProjectZomboid64")
	for _, pick := range []string{nested, filepath.Join(nested, "projectzomboid")} {
		in, err = open(pick, "linux")
		require.NoError(t, err, pick)
		require.Equal(t, nested, in.Dir)
		require.Equal(t, filepath.Join(nested, "projectzomboid", "projectzomboid.jar"), in.Jar())
		require.Equal(t, []string{filepath.Join(nested, "projectzomboid.sh"), "-nosteam"}, in.Command().Args)
	}

	win := t.TempDir()
	touch(t, win, "ProjectZomboid64.exe")
	touch(t, win, "java", "projectzomboid.jar")
	in, err = open(win, "windows")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(win, "java", "projectzomboid.jar"), in.Jar())

	mac := t.TempDir()
	touch(t, mac, macBundle, "Contents", "Info.plist")
	touch(t, mac, macBundle, "Contents", "Java", "projectzomboid.jar")
	in, err = open(mac, "darwin")
	require.NoError(t, err)
	require.Equal(t, []string{"open", filepath.Join(mac, macBundle), "--args", "-nosteam"}, in.Command().Args)

	_, err = open(lin, "windows")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = open(t.TempDir(), "linux")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestCandidatesReadSteamLibraries(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".local", "share", "Steam")
	vdf := `"libraryfolders"
{
	"0" { "path" "` + filepath.ToSlash(root) + `" }
	"1" { "path" "/mnt/games/SteamLibrary" }
}`
	require.NoError(t, os.MkdirAll(filepath.Join(root, "steamapps"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644))

	got := candidates("linux", home, func(string) string { return "" })
	require.Contains(t, got, filepath.Join(root, "steamapps", "common", steamFolder))
	require.Contains(t, got, filepath.Join("/mnt/games/SteamLibrary", "steamapps", "common", steamFolder))
	seen := map[string]bool{}
	for _, c := range got {
		require.False(t, seen[c], "duplicate %s", c)
		seen[c] = true
	}

	win := candidates("windows", `C:\Users\me`, func(k string) string {
		if k == "ProgramFiles(x86)" {
			return `C:\Program Files (x86)`
		}
		return ""
	})
	require.True(t, strings.Contains(strings.Join(win, "|"), "Steam"), win)
	require.Contains(t, win, filepath.Join(`C:\Program Files (x86)`, "GOG Galaxy", "Games", "Project Zomboid"))
}
