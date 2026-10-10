package sched

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/steam"
)

type WindowState string

const (
	WindowIdle        WindowState = "idle"
	WindowWaiting     WindowState = "waiting"
	WindowSaving      WindowState = "saving"
	WindowStopping    WindowState = "stopping"
	WindowBackup      WindowState = "backup"
	WindowUpdating    WindowState = "updating"
	WindowReconciling WindowState = "reconciling"
	WindowStarting    WindowState = "starting"
	WindowFailed      WindowState = "failed"
)

type Window struct {
	State      WindowState `json:"state" enum:"idle,waiting,saving,stopping,backup,updating,reconciling,starting,failed"`
	Reason     string      `json:"reason"`
	OpenedAt   time.Time   `json:"openedAt,omitzero"`
	ForceAt    time.Time   `json:"forceAt,omitzero" doc:"when the window proceeds even with players online"`
	Message    string      `json:"message"`
	Error      string      `json:"error,omitempty"`
	GameUpdate bool        `json:"gameUpdate"`
	ModUpdates []string    `json:"modUpdates"`
}

type UpdateStatus struct {
	InstalledBuild      string    `json:"installedBuild"`
	LatestBuild         string    `json:"latestBuild"`
	Branch              string    `json:"branch"`
	GameUpdateAvailable bool      `json:"gameUpdateAvailable" doc:"a newer build exists and the game version is not locked"`
	GameLocked          bool      `json:"gameLocked" doc:"the installed build is kept: SteamCMD never updates the game"`
	ModUpdates          []string  `json:"modUpdates"`
	LastCheckedAt       time.Time `json:"lastCheckedAt,omitzero"`
	CheckError          string    `json:"checkError,omitempty"`
	Window              Window    `json:"window"`
}

func (c *Coordinator) Window() Window {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.window
	w.ModUpdates = append([]string{}, w.ModUpdates...)
	return w
}

func (c *Coordinator) setWindow(f func(*Window)) {
	c.mu.Lock()
	f(&c.window)
	st := c.window.State
	c.mu.Unlock()
	c.o.Bus.Publish(events.UpdateWindow{State: string(st)})
	c.publishStatus()
}

// GameLocked reports whether the installed build is kept: no app_update at
// start or in an update window.
func (c *Coordinator) GameLocked() bool { return c.o.Settings.Get().LockGameVersion }

func (c *Coordinator) UpdateStatus() UpdateStatus {
	locked := c.GameLocked()
	c.mu.Lock()
	u := c.upd
	c.mu.Unlock()
	return UpdateStatus{InstalledBuild: u.installed, LatestBuild: u.latest, Branch: u.branch,
		GameUpdateAvailable: u.newerBuild() && !locked, GameLocked: locked,
		ModUpdates: append([]string{}, u.modUpdates...), LastCheckedAt: u.checkedAt, CheckError: u.checkErr, Window: c.Window()}
}

// RefreshInstalled reads the installed build id from the app manifest.
func (c *Coordinator) RefreshInstalled(ctx context.Context) {
	m, err := c.o.CMD.InstalledBuild(ctx)
	c.mu.Lock()
	if err == nil {
		c.upd.installed = m.BuildID
	}
	c.mu.Unlock()
}

// CheckUpdates compares the installed build with the branch's latest and the
// tracked mods with the Workshop. With autoUpdate it opens the window.
func (c *Coordinator) CheckUpdates(ctx context.Context) (UpdateStatus, error) {
	h := c.o.Tasks.Start("update-check", "Checking for updates")
	c.RefreshInstalled(ctx)
	var errs []error
	latest, err := c.o.CMD.LatestBuildID(ctx, c.o.Cfg.ServerBranch)
	if err != nil {
		errs = append(errs, fmt.Errorf("game: %w", err))
	}
	modUpdates, err := c.o.Mods.CheckUpdates(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("mods: %w", err))
	}
	joined := errors.Join(errs...)
	c.mu.Lock()
	if latest != "" {
		c.upd.latest = latest
	}
	if modUpdates != nil {
		c.upd.modUpdates = modUpdates
	}
	c.upd.checkedAt = c.o.Clock.Now().UTC()
	c.upd.checkErr = ""
	if joined != nil {
		c.upd.checkErr = joined.Error()
	}
	c.mu.Unlock()
	h.Finish(joined)
	st := c.UpdateStatus()
	c.o.Bus.Publish(events.UpdateWindow{State: string(st.Window.State)})
	if (st.GameUpdateAvailable || len(st.ModUpdates) > 0) && c.o.Settings.Get().AutoUpdate {
		if err := c.OpenWindow("auto", false); err != nil && !errors.Is(err, ErrWindowOpen) {
			c.o.Log.Warn("could not open update window", "err", err)
		}
	}
	return st, joined
}

var ErrWindowOpen = errors.New("an update window is already open")

// OpenWindow starts the update window: announce → wait for empty (max
// delay) → save → stop → backup → SteamCMD + workshop → reconcile → start.
// force skips waiting for an empty server.
func (c *Coordinator) OpenWindow(reason string, force bool) error {
	locked := c.GameLocked()
	c.mu.Lock()
	if c.window.State != WindowIdle && c.window.State != WindowFailed {
		c.mu.Unlock()
		return ErrWindowOpen
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelWin = cancel
	now := c.o.Clock.Now().UTC()
	maxDelay := time.Duration(c.o.Settings.Get().UpdateMaxDelayMinutes) * time.Minute
	c.window = Window{State: WindowWaiting, Reason: reason, OpenedAt: now, ForceAt: now.Add(maxDelay),
		GameUpdate: c.upd.newerBuild() && !locked, ModUpdates: append([]string{}, c.upd.modUpdates...)}
	if force {
		c.window.ForceAt = now
	}
	c.mu.Unlock()
	c.setWindow(func(w *Window) { w.Message = "Waiting for the server to be empty" })
	go c.runWindow(ctx, cancel)
	return nil
}

// CancelWindow aborts a window that is still waiting.
func (c *Coordinator) CancelWindow() error {
	c.mu.Lock()
	st, cancel := c.window.State, c.cancelWin
	c.mu.Unlock()
	if st != WindowWaiting || cancel == nil {
		return errors.New("no update window is waiting")
	}
	cancel()
	return nil
}

func (c *Coordinator) runWindow(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	err := c.waitForEmpty(ctx)
	if errors.Is(err, context.Canceled) {
		c.setWindow(func(w *Window) { *w = Window{State: WindowIdle, Message: "Update window cancelled"} })
		if c.o.Sup.Status().State == pz.StateRunning {
			c.announce(context.Background(), "Scheduled update restart cancelled.")
		}
		return
	}
	bg := context.Background()
	end, err := c.begin(bg, "update")
	if err != nil {
		c.failWindow(err)
		return
	}
	defer end()
	if err := c.executeWindow(bg); err != nil {
		c.failWindow(err)
		return
	}
	c.RefreshInstalled(bg)
	c.mu.Lock()
	if c.upd.latest == c.upd.installed || c.upd.latest == "" {
		c.upd.latest = c.upd.installed
	}
	c.upd.modUpdates = []string{}
	c.mu.Unlock()
	c.setWindow(func(w *Window) { *w = Window{State: WindowIdle, Message: "Update completed"} })
	c.o.Log.Info("update window completed")
}

func (c *Coordinator) failWindow(err error) {
	c.o.Log.Error("update window failed", "err", err)
	c.setWindow(func(w *Window) { w.State, w.Error = WindowFailed, err.Error() })
}

func (c *Coordinator) waitForEmpty(ctx context.Context) error {
	w := c.Window()
	warn := c.o.Settings.Get().WarnMinutes
	warned := map[int]bool{}
	announced := false
	for {
		switch c.o.Sup.Status().State {
		case pz.StateStopped, pz.StateCrashed:
			return nil
		case pz.StateStarting, pz.StateStopping:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.o.Clock.After(2 * time.Second):
			}
			continue
		}
		now := c.o.Clock.Now()
		if !now.Before(w.ForceAt) {
			return nil
		}
		ps := c.Players(ctx)
		if ps.State == "empty" {
			return nil
		}
		remaining := w.ForceAt.Sub(now)
		if !announced {
			announced = true
			mins := int((remaining + 30*time.Second) / time.Minute)
			c.announce(ctx, fmt.Sprintf("An update is available. The server will restart when empty, or in %d minutes at the latest.", mins))
		}
		due := false
		for _, m := range warn {
			if !warned[m] && remaining <= time.Duration(m)*time.Minute {
				warned[m], due = true, true
			}
		}
		if due {
			mins := max(1, int((remaining+30*time.Second)/time.Minute))
			c.announce(ctx, fmt.Sprintf("Server restarting for an update in %d minute%s.", mins, plural(mins)))
		}
		next := 30 * time.Second
		if remaining < next {
			next = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.o.Clock.After(next):
		}
	}
}

func (c *Coordinator) executeWindow(ctx context.Context) (err error) {
	w := c.Window()
	h := c.o.Tasks.Start("game-update", "Update window")
	defer func() { h.Finish(err) }()
	step := func(s WindowState, msg string) {
		h.Message(msg)
		c.setWindow(func(w *Window) { w.State, w.Message = s, msg })
	}
	was := running(c.o.Sup.Status().State)
	if was {
		step(WindowSaving, "Saving world")
		if err := c.Save(ctx); err != nil {
			c.o.Log.Warn("save failed", "err", err)
		}
		step(WindowStopping, "Stopping server")
		if err := c.o.Sup.Stop(ctx); err != nil {
			return err
		}
	}
	// From here on, bring the server back even if a step fails.
	defer func() {
		if was && !running(c.o.Sup.Status().State) {
			step(WindowStarting, "Starting server")
			if serr := c.o.Sup.Start(ctx); serr != nil {
				err = errors.Join(err, serr)
			}
		}
	}()
	step(WindowBackup, "Pre-update backup")
	if _, _, err := c.o.Backups.Run(ctx, "pre-update", "", false); err != nil && !errors.Is(err, context.Canceled) {
		c.o.Log.Warn("pre-update backup failed", "err", err)
	}
	// The lock is read again here: it may have been turned on while the window waited.
	if (w.GameUpdate || w.Reason == "manual") && !c.GameLocked() {
		step(WindowUpdating, "Updating game files (SteamCMD)")
		if err := c.o.CMD.AppUpdate(ctx, c.o.Cfg.ServerBranch, false, func(p steam.Progress) {
			h.Progress(p.Percent, p.Message)
		}); err != nil {
			return fmt.Errorf("game update: %w", err)
		}
	}
	step(WindowUpdating, "Updating workshop mods")
	if _, err := c.o.Mods.DownloadOutdated(ctx, h); err != nil {
		return fmt.Errorf("workshop update: %w", err)
	}
	step(WindowReconciling, "Reconciling mod links")
	if _, err := c.o.Mods.Apply(ctx); err != nil {
		return fmt.Errorf("mods: %w", err)
	}
	return nil
}
