package helpermod

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstall(t *testing.T) {
	mods := filepath.Join(t.TempDir(), "mods")
	require.NoError(t, Install(mods))

	dst := filepath.Join(mods, ID)
	info, err := os.ReadFile(filepath.Join(dst, "42", "mod.info"))
	require.NoError(t, err)
	require.Contains(t, string(info), "id="+ID)
	_, err = os.Stat(filepath.Join(dst, "common", "media", "lua", "client", ID+".lua"))
	require.NoError(t, err)
	require.True(t, upToDate(dst))

	// A modified or extra file is replaced by a clean copy.
	require.NoError(t, os.WriteFile(filepath.Join(dst, "stale.lua"), []byte("x"), 0o644))
	require.False(t, upToDate(dst))
	require.NoError(t, Install(mods))
	require.True(t, upToDate(dst))
	_, err = os.Stat(filepath.Join(dst, "stale.lua"))
	require.ErrorIs(t, err, os.ErrNotExist)

	entries, err := os.ReadDir(mods)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp or .old folders left behind")
}
