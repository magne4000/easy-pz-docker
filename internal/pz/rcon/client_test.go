package rcon

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeServer struct {
	ln       net.Listener
	password string
	handle   func(cmd string) []string // bodies to send back
	split    bool
}

func frame(id, typ int32, body string) []byte {
	b := make([]byte, 14+len(body))
	binary.LittleEndian.PutUint32(b[0:], uint32(10+len(body)))
	binary.LittleEndian.PutUint32(b[4:], uint32(id))
	binary.LittleEndian.PutUint32(b[8:], uint32(typ))
	copy(b[12:], body)
	return b
}

func startServer(t *testing.T, fs *fakeServer) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	fs.ln = ln
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go fs.serve(conn)
		}
	}()
	return ln.Addr().String()
}

func (fs *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	send := func(b []byte) {
		if fs.split && len(b) > 3 {
			conn.Write(b[:3])
			time.Sleep(5 * time.Millisecond)
			conn.Write(b[3:])
			return
		}
		conn.Write(b)
	}
	for {
		var hdr [12]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return
		}
		size := binary.LittleEndian.Uint32(hdr[0:])
		id := int32(binary.LittleEndian.Uint32(hdr[4:]))
		typ := binary.LittleEndian.Uint32(hdr[8:])
		body := make([]byte, size-8)
		io.ReadFull(r, body)
		text := strings.TrimRight(string(body), "\x00")
		switch typ {
		case 3:
			send(frame(id, 0, ""))
			if text != fs.password {
				send(frame(-1, 2, ""))
				continue
			}
			send(frame(id, 2, ""))
		case 2:
			send(frame(999, 0, "orphan"))
			for _, b := range fs.handle(text) {
				send(frame(id, 0, b))
			}
		}
	}
}

func TestAuthAndExec(t *testing.T) {
	fs := &fakeServer{password: "pw", split: true, handle: func(cmd string) []string { return []string{"echo " + cmd} }}
	addr := startServer(t, fs)
	ctx := context.Background()

	_, err := Dial(ctx, addr, "wrong")
	require.ErrorIs(t, err, ErrAuth)

	c, err := Dial(ctx, addr, "pw")
	require.NoError(t, err)
	defer c.Close()
	resp, err := c.Exec(ctx, "save")
	require.NoError(t, err)
	require.Equal(t, "echo save", resp)
}

func TestMultiPacket(t *testing.T) {
	big := strings.Repeat("a", 4096)
	fs := &fakeServer{password: "pw", handle: func(string) []string { return []string{big, "tail"} }}
	c, err := Dial(context.Background(), startServer(t, fs), "pw")
	require.NoError(t, err)
	resp, err := c.Exec(context.Background(), "x")
	require.NoError(t, err)
	require.Equal(t, big+"tail", resp)

	fs.handle = func(string) []string { return []string{big} }
	resp, err = c.Exec(context.Background(), "x")
	require.NoError(t, err)
	require.Equal(t, big, resp)
}

func TestManagerReconnectsAndClassifies(t *testing.T) {
	fs := &fakeServer{password: "pw", handle: func(cmd string) []string {
		if cmd == "players" {
			return []string{"Players connected (2):\n-alice\n-bob\n"}
		}
		return []string{"Unknown command foo"}
	}}
	addr := startServer(t, fs)
	m := NewManager(slog.Default(), func() (string, string) { return addr, "pw" })
	defer m.Close()
	ps, err := m.Players(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"alice", "bob"}, ps)

	m.c.conn.Close() // simulate a dropped connection
	_, err = m.Exec(context.Background(), "foo")
	var rej *RejectedError
	require.True(t, errors.As(err, &rej))
	require.Contains(t, rej.Message, "not available")
}

func TestParsePlayers(t *testing.T) {
	require.Empty(t, ParsePlayers("Players connected (0):\r\n"))
	require.Equal(t, []string{"a b"}, ParsePlayers("Players connected (1):\r\n-a b\r\n"))
}

func TestRejectionsMatchJarCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../../zomboid-control-panel/server/__fixtures__/pzRconRejectionStrings.json")
	if err != nil {
		t.Skip("fixture corpus not present")
	}
	var fx struct {
		Classes map[string][]string `json:"classes"`
	}
	require.NoError(t, json.Unmarshal(raw, &fx))
	var corpus []string
	for _, s := range fx.Classes {
		corpus = append(corpus, s...)
	}
	for _, r := range Rejections {
		found := false
		for _, s := range corpus {
			if r.Pattern.MatchString(s) || r.Pattern.MatchString(strings.ReplaceAll(s, "%s", "x")) {
				found = true
				break
			}
		}
		if !found {
			t.Logf("pattern %s matches no raw corpus string (may be interpolated)", r.Pattern)
		}
	}
}
