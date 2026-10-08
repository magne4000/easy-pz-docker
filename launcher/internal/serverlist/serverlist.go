package serverlist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Account struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	SavePassword bool   `json:"savePassword"`
	// AuthType 1 is password only; others need a second factor.
	AuthType  int    `json:"authType"`
	LastLogon string `json:"lastLogon"`
}

func (a Account) CanAutoLogin() bool {
	return a.AuthType == 1 && a.SavePassword && a.PasswordHash != ""
}

type Server struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	IP             string    `json:"ip"`
	Port           int       `json:"port"`
	ServerPassword string    `json:"-"`
	Description    string    `json:"description"`
	Accounts       []Account `json:"accounts"`
}

func Read(ctx context.Context, path string) ([]Server, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return []Server{}, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", readOnlyURI(abs))
	if err != nil {
		return nil, fmt.Errorf("open server list: %w", err)
	}
	defer db.Close()

	servers := []Server{}
	byID := map[int64]int{}
	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.name, s.ip, s.port, COALESCE(s.serverPassword, ''), COALESCE(s.description, '')
		FROM server s LEFT JOIN account a ON s.id = a.serverId
		GROUP BY s.id ORDER BY MAX(a.lastLogon) DESC NULLS LAST, s.id`)
	if err != nil {
		return nil, fmt.Errorf("read servers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s Server
		if err := rows.Scan(&s.ID, &s.Name, &s.IP, &s.Port, &s.ServerPassword, &s.Description); err != nil {
			return nil, fmt.Errorf("read servers: %w", err)
		}
		s.Accounts = []Account{}
		byID[s.ID] = len(servers)
		servers = append(servers, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	arows, err := db.QueryContext(ctx, `
		SELECT id, serverId, username, COALESCE(password, ''), COALESCE(isSavePassword, 0),
		       COALESCE(authType, 1), COALESCE(lastLogon, '')
		FROM account ORDER BY lastLogon DESC NULLS LAST, id`)
	if err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	defer arows.Close()
	for arows.Next() {
		var a Account
		var serverID int64
		if err := arows.Scan(&a.ID, &serverID, &a.Username, &a.PasswordHash, &a.SavePassword, &a.AuthType, &a.LastLogon); err != nil {
			return nil, fmt.Errorf("read accounts: %w", err)
		}
		if i, ok := byID[serverID]; ok {
			servers[i].Accounts = append(servers[i].Accounts, a)
		}
	}
	return servers, arows.Err()
}

// readOnlyURI builds a SQLite file URI. The path must start with a slash:
// "file://C:/..." makes SQLite read "C:" as the URI authority and fail.
func readOnlyURI(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro&_pragma=busy_timeout(3000)"}
	return u.String()
}

func Find(servers []Server, host string, port int) (Server, bool) {
	for _, s := range servers {
		if s.IP == host && s.Port == port {
			return s, true
		}
	}
	return Server{}, false
}
