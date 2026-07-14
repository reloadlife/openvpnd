package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/reloadlife/openvpnd/migrations"
)

// Store wraps the state SQLite DB plus a dedicated timeseries SQLite DB.
type Store struct {
	db     *sql.DB
	ts     *sql.DB
	memory bool
	tsPath string
}

// OpenOptions configures both SQLite files.
type OpenOptions struct {
	Path           string
	TimeseriesPath string
}

// Open opens state + timeseries databases.
func Open(path string) (*Store, error) {
	return OpenWithOptions(OpenOptions{Path: path})
}

// OpenWithOptions opens state and timeseries SQLite files.
func OpenWithOptions(opts OpenOptions) (*Store, error) {
	tsPath := opts.TimeseriesPath
	if tsPath == "" {
		tsPath = DefaultTimeseriesPath(opts.Path)
	}

	tsDB, tsMem, err := openSQLite(tsPath, true)
	if err != nil {
		return nil, fmt.Errorf("open timeseries db: %w", err)
	}
	if err := ensureTimeseriesSchema(tsDB); err != nil {
		_ = tsDB.Close()
		return nil, err
	}

	stateDB, stateMem, err := openSQLite(opts.Path, false)
	if err != nil {
		_ = tsDB.Close()
		return nil, fmt.Errorf("open state db: %w", err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		_ = stateDB.Close()
		_ = tsDB.Close()
		return nil, err
	}
	if err := goose.Up(stateDB, "."); err != nil {
		_ = stateDB.Close()
		_ = tsDB.Close()
		return nil, fmt.Errorf("migrate state: %w", err)
	}

	if err := applyPerformancePragmas(stateDB, stateMem); err != nil {
		_ = stateDB.Close()
		_ = tsDB.Close()
		return nil, fmt.Errorf("state pragmas: %w", err)
	}
	if err := applyPerformancePragmas(tsDB, tsMem); err != nil {
		_ = stateDB.Close()
		_ = tsDB.Close()
		return nil, fmt.Errorf("timeseries pragmas: %w", err)
	}
	if !tsMem {
		_, _ = tsDB.Exec(`PRAGMA cache_size=-131072`)
		_, _ = tsDB.Exec(`PRAGMA mmap_size=536870912`)
		_, _ = tsDB.Exec(`PRAGMA foreign_keys=OFF`)
	}
	_, _ = stateDB.Exec(`PRAGMA optimize`)
	_, _ = tsDB.Exec(`PRAGMA optimize`)

	return &Store{
		db:     stateDB,
		ts:     tsDB,
		memory: stateMem && tsMem,
		tsPath: tsPath,
	}, nil
}

// Close optimizes and closes both databases.
func (s *Store) Close() error {
	var first error
	if s.ts != nil {
		_, _ = s.ts.Exec(`PRAGMA optimize`)
		if !s.memory {
			_, _ = s.ts.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		}
		if err := s.ts.Close(); err != nil && first == nil {
			first = err
		}
		s.ts = nil
	}
	if s.db != nil {
		s.Optimize()
		if !s.memory {
			_ = s.CheckpointWAL()
		}
		if err := s.db.Close(); err != nil && first == nil {
			first = err
		}
		s.db = nil
	}
	return first
}

// DB exposes the underlying state *sql.DB (for tests).
func (s *Store) DB() *sql.DB { return s.db }

// TimeseriesPath returns the path of the timeseries database.
func (s *Store) TimeseriesPath() string { return s.tsPath }

// Ping checks both databases.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	if s.ts != nil {
		return s.ts.PingContext(ctx)
	}
	return nil
}

// WithTx runs fn inside a state-DB transaction.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
