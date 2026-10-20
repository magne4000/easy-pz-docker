package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPelicanLogFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	cfg := Config{Host: HostPelican, LogLevel: "info", LogDir: dir}
	log, closer, err := NewLogger(cfg)
	require.NoError(t, err)
	log.Info("hello", "k", "v")
	require.NoError(t, closer.Close())

	b, err := os.ReadFile(filepath.Join(dir, LogFileName))
	require.NoError(t, err)
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(b))), &rec), "JSON lines")
	require.Equal(t, "hello", rec["msg"])

	// Appends below the threshold, rotates above it.
	_, closer, err = NewLogger(cfg)
	require.NoError(t, err)
	require.NoError(t, closer.Close())
	_, err = os.Stat(filepath.Join(dir, LogFileName+".1"))
	require.ErrorIs(t, err, os.ErrNotExist)

	big := make([]byte, logRotateSize+1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, LogFileName), big, 0o644))
	log, closer, err = NewLogger(cfg)
	require.NoError(t, err)
	log.Info("after rotation")
	require.NoError(t, closer.Close())
	st, err := os.Stat(filepath.Join(dir, LogFileName+".1"))
	require.NoError(t, err)
	require.EqualValues(t, logRotateSize+1, st.Size())
	b, err = os.ReadFile(filepath.Join(dir, LogFileName))
	require.NoError(t, err)
	require.Contains(t, string(b), "after rotation")
	require.Less(t, len(b), 1024)
}
