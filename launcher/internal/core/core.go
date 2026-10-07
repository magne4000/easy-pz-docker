package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/launcher/helpermod"
	"github.com/magne4000/easy-pz-docker/launcher/internal/config"
	"github.com/magne4000/easy-pz-docker/launcher/internal/game"
	"github.com/magne4000/easy-pz-docker/launcher/internal/modsync"
	"github.com/magne4000/easy-pz-docker/launcher/internal/pzclient"
	"github.com/magne4000/easy-pz-docker/launcher/internal/pzhash"
	"github.com/magne4000/easy-pz-docker/launcher/internal/serverlist"
	"github.com/magne4000/easy-pz-docker/launcher/internal/zomboid"
)

const (
	EventSyncProgress = "sync:progress"
	EventPlayPhase    = "play:phase"
)

type Emitter interface {
	Emit(name string, data any)
}

type Starter func(game.Install) error

type Service struct {
	Version string
	cfg     *config.Store
	user    zomboid.Dir
	client  *pzclient.Client
	emit    Emitter
	log     *slog.Logger
	start   Starter

	PollInterval   time.Duration
	ConsumeTimeout time.Duration
	RetryInterval  time.Duration
	RetryAttempts  int

	busy     sync.Mutex
	launches atomic.Int64
}

func New(version string, cfg *config.Store, user zomboid.Dir, emit Emitter, log *slog.Logger) *Service {
	return &Service{Version: version, cfg: cfg, user: user, client: pzclient.New(), emit: emit, log: log,
		start: game.Install.Start, PollInterval: 10 * time.Second, ConsumeTimeout: 10 * time.Minute,
		RetryInterval: 10 * time.Second, RetryAttempts: 12}
}

type GameInfo struct {
	Dir      string `json:"dir"`
	Found    bool   `json:"found"`
	Detected bool   `json:"detected"`
	Version  string `json:"version"`
}

func (s *Service) install() (game.Install, bool, bool) {
	if dir := s.cfg.Get().GameDir; dir != "" {
		if in, err := game.Open(dir); err == nil {
			return in, true, false
		}
	}
	in, ok := game.Detect()
	return in, ok, ok
}

func (s *Service) Game() GameInfo {
	in, ok, detected := s.install()
	if !ok {
		return GameInfo{Dir: s.cfg.Get().GameDir}
	}
	return GameInfo{Dir: in.Dir, Found: true, Detected: detected, Version: in.Version()}
}

func (s *Service) SetGameDir(dir string) (GameInfo, error) {
	in, err := game.Open(dir)
	if err != nil {
		return GameInfo{}, err
	}
	if err := s.cfg.Update(func(c *config.Config) error { c.GameDir = in.Dir; return nil }); err != nil {
		return GameInfo{}, err
	}
	return s.Game(), nil
}

// AutoConnect is on unless the player turned it off.
func (s *Service) AutoConnect() bool {
	on := s.cfg.Get().AutoConnect
	return on == nil || *on
}

func (s *Service) SetAutoConnect(on bool) error {
	return s.cfg.Update(func(c *config.Config) error { c.AutoConnect = &on; return nil })
}

func (s *Service) Servers() []config.Server { return s.cfg.Get().Servers }

func (s *Service) AddServerURL(ctx context.Context, raw string) (config.Server, error) {
	page, err := pzclient.PageURL(raw)
	if err != nil {
		return config.Server{}, err
	}
	data, err := s.client.Fetch(ctx, page)
	if err != nil {
		return config.Server{}, err
	}
	srv := config.Server{ID: config.NewID(), Name: data.ServerName, PageURL: page}
	if data.Connect != nil {
		srv.Host, srv.Port = data.Connect.Host, data.Connect.Port
	}
	err = s.cfg.Update(func(c *config.Config) error {
		for _, o := range c.Servers {
			if o.PageURL == page {
				return fmt.Errorf("%q is already in your list", o.Name)
			}
		}
		c.Servers = append(c.Servers, srv)
		return nil
	})
	return srv, err
}

func (s *Service) SavedServers(ctx context.Context) ([]serverlist.Server, error) {
	return serverlist.Read(ctx, s.user.ServerListDB())
}

func (s *Service) ImportSaved(ctx context.Context, savedID int64, pageURL string) (config.Server, error) {
	saved, err := s.SavedServers(ctx)
	if err != nil {
		return config.Server{}, err
	}
	var found *serverlist.Server
	for i := range saved {
		if saved[i].ID == savedID {
			found = &saved[i]
		}
	}
	if found == nil {
		return config.Server{}, errors.New("this server is no longer saved in the game")
	}
	srv := config.Server{ID: config.NewID(), Name: found.Name, Host: found.IP, Port: found.Port}
	if len(found.Accounts) > 0 {
		srv.Account = found.Accounts[0].Username
	}
	if pageURL != "" {
		if srv.PageURL, err = pzclient.PageURL(pageURL); err != nil {
			return config.Server{}, err
		}
		if _, err := s.client.Fetch(ctx, srv.PageURL); err != nil {
			return config.Server{}, err
		}
	}
	err = s.cfg.Update(func(c *config.Config) error {
		c.Servers = append(c.Servers, srv)
		return nil
	})
	return srv, err
}

func (s *Service) UpdateServer(id, name, host string, port int, account string) (config.Server, error) {
	host = strings.TrimSpace(host)
	if port < 0 || port > 65535 {
		return config.Server{}, errors.New("invalid port")
	}
	var out config.Server
	err := s.cfg.Update(func(c *config.Config) error {
		srv, ok := c.Server(id)
		if !ok {
			return errUnknownServer
		}
		if strings.TrimSpace(name) != "" {
			srv.Name = strings.TrimSpace(name)
		}
		srv.Host, srv.Port, srv.Account = host, port, account
		out = *srv
		return nil
	})
	return out, err
}

func (s *Service) RemoveServer(id string) error {
	return s.cfg.Update(func(c *config.Config) error {
		for i, srv := range c.Servers {
			if srv.ID == id {
				c.Servers = append(c.Servers[:i], c.Servers[i+1:]...)
				return nil
			}
		}
		return errUnknownServer
	})
}

var errUnknownServer = errors.New("unknown server")

func (s *Service) server(id string) (config.Server, error) {
	c := s.cfg.Get()
	srv, ok := c.Server(id)
	if !ok {
		return config.Server{}, errUnknownServer
	}
	return *srv, nil
}

type Status struct {
	ID        string `json:"id"`
	Reachable bool   `json:"reachable"`
	// Busy: the server turned this network away (429); it is up.
	Busy          bool                     `json:"busy"`
	Error         string                   `json:"error,omitempty"`
	HasModPage    bool                     `json:"hasModPage"`
	Status        string                   `json:"status,omitempty"`
	StatusMessage string                   `json:"statusMessage,omitempty"`
	Players       *int                     `json:"players,omitempty"`
	Connect       *publicapi.PublicConnect `json:"connect,omitempty"`
	GameVersion   string                   `json:"gameVersion,omitempty"`
	ModCount      int                      `json:"modCount"`
	Plan          modsync.Plan             `json:"plan"`
	Saved         *serverlist.Server       `json:"saved,omitempty"`
}

func (s *Service) Status(ctx context.Context, id string) (Status, error) {
	srv, err := s.server(id)
	if err != nil {
		return Status{}, err
	}
	st := Status{ID: id, Reachable: true, HasModPage: srv.PageURL != "", Plan: modsync.Plan{Download: []publicapi.PublicItem{}, Foreign: []string{}}}
	if srv.PageURL != "" {
		data, err := s.client.Fetch(ctx, srv.PageURL)
		if err != nil {
			st.Reachable, st.Busy, st.Error = false, errors.Is(err, pzclient.ErrBusy), err.Error()
		} else {
			st.Status, st.StatusMessage, st.Players = data.Status, data.StatusMessage, data.Players
			st.Connect, st.GameVersion, st.ModCount = data.Connect, data.GameVersion, len(data.Items)
			st.Plan = modsync.MakePlan(s.user.Mods(), data, s.cfg.Get().Mods)
			s.refreshAddress(&srv, data)
		}
	}
	if saved, err := s.SavedServers(ctx); err == nil {
		if found, ok := serverlist.Find(saved, srv.Host, srv.Port); ok {
			st.Saved = &found
		}
	}
	return st, nil
}

func (s *Service) refreshAddress(srv *config.Server, data *publicapi.PublicData) {
	if data.Connect == nil || (data.Connect.Host == srv.Host && data.Connect.Port == srv.Port) {
		return
	}
	if err := s.cfg.Update(func(c *config.Config) error {
		if cur, ok := c.Server(srv.ID); ok {
			cur.Host, cur.Port = data.Connect.Host, data.Connect.Port
		}
		return nil
	}); err != nil {
		s.log.Warn("save the server's new address", "err", err)
	}
	srv.Host, srv.Port = data.Connect.Host, data.Connect.Port
}

var ErrBusy = errors.New("the launcher is already syncing or launching")

// ErrForeignMods: retry with TakeOver once the player agrees.
var ErrForeignMods = errors.New("foreign-mods")

type SyncProgress struct {
	ServerID string `json:"serverId"`
	modsync.Progress
}

func (s *Service) Sync(ctx context.Context, id string, takeOver bool) error {
	if !s.busy.TryLock() {
		return ErrBusy
	}
	defer s.busy.Unlock()
	srv, err := s.server(id)
	if err != nil {
		return err
	}
	_, err = s.sync(ctx, srv, takeOver)
	return err
}

func (s *Service) sync(ctx context.Context, srv config.Server, takeOver bool) (*publicapi.PublicData, error) {
	if srv.PageURL == "" {
		return nil, nil
	}
	data, err := s.client.Fetch(ctx, srv.PageURL)
	if err != nil {
		return nil, err
	}
	s.refreshAddress(&srv, data)
	mods := s.user.Mods()
	modsync.Cleanup(mods)
	plan := modsync.MakePlan(mods, data, s.cfg.Get().Mods)
	sy := &modsync.Syncer{ModsDir: mods, Page: srv.PageURL, DL: s.client, Retry: s.RetryInterval, Attempts: s.RetryAttempts}
	err = sy.Apply(ctx, plan, s.cfg.Get().Mods, takeOver,
		func(p modsync.Progress) { s.emit.Emit(EventSyncProgress, SyncProgress{ServerID: srv.ID, Progress: p}) },
		func(in modsync.Installed) error {
			return s.cfg.Update(func(c *config.Config) error { c.Mods = in; return nil })
		})
	if errors.Is(err, modsync.ErrForeign) {
		return nil, fmt.Errorf("%w: %s", ErrForeignMods, strings.Join(plan.Foreign, ", "))
	}
	return data, err
}

const (
	PhaseSyncing   = "syncing"
	PhaseWaiting   = "waiting"
	PhaseLaunching = "launching"
	PhaseLaunched  = "launched"
)

type Phase struct {
	ServerID string `json:"serverId"`
	Phase    string `json:"phase"`
	Message  string `json:"message,omitempty"`
}

// PlayRequest logs in with a saved account (AccountID) or a login typed for this launch.
type PlayRequest struct {
	AccountID      int64  `json:"accountId"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	ServerPassword string `json:"serverPassword"`
	TakeOver       bool   `json:"takeOver"`
}

func (s *Service) Play(ctx context.Context, id string, req PlayRequest) error {
	return s.launch(ctx, id, &req, true)
}

func (s *Service) StartGame(ctx context.Context, id string, takeOver bool) error {
	return s.launch(ctx, id, &PlayRequest{TakeOver: takeOver}, false)
}

func (s *Service) launch(ctx context.Context, id string, req *PlayRequest, join bool) error {
	if !s.busy.TryLock() {
		return ErrBusy
	}
	defer s.busy.Unlock()
	in, ok, _ := s.install()
	if !ok {
		return errors.New("the game was not found: choose the Project Zomboid folder in Settings")
	}
	srv, err := s.server(id)
	if err != nil {
		return err
	}
	var ac zomboid.AutoConnect
	if join {
		if ac, err = s.autoConnect(ctx, srv, req); err != nil {
			return err
		}
	}

	phase := func(p, msg string) { s.emit.Emit(EventPlayPhase, Phase{ServerID: id, Phase: p, Message: msg}) }
	phase(PhaseSyncing, "")
	data, err := s.sync(ctx, srv, req.TakeOver)
	if err != nil {
		return err
	}
	if join && data != nil && data.Status != "available" {
		if err := s.waitAvailable(ctx, srv, data, phase); err != nil {
			return err
		}
	}

	phase(PhaseLaunching, "")
	if join {
		if err := helpermod.Install(s.user.Mods()); err != nil {
			return fmt.Errorf("install the auto-connect helper: %w", err)
		}
		if err := s.user.ActivateMod(helpermod.ID); err != nil {
			return fmt.Errorf("enable the auto-connect helper: %w", err)
		}
		if err := s.user.WriteAutoConnect(ac); err != nil {
			return err
		}
	} else if err := s.user.ClearAutoConnect(); err != nil {
		// A leftover file would make this plain start join a server.
		return err
	}
	gen := s.launches.Add(1)
	if err := s.start(in); err != nil {
		if cerr := s.user.ClearAutoConnect(); cerr != nil {
			s.log.Warn("remove the auto-connect file", "err", cerr)
		}
		return fmt.Errorf("start the game: %w", err)
	}
	if join {
		go s.clearUnconsumed(gen)
		if req.AccountID > 0 {
			if err := s.cfg.Update(func(c *config.Config) error {
				if cur, ok := c.Server(id); ok {
					cur.Account = ac.User
				}
				return nil
			}); err != nil {
				s.log.Warn("remember the account", "err", err)
			}
		}
	}
	phase(PhaseLaunched, "")
	return nil
}

func (s *Service) autoConnect(ctx context.Context, srv config.Server, req *PlayRequest) (zomboid.AutoConnect, error) {
	if srv.PageURL != "" && srv.Host == "" {
		if data, err := s.client.Fetch(ctx, srv.PageURL); err == nil {
			s.refreshAddress(&srv, data)
		}
	}
	if srv.Host == "" || srv.Port == 0 {
		return zomboid.AutoConnect{}, errors.New("this server has no address yet: set it in the server's settings")
	}
	ac := zomboid.AutoConnect{Host: srv.Host, Port: srv.Port, ServerName: srv.Name}
	var saved *serverlist.Server
	if list, err := s.SavedServers(ctx); err == nil {
		if found, ok := serverlist.Find(list, srv.Host, srv.Port); ok {
			saved = &found
			ac.ServerPassword = found.ServerPassword
		}
	}
	if req.ServerPassword != "" {
		ac.ServerPassword = req.ServerPassword
	}
	switch {
	case req.AccountID > 0:
		if saved == nil {
			return ac, errors.New("this account is no longer saved in the game")
		}
		for _, a := range saved.Accounts {
			if a.ID == req.AccountID {
				if !a.CanAutoLogin() {
					return ac, errors.New("this account's password is not saved in the game")
				}
				ac.User, ac.PasswordHash = a.Username, a.PasswordHash
				return ac, nil
			}
		}
		return ac, errors.New("this account is no longer saved in the game")
	case strings.TrimSpace(req.Username) != "" && req.Password != "":
		ac.User, ac.PasswordHash = strings.TrimSpace(req.Username), pzhash.Hash(req.Password)
		return ac, nil
	}
	return ac, errors.New("enter a username and password")
}

func (s *Service) waitAvailable(ctx context.Context, srv config.Server, data *publicapi.PublicData, phase func(string, string)) error {
	for data.Status != "available" {
		phase(PhaseWaiting, data.StatusMessage)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.PollInterval):
		}
		next, err := s.client.Fetch(ctx, srv.PageURL)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			msg := "Server unreachable, retrying…"
			if errors.Is(err, pzclient.ErrBusy) {
				msg = "Server busy, retrying…"
			}
			phase(PhaseWaiting, msg)
			continue
		}
		data = next
	}
	return nil
}

// clearUnconsumed removes the auto-connect file if the game never read it.
func (s *Service) clearUnconsumed(gen int64) {
	deadline := time.Now().Add(s.ConsumeTimeout)
	for time.Now().Before(deadline) {
		st, err := os.Stat(s.user.AutoConnectFile())
		if err != nil || st.Size() == 0 || s.launches.Load() != gen {
			return
		}
		time.Sleep(min(time.Second, s.ConsumeTimeout))
	}
	s.busy.Lock()
	defer s.busy.Unlock()
	if s.launches.Load() != gen {
		return
	}
	s.log.Warn("the game did not read the auto-connect file; removing it")
	if err := s.user.ClearAutoConnect(); err != nil {
		s.log.Warn("remove the auto-connect file", "err", err)
	}
}
