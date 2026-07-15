package ovpnbackend_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
)

func TestMockEnsureStopList(t *testing.T) {
	ctx := context.Background()
	m := ovpnbackend.NewMock()
	m.SetBinaryVersion("/usr/sbin/openvpn", "OpenVPN 2.6.12 mock")

	ver, err := m.ProbeBinary(ctx, "/usr/sbin/openvpn")
	require.NoError(t, err)
	require.Contains(t, ver, "2.6.12")

	err = m.EnsureInstance(ctx, ovpnbackend.DesiredInstance{
		Name: "ovpn0", Enabled: true, BinaryPath: "/usr/sbin/openvpn",
		ConfPath: "/tmp/ovpn0.conf", ConfHash: "abc", MgmtPath: "/tmp/ovpn0.sock",
		Env: []string{"FOO=bar"},
	})
	require.NoError(t, err)

	live, err := m.ListLive(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.True(t, live[0].Up)
	require.Equal(t, "ovpn0", live[0].Name)

	m.SetClients("ovpn0", []ovpnbackend.LiveClient{{
		CommonName: "alice", RxBytes: 100, TxBytes: 50,
	}})
	mgmt, err := m.Management(ctx, "ovpn0")
	require.NoError(t, err)
	st, err := mgmt.Status(ctx)
	require.NoError(t, err)
	require.Len(t, st.Clients, 1)
	require.Equal(t, int64(100), st.RxBytes)
	_ = mgmt.Close()

	// conf change → still up
	err = m.EnsureInstance(ctx, ovpnbackend.DesiredInstance{
		Name: "ovpn0", Enabled: true, BinaryPath: "/usr/sbin/openvpn",
		ConfPath: "/tmp/ovpn0.conf", ConfHash: "def", MgmtPath: "/tmp/ovpn0.sock",
	})
	require.NoError(t, err)

	require.NoError(t, m.StopInstance(ctx, "ovpn0"))
	live, err = m.ListLive(ctx)
	require.NoError(t, err)
	require.Empty(t, live)

	require.NoError(t, m.RemoveInstance(ctx, "ovpn0"))
	_, err = m.Management(ctx, "ovpn0")
	require.Error(t, err)
	require.NoError(t, m.Close())
}

func TestMockDisabledDoesNotListUp(t *testing.T) {
	ctx := context.Background()
	m := ovpnbackend.NewMock()
	require.NoError(t, m.EnsureInstance(ctx, ovpnbackend.DesiredInstance{
		Name: "x", Enabled: true, ConfHash: "1", BinaryPath: "/bin/openvpn",
	}))
	require.NoError(t, m.EnsureInstance(ctx, ovpnbackend.DesiredInstance{
		Name: "x", Enabled: false, ConfHash: "1", BinaryPath: "/bin/openvpn",
	}))
	live, err := m.ListLive(ctx)
	require.NoError(t, err)
	require.Empty(t, live)
}
