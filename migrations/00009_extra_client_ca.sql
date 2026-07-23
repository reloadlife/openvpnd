-- +goose Up
-- Additional trusted client CAs (a fleet-wide CA held by the control plane).
-- Appended after the instance's own CA in the rendered server `ca` bundle, so
-- clients signed by either CA validate. Strictly additive: '[]' = today's behavior.

ALTER TABLE instances ADD COLUMN extra_client_ca_pems TEXT NOT NULL DEFAULT '[]';

-- +goose Down
-- SQLite leave columns
