-- +goose Up
-- CRL path on CA; advanced instance knobs; client iroutes.

ALTER TABLE cas ADD COLUMN crl_path TEXT NOT NULL DEFAULT '';
ALTER TABLE cas ADD COLUMN crl_number INTEGER NOT NULL DEFAULT 1;

ALTER TABLE certificates ADD COLUMN revoked_at TEXT NOT NULL DEFAULT '';
ALTER TABLE certificates ADD COLUMN revoke_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE instances ADD COLUMN pki_crl_path TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN max_clients INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN tls_version_min TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN tun_mtu INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN sndbuf INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN rcvbuf INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN server_ipv6 TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN auth_user_pass INTEGER NOT NULL DEFAULT 0;

ALTER TABLE clients ADD COLUMN iroutes TEXT NOT NULL DEFAULT '[]';

-- +goose Down
-- SQLite cannot DROP COLUMN portably; leave columns for down no-op.
