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

	require.Nil(t, publicConnect(d), "no PANEL_PUBLIC_HOST: omitted")

	d.Cfg.PublicHost = "pz.example.com"
	require.Equal(t, &publicapi.PublicConnect{Host: "pz.example.com", Port: 16261}, publicConnect(d), "no ini: env default")

	require.NoError(t, os.MkdirAll(filepath.Dir(iniPath(d)), 0o755))
	require.NoError(t, os.WriteFile(iniPath(d), []byte("DefaultPort=17000\n"), 0o644))
	require.Equal(t, 17000, publicConnect(d).Port, "ini wins")

	require.NoError(t, os.WriteFile(iniPath(d), []byte("DefaultPort=nope\n"), 0o644))
	require.Equal(t, 16261, publicConnect(d).Port, "invalid ini value: env default")
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
