package ovpnbackend

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseClientStatisticsUDP(t *testing.T) {
	lines := []string{
		"OpenVPN STATISTICS",
		"Updated,Wed Jul 15 10:00:00 2026",
		"TUN/TAP read bytes,100",
		"TUN/TAP write bytes,200",
		"TCP/UDP read bytes,1000",
		"TCP/UDP write bytes,2000",
		"END",
	}
	rx, tx, ok := parseClientStatistics(lines)
	require.True(t, ok)
	require.Equal(t, int64(1000), rx)
	require.Equal(t, int64(2000), tx)
}

func TestParseClientStatisticsTUNFallback(t *testing.T) {
	lines := []string{
		"TUN/TAP read bytes,11",
		"TUN/TAP write bytes,22",
	}
	rx, tx, ok := parseClientStatistics(lines)
	require.True(t, ok)
	// TUN write → rx (into apps), TUN read → tx
	require.Equal(t, int64(22), rx)
	require.Equal(t, int64(11), tx)
}

func TestParseStatus2ServerClients(t *testing.T) {
	lines := []string{
		"HEADER,CLIENT_LIST,Common Name,Real Address,Virtual Address,Virtual IPv6 Address,Bytes Received,Bytes Sent,Connected Since",
		"CLIENT_LIST,alice,1.2.3.4:1194,10.8.0.2,,100,200,2026-07-15T10:00:00Z",
	}
	live := parseStatus2(lines)
	require.Len(t, live.Clients, 1)
	require.Equal(t, "alice", live.Clients[0].CommonName)
	require.Equal(t, int64(100), live.RxBytes)
	require.Equal(t, int64(200), live.TxBytes)
}
