-- +goose Up
CREATE TABLE IF NOT EXISTS cas (
    name TEXT PRIMARY KEY,
    common_name TEXT NOT NULL,
    org TEXT NOT NULL DEFAULT '',
    cert_path TEXT NOT NULL,
    key_path TEXT NOT NULL,
    not_after TEXT NOT NULL DEFAULT '',
    serial_next INTEGER NOT NULL DEFAULT 2,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS certificates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ca_name TEXT NOT NULL REFERENCES cas(name) ON DELETE CASCADE,
    kind TEXT NOT NULL, -- server | client
    common_name TEXT NOT NULL,
    cert_path TEXT NOT NULL,
    key_path TEXT NOT NULL,
    not_before TEXT NOT NULL DEFAULT '',
    not_after TEXT NOT NULL DEFAULT '',
    serial INTEGER NOT NULL DEFAULT 0,
    fingerprint TEXT NOT NULL DEFAULT '',
    revoked INTEGER NOT NULL DEFAULT 0,
    instance_name TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE(ca_name, kind, common_name)
);

CREATE INDEX IF NOT EXISTS idx_certs_ca ON certificates(ca_name);
CREATE INDEX IF NOT EXISTS idx_certs_cn ON certificates(common_name);

-- optional named tls-crypt keys
CREATE TABLE IF NOT EXISTS tls_crypt_keys (
    name TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS tls_crypt_keys;
DROP TABLE IF EXISTS certificates;
DROP TABLE IF EXISTS cas;
