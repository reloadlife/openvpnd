-- +goose Up
ALTER TABLE instances ADD COLUMN public_endpoint TEXT NOT NULL DEFAULT '';

ALTER TABLE clients ADD COLUMN client_cert_path TEXT NOT NULL DEFAULT '';
ALTER TABLE clients ADD COLUMN client_key_path TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS profile_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token TEXT NOT NULL UNIQUE,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    client_id INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    max_uses INTEGER NOT NULL DEFAULT 1,
    use_count INTEGER NOT NULL DEFAULT 0,
    revoked INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_profile_tokens_token ON profile_tokens(token);
CREATE INDEX IF NOT EXISTS idx_profile_tokens_client ON profile_tokens(client_id);

-- +goose Down
DROP TABLE IF EXISTS profile_tokens;
-- SQLite cannot drop columns portably; leave added columns on down.
