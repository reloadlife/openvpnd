package metrics_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/metrics"
	"github.com/reloadlife/openvpnd/internal/stats"
)

func TestCollectorScrapesCache(t *testing.T) {
	cache := stats.NewCache()
	cache.SetInstance(stats.InstanceStats{
		Name: "ovpn0", Role: "server", Up: true, Port: 1194,
		ConnectedClients: 1, RxBytes: 100, TxBytes: 200, RxBps: 10, TxBps: 20,
	})
	cache.SetClient(stats.ClientStats{
		Instance: "ovpn0", CommonName: "alice", Name: "Alice",
		Connected: true, ConnectedSince: time.Unix(1700000000, 0),
		RxBytes: 50, TxBytes: 60, RxBps: 5, TxBps: 6,
	})

	reg := prometheus.NewRegistry()
	_ = metrics.New(cache, reg)

	// Gather
	mfs, err := reg.Gather()
	require.NoError(t, err)
	var names []string
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	joined := strings.Join(names, ",")
	require.Contains(t, joined, "openvpnd_up")
	require.Contains(t, joined, "openvpn_instance_up")
	require.Contains(t, joined, "openvpn_client_connected")

	require.Equal(t, 1, testutil.CollectAndCount(reg, "openvpn_instance_up"))
}

func TestWALMetrics(t *testing.T) {
	dir := t.TempDir()
	wal := filepath.Join(dir, "state.db-wal")
	require.NoError(t, os.WriteFile(wal, make([]byte, 4096), 0o600))

	reg := prometheus.NewRegistry()
	c := metrics.New(stats.NewCache(), reg)
	c.WatchWAL(map[string]string{"state": wal, "timeseries": filepath.Join(dir, "missing-wal")})
	c.ObserveWALCheckpoint("state", false)
	c.ObserveWALCheckpoint("state", false)
	c.ObserveWALCheckpoint("timeseries", true)

	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP openvpnd_sqlite_wal_bytes Size of the SQLite -wal file on disk
# TYPE openvpnd_sqlite_wal_bytes gauge
openvpnd_sqlite_wal_bytes{db="state"} 4096
openvpnd_sqlite_wal_bytes{db="timeseries"} 0
# HELP openvpnd_sqlite_wal_checkpoint_blocked_total Periodic WAL checkpoints that could not copy every frame back (a reader pins the WAL)
# TYPE openvpnd_sqlite_wal_checkpoint_blocked_total counter
openvpnd_sqlite_wal_checkpoint_blocked_total{db="state"} 2
`), "openvpnd_sqlite_wal_bytes", "openvpnd_sqlite_wal_checkpoint_blocked_total"))
	require.Equal(t, 1, testutil.CollectAndCount(reg, "openvpnd_sqlite_wal_last_checkpoint_timestamp_seconds"))
}
