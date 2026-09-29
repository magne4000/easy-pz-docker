package sys

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProcMergesOutputAndSetsHome(t *testing.T) {
	var stdin io.WriteCloser
	p, err := Proc{Name: "t", Bin: "/bin/sh", Args: []string{"-c", `echo out; echo err >&2; read x; echo "$x $HOME"`},
		Home: "/some/home", UID: -1, Stdin: &stdin}.Start(context.Background())
	require.NoError(t, err)
	_, err = io.WriteString(stdin, "hi\n")
	require.NoError(t, err)
	b, err := io.ReadAll(p.Output)
	require.NoError(t, err)
	require.NoError(t, <-p.Done)
	require.Equal(t, "out\nerr\nhi /some/home\n", string(b))
}

// Cancelling kills the whole group, including grandchildren the child spawned.
func TestProcCancelKillsGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	p, err := Proc{Name: "t", Bin: "/bin/sh", Args: []string{"-c", `sleep 60 & echo $! > ` + pidFile + `; wait`}, UID: -1}.Start(ctx)
	require.NoError(t, err)
	go io.Copy(io.Discard, p.Output)
	var grandchild int
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(pidFile)
		grandchild, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil && grandchild > 0
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.Error(t, <-p.Done)
	require.Eventually(t, func() bool { return syscall.Kill(grandchild, 0) != nil }, 5*time.Second, 10*time.Millisecond,
		"grandchild survived")
}
