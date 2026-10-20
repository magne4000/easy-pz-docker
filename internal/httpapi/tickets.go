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
	// maxTicketsPerSubject keeps one session from filling the shared store:
	// the SPA holds one or two at a time.
	maxTicketsPerSubject = 16
	ticketMeta           = "ticket"
)

var errTooManyTickets = errors.New("too many tickets in flight for this user, retry in a few seconds")

type ticket struct {
	p    principal
	path string
	exp  time.Time
}

type tickets struct {
	now       func() time.Time
	mu        sync.Mutex
	m         map[string]ticket
	bySubject map[string]int // live tickets per principal.Subject
}

func newTickets(now func() time.Time) *tickets {
	return &tickets{now: now, m: map[string]ticket{}, bySubject: map[string]int{}}
}

// remove drops a ticket; t.mu is held.
func (t *tickets) remove(id string, tk ticket) {
	delete(t.m, id)
	if t.bySubject[tk.p.Subject]--; t.bySubject[tk.p.Subject] <= 0 {
		delete(t.bySubject, tk.p.Subject)
	}
}

// sweep drops the expired tickets; t.mu is held.
func (t *tickets) sweep(now time.Time) {
	for id, tk := range t.m {
		if now.After(tk.exp) {
			t.remove(id, tk)
		}
	}
}

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
	if len(t.m) >= maxLiveTickets || t.bySubject[p.Subject] >= maxTicketsPerSubject {
		t.sweep(now)
	}
	switch {
	case t.bySubject[p.Subject] >= maxTicketsPerSubject:
		return "", time.Time{}, errTooManyTickets
	case len(t.m) >= maxLiveTickets:
		return "", time.Time{}, errors.New("too many tickets in flight, retry in a minute")
	}
	t.m[id] = ticket{p: p, path: path, exp: exp}
	t.bySubject[p.Subject]++
	return id, exp, nil
}

// use consumes a ticket for path: any use, right or wrong, spends it.
func (t *tickets) use(id, path string) (principal, error) {
	t.mu.Lock()
	tk, ok := t.m[id]
	if ok {
		t.remove(id, tk)
	}
	t.mu.Unlock()
	switch {
	case !ok:
		return principal{}, errors.New("unknown or already used ticket")
	case t.now().After(tk.exp):
		return principal{}, errors.New("expired ticket")
	case tk.path != path:
		return principal{}, errors.New("the ticket is for another path")
	case !tk.p.Expires.IsZero() && t.now().After(tk.p.Expires):
		return principal{}, errors.New("the session that made the ticket ended")
	}
	return tk.p, nil
}
