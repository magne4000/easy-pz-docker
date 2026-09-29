package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/events"
)

type State string

const (
	Running State = "running"
	Done    State = "done"
	Failed  State = "failed"
)

type Task struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind" doc:"backup|restore|game-update|workshop|mods-apply|update-check|collection-import"`
	Title     string    `json:"title"`
	State     State     `json:"state"`
	Progress  float64   `json:"progress" doc:"0-100, -1 when indeterminate"`
	Message   string    `json:"message"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitzero"`
}

// Registry tracks long-running operations for the UI; only the most recent
// ones are kept.
type Registry struct {
	bus      *events.Bus
	mu       sync.Mutex
	items    map[string]*Task
	lastPub  map[string]time.Time
	keep     int
	throttle time.Duration
}

func NewRegistry(bus *events.Bus) *Registry {
	return &Registry{bus: bus, items: map[string]*Task{}, lastPub: map[string]time.Time{}, keep: 30, throttle: 500 * time.Millisecond}
}

type Handle struct {
	r  *Registry
	id string
}

func (r *Registry) Start(kind, title string) *Handle {
	b := make([]byte, 6)
	rand.Read(b)
	t := &Task{ID: hex.EncodeToString(b), Kind: kind, Title: title, State: Running, Progress: -1, StartedAt: time.Now().UTC()}
	r.mu.Lock()
	r.items[t.ID] = t
	r.prune()
	r.mu.Unlock()
	r.publish(t, true)
	return &Handle{r: r, id: t.ID}
}

func (r *Registry) prune() {
	if len(r.items) <= r.keep {
		return
	}
	var ended []*Task
	for _, t := range r.items {
		if t.State != Running {
			ended = append(ended, t)
		}
	}
	sort.Slice(ended, func(i, j int) bool { return ended[i].StartedAt.Before(ended[j].StartedAt) })
	for i := 0; i < len(ended) && len(r.items) > r.keep; i++ {
		delete(r.items, ended[i].ID)
		delete(r.lastPub, ended[i].ID)
	}
}

func (r *Registry) publish(t *Task, force bool) {
	r.mu.Lock()
	now := time.Now()
	if !force && now.Sub(r.lastPub[t.ID]) < r.throttle {
		r.mu.Unlock()
		return
	}
	r.lastPub[t.ID] = now
	ev := events.TaskChanged{ID: t.ID, Kind: t.Kind}
	r.mu.Unlock()
	if r.bus != nil {
		r.bus.Publish(ev)
	}
}

func (h *Handle) update(force bool, f func(*Task)) {
	h.r.mu.Lock()
	t, ok := h.r.items[h.id]
	if ok {
		f(t)
	}
	h.r.mu.Unlock()
	if ok {
		h.r.publish(t, force)
	}
}

func (h *Handle) ID() string { return h.id }

// Progress sets percent (0-100) and message; events are throttled.
func (h *Handle) Progress(pct float64, msg string) {
	h.update(false, func(t *Task) { t.Progress, t.Message = pct, msg })
}

func (h *Handle) Message(msg string) {
	h.update(true, func(t *Task) { t.Message = msg })
}

// Finish marks the task done, or failed when err != nil.
func (h *Handle) Finish(err error) {
	h.update(true, func(t *Task) {
		t.EndedAt = time.Now().UTC()
		if err != nil {
			t.State, t.Error = Failed, err.Error()
			return
		}
		t.State, t.Progress = Done, 100
	})
}

// List returns tasks newest first.
func (r *Registry) List() []Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Task, 0, len(r.items))
	for _, t := range r.items {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// Running reports whether a task of the given kind is in progress.
func (r *Registry) Running(kind string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.items {
		if t.Kind == kind && t.State == Running {
			return true
		}
	}
	return false
}
