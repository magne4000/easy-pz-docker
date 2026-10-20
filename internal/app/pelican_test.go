package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// server lays out <tmp>/home/container/.easy-pz/ with an optional pzman.env
// and returns the root and the executable's path.
func server(t *testing.T, envFile string) (root, exe string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "home", "container")
	dir := filepath.Join(root, ".easy-pz")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	if envFile != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, EnvFileName), []byte(envFile), 0o644))
	}
	return root, filepath.Join(dir, "pzman")
}

func TestReadEnvFile(t *testing.T) {
	_, exe := server(t, "# written by the plugin\n\n  # indented comment\nPZMAN_HOST=pelican\r\n"+
		"PANEL_PUBLIC_URL=https://panel.example.com/easy-pz/abc\nQUOTED=\"kept\"\nEXPANDED=$HOME/x\nEQ=a=b\nEMPTY=\nSPACES= x \n")
	got, err := readEnvFile(filepath.Join(filepath.Dir(exe), EnvFileName))
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"PZMAN_HOST": "pelican", "PANEL_PUBLIC_URL": "https://panel.example.com/easy-pz/abc",
		"QUOTED": `"kept"`, "EXPANDED": "$HOME/x", "EQ": "a=b", "EMPTY": "", "SPACES": " x ",
	}, got, "values are verbatim")

	_, exe = server(t, "A=1\nnot a line\n")
	_, err = readEnvFile(filepath.Join(filepath.Dir(exe), EnvFileName))
	require.ErrorContains(t, err, "pzman.env:2: expected KEY=value")

	got, err = readEnvFile(filepath.Join(t.TempDir(), EnvFileName))
	require.NoError(t, err, "a missing file is fine")
	require.Empty(t, got)
}

func TestLoadEnvFileAndEnvironment(t *testing.T) {
	_, exe := server(t, "PANEL_ADMIN_PASSWORD_HASH="+testHash+"\nPANEL_PORT=9000\nSERVER_NAME=fromfile\n")
	cfg, err := load([]string{"SERVER_NAME=fromenv"}, exe)
	require.NoError(t, err)
	require.Equal(t, HostDocker, cfg.Host, "docker is the default")
	require.Equal(t, 9000, cfg.Port, "the file applies")
	require.Equal(t, "fromenv", cfg.ServerName, "the environment wins")
	require.Equal(t, "/project-zomboid", cfg.InstallDir, "no Pelican defaults in Docker mode")
	require.Empty(t, cfg.Root)
	require.Empty(t, cfg.SteamPlatform)

	cfg, err = load([]string{"PANEL_ADMIN_PASSWORD_HASH=" + testHash, "SERVER_PORT=27000", "SERVER_MEMORY=6144"},
		filepath.Join(t.TempDir(), "pzman"))
	require.NoError(t, err, "no pzman.env is fine")
	require.Equal(t, 16261, cfg.DefaultPort, "egg aliases are Pelican-only")
	require.Equal(t, 8, cfg.MemoryXmxGB, "heap derivation is Pelican-only")
}

func TestLoadReportsSource(t *testing.T) {
	_, exe := server(t, "PANEL_ADMIN_PASSWORD_HASH="+testHash+"\nPANEL_PORT=notaport\n")
	_, err := load([]string{"PANEL_DRIVERS=bogus"}, exe)
	require.ErrorContains(t, err, "PANEL_PORT from "+filepath.Join(filepath.Dir(exe), EnvFileName))
	require.ErrorContains(t, err, "PANEL_DRIVERS from the environment")

	_, exe = server(t, "PZMAN_HOST=pelican\nPANEL_ADMIN_PASSWORD_HASH="+testHash+"\n")
	_, err = load([]string{"SERVER_PORT=abc", "SERVER_MEMORY=lots"}, exe)
	require.ErrorContains(t, err, "DEFAULT_PORT from SERVER_PORT from the environment")
	require.ErrorContains(t, err, `SERVER_MEMORY "lots" is not a number of MiB (from the environment)`)

	_, exe = server(t, "PZMAN_HOST=elsewhere\nPANEL_ADMIN_PASSWORD_HASH="+testHash+"\n")
	_, err = load(nil, exe)
	require.ErrorContains(t, err, `PZMAN_HOST must be "docker" or "pelican", got "elsewhere" (PZMAN_HOST from `)
}

func TestPelicanPrecedence(t *testing.T) {
	const base = "PZMAN_HOST=pelican\nPANEL_ADMIN_PASSWORD_HASH=" + testHash + "\n"
	type got struct {
		port, udp, xmx int
		admin, branch  string
	}
	defaults := got{port: 16261, udp: 16262, xmx: 8, admin: "admin", branch: ""}
	for _, tc := range []struct {
		name    string
		file    string
		environ []string
		want    func(*got)
	}{
		{name: "nothing set: pzman defaults", want: func(*got) {}},
		{name: "egg variables apply",
			environ: []string{"SERVER_PORT=27015", "STEAM_PORT=27016", "ADMIN_USER=boss", "SRCDS_BETAID=unstable", "SERVER_MEMORY=6144"},
			want:    func(g *got) { g.port, g.udp, g.admin, g.branch, g.xmx = 27015, 27016, "boss", "unstable", 4 }},
		{name: "explicit pzman variable in the environment beats the egg's",
			environ: []string{"SERVER_PORT=27015", "DEFAULT_PORT=28000", "ADMIN_USER=boss", "ADMIN_USERNAME=root",
				"SERVER_MEMORY=6144", "MEMORY_XMX_GB=3"},
			want: func(g *got) { g.port, g.admin, g.xmx = 28000, "root", 3 }},
		{name: "explicit pzman variable in pzman.env beats the egg's in the environment",
			file:    "UDP_PORT=29000\nSERVER_BRANCH=\nMEMORY_XMX_GB=2\n",
			environ: []string{"STEAM_PORT=27016", "SRCDS_BETAID=unstable", "SERVER_MEMORY=6144"},
			want:    func(g *got) { g.udp, g.xmx = 29000, 2 }},
		{name: "egg variable from pzman.env applies", file: "SERVER_PORT=27100\n",
			want: func(g *got) { g.port = 27100 }},
		{name: "empty pzman variable with a default is unset: the egg's applies",
			file: "DEFAULT_PORT=\nMEMORY_XMX_GB=\n",
			environ: []string{"SERVER_PORT=27015", "UDP_PORT=", "STEAM_PORT=27016", "ADMIN_USERNAME=", "ADMIN_USER=boss",
				"SERVER_MEMORY=6144"},
			want: func(g *got) { g.port, g.udp, g.admin, g.xmx = 27015, 27016, "boss", 4 }},
		{name: "empty pzman variable with a default and no egg value: the default",
			environ: []string{"DEFAULT_PORT=", "MEMORY_XMX_GB="}, want: func(*got) {}},
		{name: "empty pzman variable without a default stays explicit",
			environ: []string{"SERVER_BRANCH=", "SRCDS_BETAID=unstable"}, want: func(*got) {}},
		{name: "empty egg variable is ignored", environ: []string{"ADMIN_USER=", "SRCDS_BETAID="},
			want: func(*got) {}},
		{name: "no memory limit keeps the default heap", environ: []string{"SERVER_MEMORY=0"}, want: func(*got) {}},
		{name: "small limit: at least 1 GiB", environ: []string{"SERVER_MEMORY=1024"}, want: func(g *got) { g.xmx = 1 }},
		{name: "16 GiB limit: 12 GiB heap", environ: []string{"SERVER_MEMORY=16384"}, want: func(g *got) { g.xmx = 12 }},
		{name: "heap rounds down", environ: []string{"SERVER_MEMORY=10000"}, want: func(g *got) { g.xmx = 7 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, exe := server(t, base+tc.file)
			cfg, err := load(tc.environ, exe)
			require.NoError(t, err)
			want := defaults
			tc.want(&want)
			require.Equal(t, want, got{port: cfg.DefaultPort, udp: cfg.UDPPort, xmx: cfg.MemoryXmxGB,
				admin: cfg.GameAdminUser, branch: cfg.ServerBranch})
		})
	}
}

func TestPelicanPathDefaults(t *testing.T) {
	root, exe := server(t, "PZMAN_HOST=pelican\nPANEL_ADMIN_PASSWORD_HASH="+testHash+"\n")
	cfg, err := load(nil, exe)
	require.NoError(t, err)
	require.True(t, cfg.Pelican())
	require.Equal(t, root, cfg.Root)
	require.Equal(t, root, cfg.InstallDir)
	require.Equal(t, filepath.Join(root, ".cache"), cfg.DataDir)
	require.Equal(t, filepath.Join(root, ".easy-pz", "pzman.db"), cfg.DBPath)
	require.Equal(t, filepath.Join(root, ".easy-pz", "backups"), cfg.BackupDir)
	require.Equal(t, filepath.Join(root, ".easy-pz", "cache"), cfg.CacheDir)
	require.Equal(t, filepath.Join(root, ".easy-pz", "steam"), cfg.SteamHome)
	require.Equal(t, filepath.Join(root, ".easy-pz", "logs"), cfg.LogDir)
	require.Equal(t, filepath.Join(root, "steamcmd", "steamcmd.sh"), cfg.SteamCMD)
	require.Equal(t, "linux64", cfg.SteamPlatform)

	root, exe = server(t, "PZMAN_HOST=pelican\nPANEL_ADMIN_PASSWORD_HASH="+testHash+"\nPANEL_STEAMCMD=./tools/steamcmd.sh\n")
	cfg, err = load([]string{"PANEL_BACKUP_DIR=/mnt/backups", "STEAM_PLATFORM=custom"}, exe)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "tools", "steamcmd.sh"), cfg.SteamCMD, "relative paths are from the root")
	require.Equal(t, "/mnt/backups", cfg.BackupDir, "explicit values win")
	require.Equal(t, "custom", cfg.SteamPlatform)
}

func TestHeapFromServerMemory(t *testing.T) {
	for mib, want := range map[int]int{1: 1, 1365: 1, 2731: 2, 6144: 4, 8192: 6, 16384: 12} {
		require.Equal(t, want, HeapFromServerMemory(mib), mib)
	}
}

func TestEggAutoUpdateOn(t *testing.T) {
	require.True(t, Config{Host: HostPelican, EggAutoUpdate: "1"}.EggAutoUpdateOn())
	require.False(t, Config{Host: HostPelican, EggAutoUpdate: "0"}.EggAutoUpdateOn())
	require.False(t, Config{Host: HostDocker, EggAutoUpdate: "1"}.EggAutoUpdateOn(), "docker mode has no egg")
}
