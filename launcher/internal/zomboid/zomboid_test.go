package zomboid

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// As written by the game (42.21) after enabling a mod in the Mods screen.
const gameDefaultTxt = "VERSION = 1,\n\nmods\n{\n    mod = EasyPZAutoConnect,\n}\n\nmaps\n{\n}\n"

func TestActivateModCreatesFile(t *testing.T) {
	d := Dir(t.TempDir())
	require.NoError(t, d.ActivateMod("EasyPZAutoConnect"))
	got, err := os.ReadFile(filepath.Join(string(d), "mods", "default.txt"))
	require.NoError(t, err)
	require.Equal(t, gameDefaultTxt, string(got))
}

func TestActivateModKeepsOthers(t *testing.T) {
	doc := "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = \\Other,\r\n}\r\n\r\nmaps\r\n{\r\n    map = Muldraugh, KY,\r\n}\r\n"
	out, changed, err := activateMod(doc, "EasyPZAutoConnect")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "VERSION = 1,\r\n\r\nmods\r\n{\r\n    mod = \\Other,\r\n    mod = EasyPZAutoConnect,\r\n}\r\n\r\nmaps\r\n{\r\n    map = Muldraugh, KY,\r\n}\r\n", out)
}

func TestActivateModAlreadyActive(t *testing.T) {
	for _, doc := range []string{gameDefaultTxt, "mods\n{\n  mod = \\EasyPZAutoConnect,\n}\n"} {
		out, changed, err := activateMod(doc, "EasyPZAutoConnect")
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, doc, out)
	}
}

func TestActivateModMalformed(t *testing.T) {
	_, _, err := activateMod("VERSION = 1,\n", "X")
	require.Error(t, err)
}

func TestAutoConnectFile(t *testing.T) {
	d := Dir(t.TempDir())
	a := AutoConnect{Host: "pz.example.com", Port: 16261, User: "alice", PasswordHash: "$2a$12$abc",
		ServerPassword: "srv=pw", ServerName: "My Server"}
	require.NoError(t, d.WriteAutoConnect(a))
	got, err := os.ReadFile(d.AutoConnectFile())
	require.NoError(t, err)
	require.Equal(t, "host=pz.example.com\nport=16261\nuser=alice\npassword=$2a$12$abc\ndoHash=false\nauthType=1\n"+
		"serverPassword=srv=pw\nserverName=My Server\n", string(got))
	require.Equal(t, filepath.Join(string(d), "Lua", "easypz-autoconnect.ini"), d.AutoConnectFile())

	require.NoError(t, d.ClearAutoConnect())
	require.NoError(t, d.ClearAutoConnect(), "already gone")
	_, err = os.Stat(d.AutoConnectFile())
	require.ErrorIs(t, err, os.ErrNotExist)

	a.User = "bob\nhost=evil"
	require.Error(t, d.WriteAutoConnect(a))
}
