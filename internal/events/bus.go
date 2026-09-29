package events

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

type Bus struct {
	log     *slog.Logger
	queue   int
	mu      sync.Mutex
	nextID  uint64
	subs    map[uint64]*subscriber
	publish atomic.Uint64
	dropped atomic.Uint64
}

type subscriber struct {
	ch            chan Event
	dropped       uint64
	desyncPending bool
}

type BusStats struct {
	Subscribers int    `json:"subscribers"`
	Published   uint64 `json:"published"`
	Dropped     uint64 `json:"dropped"`
}

func NewBus(log *slog.Logger, subQueue int) *Bus {
	if subQueue < 2 {
		subQueue = 2
	}
	return &Bus{log: log, queue: subQueue, subs: map[uint64]*subscriber{}}
}

// Publish never blocks and never fails. A subscriber that cannot keep up loses
// events and later receives a stream:desync. One slot of every
// queue is reserved for that desync so it always fits.
func (b *Bus) Publish(ev Event) {
	b.publish.Add(1)
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if len(s.ch) < cap(s.ch)-1 {
			s.ch <- ev
			s.desyncPending = false
			continue
		}
		s.dropped++
		b.dropped.Add(1)
		if !s.desyncPending {
			select {
			case s.ch <- StreamDesync{Dropped: s.dropped}:
				s.desyncPending = true
			default:
			}
		}
	}
}

// Subscribe returns a receive-only channel and a cancel func. The channel is
// closed after cancel runs. Cancel is idempotent.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	s := &subscriber{ch: make(chan Event, b.queue)}
	b.subs[id] = s
	b.mu.Unlock()
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			close(s.ch)
			b.mu.Unlock()
		})
	}
}

func (b *Bus) Stats() BusStats {
	b.mu.Lock()
	n := len(b.subs)
	b.mu.Unlock()
	return BusStats{Subscribers: n, Published: b.publish.Load(), Dropped: b.dropped.Load()}
}
