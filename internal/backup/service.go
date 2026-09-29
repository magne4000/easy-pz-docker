package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

var (
	ErrBusy    = errors.New("a backup or restore is already running")
	ErrMissing = errors.New("backup archive is missing on disk")
)

type ServiceOptions struct {
	DB         *store.DB
	Bus        *events.Bus
	Tasks      *tasks.Registry
	Log        *slog.Logger
	Set        Set
	Dir        string
	ServerName string
	BuildID    func() string
	Mods       func(ctx context.Context) []ModRef
	Policy     func() Policy
	// OnProgress lets dev fakes slow the engine down to make progress visible.
	OnProgress func(Progress)
}

type Service struct {
	o       ServiceOptions
	mu      sync.Mutex
	running bool
	lastRun time.Time
	checked contentCheck
}

// contentCheck caches the last sameContent verdict: Status polls it and
// re-hashing the same files for an unchanged fingerprint would be wasted reads.
type contentCheck struct {
	backupID    int64
	fingerprint string
	unchanged   bool
}

func NewService(o ServiceOptions) *Service {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{o: o}
}

type Summary struct {
	Running   bool      `json:"running"`
	LastRunAt time.Time `json:"lastRunAt,omitzero"`
	TotalSize int64     `json:"totalSize"`
	Count     int       `json:"count"`
	Unchanged bool      `json:"unchanged" doc:"the world has not changed since the newest backup"`
	BackupDir string    `json:"backupDir"`
}

func (s *Service) acquire() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return ErrBusy
	}
	s.running = true
	return nil
}

func (s *Service) release() {
	s.mu.Lock()
	s.running = false
	s.lastRun = time.Now().UTC()
	s.mu.Unlock()
	s.o.Bus.Publish(events.BackupsChanged{})
}

func (s *Service) Path(b store.Backup) string { return filepath.Join(s.o.Dir, b.File) }

// Unchanged reports whether the world matches the newest backup.
func (s *Service) Unchanged(ctx context.Context) (bool, error) {
	files, fp, err := walk(s.o.Set)
	if err != nil {
		return false, err
	}
	_, same, err := s.unchanged(ctx, files, fp)
	return same, err
}

// unchanged compares the set with the newest backup. A matching fingerprint
// decides without reading anything; otherwise the files whose mtime moved are
// hashed against that backup's manifest, because a running server's save
// (sent before every backup) bumps mtimes even when no byte changed.
func (s *Service) unchanged(ctx context.Context, files []walked, fp Fingerprint) (store.Backup, bool, error) {
	last, err := s.o.DB.LatestBackup(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return last, fp.FileCount == 0, nil
	}
	if err != nil {
		return last, false, err
	}
	key := fp.String()
	if last.Fingerprint == key {
		return last, true, nil
	}
	s.mu.Lock()
	c := s.checked
	s.mu.Unlock()
	if c.backupID == last.ID && c.fingerprint == key {
		return last, c.unchanged, nil
	}
	m, err := ReadManifest(s.Path(last))
	if err != nil {
		// Nothing to compare against (archive gone or unreadable): back up.
		s.o.Log.Warn("backup gate: newest backup's manifest unreadable", "id", last.ID, "err", err)
		return last, false, nil
	}
	same, err := sameContent(ctx, s.o.Set.Root, files, m.Files)
	if err != nil {
		return last, false, err
	}
	s.mu.Lock()
	s.checked = contentCheck{backupID: last.ID, fingerprint: key, unchanged: same}
	s.mu.Unlock()
	return last, same, nil
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// Run creates a backup unless the world is unchanged since the last one
// (force skips the gate). created=false means it was skipped.
func (s *Service) Run(ctx context.Context, reason, note string, force bool) (store.Backup, bool, error) {
	if err := s.acquire(); err != nil {
		return store.Backup{}, false, err
	}
	defer s.release()
	return s.run(ctx, reason, note, force, true)
}

func (s *Service) run(ctx context.Context, reason, note string, force, prune bool) (store.Backup, bool, error) {
	files, fp, err := walk(s.o.Set)
	if err != nil {
		return store.Backup{}, false, err
	}
	if fp.FileCount == 0 {
		return store.Backup{}, false, nil
	}
	if !force {
		last, same, err := s.unchanged(ctx, files, fp)
		if err != nil {
			return store.Backup{}, false, err
		}
		if same {
			s.o.Log.Info("backup skipped: world unchanged since last backup", "reason", reason, "last", last.ID)
			return last, false, nil
		}
	}
	h := s.o.Tasks.Start("backup", "Backup ("+reason+")")
	now := time.Now().UTC()
	// Milliseconds: two backups in the same second must not share a file name —
	// Create would overwrite the earlier archive and the failed insert then delete it.
	name := fmt.Sprintf("%s-%s-%s.tar.zst", unsafeChars.ReplaceAllString(s.o.ServerName, "_"), now.Format("20060102-150405.000"), unsafeChars.ReplaceAllString(reason, "_"))
	m := Manifest{CreatedAt: now, ServerName: s.o.ServerName, Reason: reason, Note: note}
	if s.o.BuildID != nil {
		m.BuildID = s.o.BuildID()
	}
	if s.o.Mods != nil {
		m.Mods = s.o.Mods(ctx)
	}
	m, size, err := Create(ctx, s.o.Set, filepath.Join(s.o.Dir, name), m, func(p Progress) {
		if s.o.OnProgress != nil {
			s.o.OnProgress(p)
		}
		if p.TotalBytes > 0 {
			h.Progress(100*float64(p.DoneBytes)/float64(p.TotalBytes), p.File)
		}
	})
	if err != nil {
		h.Finish(err)
		return store.Backup{}, false, err
	}
	b, err := s.o.DB.InsertBackup(ctx, store.Backup{File: name, CreatedAt: now, Size: size, Fingerprint: m.Fingerprint.String(),
		Reason: reason, Note: note, BuildID: m.BuildID, FileCount: len(m.Files), ModCount: len(m.Mods)})
	if err != nil {
		_ = Delete(filepath.Join(s.o.Dir, name))
		h.Finish(err)
		return store.Backup{}, false, err
	}
	h.Finish(nil)
	s.o.Log.Info("backup created", "id", b.ID, "file", name, "size", size, "files", len(m.Files), "reason", reason)
	if prune {
		if err := s.prune(ctx); err != nil {
			s.o.Log.Warn("backup retention", "err", err)
		}
	}
	return b, true, nil
}

func (s *Service) prune(ctx context.Context) error {
	if s.o.Policy == nil {
		return nil
	}
	all, err := s.o.DB.ListBackups(ctx)
	if err != nil {
		return err
	}
	infos := make([]Info, len(all))
	byID := map[int64]store.Backup{}
	for i, b := range all {
		infos[i] = Info{ID: b.ID, CreatedAt: b.CreatedAt, Size: b.Size, Pinned: b.Pinned}
		byID[b.ID] = b
	}
	var errs []error
	for _, id := range PlanRetention(infos, s.o.Policy(), time.Now()) {
		b := byID[id]
		if err := Delete(s.Path(b)); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := s.o.DB.DeleteBackup(ctx, id); err != nil {
			errs = append(errs, err)
			continue
		}
		s.o.Log.Info("backup pruned", "id", id, "file", b.File)
	}
	return errors.Join(errs...)
}

func (s *Service) List(ctx context.Context) ([]store.Backup, error) { return s.o.DB.ListBackups(ctx) }

func (s *Service) Get(ctx context.Context, id int64) (store.Backup, error) {
	return s.o.DB.GetBackup(ctx, id)
}

func (s *Service) Status(ctx context.Context) (Summary, error) {
	all, err := s.o.DB.ListBackups(ctx)
	if err != nil {
		return Summary{}, err
	}
	s.mu.Lock()
	st := Summary{Running: s.running, LastRunAt: s.lastRun, Count: len(all), BackupDir: s.o.Dir}
	s.mu.Unlock()
	for _, b := range all {
		st.TotalSize += b.Size
	}
	st.Unchanged, _ = s.Unchanged(ctx)
	return st, nil
}

func (s *Service) SetPinned(ctx context.Context, id int64, pinned bool) error {
	if err := s.o.DB.SetBackupPinned(ctx, id, pinned); err != nil {
		return err
	}
	s.o.Bus.Publish(events.BackupsChanged{})
	return nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	b, err := s.o.DB.GetBackup(ctx, id)
	if err != nil {
		return err
	}
	if err := Delete(s.Path(b)); err != nil {
		return err
	}
	if err := s.o.DB.DeleteBackup(ctx, id); err != nil {
		return err
	}
	s.o.Bus.Publish(events.BackupsChanged{})
	return nil
}

func (s *Service) Manifest(ctx context.Context, id int64) (Manifest, error) {
	b, err := s.o.DB.GetBackup(ctx, id)
	if err != nil {
		return Manifest{}, err
	}
	return ReadManifest(s.Path(b))
}

// Restore verifies the archive, takes a pre-restore safety backup, then
// restores. The caller guarantees the server is stopped.
func (s *Service) Restore(ctx context.Context, id int64) error {
	b, err := s.o.DB.GetBackup(ctx, id)
	if err != nil {
		return err
	}
	path := s.Path(b)
	if _, err := os.Stat(path); err != nil {
		return ErrMissing
	}
	if err := s.acquire(); err != nil {
		return err
	}
	defer s.release()
	h := s.o.Tasks.Start("restore", fmt.Sprintf("Restore backup #%d", id))
	h.Message("Verifying archive")
	if err := Verify(ctx, path); err != nil {
		h.Finish(err)
		return err
	}
	h.Message("Taking a pre-restore safety backup")
	if _, _, err := s.run(ctx, "pre-restore", fmt.Sprintf("before restoring #%d", id), false, false); err != nil {
		err = fmt.Errorf("pre-restore backup failed, not restoring: %w", err)
		h.Finish(err)
		return err
	}
	err = Restore(ctx, path, s.o.Set.Root, func(p Progress) {
		if p.TotalBytes > 0 {
			h.Progress(100*float64(p.DoneBytes)/float64(p.TotalBytes), p.File)
		}
	})
	h.Finish(err)
	if err == nil {
		s.o.Log.Info("backup restored", "id", id)
	}
	return err
}
