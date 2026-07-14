package instance_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/instance"
)

func TestPrepareServerAuto(t *testing.T) {
	res, err := instance.Prepare(instance.CreateInput{
		Role: "server",
		// name, network, port empty → auto
	}, instance.Context{
		ExistingNames: map[string]struct{}{"ovpn0": {}},
		UsedPorts:     map[int]struct{}{1194: {}},
		UsedNetworks:  []string{"10.8.0.0/24"},
		DefaultBinary: "default",
		BinaryNames:   map[string]struct{}{"default": {}},
		HasCA:         true,
		DefaultCA:     "main",
	})
	require.NoError(t, err)
	require.Equal(t, "ovpn1", res.Instance.Name)
	require.Equal(t, 1195, res.Instance.Port)
	require.Equal(t, "10.9.0.0/24", res.Instance.ServerNetwork)
	require.Equal(t, "subnet", res.Instance.Topology)
	require.True(t, res.IssueServerCert)
	require.True(t, res.GenerateTLSCrypt)
	require.Equal(t, "main", res.CAName)
	require.NotEmpty(t, res.Auto)
}

func TestPrepareClientRequiresRemote(t *testing.T) {
	_, err := instance.Prepare(instance.CreateInput{
		Name: "home", Role: "client",
	}, instance.Context{BinaryNames: map[string]struct{}{"default": {}}, DefaultBinary: "default"})
	require.Error(t, err)

	res, err := instance.Prepare(instance.CreateInput{
		Name: "home", Role: "client",
		Remotes: []db.Remote{{Host: "vpn.example.com"}},
	}, instance.Context{BinaryNames: map[string]struct{}{"default": {}}, DefaultBinary: "default"})
	require.NoError(t, err)
	require.Equal(t, 1194, res.Instance.Remotes[0].Port)
}

func TestPrepareRejectsBadName(t *testing.T) {
	_, err := instance.Prepare(instance.CreateInput{
		Name: "1bad", Role: "server", ServerNetwork: "10.8.0.0/24",
	}, instance.Context{BinaryNames: map[string]struct{}{"default": {}}, DefaultBinary: "default", HasCA: true})
	require.Error(t, err)
}

func TestPrepareOverlap(t *testing.T) {
	_, err := instance.Prepare(instance.CreateInput{
		Name: "x", Role: "server", ServerNetwork: "10.8.0.0/24",
	}, instance.Context{
		UsedNetworks:  []string{"10.8.0.0/24"},
		BinaryNames:   map[string]struct{}{"default": {}},
		DefaultBinary: "default",
		HasCA:         true,
	})
	require.Error(t, err)
}

func TestParseRemoteCSV(t *testing.T) {
	r, err := instance.ParseRemoteCSV("vpn.example.com:1194, backup.example.com:443:tcp")
	require.NoError(t, err)
	require.Len(t, r, 2)
	require.Equal(t, 1194, r[0].Port)
}
