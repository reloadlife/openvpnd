package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
)

func TestStoreCRUD(t *testing.T) {
	store, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureBinaryDefaults(ctx, map[string]string{
		"default": "/usr/sbin/openvpn",
		"v26":     "/opt/openvpn/sbin/openvpn",
	}))

	bins, err := store.ListBinaries(ctx)
	require.NoError(t, err)
	require.Len(t, bins, 2)

	inst, err := store.CreateInstance(ctx, db.Instance{
		Name:          "ovpn0",
		Role:          "server",
		Enabled:       true,
		BinaryName:    "v26",
		Port:          1194,
		ServerNetwork: "10.8.0.0/24",
		Topology:      "subnet",
	})
	require.NoError(t, err)
	require.Equal(t, "server", inst.Role)
	require.Equal(t, "v26", inst.BinaryName)

	cli, err := store.CreateClient(ctx, "ovpn0", db.Client{
		CommonName: "alice",
		Name:       "Alice",
		StaticIP:   "10.8.0.2",
		IRoutes:    []string{"192.168.10.0/24"},
		PushDNS:    []string{"1.1.1.1"},
		PushDomain: "corp.lan",
		DisablePush: []string{"redirect-gateway"},
	})
	require.NoError(t, err)
	require.Equal(t, "alice", cli.CommonName)
	require.Equal(t, "ovpn0", cli.InstanceName)
	require.Equal(t, []string{"192.168.10.0/24"}, cli.IRoutes)
	require.Equal(t, []string{"1.1.1.1"}, cli.PushDNS)
	require.Equal(t, "corp.lan", cli.PushDomain)
	require.Equal(t, []string{"redirect-gateway"}, cli.DisablePush)

	updated, err := store.UpdateClient(ctx, "ovpn0", "alice", db.Client{
		Name: "Alice", StaticIP: "10.8.0.2",
		IRoutes:         []string{"192.168.10.0/24", "10.20.0.0/16"},
		PushDNS:         []string{"8.8.8.8"},
		PushDomain:      "example.com",
		RedirectGateway: true,
		DisablePush:     []string{"route"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.10.0/24", "10.20.0.0/16"}, updated.IRoutes)
	require.Equal(t, []string{"8.8.8.8"}, updated.PushDNS)
	require.Equal(t, "example.com", updated.PushDomain)
	require.True(t, updated.RedirectGateway)
	require.Equal(t, []string{"route"}, updated.DisablePush)

	clients, err := store.ListClientsByInstance(ctx, "ovpn0")
	require.NoError(t, err)
	require.Len(t, clients, 1)
	require.Equal(t, []string{"192.168.10.0/24", "10.20.0.0/16"}, clients[0].IRoutes)

	// Roadmap instance fields round-trip
	inst.BridgeMode = true
	inst.BridgeGateway = "192.168.1.1"
	inst.BridgeNetmask = "255.255.255.0"
	inst.BridgePoolStart = "192.168.1.100"
	inst.BridgePoolEnd = "192.168.1.200"
	inst.TLSGroups = "X25519"
	inst.AuthUserPassVerify = "/bin/auth"
	inst.ScriptSecurity = 2
	inst.UsernameAsCommonName = true
	inst.IfconfigIPv6 = "fd00::1/64"
	got, err := store.UpdateInstance(ctx, inst)
	require.NoError(t, err)
	require.True(t, got.BridgeMode)
	require.Equal(t, "192.168.1.1", got.BridgeGateway)
	require.Equal(t, "X25519", got.TLSGroups)
	require.Equal(t, "/bin/auth", got.AuthUserPassVerify)
	require.True(t, got.UsernameAsCommonName)
	require.Equal(t, "fd00::1/64", got.IfconfigIPv6)

	require.NoError(t, store.AddEvent(ctx, "info", "test", "ovpn0", "alice", "hello", "{}"))
	ev, err := store.ListEvents(ctx, 10)
	require.NoError(t, err)
	require.NotEmpty(t, ev)
}
