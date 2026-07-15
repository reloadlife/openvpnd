-- +goose Up
-- Instance-level bandwidth caps (role-aware: whole tunnel for client, device ceiling for server).

ALTER TABLE instances ADD COLUMN bandwidth_rx_bps INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN bandwidth_tx_bps INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite leave columns
