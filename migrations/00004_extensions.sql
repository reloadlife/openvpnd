-- +goose Up
-- OpenVPN extensions: plugins, process env, named feature sets.
ALTER TABLE instances ADD COLUMN plugins TEXT NOT NULL DEFAULT '[]';
ALTER TABLE instances ADD COLUMN env_vars TEXT NOT NULL DEFAULT '[]';
ALTER TABLE instances ADD COLUMN feature_sets TEXT NOT NULL DEFAULT '[]';

-- Named reusable snippets (admin-defined), expanded into conf at render time.
CREATE TABLE IF NOT EXISTS feature_presets (
    id TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    extra_directives TEXT NOT NULL DEFAULT '',
    plugins TEXT NOT NULL DEFAULT '[]',
    env_vars TEXT NOT NULL DEFAULT '[]',
    notes TEXT NOT NULL DEFAULT '',
    builtin INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS feature_presets;
