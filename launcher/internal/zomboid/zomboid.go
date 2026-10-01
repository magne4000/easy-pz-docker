package zomboid

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/magne4000/easy-pz-docker/launcher/internal/fsutil"
)

type Dir string

func DefaultDir() (Dir, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return Dir(filepath.Join(home, "Zomboid")), nil
}

func (d Dir) Mods() string { return filepath.Join(string(d), "mods") }

func (d Dir) ServerListDB() string { return filepath.Join(string(d), "db", "ServerList.db") }

func (d Dir) activeModsFile() string { return filepath.Join(d.Mods(), "default.txt") }

func (d Dir) AutoConnectFile() string {
	return filepath.Join(string(d), "Lua", "easypz-autoconnect.ini")
}

const defaultActiveMods = "VERSION = 1,\n\nmods\n{\n}\n\nmaps\n{\n}\n"

func (d Dir) ActivateMod(id string) error {
	path := d.activeModsFile()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		data, err = []byte(defaultActiveMods), nil
	}
	if err != nil {
		return fmt.Errorf("read active mods: %w", err)
	}
	out, changed, err := activateMod(string(data), id)
	if err != nil || !changed {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFile(path, []byte(out), 0o644)
}

func activateMod(doc, id string) (string, bool, error) {
	eol := "\n"
	if strings.Contains(doc, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n")
	inMods, sawOpen := false, false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case !inMods && t == "mods":
			inMods = true
		case inMods && !sawOpen && t == "{":
			sawOpen = true
		case inMods && sawOpen && t == "}":
			entry := "    mod = " + id + ","
			lines = append(lines[:i], append([]string{entry}, lines[i:]...)...)
			return strings.Join(lines, eol), true, nil
		case inMods && sawOpen:
			k, v, ok := strings.Cut(t, "=")
			if ok && strings.TrimSpace(k) == "mod" {
				v = strings.TrimSuffix(strings.TrimSpace(v), ",")
				if strings.TrimPrefix(strings.TrimSpace(v), `\`) == id {
					return doc, false, nil
				}
			}
		}
	}
	return "", false, errors.New("mods/default.txt has no mods { } block")
}

type AutoConnect struct {
	Host           string
	Port           int
	User           string
	PasswordHash   string
	ServerPassword string
	ServerName     string
}

func (d Dir) WriteAutoConnect(a AutoConnect) error {
	for _, v := range []string{a.Host, a.User, a.PasswordHash, a.ServerPassword, a.ServerName} {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("auto-connect values cannot contain line breaks")
		}
	}
	var b strings.Builder
	kv := func(k, v string) { b.WriteString(k + "=" + v + "\n") }
	kv("host", a.Host)
	kv("port", strconv.Itoa(a.Port))
	kv("user", a.User)
	kv("password", a.PasswordHash)
	kv("doHash", "false")
	kv("authType", "1")
	kv("serverPassword", a.ServerPassword)
	kv("serverName", a.ServerName)
	path := d.AutoConnectFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFile(path, []byte(b.String()), 0o600)
}

func (d Dir) ClearAutoConnect() error {
	err := os.Remove(d.AutoConnectFile())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
