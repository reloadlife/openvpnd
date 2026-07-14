package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultTimeseriesPath returns the default timeseries DB path beside the state DB.
func DefaultTimeseriesPath(statePath string) string {
	if statePath == "" || statePath == ":memory:" {
		return "file:openvpnd_ts?mode=memory&cache=shared"
	}
	dir := filepath.Dir(statePath)
	return filepath.Join(dir, "timeseries.db")
}

func openSQLite(path string, timeseries bool) (*sql.DB, bool, error) {
	memory := path == "" || path == ":memory:" || (strings.HasPrefix(path, "file:") && strings.Contains(path, "mode=memory"))
	if !memory && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, false, fmt.Errorf("create db dir: %w", err)
		}
	}

	var dsn string
	switch {
	case memory && strings.HasPrefix(path, "file:"):
		dsn = path
		if !strings.Contains(dsn, "_pragma") {
			dsn += "&_pragma=busy_timeout(10000)&_pragma=temp_store(MEMORY)&_pragma=synchronous(OFF)"
		}
	case memory:
		name := "mem_state"
		if timeseries {
			name = "mem_ts"
		}
		dsn = fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=busy_timeout(10000)&_pragma=temp_store(MEMORY)&_pragma=synchronous(OFF)", name)
	default:
		if timeseries {
			dsn = "file:" + path +
				"?_pragma=busy_timeout(10000)" +
				"&_pragma=journal_mode(WAL)" +
				"&_pragma=synchronous(NORMAL)" +
				"&_pragma=temp_store(MEMORY)" +
				"&_pragma=cache_size(-131072)" +
				"&_pragma=mmap_size(536870912)"
		} else {
			dsn = "file:" + path +
				"?_pragma=busy_timeout(10000)" +
				"&_pragma=foreign_keys(1)" +
				"&_pragma=journal_mode(WAL)" +
				"&_pragma=synchronous(NORMAL)" +
				"&_pragma=temp_store(MEMORY)" +
				"&_pragma=cache_size(-65536)" +
				"&_pragma=mmap_size(268435456)"
		}
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, false, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	sqlDB.SetConnMaxIdleTime(0)

	if !memory {
		enableIncrementalVacuum(sqlDB)
	}
	if err := applyPerformancePragmas(sqlDB, memory); err != nil {
		_ = sqlDB.Close()
		return nil, false, err
	}
	if timeseries && !memory {
		_, _ = sqlDB.Exec(`PRAGMA cache_size=-131072`)
		_, _ = sqlDB.Exec(`PRAGMA mmap_size=536870912`)
		_, _ = sqlDB.Exec(`PRAGMA foreign_keys=OFF`)
	}
	if !memory && path != "" && !strings.HasPrefix(path, "file:") {
		_ = os.Chmod(path, 0o600)
	}
	return sqlDB, memory, nil
}

func ensureTimeseriesSchema(ts *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS traffic_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER NOT NULL,
			sampled_at TEXT NOT NULL,
			rx_bytes INTEGER NOT NULL,
			tx_bytes INTEGER NOT NULL,
			rx_bps REAL NOT NULL,
			tx_bps REAL NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_samples_client_time ON traffic_samples(client_id, sampled_at DESC)`,
	}
	for _, q := range stmts {
		if _, err := ts.Exec(q); err != nil {
			return fmt.Errorf("timeseries schema: %w", err)
		}
	}
	return nil
}
