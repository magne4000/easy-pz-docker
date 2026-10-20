package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	fiberlog "github.com/gofiber/fiber/v3/log"
	"github.com/lmittmann/tint"
)

// LogFileName is pzman's JSON log in Pelican mode, in Config.LogDir.
const LogFileName = "pzman.log"

// logRotateSize: a larger pzman.log is moved to pzman.log.1 at start.
const logRotateSize = 10 << 20

// NewLogger builds the process logger. Handler choice is made here, once.
// In Pelican mode stdout is the panel console, so the JSON log goes to
// LogDir/pzman.log instead, and so does whatever libraries print to stderr
// (which Pelican also shows in its console); close it on exit.
func NewLogger(cfg Config) (*slog.Logger, io.Closer, error) {
	level, err := parseLevel(cfg.LogLevel)
	if err != nil {
		level = slog.LevelInfo
	}
	var h slog.Handler
	var closer io.Closer = io.NopCloser(nil)
	var f *os.File
	switch {
	case cfg.Pelican():
		if f, err = openLogFile(cfg.LogDir); err != nil {
			return nil, nil, err
		}
		h = slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})
	case cfg.IsDev():
		h = tint.NewTextHandler(os.Stderr, &tint.Options{Level: level})
	default:
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	log := slog.New(h)
	// Also routes the standard log package (e.g. net/http) to h.
	slog.SetDefault(log)
	if f != nil {
		if closer, err = captureStderr(log, f); err != nil {
			f.Close()
			return nil, nil, err
		}
	}
	return log, closer, nil
}

// stderrCapture restores os.Stderr, drains the pipe, then closes the log file.
type stderrCapture struct {
	orig *os.File
	w    *os.File
	done chan struct{}
	file *os.File
}

func (s *stderrCapture) Close() error {
	os.Stderr = s.orig
	fiberlog.SetOutput(s.orig)
	err := s.w.Close()
	<-s.done
	return errors.Join(err, s.file.Close())
}

// captureStderr points os.Stderr (and Fiber's logger, which kept the old
// one) at a pipe whose lines become log records, e.g. the SSE library's
// "unable to set write deadline" on every closed tab. A runtime crash still
// reaches the real stderr: the runtime writes to file descriptor 2.
func captureStderr(log *slog.Logger, file *os.File) (io.Closer, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("capture stderr: %w", err)
	}
	s := &stderrCapture{orig: os.Stderr, w: w, done: make(chan struct{}), file: file}
	go func() {
		defer close(s.done)
		defer r.Close()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			log.Warn("stderr", "line", sc.Text())
		}
	}()
	os.Stderr = w
	fiberlog.SetOutput(w)
	return s, nil
}

// openLogFile appends to dir/pzman.log, first rotating it to pzman.log.1
// when it has grown past logRotateSize.
func openLogFile(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	path := filepath.Join(dir, LogFileName)
	if st, err := os.Stat(path); err == nil && st.Size() > logRotateSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, fmt.Errorf("rotate %s: %w", path, err)
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return f, nil
}
