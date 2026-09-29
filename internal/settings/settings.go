package settings

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/store"
)

// Settings are the UI-editable knobs; env provides their defaults.
type Settings struct {
	BackupIntervalMinutes int     `json:"backupIntervalMinutes" minimum:"0" doc:"0 disables scheduled backups"`
	BackupKeep            int     `json:"backupKeep" minimum:"1"`
	BackupKeepDaily       int     `json:"backupKeepDaily" minimum:"0"`
	BackupKeepWeekly      int     `json:"backupKeepWeekly" minimum:"0"`
	BackupMaxTotalGB      float64 `json:"backupMaxTotalGB" minimum:"0" doc:"0 means unlimited"`
	UpdateCheckMinutes    int     `json:"updateCheckMinutes" minimum:"0" doc:"0 disables update checks"`
	UpdateMaxDelayMinutes int     `json:"updateMaxDelayMinutes" minimum:"0" doc:"force the update window after this long"`
	AutoUpdate            bool    `json:"autoUpdate" doc:"open the update window automatically when an update is detected"`
	WarnMinutes           []int   `json:"warnMinutes" doc:"countdown warnings (minutes) before a forced restart"`
	WorkshopCollection    string  `json:"workshopCollection" pattern:"^[0-9]*$"`
	PublicModsPage        bool    `json:"publicModsPage"`
}

const key = "panel_settings"
const tokenKey = "mods_token"

type Store struct {
	db  *store.DB
	bus *events.Bus
	mu  sync.RWMutex
	cur Settings
	tok string
}

func Defaults(cfg app.Config) Settings {
	return Settings{
		BackupIntervalMinutes: int(cfg.BackupInterval.Minutes()),
		BackupKeep:            cfg.BackupKeep,
		BackupKeepDaily:       cfg.BackupKeepDaily,
		BackupKeepWeekly:      cfg.BackupKeepWeekly,
		BackupMaxTotalGB:      cfg.BackupMaxTotalGB,
		UpdateCheckMinutes:    int(cfg.UpdateCheckInterval.Minutes()),
		UpdateMaxDelayMinutes: int(cfg.UpdateMaxDelay.Minutes()),
		AutoUpdate:            true,
		WarnMinutes:           []int{30, 15, 5, 1},
		WorkshopCollection:    cfg.WorkshopCollection,
		PublicModsPage:        true,
	}
}

func Open(ctx context.Context, cfg app.Config, db *store.DB, bus *events.Bus) (*Store, error) {
	s := &Store{db: db, bus: bus, cur: Defaults(cfg)}
	if err := db.GetJSON(ctx, key, &s.cur); err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	s.cur = normalize(s.cur)
	s.tok = cfg.ModsToken
	if s.tok == "" {
		if err := db.GetJSON(ctx, tokenKey, &s.tok); errors.Is(err, store.ErrNotFound) {
			b := make([]byte, 18)
			rand.Read(b)
			s.tok = base64.RawURLEncoding.EncodeToString(b)
			if err := db.PutJSON(ctx, tokenKey, s.tok); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// NormalizeWarnMinutes returns the positive minutes of ms, deduplicated and
// in countdown order (largest first); never nil.
func NormalizeWarnMinutes(ms []int) []int {
	w := []int{}
	for _, m := range ms {
		if m > 0 && !slices.Contains(w, m) {
			w = append(w, m)
		}
	}
	slices.Sort(w)
	slices.Reverse(w)
	return w
}

func normalize(s Settings) Settings {
	s.WarnMinutes = NormalizeWarnMinutes(s.WarnMinutes)
	if s.BackupKeep < 1 {
		s.BackupKeep = 1
	}
	return s
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.cur
	c.WarnMinutes = append([]int(nil), c.WarnMinutes...)
	return c
}

// ModsToken is the unlisted path segment of the public mod page.
func (s *Store) ModsToken() string { return s.tok }

func (s *Store) Update(ctx context.Context, n Settings) (Settings, error) {
	n = normalize(n)
	if n.BackupIntervalMinutes < 0 || n.UpdateCheckMinutes < 0 || n.UpdateMaxDelayMinutes < 0 || n.BackupMaxTotalGB < 0 {
		return s.Get(), fmt.Errorf("settings: negative values are not allowed")
	}
	if err := s.db.PutJSON(ctx, key, n); err != nil {
		return s.Get(), err
	}
	s.mu.Lock()
	s.cur = n
	s.mu.Unlock()
	s.bus.Publish(events.SettingsChanged{})
	return s.Get(), nil
}
