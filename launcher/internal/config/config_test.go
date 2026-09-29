package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/launcher/internal/modsync"
)

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	s, err := Open(path)
	require.NoError(t, err)
	require.Empty(t, s.Get().Servers)

	require.NoError(t, s.Update(func(c *Config) error {
		c.GameDir = "/games/pz"
		c.Servers = append(c.Servers, Server{ID: "a", Name: "Srv", Host: "h", Port: 16261})
		c.Mods["1"] = modsync.Item{SHA256: "aa", Folders: []string{"ModA"}}
		return nil
	}))
	boom := errors.New("boom")
	require.ErrorIs(t, s.Update(func(c *Config) error { c.GameDir = "changed"; return boom }), boom)

	s2, err := Open(path)
	require.NoError(t, err)
	c := s2.Get()
	require.Equal(t, "/games/pz", c.GameDir)
	srv, ok := c.Server("a")
	require.True(t, ok)
	require.Equal(t, "Srv", srv.Name)
	require.Equal(t, []string{"ModA"}, c.Mods["1"].Folders)

	// Get returns a copy.
	c.Mods["1"].Folders[0] = "mutated"
	require.Equal(t, "ModA", s2.Get().Mods["1"].Folders[0])

	st, err := os.Stat(path)
	require.NoError(t, err)
	if os.PathSeparator == '/' {
		require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	}
}

func TestOpenCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
	_, err := Open(path)
	require.Error(t, err)
}
