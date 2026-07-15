package stats_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/stats"
)

func TestCacheInstanceAndClients(t *testing.T) {
	c := stats.NewCache()
	c.SetInstance(stats.InstanceStats{
		Name: "ovpn0", Up: true, PID: 42, RxBytes: 100, TxBytes: 50,
		UpdatedAt: time.Now().UTC(),
	})
	st, ok := c.GetInstance("ovpn0")
	require.True(t, ok)
	require.True(t, st.Up)
	require.Equal(t, 42, st.PID)

	c.SetClient(stats.ClientStats{
		Instance: "ovpn0", CommonName: "alice", Connected: true, RxBytes: 10, TxBytes: 5,
	})
	list := c.ListClients()
	require.Len(t, list, 1)
	require.Equal(t, "alice", list[0].CommonName)

	all := c.ListInstances()
	require.NotEmpty(t, all)
}
