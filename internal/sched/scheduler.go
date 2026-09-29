package sched

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/settings"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/sys"
)

var Actions = []string{"restart", "stop", "save", "broadcast", "backup"}

type ScheduleInput struct {
	Name        string `json:"name" minLength:"1" maxLength:"100"`
	Action      string `json:"action" enum:"restart,stop,save,broadcast,backup"`
	Cron        string `json:"cron" doc:"standard 5-field cron expression, evaluated in the panel's timezone" example:"0 6 * * *"`
	Message     string `json:"message,omitempty" maxLength:"500" doc:"broadcast text, or the countdown prefix for restart/stop"`
	WarnMinutes []int  `json:"warnMinutes,omitempty" doc:"countdown warnings before restart/stop, e.g. [15,5,1]"`
	Enabled     bool   `json:"enabled"`
}

type Schedule struct {
	ID int64 `json:"id"`
	ScheduleInput
	CreatedAt  time.Time `json:"createdAt"`
	LastRunAt  time.Time `json:"lastRunAt,omitzero"`
	LastResult string    `json:"lastResult"`
	NextRunAt  time.Time `json:"nextRunAt,omitzero"`
}

var ErrInvalidSchedule = errors.New("invalid schedule")

func (in *ScheduleInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidSchedule)
	}
	if !slices.Contains(Actions, in.Action) {
		return fmt.Errorf("%w: unknown action %q", ErrInvalidSchedule, in.Action)
	}
	if _, err := cron.ParseStandard(in.Cron); err != nil {
		return fmt.Errorf("%w: cron %q: %v", ErrInvalidSchedule, in.Cron, err)
	}
	if in.Action == "broadcast" && strings.TrimSpace(in.Message) == "" {
		return fmt.Errorf("%w: a broadcast needs a message", ErrInvalidSchedule)
	}
	in.WarnMinutes = settings.NormalizeWarnMinutes(in.WarnMinutes)
	return nil
}

type Scheduler struct {
	c        *Coordinator
	db       *store.DB
	settings *settings.Store
	bus      *events.Bus
	log      *slog.Logger
	s        gocron.Scheduler

	mu       sync.Mutex
	jobs     map[int64]uuid.UUID
	backupJ  gocron.Job
	updateJ  gocron.Job
	lastSett settings.Settings
}

func NewScheduler(c *Coordinator, db *store.DB, st *settings.Store, bus *events.Bus, loc *time.Location, clock sys.Clock, log *slog.Logger) (*Scheduler, error) {
	opts := []gocron.SchedulerOption{gocron.WithLocation(loc)}
	if clock != nil {
		opts = append(opts, gocron.WithClock(clock))
	}
	s, err := gocron.NewScheduler(opts...)
	if err != nil {
		return nil, err
	}
	return &Scheduler{c: c, db: db, settings: st, bus: bus, log: log, s: s, jobs: map[int64]uuid.UUID{}}, nil
}

// Start registers every job and follows settings changes until ctx ends.
func (s *Scheduler) Start(ctx context.Context) error {
	list, err := s.db.ListSchedules(ctx)
	if err != nil {
		return err
	}
	for _, sc := range list {
		if err := s.register(sc); err != nil {
			s.log.Error("schedule could not be registered", "id", sc.ID, "err", err)
		}
	}
	s.syncInternal()
	s.s.Start()
	ch, cancel := s.bus.Subscribe()
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if _, is := ev.(events.SettingsChanged); is {
					s.syncInternal()
				}
			}
		}
	}()
	return nil
}

func (s *Scheduler) Shutdown() error { return s.s.Shutdown() }

func (s *Scheduler) syncInternal() {
	cur := s.settings.Get()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backupJ != nil && cur.BackupIntervalMinutes == s.lastSett.BackupIntervalMinutes &&
		s.updateJ != nil && cur.UpdateCheckMinutes == s.lastSett.UpdateCheckMinutes {
		s.lastSett = cur
		return
	}
	s.s.RemoveByTags("internal")
	s.backupJ = s.internalJob("backup", cur.BackupIntervalMinutes, s.backupTick)
	s.updateJ = s.internalJob("update-check", cur.UpdateCheckMinutes, s.updateTick)
	s.lastSett = cur
}

// internalJob schedules a settings-driven periodic task every minutes
// minutes; it returns nil when minutes is 0 (disabled) or on failure.
func (s *Scheduler) internalJob(name string, minutes int, task func()) gocron.Job {
	if minutes <= 0 {
		return nil
	}
	j, err := s.s.NewJob(gocron.DurationJob(time.Duration(minutes)*time.Minute), gocron.NewTask(task),
		gocron.WithTags("internal"), gocron.WithName(name), gocron.WithSingletonMode(gocron.LimitModeReschedule))
	if err != nil {
		s.log.Error("internal job", "job", name, "err", err)
		return nil
	}
	return j
}

func (s *Scheduler) backupTick() {
	created, err := s.c.RunBackup(context.Background(), "scheduled", "", false)
	var busy *BusyError
	switch {
	case errors.As(err, &busy):
		s.log.Info("scheduled backup skipped", "reason", busy.Error())
	case err != nil:
		s.log.Error("scheduled backup failed", "err", err)
	case !created:
		s.log.Debug("scheduled backup: world unchanged")
	}
}

func (s *Scheduler) updateTick() {
	if _, err := s.c.CheckUpdates(context.Background()); err != nil {
		s.log.Warn("update check", "err", err)
	}
}

func nextRun(j gocron.Job) time.Time {
	if j == nil {
		return time.Time{}
	}
	t, err := j.NextRun()
	if err != nil {
		return time.Time{}
	}
	return t
}

// NextInternal returns when the next scheduled backup and update check fire.
func (s *Scheduler) NextInternal() (backupAt, updateAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nextRun(s.backupJ), nextRun(s.updateJ)
}

func (s *Scheduler) register(sc store.Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.jobs[sc.ID]; ok {
		s.s.RemoveJob(id)
		delete(s.jobs, sc.ID)
	}
	if !sc.Enabled {
		return nil
	}
	id := sc.ID
	j, err := s.s.NewJob(gocron.CronJob(sc.Cron, false), gocron.NewTask(func() { s.execute(id) }),
		gocron.WithName(sc.Name), gocron.WithSingletonMode(gocron.LimitModeReschedule))
	if err != nil {
		return err
	}
	s.jobs[sc.ID] = j.ID()
	return nil
}

func (s *Scheduler) execute(id int64) {
	ctx := context.Background()
	sc, err := s.db.GetSchedule(ctx, id)
	if err != nil {
		s.log.Error("schedule vanished", "id", id, "err", err)
		return
	}
	err = s.run(ctx, sc)
	result := "ok"
	if err != nil {
		result = err.Error()
		s.log.Warn("schedule failed", "id", id, "name", sc.Name, "err", err)
	} else {
		s.log.Info("schedule ran", "id", id, "name", sc.Name, "action", sc.Action)
	}
	if err := s.db.RecordScheduleRun(ctx, id, time.Now(), result); err != nil {
		s.log.Error("record schedule run", "err", err)
	}
	s.bus.Publish(events.SchedulesChanged{})
}

func (s *Scheduler) run(ctx context.Context, sc store.Schedule) error {
	countdown := time.Duration(0)
	if len(sc.WarnMinutes) > 0 {
		countdown = time.Duration(slices.Max(sc.WarnMinutes)) * time.Minute
	}
	req := LifecycleRequest{Countdown: countdown, Message: sc.Message, Reason: "schedule: " + sc.Name, Warn: sc.WarnMinutes}
	switch sc.Action {
	case "restart":
		return s.c.Restart(req)
	case "stop":
		return s.c.Stop(req)
	case "save":
		return s.c.Save(ctx)
	case "broadcast":
		return s.c.Broadcast(ctx, sc.Message)
	case "backup":
		_, err := s.c.RunBackup(ctx, "scheduled", sc.Name, false)
		return err
	}
	return fmt.Errorf("unknown action %q", sc.Action)
}

func (s *Scheduler) view(sc store.Schedule) Schedule {
	v := Schedule{ID: sc.ID, ScheduleInput: ScheduleInput{Name: sc.Name, Action: sc.Action, Cron: sc.Cron, Message: sc.Message,
		WarnMinutes: sc.WarnMinutes, Enabled: sc.Enabled}, CreatedAt: sc.CreatedAt, LastRunAt: sc.LastRunAt, LastResult: sc.LastResult}
	s.mu.Lock()
	id, ok := s.jobs[sc.ID]
	s.mu.Unlock()
	if ok {
		for _, j := range s.s.Jobs() {
			if j.ID() == id {
				v.NextRunAt = nextRun(j)
			}
		}
	}
	return v
}

func (s *Scheduler) List(ctx context.Context) ([]Schedule, error) {
	list, err := s.db.ListSchedules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Schedule, 0, len(list))
	for _, sc := range list {
		out = append(out, s.view(sc))
	}
	return out, nil
}

func toStore(in ScheduleInput) store.Schedule {
	return store.Schedule{Name: in.Name, Action: in.Action, Cron: in.Cron, Message: in.Message, WarnMinutes: in.WarnMinutes, Enabled: in.Enabled}
}

func (s *Scheduler) Create(ctx context.Context, in ScheduleInput) (Schedule, error) {
	if err := in.validate(); err != nil {
		return Schedule{}, err
	}
	sc := toStore(in)
	sc.CreatedAt = time.Now()
	sc, err := s.db.CreateSchedule(ctx, sc)
	if err != nil {
		return Schedule{}, err
	}
	if err := s.register(sc); err != nil {
		return Schedule{}, err
	}
	s.bus.Publish(events.SchedulesChanged{})
	return s.view(sc), nil
}

func (s *Scheduler) Update(ctx context.Context, id int64, in ScheduleInput) (Schedule, error) {
	if err := in.validate(); err != nil {
		return Schedule{}, err
	}
	sc := toStore(in)
	sc.ID = id
	sc, err := s.db.UpdateSchedule(ctx, sc)
	if err != nil {
		return Schedule{}, err
	}
	if err := s.register(sc); err != nil {
		return Schedule{}, err
	}
	s.bus.Publish(events.SchedulesChanged{})
	return s.view(sc), nil
}

func (s *Scheduler) Delete(ctx context.Context, id int64) error {
	if err := s.db.DeleteSchedule(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	if jid, ok := s.jobs[id]; ok {
		s.s.RemoveJob(jid)
		delete(s.jobs, id)
	}
	s.mu.Unlock()
	s.bus.Publish(events.SchedulesChanged{})
	return nil
}

// RunNow executes a schedule immediately, in the background.
func (s *Scheduler) RunNow(ctx context.Context, id int64) error {
	if _, err := s.db.GetSchedule(ctx, id); err != nil {
		return err
	}
	go s.execute(id)
	return nil
}
