package console

import (
	"sync"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/events"
)

// Ring is a bounded, concurrency-safe buffer of the newest console lines.
type Ring struct {
	mu    sync.RWMutex
	buf   []events.ConsoleLine
	start int // index of the oldest line
	n     int
	seq   uint64
	now   func() time.Time
}

func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{buf: make([]events.ConsoleLine, capacity), now: time.Now}
}

// Append assigns the next sequence number and returns the stored line.
func (r *Ring) Append(text string) events.ConsoleLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	line := events.ConsoleLine{Seq: r.seq, At: r.now().UTC(), Text: text}
	c := len(r.buf)
	if r.n < c {
		r.buf[(r.start+r.n)%c] = line
		r.n++
	} else {
		r.buf[r.start] = line
		r.start = (r.start + 1) % c
	}
	return line
}

// Tail returns up to n newest lines, oldest-first. n <= 0 or n > capacity
// clamps to capacity.
func (r *Ring) Tail(n int) []events.ConsoleLine {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n <= 0 || n > len(r.buf) {
		n = len(r.buf)
	}
	if n > r.n {
		n = r.n
	}
	return r.copyFrom(r.n-n, n)
}

// Since returns lines with Seq > seq, oldest-first, plus whether the caller's
// seq has already been evicted (so the client knows it has a hole).
func (r *Ring) Since(seq uint64) ([]events.ConsoleLine, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.n == 0 || seq >= r.seq {
		return []events.ConsoleLine{}, false
	}
	oldest := r.buf[r.start].Seq
	gap := seq+1 < oldest
	skip := 0
	if !gap {
		skip = int(seq + 1 - oldest)
	}
	return r.copyFrom(skip, r.n-skip), gap
}

func (r *Ring) copyFrom(offset, count int) []events.ConsoleLine {
	out := make([]events.ConsoleLine, count)
	c := len(r.buf)
	for i := range count {
		out[i] = r.buf[(r.start+offset+i)%c]
	}
	return out
}
