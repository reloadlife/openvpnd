-- +goose Up
-- Roadmap features: bridge, TLS control-channel, server auth, CCD ACL, dual-stack helpers.

ALTER TABLE instances ADD COLUMN bridge_mode INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN bridge_gateway TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN bridge_pool_start TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN bridge_pool_end TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN bridge_netmask TEXT NOT NULL DEFAULT '';

ALTER TABLE instances ADD COLUMN tls_cipher TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN tls_ciphersuites TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN tls_groups TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN tls_cert_profile TEXT NOT NULL DEFAULT '';

ALTER TABLE instances ADD COLUMN auth_user_pass_verify TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN script_security INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN username_as_common_name INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN auth_user_pass_file TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ifconfig_ipv6 TEXT NOT NULL DEFAULT '';

ALTER TABLE clients ADD COLUMN push_dns TEXT NOT NULL DEFAULT '[]';
ALTER TABLE clients ADD COLUMN push_domain TEXT NOT NULL DEFAULT '';
ALTER TABLE clients ADD COLUMN redirect_gateway INTEGER NOT NULL DEFAULT 0;
ALTER TABLE clients ADD COLUMN disable_push TEXT NOT NULL DEFAULT '[]';

-- +goose Down
-- SQLite leave columns
