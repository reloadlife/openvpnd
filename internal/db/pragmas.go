package db

import (
	"database/sql"
	"fmt"
	"strings"
)

func applyPerformancePragmas(sqlDB *sql.DB, memory bool) error {
	stmts := []string{
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=10000`,
		`PRAGMA temp_store=MEMORY`,
		`PRAGMA recursive_triggers=ON`,
		`PRAGMA secure_delete=OFF`,
		`PRAGMA cache_size=-65536`,
	}
	if !memory {
		stmts = append([]string{
			`PRAGMA journal_mode=WAL`,
			`PRAGMA synchronous=NORMAL`,
			`PRAGMA mmap_size=268435456`,
			`PRAGMA wal_autocheckpoint=1000`,
			`PRAGMA journal_size_limit=67108864`,
		}, stmts...)
	} else {
		stmts = append([]string{
			`PRAGMA synchronous=OFF`,
		}, stmts...)
	}

	for _, q := range stmts {
		if _, err := sqlDB.Exec(q); err != nil {
			if isCriticalPragma(q) {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
	}
	return nil
}

func enableIncrementalVacuum(sqlDB *sql.DB) {
	var tables int
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&tables)
	if tables > 0 {
		return
	}
	_, _ = sqlDB.Exec(`PRAGMA auto_vacuum=INCREMENTAL`)
	_, _ = sqlDB.Exec(`VACUUM`)
	_, _ = sqlDB.Exec(`PRAGMA auto_vacuum=INCREMENTAL`)
}

func isCriticalPragma(q string) bool {
	u := strings.ToUpper(q)
	return strings.Contains(u, "FOREIGN_KEYS") ||
		strings.Contains(u, "JOURNAL_MODE") ||
		strings.Contains(u, "BUSY_TIMEOUT")
}

func pragmaGet(sqlDB *sql.DB, name string) (string, error) {
	row := sqlDB.QueryRow(`PRAGMA ` + name)
	var v any
	if err := row.Scan(&v); err != nil {
		return "", err
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case []byte:
		return string(t), nil
	case int64:
		return fmt.Sprintf("%d", t), nil
	case int:
		return fmt.Sprintf("%d", t), nil
	case float64:
		return fmt.Sprintf("%g", t), nil
	default:
		return fmt.Sprint(t), nil
	}
}

// PerformanceInfo is a snapshot of active SQLite performance settings.
type PerformanceInfo struct {
	JournalMode string `json:"journal_mode"`
	Synchronous string `json:"synchronous"`
	CacheSize   string `json:"cache_size"`
	ForeignKeys string `json:"foreign_keys"`
}

// Optimize runs SQLite's query planner maintenance.
func (s *Store) Optimize() {
	if s.db != nil {
		_, _ = s.db.Exec(`PRAGMA optimize`)
	}
	if s.ts != nil {
		_, _ = s.ts.Exec(`PRAGMA optimize`)
	}
}

// CheckpointWAL flushes the write-ahead log.
func (s *Store) CheckpointWAL() error {
	_, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}
