package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Tickets let the requests that cannot send an Authorization header (the
// browser's EventSource, plain download links) authenticate: a session mints
// one for one path with POST /api/auth/ticket and passes it as ?ticket=,
// which only the operations marked ticketMeta accept.
const (
	ticketTTL      = 30 * time.Second
	maxLiveTickets = 10000
	ticketMeta     = "ticket"
)

type ticket struct {
	p    principal
	path string
	exp  time.Time
}

type tickets struct {
	now func() time.Time
	mu  sync.Mutex
	m   map[string]ticket
}

func newTickets(now func() time.Time) *tickets { return &tickets{now: now, m: map[string]ticket{}} }

// issue returns a new ticket for p, valid once on path for ticketTTL.
func (t *tickets) issue(p principal, path string) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	now := t.now()
	exp := now.Add(ticketTTL)
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.m) >= maxLiveTickets {
		for k, v := range t.m {
			if now.After(v.exp) {
				delete(t.m, k)
			}
		}
		if len(t.m) >= maxLiveTickets {
			return "", time.Time{}, errors.New("too many tickets in flight, retry in a minute")
		}
	}
	t.m[id] = ticket{p: p, path: path, exp: exp}
	return id, exp, nil
}

// use consumes a ticket for path: any use, right or wrong, spends it.
func (t *tickets) use(id, path string) (principal, error) {
	t.mu.Lock()
	tk, ok := t.m[id]
	delete(t.m, id)
	t.mu.Unlock()
	switch {
	case !ok:
		return principal{}, errors.New("unknown or already used ticket")
	case t.now().After(tk.exp):
		return principal{}, errors.New("expired ticket")
	case tk.path != path:
		return principal{}, errors.New("the ticket is for another path")
	}
	return tk.p, nil
}
