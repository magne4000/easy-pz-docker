package app

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lmittmann/tint"
)

// LogFileName is pzman's JSON log in Pelican mode, in Config.LogDir.
const LogFileName = "pzman.log"

// logRotateSize: a larger pzman.log is moved to pzman.log.1 at start.
const logRotateSize = 10 << 20

// NewLogger builds the process logger. Handler choice is made here, once.
// In Pelican mode stdout is the panel console, so the JSON log goes to
// LogDir/pzman.log instead; close it on exit.
func NewLogger(cfg Config) (*slog.Logger, io.Closer, error) {
	level, err := parseLevel(cfg.LogLevel)
	if err != nil {
		level = slog.LevelInfo
	}
	var h slog.Handler
	var closer io.Closer = io.NopCloser(nil)
	switch {
	case cfg.Pelican():
		f, err := openLogFile(cfg.LogDir)
		if err != nil {
			return nil, nil, err
		}
		h, closer = slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level}), f
	case cfg.IsDev():
		h = tint.NewTextHandler(os.Stderr, &tint.Options{Level: level})
	default:
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	log := slog.New(h)
	slog.SetDefault(log)
	return log, closer, nil
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
