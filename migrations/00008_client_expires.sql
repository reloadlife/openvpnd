-- +goose Up
-- Peer policy: optional expiry and combined bandwidth total (bits/sec).

ALTER TABLE clients ADD COLUMN expires_at TEXT NOT NULL DEFAULT '';
ALTER TABLE clients ADD COLUMN bandwidth_total_bps INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite leave columns
