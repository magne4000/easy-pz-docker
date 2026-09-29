package sched

import (
	"context"

	"github.com/magne4000/easy-pz-docker/internal/pz"
)

// RunBackup never runs mid-restart or mid-update; if the server is running the
// world is saved first.
func (c *Coordinator) RunBackup(ctx context.Context, reason, note string, force bool) (bool, error) {
	end, err := c.tryBegin("backup")
	if err != nil {
		return false, err
	}
	defer end()
	return c.backupLocked(ctx, reason, note, force)
}

func (c *Coordinator) backupLocked(ctx context.Context, reason, note string, force bool) (bool, error) {
	if c.o.Sup.Status().State == pz.StateRunning {
		if err := c.Save(ctx); err != nil {
			c.o.Log.Warn("save before backup failed", "err", err)
		} else if err := c.sleepUntil(ctx, c.o.Clock.Now().Add(c.o.SaveWait)); err != nil {
			return false, err
		}
	}
	_, created, err := c.o.Backups.Run(ctx, reason, note, force)
	return created, err
}

// Restore stops the server if needed, restores, and starts it again if it was running.
func (c *Coordinator) Restore(id int64) error {
	if _, err := c.o.Backups.Get(context.Background(), id); err != nil {
		return err
	}
	end, err := c.tryBegin("restore")
	if err != nil {
		return err
	}
	c.publishStatus()
	go func() {
		defer end()
		ctx := context.Background()
		was := running(c.o.Sup.Status().State)
		if was {
			if err := c.stopServer(ctx, nil); err != nil {
				c.o.Log.Error("restore: stop failed", "err", err)
				return
			}
		}
		if err := c.o.Backups.Restore(ctx, id); err != nil {
			c.o.Log.Error("restore failed", "id", id, "err", err)
		}
		if was {
			if err := c.o.Sup.Start(ctx); err != nil {
				c.o.Log.Error("restore: start failed", "err", err)
			}
		}
	}()
	return nil
}
