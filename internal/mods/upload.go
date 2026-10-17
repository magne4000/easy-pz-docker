package mods

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	MaxUploadBytes = 4 << 30
	MaxUploadFiles = 100_000
	// uploadPrefix names the staging folders, inside the mods folder so the
	// install is a rename. Without a mod.info at their root, neither PZ nor
	// ScanLocalMods sees them as mods.
	uploadPrefix = ".pzman-upload-"
)

var (
	ErrUploadBusy     = errors.New("another mod upload is in progress")
	ErrInvalidUpload  = errors.New("invalid mod upload")
	ErrUploadConflict = errors.New("mod upload conflict")
)

// LocalUpload stages an uploaded mod folder until CommitLocalUpload installs
// it. Abort must always be called, also after a successful commit: it deletes
// the staging folder (with a replaced folder in it) and ends the upload.
type LocalUpload struct {
	s      *Service
	dir    string
	folder string
	size   int64
	files  int
	done   sync.Once
}

// BeginLocalUpload starts the only upload allowed at a time.
func (s *Service) BeginLocalUpload() (*LocalUpload, error) {
	if !s.uploadMu.TryLock() {
		return nil, ErrUploadBusy
	}
	// Leftovers of an interrupted upload: none can be in progress.
	stale, _ := filepath.Glob(filepath.Join(s.ModsDir(), uploadPrefix+"*"))
	for _, p := range stale {
		os.RemoveAll(p)
	}
	if err := os.MkdirAll(s.ModsDir(), 0o755); err != nil {
		s.uploadMu.Unlock()
		return nil, err
	}
	dir, err := os.MkdirTemp(s.ModsDir(), uploadPrefix)
	if err != nil {
		s.uploadMu.Unlock()
		return nil, err
	}
	return &LocalUpload{s: s, dir: dir}, nil
}

// Abort deletes what was staged (and a replaced folder) and ends the upload.
func (u *LocalUpload) Abort() {
	u.done.Do(func() {
		os.RemoveAll(u.dir)
		u.s.uploadMu.Unlock()
	})
}

// uploadPath splits a slash-separated upload path, refusing anything that
// could leave the staging folder.
func uploadPath(rel string) ([]string, error) {
	segs := strings.Split(rel, "/")
	if len(segs) < 2 {
		return nil, fmt.Errorf("%w: %q is not inside a mod folder", ErrInvalidUpload, rel)
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "\\\x00") {
			return nil, fmt.Errorf("%w: unsafe path %q", ErrInvalidUpload, rel)
		}
	}
	if strings.HasPrefix(segs[0], ".") {
		return nil, fmt.Errorf("%w: the mod folder name %q starts with a dot", ErrInvalidUpload, segs[0])
	}
	return segs, nil
}

// WriteFile stages one file; rel starts with the mod folder.
func (u *LocalUpload) WriteFile(rel string, r io.Reader) error {
	segs, err := uploadPath(rel)
	if err != nil {
		return err
	}
	if u.folder == "" {
		u.folder = segs[0]
	} else if segs[0] != u.folder {
		return fmt.Errorf("%w: upload one mod folder at a time (%s, %s)", ErrInvalidUpload, u.folder, segs[0])
	}
	if u.files++; u.files > MaxUploadFiles {
		return fmt.Errorf("%w: more than %d files", ErrInvalidUpload, MaxUploadFiles)
	}
	p := filepath.Join(append([]string{u.dir}, segs...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %q is listed twice", ErrInvalidUpload, rel)
	}
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, MaxUploadBytes-u.size+1))
	u.size += n
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && u.size > MaxUploadBytes {
		err = fmt.Errorf("%w: larger than %d GiB", ErrInvalidUpload, MaxUploadBytes>>30)
	}
	return err
}

// CommitLocalUpload installs the staged folder into the mods folder and
// rewrites the ini. A local mod with the same folder is replaced only when
// asked and while the server is stopped: a live server loaded its files.
func (s *Service) CommitLocalUpload(ctx context.Context, u *LocalUpload, replace bool) (string, error) {
	if u.folder == "" {
		return "", fmt.Errorf("%w: no files", ErrInvalidUpload)
	}
	staged := filepath.Join(u.dir, u.folder)
	switch _, ok, err := readMod(staged, ""); {
	case err != nil:
		return "", fmt.Errorf("%w: %s: %v", ErrInvalidUpload, u.folder, err)
	case !ok:
		return "", fmt.Errorf("%w: %s has no mod.info in common/ or a version folder (42, 42.x)", ErrInvalidUpload, u.folder)
	}
	target := filepath.Join(s.ModsDir(), u.folder)
	old := ""
	st, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", err
	case !st.IsDir() || isMirror(target):
		return "", fmt.Errorf("%w: %s is taken by a Workshop mod or is not a folder", ErrUploadConflict, u.folder)
	case !replace:
		return "", fmt.Errorf("%w: %s already exists", ErrUploadConflict, u.folder)
	case s.o.Loaded != nil && s.o.Loaded() != nil:
		return "", fmt.Errorf("%w: stop the server to replace %s; it has the current files loaded", ErrUploadConflict, u.folder)
	default:
		// Inside the staging folder, so Abort deletes it; the dot keeps it
		// apart from the mod folder.
		old = filepath.Join(u.dir, ".replaced")
		if err := os.Rename(target, old); err != nil {
			return "", err
		}
	}
	if err := os.Rename(staged, target); err != nil {
		if old != "" {
			err = errors.Join(err, os.Rename(old, target))
		}
		return "", err
	}
	_, err = s.Apply(ctx)
	return u.folder, err
}
