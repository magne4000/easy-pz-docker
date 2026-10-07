package httpapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
)

func TestPublicConnect(t *testing.T) {
	cfg := testConfig()
	cfg.DataDir = t.TempDir()
	cfg.DefaultPort = 16261
	d := Deps{Cfg: cfg}

	require.Nil(t, publicConnect(d), "neither PANEL_PUBLIC_URL nor PANEL_PUBLIC_GAME_ADDRESS: omitted")

	d.Cfg.PublicURL = "https://pz.example.com:8443"
	require.Equal(t, &publicapi.PublicConnect{Host: "pz.example.com", Port: 16261}, publicConnect(d),
		"no ini: env default; the URL's port is the panel's, not the game's")

	require.NoError(t, os.MkdirAll(filepath.Dir(iniPath(d)), 0o755))
	require.NoError(t, os.WriteFile(iniPath(d), []byte("DefaultPort=17000\n"), 0o644))
	require.Equal(t, 17000, publicConnect(d).Port, "ini wins")

	require.NoError(t, os.WriteFile(iniPath(d), []byte("DefaultPort=nope\n"), 0o644))
	require.Equal(t, 16261, publicConnect(d).Port, "invalid ini value: env default")

	require.NoError(t, os.WriteFile(iniPath(d), []byte("DefaultPort=17000\n"), 0o644))
	d.Cfg.PublicGameAddress = "play.example.com"
	require.Equal(t, &publicapi.PublicConnect{Host: "play.example.com", Port: 17000}, publicConnect(d),
		"game address wins over the URL's host; without a port, the ini's")

	d.Cfg.PublicGameAddress = "203.0.113.7:26261"
	require.Equal(t, &publicapi.PublicConnect{Host: "203.0.113.7", Port: 26261}, publicConnect(d),
		"an explicit port wins over the ini (port forwarding)")

	d.Cfg.PublicURL = ""
	require.Equal(t, "203.0.113.7", publicConnect(d).Host, "works without PANEL_PUBLIC_URL")
}

func TestPublicLauncher(t *testing.T) {
	d := Deps{Cfg: testConfig(), Version: "v0.2.0"}
	rel := "https://github.com/magne4000/easy-pz-docker/releases/"
	require.Equal(t, &publicapi.PublicLauncher{Version: "v0.2.0", ReleaseURL: rel + "tag/v0.2.0", Downloads: []publicapi.PublicDownload{
		{OS: "windows", Arch: "amd64", URL: rel + "download/v0.2.0/easypz-launcher-windows-amd64.exe"},
		{OS: "darwin", Arch: "universal", URL: rel + "download/v0.2.0/easypz-launcher-darwin-universal.zip"},
		{OS: "linux", Arch: "amd64", URL: rel + "download/v0.2.0/easypz-launcher-linux-amd64"},
		{OS: "linux", Arch: "arm64", URL: rel + "download/v0.2.0/easypz-launcher-linux-arm64"},
	}}, publicLauncher(d))

	for _, v := range []string{"dev", "", "main", "v0.2.0-3-gf058b26", "v0.2.0-dirty", "v0.2.0-rc.1", "v0.2", "0.2.0"} {
		d.Version = v
		require.Nil(t, publicLauncher(d), "no release for %q", v)
	}

	d.Version, d.Cfg.UseSteam = "v0.2.0", true
	require.Nil(t, publicLauncher(d), "Steam mode: the launcher's -nosteam cannot join")
}

func TestJarVersionUnreadable(t *testing.T) {
	var j jarVersion
	dir := t.TempDir()
	require.Empty(t, j.get(filepath.Join(dir, "missing.jar")))

	bad := filepath.Join(dir, "projectzomboid.jar")
	require.NoError(t, os.WriteFile(bad, []byte("not a zip"), 0o644))
	require.Empty(t, j.get(bad))
	require.Equal(t, bad, j.path, "the failed read is cached for this file")
}
