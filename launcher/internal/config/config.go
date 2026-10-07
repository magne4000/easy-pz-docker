package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/magne4000/easy-pz-docker/launcher/internal/fsutil"
	"github.com/magne4000/easy-pz-docker/launcher/internal/modsync"
)

type Server struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	PageURL string `json:"pageUrl,omitempty"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Account string `json:"account,omitempty"`
}

type Config struct {
	GameDir string            `json:"gameDir,omitempty"`
	Servers []Server          `json:"servers"`
	Mods    modsync.Installed `json:"mods"`
	// AutoConnect: Play joins the server; off, it only syncs and starts the
	// game. Unset means on.
	AutoConnect *bool `json:"autoConnect,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
	cfg  Config
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "easypz-launcher", "config.json"), nil
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read settings: %w", err)
	default:
		if err := json.Unmarshal(data, &s.cfg); err != nil {
			return nil, fmt.Errorf("read settings %s: %w", path, err)
		}
	}
	if s.cfg.Servers == nil {
		s.cfg.Servers = []Server{}
	}
	if s.cfg.Mods == nil {
		s.cfg.Mods = modsync.Installed{}
	}
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.cfg)
}

func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.cfg)
	if err := fn(&next); err != nil {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := fsutil.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	s.cfg = next
	return nil
}

func clone(c Config) Config {
	out := c
	out.Servers = append([]Server{}, c.Servers...)
	out.Mods = modsync.Installed{}
	for k, v := range c.Mods {
		v.Folders = append([]string{}, v.Folders...)
		out.Mods[k] = v
	}
	return out
}

func (c *Config) Server(id string) (*Server, bool) {
	for i := range c.Servers {
		if c.Servers[i].ID == id {
			return &c.Servers[i], true
		}
	}
	return nil, false
}

func NewID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
