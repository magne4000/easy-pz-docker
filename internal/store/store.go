package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/magne4000/easy-pz-docker/internal/store/sqlcdb"
)

//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

//go:embed migrations/*.sql
var migrations embed.FS

var ErrNotFound = errors.New("not found")

type DB struct {
	sql *sql.DB
	q   *sqlcdb.Queries
}

// Open opens (creating if needed) the database and runs every pending
// migration before returning.
func Open(ctx context.Context, log *slog.Logger, path string) (*DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	sub, _ := fs.Sub(migrations, "migrations")
	p, err := goose.NewProvider(goose.DialectSQLite3, db, sub)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrations: %w", err)
	}
	res, err := p.Up(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	for _, r := range res {
		log.Info("applied migration", "version", r.Source.Version, "duration", r.Duration)
	}
	return &DB{sql: db, q: sqlcdb.New(db)}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnix(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0).UTC()
}

// affected turns a zero-row write into ErrNotFound.
func affected(n int64, err error) error {
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// notFound maps sql.ErrNoRows to ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
