package sched

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/backup"
	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/settings"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/sys"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

type RCON interface {
	Exec(ctx context.Context, cmd string) (string, error)
	Players(ctx context.Context) ([]string, error)
}

// BusyError reports that another lifecycle operation owns the server.
type BusyError struct{ Op string }

func (e *BusyError) Error() string { return "another operation is in progress: " + e.Op }

type Options struct {
	Cfg      app.Config
	Sup      pz.Supervisor
	RCON     RCON
	CMD      steam.CMD
	Mods     *mods.Service
	Backups  *backup.Service
	Settings *settings.Store
	Bus      *events.Bus
	Tasks    *tasks.Registry
	Clock    sys.Clock
	Log      *slog.Logger
	// SaveWait is how long to let PZ flush after an RCON "save" before archiving.
	SaveWait time.Duration
}

type Coordinator struct {
	o Options

	opSem     *semaphore.Weighted // one exclusive lifecycle operation at a time
	mu        sync.Mutex
	op        string
	opSince   time.Time
	cancelOp  context.CancelFunc
	loaded    []string
	window    Window
	cancelWin context.CancelFunc
	upd       updateInfo
	players   PlayerState
	playersAt time.Time
}

type updateInfo struct {
	installed, latest, branch string
	modUpdates                []string
	checkedAt                 time.Time
	checkErr                  string
}

func NewCoordinator(o Options) *Coordinator {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Clock == nil {
		o.Clock = sys.RealClock()
	}
	if o.SaveWait == 0 {
		o.SaveWait = 10 * time.Second
	}
	return &Coordinator{o: o, opSem: semaphore.NewWeighted(1), window: Window{State: WindowIdle}, upd: updateInfo{branch: o.Cfg.ServerBranch}}
}

// ---- exclusive operations ----

func (c *Coordinator) tryBegin(op string) (func(), error) {
	if !c.opSem.TryAcquire(1) {
		cur, _ := c.Operation()
		if cur == "" {
			cur = "finishing"
		}
		return nil, &BusyError{Op: cur}
	}
	c.setOp(op)
	return c.end, nil
}

// begin waits for the current operation to finish.
func (c *Coordinator) begin(ctx context.Context, op string) (func(), error) {
	if err := c.opSem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	c.setOp(op)
	return c.end, nil
}

func (c *Coordinator) setOp(op string) {
	c.mu.Lock()
	c.op, c.opSince = op, c.o.Clock.Now()
	c.mu.Unlock()
}

func (c *Coordinator) end() {
	c.mu.Lock()
	c.op, c.opSince, c.cancelOp = "", time.Time{}, nil
	c.mu.Unlock()
	c.opSem.Release(1)
	c.publishStatus()
}

func (c *Coordinator) publishStatus() {
	c.o.Bus.Publish(events.ServerStatus{State: string(c.o.Sup.Status().State)})
}

// Operation returns the running exclusive operation, if any.
func (c *Coordinator) Operation() (string, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.op, c.opSince
}

// CancelOperation aborts a pending countdown (restart/stop), if one is waiting.
func (c *Coordinator) CancelOperation() bool {
	c.mu.Lock()
	cancel := c.cancelOp
	c.mu.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	return false
}

func running(s pz.State) bool {
	return s == pz.StateRunning || s == pz.StateStarting
}

// ---- players: unknown, empty, populated or stopped ----

type PlayerState struct {
	State string   `json:"state" enum:"unknown,empty,populated,stopped"`
	Count *int     `json:"count" doc:"null when unknown"`
	Names []string `json:"names"`
}

func (c *Coordinator) Players(ctx context.Context) PlayerState {
	if c.o.Sup.Status().State != pz.StateRunning {
		return PlayerState{State: "stopped", Names: []string{}}
	}
	c.mu.Lock()
	if c.o.Clock.Since(c.playersAt) < 5*time.Second {
		p := c.players
		c.mu.Unlock()
		return p
	}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	names, err := c.o.RCON.Players(ctx)
	p := PlayerState{State: "unknown", Names: []string{}}
	if err == nil {
		n := len(names)
		p.Count, p.Names, p.State = &n, names, "populated"
		if n == 0 {
			p.State = "empty"
		}
	}
	c.mu.Lock()
	c.players, c.playersAt = p, c.o.Clock.Now()
	c.mu.Unlock()
	return p
}

// ---- RCON helpers ----

func quote(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
	return `"` + strings.ReplaceAll(s, `"`, `'`) + `"`
}

func (c *Coordinator) Broadcast(ctx context.Context, msg string) error {
	if c.o.Sup.Status().State != pz.StateRunning {
		return pz.ErrNotRunning
	}
	_, err := c.o.RCON.Exec(ctx, "servermsg "+quote(msg))
	return err
}

func (c *Coordinator) Save(ctx context.Context) error {
	if c.o.Sup.Status().State != pz.StateRunning {
		return pz.ErrNotRunning
	}
	_, err := c.o.RCON.Exec(ctx, "save")
	return err
}

func (c *Coordinator) Exec(ctx context.Context, cmd string) (string, error) {
	if c.o.Sup.Status().State != pz.StateRunning {
		return "", pz.ErrNotRunning
	}
	return c.o.RCON.Exec(ctx, cmd)
}

// ---- status ----

type ServerView struct {
	pz.Status
	Players        PlayerState `json:"players"`
	Operation      string      `json:"operation" doc:"exclusive operation in progress, empty when idle"`
	OperationSince time.Time   `json:"operationSince,omitzero"`
	Availability   string      `json:"availability" enum:"available,restarting,unavailable"`
	ServerName     string      `json:"serverName"`
	BuildID        string      `json:"buildId"`
	Branch         string      `json:"branch"`
	NonSteam       bool        `json:"nonSteam"`
	UptimeSeconds  int64       `json:"uptimeSeconds"`
}

func (c *Coordinator) Availability() string {
	st := c.o.Sup.Status().State
	op, _ := c.Operation()
	win := c.Window().State
	switch {
	case st == pz.StateRunning && (op == "" || op == "backup"):
		return "available"
	case st == pz.StateRunning:
		return "restarting"
	case st == pz.StateStarting || st == pz.StateStopping || op != "" || (win != WindowIdle && win != WindowFailed):
		return "restarting"
	default:
		return "unavailable"
	}
}

func (c *Coordinator) View(ctx context.Context) ServerView {
	st := c.o.Sup.Status()
	op, since := c.Operation()
	v := ServerView{Status: st, Players: c.Players(ctx), Operation: op, OperationSince: since, Availability: c.Availability(),
		ServerName: c.o.Cfg.ServerName, Branch: c.o.Cfg.ServerBranch, NonSteam: !c.o.Cfg.UseSteam}
	c.mu.Lock()
	v.BuildID = c.upd.installed
	c.mu.Unlock()
	if st.State == pz.StateRunning && !st.StartedAt.IsZero() {
		v.UptimeSeconds = int64(c.o.Clock.Since(st.StartedAt).Seconds())
	}
	return v
}

// announce broadcasts best-effort: a failed warning must not block the operation.
func (c *Coordinator) announce(ctx context.Context, msg string) {
	if err := c.Broadcast(ctx, msg); err != nil {
		c.o.Log.Warn("broadcast failed", "message", msg, "err", err)
	}
}
