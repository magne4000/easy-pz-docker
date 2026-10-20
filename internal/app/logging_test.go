package app

import (
	"encoding/json"
	"fmt"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fiberlog "github.com/gofiber/fiber/v3/log"
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

// Pelican shows stderr in its console: in Pelican mode the standard logger,
// Fiber's logger and raw stderr writes (the SSE library's warnings) land in
// pzman.log instead, as JSON records; stdout is untouched.
func TestPelicanStderrToLogFile(t *testing.T) {
	dir := t.TempDir()
	orig := os.Stderr
	log, closer, err := NewLogger(Config{Host: HostPelican, LogLevel: "info", LogDir: dir})
	require.NoError(t, err)
	require.NotSame(t, orig, os.Stderr)
	stdlog.Print("from the std logger")
	fiberlog.Warn("from fiber")
	fmt.Fprintf(os.Stderr, "warning: unable to set write deadline: %v\n", "use of closed network connection")
	log.Info("from pzman")
	require.NoError(t, closer.Close())
	require.Same(t, orig, os.Stderr, "restored on close")

	b, err := os.ReadFile(filepath.Join(dir, LogFileName))
	require.NoError(t, err)
	var msgs []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &rec), "JSON lines only: %q", l)
		msgs = append(msgs, fmt.Sprint(rec["msg"], " ", rec["line"]))
	}
	require.Contains(t, msgs, "from the std logger <nil>")
	require.Contains(t, msgs, "from pzman <nil>")
	require.Contains(t, msgs, "stderr warning: unable to set write deadline: use of closed network connection")
	found := false
	for _, m := range msgs {
		found = found || strings.Contains(m, "from fiber")
	}
	require.True(t, found, "fiber's logger: %v", msgs)
}
