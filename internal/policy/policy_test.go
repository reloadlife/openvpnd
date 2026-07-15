package policy_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/policy"
)

func TestShouldAutoSuspend_None(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	ok, reason := policy.ShouldAutoSuspend(db.Client{}, now)
	require.False(t, ok)
	require.Empty(t, reason)
}

func TestShouldAutoSuspend_AlreadySuspended(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	ok, _ := policy.ShouldAutoSuspend(db.Client{
		Suspended: true,
		ExpiresAt: now.Add(-time.Hour),
		TrafficLimitBytes: 1,
		LastRxBytes:       100,
	}, now)
	require.False(t, ok)
}

func TestShouldAutoSuspend_Expired(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	ok, reason := policy.ShouldAutoSuspend(db.Client{
		ExpiresAt: now.Add(-time.Second),
	}, now)
	require.True(t, ok)
	require.Equal(t, policy.ReasonExpired, reason)

	// Exactly at expiry is not after → no suspend.
	ok, _ = policy.ShouldAutoSuspend(db.Client{ExpiresAt: now}, now)
	require.False(t, ok)

	// Future expiry.
	ok, _ = policy.ShouldAutoSuspend(db.Client{ExpiresAt: now.Add(time.Hour)}, now)
	require.False(t, ok)
}

func TestShouldAutoSuspend_TrafficLimit(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	ok, reason := policy.ShouldAutoSuspend(db.Client{
		TrafficLimitBytes: 1000,
		LastRxBytes:       600,
		LastTxBytes:       400,
	}, now)
	require.True(t, ok)
	require.Equal(t, policy.ReasonTrafficLimit, reason)

	// Under limit.
	ok, _ = policy.ShouldAutoSuspend(db.Client{
		TrafficLimitBytes: 1000,
		LastRxBytes:       100,
		LastTxBytes:       100,
	}, now)
	require.False(t, ok)

	// Offsets reduce effective traffic (eff 400+400 = 800 < 1000).
	ok, _ = policy.ShouldAutoSuspend(db.Client{
		TrafficLimitBytes: 1000,
		LastRxBytes:       2000,
		LastTxBytes:       2000,
		RxBytesOffset:     1600,
		TxBytesOffset:     1600,
	}, now)
	require.False(t, ok)
}

func TestShouldAutoSuspend_ExpiredPreferredOverTraffic(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	ok, reason := policy.ShouldAutoSuspend(db.Client{
		ExpiresAt:         now.Add(-time.Minute),
		TrafficLimitBytes: 10,
		LastRxBytes:       100,
		LastTxBytes:       100,
	}, now)
	require.True(t, ok)
	require.Equal(t, policy.ReasonExpired, reason)
}

func TestEffectiveBandwidth(t *testing.T) {
	rx, tx := policy.EffectiveBandwidth(1_000_000, 500_000, 0)
	require.Equal(t, int64(1_000_000), rx)
	require.Equal(t, int64(500_000), tx)

	// Total overrides both directions.
	rx, tx = policy.EffectiveBandwidth(1_000_000, 500_000, 8_000_000)
	require.Equal(t, int64(8_000_000), rx)
	require.Equal(t, int64(8_000_000), tx)

	// Total only.
	rx, tx = policy.EffectiveBandwidth(0, 0, 8_000_000)
	require.Equal(t, int64(8_000_000), rx)
	require.Equal(t, int64(8_000_000), tx)

	// All zero.
	rx, tx = policy.EffectiveBandwidth(0, 0, 0)
	require.Equal(t, int64(0), rx)
	require.Equal(t, int64(0), tx)
}
