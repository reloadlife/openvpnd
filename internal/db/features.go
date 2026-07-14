package db

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertFeaturePreset stores a custom feature preset.
func (s *Store) UpsertFeaturePreset(ctx context.Context, p FeaturePreset) (FeaturePreset, error) {
	if p.ID == "" {
		return FeaturePreset{}, fmt.Errorf("preset id required")
	}
	now := nowRFC3339()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO feature_presets (id, description, extra_directives, plugins, env_vars, notes, builtin, created_at, updated_at)
VALUES (?,?,?,?,?,?,0,?,?)
ON CONFLICT(id) DO UPDATE SET
  description=excluded.description,
  extra_directives=excluded.extra_directives,
  plugins=excluded.plugins,
  env_vars=excluded.env_vars,
  notes=excluded.notes,
  updated_at=excluded.updated_at`,
		p.ID, p.Description, p.ExtraDirectives, encodePlugins(p.Plugins), encodeEnvVars(p.EnvVars), p.Notes, now, now)
	if err != nil {
		return FeaturePreset{}, err
	}
	return s.GetFeaturePreset(ctx, p.ID)
}

// GetFeaturePreset loads a custom preset (not builtins).
func (s *Store) GetFeaturePreset(ctx context.Context, id string) (FeaturePreset, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, description, extra_directives, plugins, env_vars, notes, builtin, created_at, updated_at
FROM feature_presets WHERE id=?`, id)
	var p FeaturePreset
	var plugins, env string
	var builtin int
	var created, updated string
	if err := row.Scan(&p.ID, &p.Description, &p.ExtraDirectives, &plugins, &env, &p.Notes, &builtin, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return FeaturePreset{}, fmt.Errorf("preset %q not found", id)
		}
		return FeaturePreset{}, err
	}
	p.Plugins = decodePlugins(plugins)
	p.EnvVars = decodeEnvVars(env)
	p.Builtin = builtin != 0
	p.CreatedAt = parseTime(created)
	p.UpdatedAt = parseTime(updated)
	return p, nil
}

// ListFeaturePresets returns custom (DB) presets only.
func (s *Store) ListFeaturePresets(ctx context.Context) ([]FeaturePreset, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, description, extra_directives, plugins, env_vars, notes, builtin, created_at, updated_at
FROM feature_presets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FeaturePreset
	for rows.Next() {
		var p FeaturePreset
		var plugins, env string
		var builtin int
		var created, updated string
		if err := rows.Scan(&p.ID, &p.Description, &p.ExtraDirectives, &plugins, &env, &p.Notes, &builtin, &created, &updated); err != nil {
			return nil, err
		}
		p.Plugins = decodePlugins(plugins)
		p.EnvVars = decodeEnvVars(env)
		p.Builtin = builtin != 0
		p.CreatedAt = parseTime(created)
		p.UpdatedAt = parseTime(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteFeaturePreset removes a custom preset.
func (s *Store) DeleteFeaturePreset(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM feature_presets WHERE id=? AND builtin=0`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("preset %q not found or builtin", id)
	}
	return nil
}
