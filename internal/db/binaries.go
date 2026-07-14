package db

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertBinary inserts or updates a binary registry entry.
func (s *Store) UpsertBinary(ctx context.Context, b Binary) (Binary, error) {
	if b.Name == "" {
		return Binary{}, fmt.Errorf("binary name required")
	}
	if b.Path == "" {
		return Binary{}, fmt.Errorf("binary path required")
	}
	now := nowRFC3339()
	existing, err := s.GetBinary(ctx, b.Name)
	if err == nil && existing != nil {
		_, err = s.db.ExecContext(ctx, `
UPDATE binaries SET path=?, version=?, notes=?, updated_at=? WHERE name=?`,
			b.Path, b.Version, b.Notes, now, b.Name)
		if err != nil {
			return Binary{}, err
		}
		out, err := s.GetBinary(ctx, b.Name)
		if err != nil {
			return Binary{}, err
		}
		return *out, nil
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO binaries (name, path, version, notes, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		b.Name, b.Path, b.Version, b.Notes, now, now)
	if err != nil {
		return Binary{}, fmt.Errorf("insert binary: %w", err)
	}
	out, err := s.GetBinary(ctx, b.Name)
	if err != nil {
		return Binary{}, err
	}
	return *out, nil
}

// GetBinary returns a binary by name.
func (s *Store) GetBinary(ctx context.Context, name string) (*Binary, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT name, path, version, notes, created_at, updated_at FROM binaries WHERE name=?`, name)
	var b Binary
	var created, updated string
	if err := row.Scan(&b.Name, &b.Path, &b.Version, &b.Notes, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("binary %q not found", name)
		}
		return nil, err
	}
	b.CreatedAt = parseTime(created)
	b.UpdatedAt = parseTime(updated)
	return &b, nil
}

// ListBinaries returns all registered binaries.
func (s *Store) ListBinaries(ctx context.Context) ([]Binary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, path, version, notes, created_at, updated_at FROM binaries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Binary
	for rows.Next() {
		var b Binary
		var created, updated string
		if err := rows.Scan(&b.Name, &b.Path, &b.Version, &b.Notes, &created, &updated); err != nil {
			return nil, err
		}
		b.CreatedAt = parseTime(created)
		b.UpdatedAt = parseTime(updated)
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteBinary removes a binary by name.
func (s *Store) DeleteBinary(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM binaries WHERE name=?`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("binary %q not found", name)
	}
	return nil
}

// UpdateBinaryVersion stores the probed version string.
func (s *Store) UpdateBinaryVersion(ctx context.Context, name, version string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE binaries SET version=?, updated_at=? WHERE name=?`,
		version, nowRFC3339(), name)
	return err
}

// EnsureBinaryDefaults seeds binaries from a map if missing.
func (s *Store) EnsureBinaryDefaults(ctx context.Context, binaries map[string]string) error {
	for name, path := range binaries {
		if name == "" || path == "" {
			continue
		}
		_, err := s.GetBinary(ctx, name)
		if err == nil {
			continue
		}
		_, err = s.UpsertBinary(ctx, Binary{Name: name, Path: path})
		if err != nil {
			return err
		}
	}
	return nil
}

// ResolveBinaryPath returns path for name, with optional override.
func (s *Store) ResolveBinaryPath(ctx context.Context, name, pathOverride string) (string, error) {
	if pathOverride != "" {
		return pathOverride, nil
	}
	if name == "" {
		name = "default"
	}
	b, err := s.GetBinary(ctx, name)
	if err != nil {
		return "", err
	}
	return b.Path, nil
}
