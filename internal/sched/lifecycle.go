package sched

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

// countdown broadcasts the configured warnings that fit in total, then waits it out.
func (c *Coordinator) countdown(ctx context.Context, total time.Duration, what string, h *tasks.Handle, warn []int) error {
	if total <= 0 || c.o.Sup.Status().State != pz.StateRunning {
		return nil
	}
	deadline := c.o.Clock.Now().Add(total)
	if warn == nil {
		warn = c.o.Settings.Get().WarnMinutes
	}
	first := true
	for _, m := range warn {
		at := deadline.Add(-time.Duration(m) * time.Minute)
		if at.Before(c.o.Clock.Now()) {
			if !first {
				continue
			}
			m = int((total + 30*time.Second) / time.Minute)
			at = c.o.Clock.Now()
		}
		first = false
		if err := c.sleepUntil(ctx, at); err != nil {
			return err
		}
		msg := fmt.Sprintf("%s in %d minute%s.", what, m, plural(m))
		if h != nil {
			h.Message(msg)
		}
		if err := c.Broadcast(ctx, msg); err != nil {
			c.o.Log.Warn("countdown broadcast failed", "err", err)
		}
	}
	return c.sleepUntil(ctx, deadline)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (c *Coordinator) sleepUntil(ctx context.Context, t time.Time) error {
	d := t.Sub(c.o.Clock.Now())
	if d <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.o.Clock.After(d):
		return nil
	}
}

// Start starts the server in the background.
func (c *Coordinator) Start(ctx context.Context) error {
	end, err := c.tryBegin("start")
	if err != nil {
		return err
	}
	defer end()
	if p, ok := c.o.Sup.(interface{ ResetCrashLoop() }); ok {
		p.ResetCrashLoop()
	}
	return c.o.Sup.Start(context.WithoutCancel(ctx))
}

type LifecycleRequest struct {
	Countdown time.Duration
	Message   string
	Reason    string
	// Warn overrides the configured countdown warnings (minutes, descending).
	Warn []int
}

// Stop stops the server after an optional countdown. It returns once the
// operation is accepted; progress is visible through tasks and status events.
func (c *Coordinator) Stop(req LifecycleRequest) error {
	return c.async("stop", "Stopping server", req, func(ctx context.Context, h *tasks.Handle) error {
		return c.stopServer(ctx, h)
	})
}

func (c *Coordinator) Restart(req LifecycleRequest) error {
	return c.async("restart", "Restarting server", req, func(ctx context.Context, h *tasks.Handle) error {
		if err := c.stopServer(ctx, h); err != nil {
			return err
		}
		h.Message("Starting")
		return c.o.Sup.Start(ctx)
	})
}

func (c *Coordinator) async(op, title string, req LifecycleRequest, body func(context.Context, *tasks.Handle) error) error {
	end, err := c.tryBegin(op)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancelOp = cancel
	c.mu.Unlock()
	c.publishStatus()
	h := c.o.Tasks.Start(op, title)
	go func() {
		defer end()
		defer cancel()
		what := "Server restart"
		if op == "stop" {
			what = "Server shutdown"
		}
		if req.Message != "" {
			what = req.Message
		}
		err := c.countdown(ctx, req.Countdown, what, h, req.Warn)
		if err == nil {
			c.mu.Lock()
			c.cancelOp = nil // past the point of no return
			c.mu.Unlock()
			err = body(context.WithoutCancel(ctx), h)
		}
		if errors.Is(err, context.Canceled) {
			c.o.Log.Info("operation cancelled", "op", op)
			if c.o.Sup.Status().State == pz.StateRunning {
				c.announce(context.Background(), "Scheduled "+op+" cancelled.")
			}
			err = errors.New("cancelled")
		}
		h.Finish(err)
		if err != nil {
			c.o.Log.Error("operation failed", "op", op, "reason", req.Reason, "err", err)
		}
	}()
	return nil
}

func (c *Coordinator) stopServer(ctx context.Context, h *tasks.Handle) error {
	if c.o.Sup.Status().State == pz.StateRunning {
		if h != nil {
			h.Message("Saving world")
		}
		if err := c.Save(ctx); err != nil {
			c.o.Log.Warn("save before stop failed", "err", err)
		}
	}
	if h != nil {
		h.Message("Stopping")
	}
	return c.o.Sup.Stop(ctx)
}
