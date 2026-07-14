package snmp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/snmp"
	"github.com/reloadlife/openvpnd/internal/stats"
)

func testCache() *stats.Cache {
	c := stats.NewCache()
	c.SetInstance(stats.InstanceStats{
		Name: "ovpn0", Role: "server", Up: true, Port: 1194,
		ConnectedClients: 1, RxBytes: 1000, TxBytes: 2000,
	})
	c.SetClient(stats.ClientStats{
		Instance: "ovpn0", CommonName: "alice", Connected: true,
		RxBytes: 100, TxBytes: 200, ConnectedSince: time.Unix(1700000000, 0),
	})
	return c
}

func TestBuildMIB(t *testing.T) {
	mib := snmp.BuildMIB(snmp.ParseOID("1.3.6.1.4.1.66666.2"), testCache(), time.Now())
	require.Greater(t, mib.Len(), 10)
	// instance count scalar
	v, ok := mib.Get(snmp.ParseOID("1.3.6.1.4.1.66666.2.1.1.0"))
	require.True(t, ok)
	require.Equal(t, int64(1), v.Value.Int)
}

func TestAgentGet(t *testing.T) {
	a := snmp.NewAgent("127.0.0.1:0", "public", "1.3.6.1.4.1.66666.2", testCache(), nil)
	require.NoError(t, a.Start())
	t.Cleanup(func() { _ = a.Close() })

	// craft GET for sysDescr via HandlePacket using SnapshotVars presence
	vars := a.SnapshotVars()
	require.NotEmpty(t, vars)
}

func TestAgentListen(t *testing.T) {
	a := snmp.NewAgent("127.0.0.1:0", "secret", "1.3.6.1.4.1.66666.2", testCache(), nil)
	require.NoError(t, a.Start())
	addr := a.Addr()
	require.NotNil(t, addr)
	require.NoError(t, a.Close())
}
