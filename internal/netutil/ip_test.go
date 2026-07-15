package netutil_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/netutil"
)

func TestValidateCIDRAndIP(t *testing.T) {
	require.NoError(t, netutil.ValidateCIDR("10.8.0.0/24"))
	require.Error(t, netutil.ValidateCIDR(""))
	require.Error(t, netutil.ValidateCIDR("not-a-cidr"))
	require.NoError(t, netutil.ValidateIP("10.8.0.2"))
	require.Error(t, netutil.ValidateIP(""))
	require.Error(t, netutil.ValidateIP("999.0.0.1"))
}

func TestServerNetworkToOpenVPN(t *testing.T) {
	net, mask, err := netutil.ServerNetworkToOpenVPN("10.8.0.0/24")
	require.NoError(t, err)
	require.Equal(t, "10.8.0.0", net)
	require.Equal(t, "255.255.255.0", mask)
	_, _, err = netutil.ServerNetworkToOpenVPN("2001:db8::/64")
	require.Error(t, err)
}

func TestAllocateNextHost(t *testing.T) {
	ip, err := netutil.AllocateNextHost("10.8.0.0/24", nil)
	require.NoError(t, err)
	require.Equal(t, "10.8.0.2", ip)

	ip, err = netutil.AllocateNextHost("10.8.0.0/24", []string{"10.8.0.2", "10.8.0.3"})
	require.NoError(t, err)
	require.Equal(t, "10.8.0.4", ip)

	// /30: network, .1 server, .2 host, broadcast — only one usable client
	ip, err = netutil.AllocateNextHost("10.8.0.0/30", nil)
	require.NoError(t, err)
	require.Equal(t, "10.8.0.2", ip)
	_, err = netutil.AllocateNextHost("10.8.0.0/30", []string{"10.8.0.2"})
	require.Error(t, err)
}

func TestIsAutoToken(t *testing.T) {
	for _, s := range []string{"", "auto", "AUTO", "next", "*"} {
		require.True(t, netutil.IsAutoToken(s), s)
	}
	require.False(t, netutil.IsAutoToken("10.8.0.5"))
}

func TestValidateServerNetwork(t *testing.T) {
	require.NoError(t, netutil.ValidateServerNetwork("10.8.0.0/24"))
	require.Error(t, netutil.ValidateServerNetwork("10.8.0.0/31")) // too small
	require.Error(t, netutil.ValidateServerNetwork("10.0.0.0/7"))  // too large
}

func TestValidatePublicEndpoint(t *testing.T) {
	require.NoError(t, netutil.ValidatePublicEndpoint("vpn.example.com"))
	require.NoError(t, netutil.ValidatePublicEndpoint("vpn.example.com:1194"))
	require.NoError(t, netutil.ValidatePublicEndpoint("1.2.3.4:443"))
	require.Error(t, netutil.ValidatePublicEndpoint(""))
	require.Error(t, netutil.ValidatePublicEndpoint("host with spaces"))
	require.Error(t, netutil.ValidatePublicEndpoint("host:99999"))
}

func TestNormalizeHostIP(t *testing.T) {
	h, err := netutil.NormalizeHostIP("10.8.0.5")
	require.NoError(t, err)
	require.Equal(t, "10.8.0.5", h)
	h, err = netutil.NormalizeHostIP("10.8.0.5/32")
	require.NoError(t, err)
	require.Equal(t, "10.8.0.5", h)
}
