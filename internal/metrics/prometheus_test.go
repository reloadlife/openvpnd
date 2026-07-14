package metrics_test

import (
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
