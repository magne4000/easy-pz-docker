package httpapi

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// A fatal boot error shuts the server down right after Run started; Run must
// still return, wherever it was in starting up.
func TestShutdownRightAfterRun(t *testing.T) {
	for range 20 {
		cfg := testConfig()
		cfg.Port = freePort(t)
		s, _ := newTestServer(t, cfg)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()
		require.NoError(t, s.Shutdown(context.Background()))
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after Shutdown")
		}
	}
}

func TestShutdownBeforeRun(t *testing.T) {
	cfg := testConfig()
	cfg.Port = freePort(t)
	s, _ := newTestServer(t, cfg)
	require.NoError(t, s.Shutdown(context.Background()))
	require.NoError(t, s.Run(context.Background()), "returns at once")
}
