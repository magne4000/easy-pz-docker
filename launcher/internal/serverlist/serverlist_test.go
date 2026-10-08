package serverlist

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Schema as created by AccountDBHelper (42.21).
const schema = `
CREATE TABLE IF NOT EXISTS server (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL,
 ip TEXT NOT NULL,
 port INTEGER NOT NULL,
 serverPassword TEXT,
 description TEXT,
 mods TEXT,
 icon BLOB,
 banner BLOB,
 panelBackground BLOB,
 screenBackground BLOB,
 lastOnline TEXT,
 lastDataUpdate TEXT
);
CREATE TABLE IF NOT EXISTS account (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 serverId INTEGER NOT NULL,
 playerFirstAndLastName  TEXT,
 username TEXT NOT NULL,
 password TEXT,
 isSavePassword INTEGER DEFAULT 0,
 isUseSteamRelay INTEGER DEFAULT 0,
 authType INTEGER DEFAULT 1,
 icon BLOB,
 timePlayed INTEGER DEFAULT 0,
 lastLogon TEXT,
 FOREIGN KEY (serverId) REFERENCES server (id)
);`

func TestRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ServerList.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(schema + `
		INSERT INTO server (name, ip, port, serverPassword, description) VALUES
			('Old', 'old.example.com', 16261, NULL, NULL),
			('Main', 'pz.example.com', 16261, 'srvpw', 'desc'),
			('Empty', 'empty.example.com', 16300, '', '');
		INSERT INTO account (serverId, username, password, isSavePassword, authType, lastLogon) VALUES
			(1, 'olduser', '', 0, 1, '2026-01-01 10:00:00'),
			(2, 'alice', '$2a$12$hash', 1, 1, '2026-09-01 10:00:00'),
			(2, 'alice2fa', '$2a$12$hash2', 1, 2, '2026-08-01 10:00:00');`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	servers, err := Read(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, servers, 3)
	require.Equal(t, []string{"Main", "Old", "Empty"}, []string{servers[0].Name, servers[1].Name, servers[2].Name})

	main := servers[0]
	require.Equal(t, "srvpw", main.ServerPassword)
	require.Len(t, main.Accounts, 2)
	require.Equal(t, "alice", main.Accounts[0].Username)
	require.True(t, main.Accounts[0].CanAutoLogin())
	require.False(t, main.Accounts[1].CanAutoLogin(), "second factor")
	require.False(t, servers[1].Accounts[0].CanAutoLogin(), "password not saved")
	require.Empty(t, servers[2].Accounts)

	s, ok := Find(servers, "pz.example.com", 16261)
	require.True(t, ok)
	require.Equal(t, "Main", s.Name)
	_, ok = Find(servers, "pz.example.com", 1)
	require.False(t, ok)
}

func TestReadMissing(t *testing.T) {
	servers, err := Read(context.Background(), filepath.Join(t.TempDir(), "nope.db"))
	require.NoError(t, err)
	require.Empty(t, servers)
}

// Regression: Windows home paths produced "file://C:/...", which SQLite
// rejects ("invalid uri authority: C:"), so saved servers never showed up.
func TestReadOnlyURIWindowsDrive(t *testing.T) {
	require.Equal(t, "file:///C:/Users/Bob/Zomboid/db/ServerList.db?mode=ro&_pragma=busy_timeout(3000)",
		readOnlyURI("C:/Users/Bob/Zomboid/db/ServerList.db"))
}
