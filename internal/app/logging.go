package app

import (
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
)

// NewLogger builds the process logger. Handler choice is made here, once.
func NewLogger(cfg Config) *slog.Logger {
	level, err := parseLevel(cfg.LogLevel)
	if err != nil {
		level = slog.LevelInfo
	}
	var h slog.Handler
	if cfg.IsDev() {
		h = tint.NewTextHandler(os.Stderr, &tint.Options{Level: level})
	} else {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	log := slog.New(h)
	slog.SetDefault(log)
	return log
}
