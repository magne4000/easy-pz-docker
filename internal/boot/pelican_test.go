package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/pz"
)

type consoleSup struct {
	mu      sync.Mutex
	running bool
	sent    []string
}

func (s *consoleSup) Start(context.Context) error { return nil }
func (s *consoleSup) Stop(context.Context) error  { return nil }
func (s *consoleSup) Status() pz.Status           { return pz.Status{} }
func (s *consoleSup) SendConsole(cmd string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return pz.ErrNotRunning
	}
	s.sent = append(s.sent, cmd)
	return nil
}

func TestConsoleInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		running bool
		sent    []string
		quits   int
		lines   []string
	}{
		{name: "quit shuts down", input: "quit\n", running: true, quits: 1,
			lines: []string{"[pzman] stop requested: saving the world and stopping the game server"}},
		{name: "quit is trimmed", input: "  quit \r\n", running: true, quits: 1,
			lines: []string{"[pzman] stop requested: saving the world and stopping the game server"}},
		{name: "other lines go to the game", input: "players\nservermsg \"hi\"\nquitting\n\n", running: true,
			sent: []string{"players", `servermsg "hi"`, "quitting"}},
		{name: "QUIT is not the stop command", input: "QUIT\n", running: true, sent: []string{"QUIT"}},
		{name: "last line without newline", input: "players", running: true, sent: []string{"players"}},
		{name: "game not running", input: "players\n", running: false,
			lines: []string{`[pzman] the game server is not running; "players" was not sent`}},
		{name: "EOF alone does nothing", input: "", running: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sup := &consoleSup{running: tc.running}
			var lines []string
			quits := 0
			consoleInput(strings.NewReader(tc.input), sup, func(l string) { lines = append(lines, l) }, func() { quits++ })
			require.Equal(t, tc.sent, sup.sent)
			require.Equal(t, tc.quits, quits)
			require.Equal(t, tc.lines, lines)
		})
	}
}

func TestPelicanIgnoreBlock(t *testing.T) {
	block := ignoreBegin + "\n" + strings.Join(ignorePatterns, "\n") + "\n" + ignoreEnd + "\n"
	for _, tc := range []struct{ name, in, want string }{
		{"new file", "", block},
		{"appended after user lines", "*.log\n/tmp/", "*.log\n/tmp/\n" + block},
		{"stale block replaced in place, user lines kept",
			"# mine\n*.log\n" + ignoreBegin + "\n/old/\n" + ignoreEnd + "\n/keep/\n",
			"# mine\n*.log\n" + block + "/keep/\n"},
		{"unterminated block replaced to the end", "a\n" + ignoreBegin + "\n/old/\n", "a\n" + block},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := withIgnoreBlock(tc.in)
			require.Equal(t, tc.want, got)
			require.Equal(t, got, withIgnoreBlock(got), "idempotent")
		})
	}
}

func TestWritePelicanIgnore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".pelicanignore")
	require.NoError(t, os.WriteFile(path, []byte("/my-notes/\n"), 0o644))
	require.NoError(t, writePelicanIgnore(root))
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(first), "/my-notes/\n"+ignoreBegin+"\n"))
	for _, p := range []string{"/steamcmd/", "/.easy-pz/backups/", "/.easy-pz/cache/", "/.easy-pz/steam/", "/jre64/"} {
		require.Contains(t, string(first), "\n"+p+"\n")
	}
	require.NotContains(t, string(first), "/.cache/", "the world stays in Pelican backups")

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, writePelicanIgnore(root))
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(first), string(again))
	st2, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, st.ModTime(), st2.ModTime(), "an up-to-date block is not rewritten")
}
