// Package policy holds peer/client policy helpers (auto-suspend, bandwidth totals).
package policy

import (
	"time"

	"github.com/reloadlife/openvpnd/internal/bandwidth"
	"github.com/reloadlife/openvpnd/internal/db"
)

// Reason codes for auto-suspend (also used in event meta).
const (
	ReasonExpired      = "expired"
	ReasonTrafficLimit = "traffic_limit"
	ReasonManual       = "manual"
)

// ShouldAutoSuspend reports whether an unsuspended client must be suspended now.
// Reasons: "expired" (expires_at in the past) or "traffic_limit" (effective rx+tx ≥ quota).
// Already-suspended clients return false.
func ShouldAutoSuspend(c db.Client, now time.Time) (bool, string) {
	if c.Suspended {
		return false, ""
	}
	if !c.ExpiresAt.IsZero() && now.UTC().After(c.ExpiresAt.UTC()) {
		return true, ReasonExpired
	}
	if c.TrafficLimitBytes > 0 &&
		bandwidth.ExceedsTrafficLimit(c.EffectiveRx(), c.EffectiveTx(), c.TrafficLimitBytes) {
		return true, ReasonTrafficLimit
	}
	return false, ""
}

// EffectiveBandwidth resolves per-direction rate caps (bits/sec).
//
// When total > 0, both directions are set to total (each direction capped at
// the combined budget). Otherwise rx/tx are returned as-is (0 = unlimited).
func EffectiveBandwidth(rx, tx, total int64) (rxOut, txOut int64) {
	if total > 0 {
		return total, total
	}
	return rx, tx
}
