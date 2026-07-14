package db

import (
	"context"
	"fmt"
	"time"
)

// InsertSample stores a traffic sample for a client.
func (s *Store) InsertSample(ctx context.Context, clientID int64, rx, tx int64, rxBps, txBps float64) error {
	if s.ts == nil {
		return nil
	}
	_, err := s.ts.ExecContext(ctx, `
INSERT INTO traffic_samples (client_id, sampled_at, rx_bytes, tx_bytes, rx_bps, tx_bps)
VALUES (?, ?, ?, ?, ?, ?)`,
		clientID, nowRFC3339(), rx, tx, rxBps, txBps)
	if err != nil {
		return fmt.Errorf("insert sample: %w", err)
	}
	return nil
}

// ListSamples returns recent samples for a client.
func (s *Store) ListSamples(ctx context.Context, clientID int64, limit int) ([]TrafficSample, error) {
	if s.ts == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.ts.QueryContext(ctx, `
SELECT id, client_id, sampled_at, rx_bytes, tx_bytes, rx_bps, tx_bps
FROM traffic_samples WHERE client_id=? ORDER BY id DESC LIMIT ?`, clientID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []TrafficSample
	for rows.Next() {
		var t TrafficSample
		var at string
		if err := rows.Scan(&t.ID, &t.ClientID, &at, &t.RxBytes, &t.TxBytes, &t.RxBps, &t.TxBps); err != nil {
			return nil, err
		}
		t.SampledAt = parseTime(at)
		if t.SampledAt.IsZero() {
			t.SampledAt = time.Now().UTC()
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
