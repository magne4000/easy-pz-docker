package rcon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"syscall"
)

// Manager lazily connects, reconnects once on a broken connection, and is safe
// for concurrent use.
type Manager struct {
	log    *slog.Logger
	target func() (addr, password string)
	mu     sync.Mutex
	c      *Client
}

func NewManager(log *slog.Logger, target func() (addr, password string)) *Manager {
	return &Manager{log: log, target: target}
}

func (m *Manager) client(ctx context.Context) (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.c != nil {
		return m.c, nil
	}
	addr, pw := m.target()
	c, err := Dial(ctx, addr, pw)
	if err != nil {
		return nil, err
	}
	m.c = c
	return c, nil
}

func (m *Manager) drop(c *Client) {
	m.mu.Lock()
	if m.c == c {
		m.c = nil
	}
	m.mu.Unlock()
	c.Close()
}

func broken(err error) bool {
	var ne net.Error
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, net.ErrClosed) || (errors.As(err, &ne) && ne.Timeout())
}

// Exec returns a *RejectedError (with the raw response) when PZ rejected the command.
func (m *Manager) Exec(ctx context.Context, cmd string) (string, error) {
	for attempt := 0; ; attempt++ {
		c, err := m.client(ctx)
		if err != nil {
			return "", err
		}
		resp, err := c.Exec(ctx, cmd)
		if err != nil {
			m.drop(c)
			if attempt == 0 && broken(err) && ctx.Err() == nil {
				m.log.Debug("rcon connection lost, reconnecting", "err", err)
				continue
			}
			return "", err
		}
		if rej := Classify(resp); rej != nil {
			return resp, rej
		}
		return resp, nil
	}
}

func (m *Manager) Players(ctx context.Context) ([]string, error) {
	resp, err := m.Exec(ctx, "players")
	if err != nil {
		return nil, err
	}
	return ParsePlayers(resp), nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	c := m.c
	m.c = nil
	m.mu.Unlock()
	if c != nil {
		return c.Close()
	}
	return nil
}
