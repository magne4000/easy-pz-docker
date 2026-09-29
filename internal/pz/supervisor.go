package pz

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/sys"
)

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateCrashed  State = "crashed"
)

type ExitInfo struct {
	At             time.Time `json:"at"`
	Code           int       `json:"code"`
	Classification string    `json:"classification" doc:"clean|killed|crash|start-failed"`
	Message        string    `json:"message"`
}

type Status struct {
	State     State     `json:"state"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
	LastExit  *ExitInfo `json:"lastExit,omitempty"`
	CrashLoop bool      `json:"crashLoop"`
}

var (
	ErrAlreadyRunning = errors.New("server is already running")
	ErrNotRunning     = errors.New("server is not running")
)

// Supervisor owns the PZ JVM process.
type Supervisor interface {
	Start(ctx context.Context) error
	// Stop asks the server to save and quit, waits for it, and kills it after the timeout.
	Stop(ctx context.Context) error
	Status() Status
	// SendConsole writes one command line to the server's console (stdin).
	SendConsole(cmd string) error
}

// Hooks lets the wiring layer observe the supervisor without pz importing events.
type Hooks struct {
	OnState func(Status)
	OnLine  func(line string)
}

type ProcessOptions struct {
	InstallDir  string
	DataDir     string
	ServerName  string
	UID, GID    int
	StopTimeout time.Duration
	// PreStart runs before every spawn (re-applies ProjectZomboid64.json, since
	// a game update overwrites it) and returns extra launch arguments.
	PreStart func(ctx context.Context) ([]string, error)
	// Quit is tried first on Stop (e.g. RCON "quit"); on error the supervisor
	// falls back to writing "quit" on stdin.
	Quit        func(ctx context.Context) error
	AutoRestart bool
	Hooks       Hooks
	Log         *slog.Logger
}

const (
	crashWindow = 10 * time.Minute
	crashLimit  = 3
)

type Process struct {
	o ProcessOptions

	mu        sync.Mutex
	status    Status
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	exited    chan struct{}
	stopping  bool
	killed    bool
	crashes   []time.Time
	restartMu sync.Mutex
	killGrace time.Duration
}

func NewProcess(o ProcessOptions) *Process {
	if o.StopTimeout <= 0 {
		o.StopTimeout = 90 * time.Second
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Process{o: o, status: Status{State: StateStopped}, killGrace: 15 * time.Second}
}

var _ Supervisor = (*Process)(nil)

func (p *Process) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	if s.LastExit != nil {
		e := *s.LastExit
		s.LastExit = &e
	}
	return s
}

func (p *Process) setState(st State, mut func(*Status)) {
	p.mu.Lock()
	p.status.State = st
	if mut != nil {
		mut(&p.status)
	}
	s := p.status
	p.mu.Unlock()
	if p.o.Hooks.OnState != nil {
		p.o.Hooks.OnState(s)
	}
}

func (p *Process) line(s string) {
	if p.o.Hooks.OnLine != nil {
		p.o.Hooks.OnLine(s)
	}
}

func (p *Process) Start(ctx context.Context) error {
	p.mu.Lock()
	switch p.status.State {
	case StateStarting, StateRunning, StateStopping:
		p.mu.Unlock()
		return ErrAlreadyRunning
	}
	p.stopping, p.killed = false, false
	p.mu.Unlock()
	p.setState(StateStarting, func(s *Status) { s.PID = 0 })

	if err := p.spawn(ctx); err != nil {
		p.setState(StateStopped, func(s *Status) {
			s.LastExit = &ExitInfo{At: time.Now().UTC(), Code: -1, Classification: "start-failed", Message: err.Error()}
		})
		return err
	}
	return nil
}

func (p *Process) spawn(ctx context.Context) error {
	script := filepath.Join(p.o.InstallDir, "start-server.sh")
	st, err := os.Stat(script)
	if err != nil {
		return fmt.Errorf("start-server.sh not found (is the game installed?): %w", err)
	}
	if st.Mode().Perm()&0o111 != 0o111 {
		if err := os.Chmod(script, st.Mode().Perm()|0o755); err != nil {
			return fmt.Errorf("chmod start-server.sh: %w", err)
		}
	}
	args := []string{"-cachedir=" + p.o.DataDir, "-servername", p.o.ServerName}
	if p.o.PreStart != nil {
		extra, err := p.o.PreStart(ctx)
		if err != nil {
			return fmt.Errorf("prepare launch: %w", err)
		}
		args = append(args, extra...)
	}
	var stdin io.WriteCloser
	// Not ctx: the server outlives the request that started it.
	proc, err := sys.Proc{Name: "start server", Bin: script, Args: args, Dir: p.o.InstallDir, Home: p.o.DataDir,
		UID: p.o.UID, GID: p.o.GID, Stdin: &stdin}.Start(context.Background())
	if err != nil {
		return err
	}
	cmd, pr := proc.Cmd, proc.Output
	exited := make(chan struct{})
	p.mu.Lock()
	p.cmd, p.stdin, p.exited = cmd, stdin, exited
	p.mu.Unlock()
	p.setState(StateStarting, func(s *Status) { s.PID = cmd.Process.Pid; s.StartedAt = time.Now().UTC() })
	p.o.Log.Info("server process started", "pid", cmd.Process.Pid, "args", strings.Join(args, " "))

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			text := strings.TrimRight(sc.Text(), "\r")
			p.line(text)
			if strings.Contains(text, "SERVER STARTED") {
				p.mu.Lock()
				starting := p.status.State == StateStarting
				p.mu.Unlock()
				if starting {
					p.setState(StateRunning, nil)
				}
			}
		}
		io.Copy(io.Discard, pr)
	}()
	go func() {
		p.onExit(<-proc.Done)
		close(exited)
	}()
	return nil
}

func (p *Process) onExit(err error) {
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	p.mu.Lock()
	requested, killed := p.stopping, p.killed
	p.mu.Unlock()
	info := &ExitInfo{At: time.Now().UTC(), Code: code}
	next := StateStopped
	switch {
	case requested && killed:
		info.Classification, info.Message = "killed", "did not exit within the stop timeout and was killed"
	case requested:
		info.Classification, info.Message = "clean", "stopped on request"
	default:
		info.Classification, info.Message = "crash", fmt.Sprintf("exited unexpectedly with code %d", code)
		next = StateCrashed
	}
	p.o.Log.Info("server process exited", "code", code, "classification", info.Classification)
	crashLoop := false
	if next == StateCrashed {
		now := time.Now()
		p.mu.Lock()
		kept := p.crashes[:0]
		for _, t := range p.crashes {
			if now.Sub(t) < crashWindow {
				kept = append(kept, t)
			}
		}
		p.crashes = append(kept, now)
		crashLoop = len(p.crashes) >= crashLimit
		p.mu.Unlock()
	}
	p.setState(next, func(s *Status) { s.PID = 0; s.LastExit = info; s.CrashLoop = crashLoop })
	if next == StateCrashed && p.o.AutoRestart && !crashLoop {
		go func() {
			time.Sleep(10 * time.Second)
			p.restartMu.Lock()
			defer p.restartMu.Unlock()
			if p.Status().State == StateCrashed {
				p.o.Log.Warn("restarting crashed server")
				if err := p.Start(context.Background()); err != nil {
					p.o.Log.Error("auto-restart failed", "err", err)
				}
			}
		}()
	}
}

// ResetCrashLoop clears the crash history (an explicit start by the admin).
func (p *Process) ResetCrashLoop() {
	p.mu.Lock()
	p.crashes = nil
	p.status.CrashLoop = false
	p.mu.Unlock()
}

func (p *Process) SendConsole(cmd string) error {
	p.mu.Lock()
	stdin := p.stdin
	running := p.cmd != nil && (p.status.State == StateRunning || p.status.State == StateStarting || p.status.State == StateStopping)
	p.mu.Unlock()
	if !running || stdin == nil {
		return ErrNotRunning
	}
	_, err := io.WriteString(stdin, cmd+"\n")
	return err
}

func (p *Process) Stop(ctx context.Context) error {
	p.mu.Lock()
	st := p.status.State
	exited := p.exited
	cmd := p.cmd
	if st == StateStopped || st == StateCrashed || cmd == nil {
		p.mu.Unlock()
		return nil
	}
	alreadyStopping := p.stopping
	p.stopping = true
	p.mu.Unlock()
	if !alreadyStopping {
		p.setState(StateStopping, nil)
		quitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		qerr := errors.New("no quit hook")
		if p.o.Quit != nil {
			qerr = p.o.Quit(quitCtx)
		}
		cancel()
		if qerr != nil {
			p.o.Log.Info("sending quit on the server console", "reason", qerr)
			if err := p.SendConsole("quit"); err != nil {
				p.o.Log.Warn("could not write quit to the console", "err", err)
			}
		}
	}
	timer := time.NewTimer(p.o.StopTimeout)
	defer timer.Stop()
	select {
	case <-exited:
		return nil
	case <-timer.C:
	case <-ctx.Done():
	}
	p.o.Log.Warn("server did not stop in time, terminating")
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	pid := cmd.Process.Pid
	_ = sys.SignalGroup(pid, syscall.SIGTERM)
	select {
	case <-exited:
		return nil
	case <-time.After(p.killGrace):
	}
	_ = sys.SignalGroup(pid, syscall.SIGKILL)
	<-exited
	return nil
}
